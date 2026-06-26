package app

import (
	"context"
	"math"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/ds"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/validation"
)

// StatsInputKind identifies the source event kind for stats updates.
type StatsInputKind string

const (
	StatsInputLiquidation StatsInputKind = "liquidation"
	StatsInputMarkPrice   StatsInputKind = "markprice"
	StatsInputFundingRate StatsInputKind = "fundingrate"
)

// BuildStatsConfig controls bounded state and window durations for stats build.
type BuildStatsConfig struct {
	MaxWindows     int
	WindowCap      int
	WindowTTL      time.Duration
	LateTolerance  time.Duration
	WindowDuration map[string]time.Duration
	Clock          clock.Clock
}

// BuildStatsRequest is one normalized input update for stats derivation.
type BuildStatsRequest struct {
	Venue      string
	Instrument string
	Kind       StatsInputKind
	Seq        int64
	TsIngest   int64

	LiquidationSide string
	LiquidationQty  float64
	MarkPrice       float64
	FundingRate     float64
}

// BuildStatsResponse reports emitted close events and active state cardinality.
type BuildStatsResponse struct {
	Closed        []domain.StatsWindowClosed
	ActiveWindows int
}

// BuildStatsFromEvents builds per-window stats from liquidation/mark/funding inputs.
type BuildStatsFromEvents struct {
	publisher  ports.ArtifactPublisher
	store      ports.StatsHotReadModelStore
	windows    *ds.BoundedMap[domain.StatsKey, *domain.StatsWindowV1]
	lifecycle  domain.WindowManager
	windowMs   map[string]int64
	timeframes []string
}

const statsClosedBufferCap = 18 // 9 timeframes + up to 9 forced closes in one input tick.

// NewBuildStatsFromEvents constructs BuildStatsFromEvents.
func NewBuildStatsFromEvents(
	pub ports.ArtifactPublisher,
	store ports.StatsHotReadModelStore,
	cfg BuildStatsConfig,
) *BuildStatsFromEvents {
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

	windowMs, p := resolveWindowDurations(cfg.WindowDuration, domain.AllowedStatsTimeframes)
	if p != nil {
		windowMs, _ = resolveWindowDurations(nil, domain.AllowedStatsTimeframes)
	}
	windows := ds.NewBoundedMap[domain.StatsKey, *domain.StatsWindowV1](cfg.MaxWindows, cfg.WindowTTL, cfg.Clock)
	windows.SetSweepEveryOps(1024)
	windows.SetSweepMinInterval(time.Second)
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

	return &BuildStatsFromEvents{
		publisher:  pub,
		store:      store,
		windows:    windows,
		lifecycle:  lifecycle,
		windowMs:   windowMs,
		timeframes: []string{"1s", "5s", "1m", "5m", "15m", "30m", "1h", "4h", "1d"},
	}
}

// Execute applies one event input and emits any finalized stats windows.
func (uc *BuildStatsFromEvents) Execute(ctx context.Context, req BuildStatsRequest) (BuildStatsResponse, *problem.Problem) {
	if p := uc.validateRequest(req); p != nil {
		return BuildStatsResponse{}, p
	}
	venue := naming.CanonicalVenue(req.Venue)
	instrument := naming.CanonicalInstrument(req.Instrument)
	var closedBuf [statsClosedBufferCap]domain.StatsWindowClosed
	closedCount := 0

	for _, timeframe := range uc.timeframes {
		key := domain.StatsKey{
			Venue:      venue,
			Instrument: instrument,
			Timeframe:  timeframe,
		}
		decision, forcedClosed, p := uc.observeWindow(ctx, key, req.TsIngest, uc.windowMs[timeframe])
		if p != nil {
			return BuildStatsResponse{}, p
		}
		if forcedClosed != nil {
			if closedCount < len(closedBuf) {
				closedBuf[closedCount] = *forcedClosed
				closedCount++
			}
		}
		if decision.IsLate {
			metrics.IncMRWindowLateArrival(key.Venue, key.Instrument, key.Timeframe)
			continue
		}
		metrics.SetMRWindowOpen(key.Venue, key.Instrument, key.Timeframe, 1)
		windowStart := decision.WindowStart
		closedEvt, p := uc.closeIfWindowChanged(ctx, key, windowStart, uc.windowMs[timeframe])
		if p != nil {
			return BuildStatsResponse{}, p
		}
		if closedEvt != nil {
			if closedCount < len(closedBuf) {
				closedBuf[closedCount] = *closedEvt
				closedCount++
			}
		}

		win, p := uc.getOrCreateWindow(key, windowStart, uc.windowMs[timeframe])
		if p != nil {
			return BuildStatsResponse{}, p
		}
		if req.TsIngest > win.TsIngestMs {
			win.TsIngestMs = req.TsIngest
		}
		if p := applyStatsUpdate(win, req); p != nil {
			return BuildStatsResponse{}, p
		}
		uc.windows.Put(key, win)
	}

	var closed []domain.StatsWindowClosed
	if closedCount > 0 {
		closed = make([]domain.StatsWindowClosed, closedCount)
		copy(closed, closedBuf[:closedCount])
	}

	return BuildStatsResponse{
		Closed:        closed,
		ActiveWindows: uc.windows.Len(),
	}, nil
}

// ActiveWindows returns current bounded-map cardinality.
func (uc *BuildStatsFromEvents) ActiveWindows() int {
	return uc.windows.Len()
}

func (uc *BuildStatsFromEvents) validateRequest(req BuildStatsRequest) *problem.Problem {
	if p := validation.Collect(
		validation.NonEmptyString("venue", req.Venue),
		validation.NonEmptyString("instrument", req.Instrument),
		validation.PositiveInt("seq", req.Seq),
		validation.PositiveInt("ts_ingest", req.TsIngest),
	); p != nil {
		return p
	}
	switch req.Kind {
	case StatsInputLiquidation:
		if math.IsNaN(req.LiquidationQty) || math.IsInf(req.LiquidationQty, 0) || req.LiquidationQty <= 0 {
			return problem.New(problem.ValidationFailed, "liquidation_qty must be a positive finite number")
		}
		if p := validation.NonEmptyString("liquidation_side", req.LiquidationSide); p != nil {
			return p
		}
	case StatsInputMarkPrice:
		if math.IsNaN(req.MarkPrice) || math.IsInf(req.MarkPrice, 0) || req.MarkPrice <= 0 {
			return problem.New(problem.ValidationFailed, "mark_price must be a positive finite number")
		}
	case StatsInputFundingRate:
		if math.IsNaN(req.FundingRate) || math.IsInf(req.FundingRate, 0) {
			return problem.New(problem.ValidationFailed, "funding_rate must be a finite number")
		}
	default:
		return problem.Newf(problem.ValidationFailed, "unknown stats input kind %q", req.Kind)
	}
	return nil
}

func (uc *BuildStatsFromEvents) closeIfWindowChanged(
	ctx context.Context,
	key domain.StatsKey,
	windowStart int64,
	windowDurationMs int64,
) (*domain.StatsWindowClosed, *problem.Problem) {
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
	existing.WindowMs = windowDurationMs
	if existing.TsIngestMs <= 0 {
		existing.TsIngestMs = existing.WindowEndTs
	}
	existing.SetQualityFlags(false)
	evt, p := uc.persistClosedStats(ctx, *existing)
	if p != nil {
		return nil, p
	}
	uc.windows.Delete(key)
	return &evt, nil
}

func (uc *BuildStatsFromEvents) getOrCreateWindow(
	key domain.StatsKey,
	windowStart int64,
	windowDurationMs int64,
) (*domain.StatsWindowV1, *problem.Problem) {
	if w, ok := uc.windows.Get(key); ok {
		if w.WindowStartTs != windowStart {
			return nil, problem.New(problem.IntegrityViolation, "stats window mismatch after window roll")
		}
		if w.WindowMs <= 0 {
			w.WindowMs = windowDurationMs
		}
		return w, nil
	}
	w, p := domain.NewStatsWindowV1(key.Venue, key.Instrument, key.Timeframe, windowStart)
	if p != nil {
		return nil, p
	}
	w.WindowMs = windowDurationMs
	uc.windows.Put(key, w)
	return w, nil
}

func (uc *BuildStatsFromEvents) persistClosedStats(
	ctx context.Context,
	stats domain.StatsWindowV1,
) (domain.StatsWindowClosed, *problem.Problem) {
	evt := domain.NewStatsWindowClosed(stats)
	if uc.store != nil {
		if p := uc.store.SaveStats(ctx, evt); p != nil {
			return domain.StatsWindowClosed{}, p
		}
	}
	if uc.publisher != nil {
		if p := uc.publisher.PublishStatsClosed(ctx, evt); p != nil {
			return domain.StatsWindowClosed{}, p
		}
	}
	return evt, nil
}

func applyStatsUpdate(window *domain.StatsWindowV1, req BuildStatsRequest) *problem.Problem {
	switch req.Kind {
	case StatsInputLiquidation:
		return window.ApplyLiquidation(req.LiquidationSide, req.LiquidationQty, req.Seq)
	case StatsInputMarkPrice:
		return window.ApplyMarkPrice(req.MarkPrice, req.Seq)
	case StatsInputFundingRate:
		return window.ApplyFundingRate(req.FundingRate, req.Seq)
	default:
		return problem.Newf(problem.ValidationFailed, "unknown stats input kind %q", req.Kind)
	}
}

func (uc *BuildStatsFromEvents) observeWindow(
	ctx context.Context,
	key domain.StatsKey,
	eventTsMs int64,
	windowDurationMs int64,
) (domain.WindowDecision, *domain.StatsWindowClosed, *problem.Problem) {
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

func (uc *BuildStatsFromEvents) forceCloseWindow(
	ctx context.Context,
	forced domain.ForcedWindowClose,
) (*domain.StatsWindowClosed, *problem.Problem) {
	key := domain.StatsKey{
		Venue:      forced.Key.Venue,
		Instrument: forced.Key.Instrument,
		Timeframe:  forced.Key.Timeframe,
	}
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
	existing.WindowMs = windowDurationMs
	if existing.TsIngestMs <= 0 {
		existing.TsIngestMs = existing.WindowEndTs
	}
	existing.SetQualityFlags(true)
	evt, p := uc.persistClosedStats(ctx, *existing)
	if p != nil {
		return nil, p
	}
	uc.windows.Delete(key)
	metrics.SetMRWindowOpen(key.Venue, key.Instrument, key.Timeframe, 0)
	return &evt, nil
}
