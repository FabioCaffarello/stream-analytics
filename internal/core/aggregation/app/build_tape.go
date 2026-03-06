package app

import (
	"context"
	"math"
	"time"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/ds"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

var defaultTapeBurstThresholds = map[string]int64{
	"250ms": 25,
	"1s":    80,
	"5s":    300,
}

// BuildTapeConfig controls bounded state and window durations for tape build.
type BuildTapeConfig struct {
	MaxWindows     int
	WindowCap      int
	WindowTTL      time.Duration
	LateTolerance  time.Duration
	WindowDuration map[string]time.Duration
	BurstThreshold map[string]int64
	Clock          clock.Clock
}

// BuildTapeRequest is one normalized trade input for tape derivation.
type BuildTapeRequest struct {
	Venue      string
	Instrument string
	Price      float64
	Quantity   float64
	IsBuy      bool
	Seq        int64
	TsIngest   int64
}

// TapeCloseEvent reports one closed tape window plus burst classification.
type TapeCloseEvent struct {
	Window  *domain.TapeWindowV1
	IsBurst bool
}

// BuildTapeResponse reports emitted close events and active state cardinality.
type BuildTapeResponse struct {
	ClosedWindows []TapeCloseEvent
	ActiveWindows int
}

// BuildTapeFromTrades builds deterministic tape windows from trade events.
type BuildTapeFromTrades struct {
	publisher  ports.ArtifactPublisher
	store      ports.TapeHotReadModelStore
	windows    *ds.BoundedMap[domain.TapeKey, *domain.TapeWindowV1]
	cvdState   *ds.BoundedMap[domain.TapeKey, float64]
	lifecycle  domain.WindowManager
	windowMs   map[string]int64
	burst      map[string]int64
	timeframes []string
}

// NewBuildTapeFromTrades constructs BuildTapeFromTrades.
func NewBuildTapeFromTrades(
	pub ports.ArtifactPublisher,
	store ports.TapeHotReadModelStore,
	cfg BuildTapeConfig,
) *BuildTapeFromTrades {
	if cfg.MaxWindows <= 0 {
		cfg.MaxWindows = 50_000
	}
	if cfg.WindowTTL <= 0 {
		cfg.WindowTTL = time.Hour
	}
	if cfg.WindowCap <= 0 {
		cfg.WindowCap = 96
	}
	if cfg.LateTolerance <= 0 {
		cfg.LateTolerance = 30 * time.Second
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.NewSystemClock()
	}

	windowMs, p := resolveTapeWindowDurations(cfg.WindowDuration)
	if p != nil {
		windowMs, _ = resolveTapeWindowDurations(nil)
	}
	windows := ds.NewBoundedMap[domain.TapeKey, *domain.TapeWindowV1](cfg.MaxWindows, cfg.WindowTTL, cfg.Clock)
	windows.SetSweepEveryOps(1024)
	windows.SetSweepMinInterval(time.Second)
	cvdState := ds.NewBoundedMap[domain.TapeKey, float64](cfg.MaxWindows, cfg.WindowTTL, cfg.Clock)
	cvdState.SetSweepEveryOps(1024)
	cvdState.SetSweepMinInterval(time.Second)
	lifecycle, p := domain.NewWatermarkWindowManager(domain.WatermarkWindowConfig{
		MaxOpenWindows:  cfg.WindowCap,
		LateToleranceMs: cfg.LateTolerance.Milliseconds(),
	})
	if p != nil {
		lifecycle, _ = domain.NewWatermarkWindowManager(domain.WatermarkWindowConfig{
			MaxOpenWindows:  96,
			LateToleranceMs: 30_000,
		})
	}

	burst := make(map[string]int64, len(defaultTapeBurstThresholds))
	for tf, threshold := range defaultTapeBurstThresholds {
		burst[tf] = threshold
	}
	for tf, threshold := range cfg.BurstThreshold {
		if threshold >= 0 {
			burst[tf] = threshold
		}
	}

	return &BuildTapeFromTrades{
		publisher:  pub,
		store:      store,
		windows:    windows,
		cvdState:   cvdState,
		lifecycle:  lifecycle,
		windowMs:   windowMs,
		burst:      burst,
		timeframes: []string{"250ms", "1s", "5s"},
	}
}

// Execute applies one trade input and emits finalized tape windows.
func (uc *BuildTapeFromTrades) Execute(ctx context.Context, req BuildTapeRequest) (BuildTapeResponse, *problem.Problem) {
	if p := uc.validateRequest(req); p != nil {
		return BuildTapeResponse{}, p
	}
	venue := naming.CanonicalVenue(req.Venue)
	instrument := naming.CanonicalInstrument(req.Instrument)

	closed := make([]TapeCloseEvent, 0, len(uc.timeframes))
	for _, timeframe := range uc.timeframes {
		key := domain.TapeKey{Venue: venue, Instrument: instrument, Timeframe: timeframe}
		decision, forcedClosed, p := uc.observeWindow(ctx, key, req.TsIngest, uc.windowMs[timeframe])
		if p != nil {
			return BuildTapeResponse{}, p
		}
		if forcedClosed != nil {
			closed = append(closed, *forcedClosed)
		}
		if decision.IsLate {
			metrics.IncMRWindowLateArrival(key.Venue, key.Instrument, key.Timeframe)
			continue
		}
		metrics.SetMRWindowOpen(key.Venue, key.Instrument, key.Timeframe, 1)
		windowStart := decision.WindowStart

		closedEvt, p := uc.closeIfWindowChanged(ctx, key, windowStart, uc.windowMs[timeframe])
		if p != nil {
			return BuildTapeResponse{}, p
		}
		if closedEvt != nil {
			closed = append(closed, *closedEvt)
		}

		win, p := uc.getOrCreateWindow(key, windowStart)
		if p != nil {
			return BuildTapeResponse{}, p
		}
		if p := win.ApplyTrade(req.Price, req.Quantity, req.IsBuy, req.Seq); p != nil {
			return BuildTapeResponse{}, p
		}
		uc.windows.Put(key, win)
	}

	return BuildTapeResponse{ClosedWindows: closed, ActiveWindows: uc.windows.Len()}, nil
}

// ActiveWindows returns current bounded-map cardinality.
func (uc *BuildTapeFromTrades) ActiveWindows() int {
	if uc == nil || uc.windows == nil {
		return 0
	}
	return uc.windows.Len()
}

func (uc *BuildTapeFromTrades) validateRequest(req BuildTapeRequest) *problem.Problem {
	if p := validation.Collect(
		validation.NonEmptyString("venue", req.Venue),
		validation.NonEmptyString("instrument", req.Instrument),
		validation.PositiveInt("seq", req.Seq),
		validation.PositiveInt("ts_ingest", req.TsIngest),
	); p != nil {
		return p
	}
	if math.IsNaN(req.Price) || math.IsInf(req.Price, 0) || req.Price <= 0 {
		return problem.New(problem.ValidationFailed, "price must be a positive finite number")
	}
	if math.IsNaN(req.Quantity) || math.IsInf(req.Quantity, 0) || req.Quantity <= 0 {
		return problem.New(problem.ValidationFailed, "quantity must be a positive finite number")
	}
	return nil
}

func (uc *BuildTapeFromTrades) closeIfWindowChanged(
	ctx context.Context,
	key domain.TapeKey,
	windowStart int64,
	windowDurationMs int64,
) (*TapeCloseEvent, *problem.Problem) {
	existing, ok := uc.windows.Get(key)
	if !ok {
		return nil, nil
	}
	if existing.WindowStartTs == windowStart {
		return nil, nil
	}
	if p := existing.Close(existing.WindowStartTs + windowDurationMs); p != nil {
		return nil, p
	}
	closed, p := uc.persistClosedWindow(ctx, *existing)
	if p != nil {
		return nil, p
	}
	uc.windows.Delete(key)
	return &closed, nil
}

func (uc *BuildTapeFromTrades) getOrCreateWindow(
	key domain.TapeKey,
	windowStart int64,
) (*domain.TapeWindowV1, *problem.Problem) {
	if w, ok := uc.windows.Get(key); ok {
		if w.WindowStartTs != windowStart {
			return nil, problem.New(problem.IntegrityViolation, "tape window mismatch after window roll")
		}
		return w, nil
	}
	w, p := domain.NewTapeWindowV1(key.Venue, key.Instrument, key.Timeframe, windowStart)
	if p != nil {
		return nil, p
	}
	uc.windows.Put(key, w)
	return w, nil
}

func (uc *BuildTapeFromTrades) persistClosedWindow(
	ctx context.Context,
	win domain.TapeWindowV1,
) (TapeCloseEvent, *problem.Problem) {
	threshold := uc.burst[win.Timeframe]
	evt := domain.NewTapeClosed(win, win.IsBurst(threshold))
	if uc.store != nil {
		if p := uc.store.SaveTape(ctx, evt); p != nil {
			return TapeCloseEvent{}, p
		}
	}
	if uc.publisher != nil {
		if p := uc.publisher.PublishTapeClosed(ctx, evt); p != nil {
			return TapeCloseEvent{}, p
		}
		if p := uc.publishDerivedAnalytics(ctx, evt); p != nil {
			return TapeCloseEvent{}, p
		}
	}
	closedCopy := evt.Window
	return TapeCloseEvent{Window: &closedCopy, IsBurst: evt.IsBurst}, nil
}

func (uc *BuildTapeFromTrades) publishDerivedAnalytics(ctx context.Context, evt domain.TapeClosed) *problem.Problem {
	if uc == nil || uc.publisher == nil {
		return nil
	}
	delta := domain.NewDeltaVolumeWindowV1(evt.Window)
	deltaEvt := domain.DeltaVolumeClosed{Window: delta}
	if p := uc.publisher.PublishDeltaVolume(ctx, deltaEvt); p != nil {
		return p
	}

	key := domain.TapeKey{
		Venue:      evt.Window.Venue,
		Instrument: evt.Window.Instrument,
		Timeframe:  evt.Window.Timeframe,
	}
	nextCVD := delta.DeltaVolume
	if prev, ok := uc.cvdState.Get(key); ok {
		nextCVD += prev
	}
	uc.cvdState.Put(key, nextCVD)
	cvdEvt := domain.CVDClosed{Window: domain.NewCVDWindowV1(delta, nextCVD)}
	if p := uc.publisher.PublishCVD(ctx, cvdEvt); p != nil {
		return p
	}

	barStats := domain.NewBarStatsWindowV1(evt.Window, evt.IsBurst)
	barEvt := domain.BarStatsClosed{Window: barStats}
	if p := uc.publisher.PublishBarStats(ctx, barEvt); p != nil {
		return p
	}
	return nil
}

func (uc *BuildTapeFromTrades) observeWindow(
	ctx context.Context,
	key domain.TapeKey,
	eventTsMs int64,
	windowDurationMs int64,
) (domain.WindowDecision, *TapeCloseEvent, *problem.Problem) {
	if uc.lifecycle == nil {
		return domain.WindowDecision{WindowStart: bucketStart(eventTsMs, windowDurationMs)}, nil, nil
	}
	decision, p := uc.lifecycle.Observe(domain.WindowKey(key), eventTsMs, windowDurationMs)
	if p != nil {
		return domain.WindowDecision{}, nil, p
	}
	if decision.ForcedClose == nil {
		return decision, nil, nil
	}
	metrics.IncMRWindowForceClose(
		decision.ForcedClose.Key.Venue,
		decision.ForcedClose.Key.Instrument,
		decision.ForcedClose.Key.Timeframe,
	)
	forcedClosed, p := uc.forceCloseWindow(ctx, *decision.ForcedClose)
	if p != nil {
		return domain.WindowDecision{}, nil, p
	}
	return decision, forcedClosed, nil
}

func (uc *BuildTapeFromTrades) forceCloseWindow(
	ctx context.Context,
	forced domain.ForcedWindowClose,
) (*TapeCloseEvent, *problem.Problem) {
	key := domain.TapeKey{Venue: forced.Key.Venue, Instrument: forced.Key.Instrument, Timeframe: forced.Key.Timeframe}
	existing, ok := uc.windows.Get(key)
	if !ok {
		metrics.SetMRWindowOpen(key.Venue, key.Instrument, key.Timeframe, 0)
		return nil, nil
	}
	if existing.WindowStartTs != forced.WindowStart {
		return nil, nil
	}
	windowDurationMs := uc.windowMs[key.Timeframe]
	if windowDurationMs <= 0 {
		return nil, problem.Newf(problem.ValidationFailed, "window duration must be > 0 for timeframe=%s", key.Timeframe)
	}
	if p := existing.Close(existing.WindowStartTs + windowDurationMs); p != nil {
		return nil, p
	}
	evt, p := uc.persistClosedWindow(ctx, *existing)
	if p != nil {
		return nil, p
	}
	uc.windows.Delete(key)
	metrics.SetMRWindowOpen(key.Venue, key.Instrument, key.Timeframe, 0)
	return &evt, nil
}

func resolveTapeWindowDurations(config map[string]time.Duration) (map[string]int64, *problem.Problem) {
	defaults := map[string]time.Duration{
		"250ms": 250 * time.Millisecond,
		"1s":    time.Second,
		"5s":    5 * time.Second,
	}
	out := make(map[string]int64, len(domain.AllowedTapeTimeframes))
	for _, timeframe := range domain.AllowedTapeTimeframes {
		dur := defaults[timeframe]
		if config != nil {
			if custom, ok := config[timeframe]; ok {
				dur = custom
			}
		}
		if dur <= 0 {
			return nil, problem.Newf(problem.ValidationFailed, "window duration must be > 0 for timeframe=%s", timeframe)
		}
		out[timeframe] = dur.Milliseconds()
	}
	return out, nil
}
