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

const candleTimeframeBase = "1s"

// BuildCandleConfig controls bounded state and window durations for candle build.
type BuildCandleConfig struct {
	MaxCandles     int
	WindowCap      int
	CandleTTL      time.Duration
	LateTolerance  time.Duration
	WindowDuration map[string]time.Duration
	Clock          clock.Clock
}

// BuildCandleRequest is one normalized trade input for candle derivation.
type BuildCandleRequest struct {
	Venue      string
	Instrument string
	Price      float64
	Quantity   float64
	IsBuy      bool
	Seq        int64
	TsIngest   int64
}

// BuildCandleResponse reports emitted close events and active state cardinality.
type BuildCandleResponse struct {
	Closed        []domain.CandleClosed
	ActiveCandles int
}

// BuildCandleFromEvents builds OHLCV candles from trade events.
type BuildCandleFromEvents struct {
	publisher  ports.ArtifactPublisher
	store      ports.CandleHotReadModelStore
	candles    *ds.BoundedMap[domain.CandleKey, *domain.CandleV1]
	windows    domain.WindowManager
	windowMs   map[string]int64
	timeframes []string
}

// NewBuildCandleFromEvents constructs BuildCandleFromEvents.
func NewBuildCandleFromEvents(
	pub ports.ArtifactPublisher,
	store ports.CandleHotReadModelStore,
	cfg BuildCandleConfig,
) *BuildCandleFromEvents {
	if cfg.MaxCandles <= 0 {
		cfg.MaxCandles = 50_000
	}
	if cfg.CandleTTL <= 0 {
		cfg.CandleTTL = time.Hour
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

	windowMs, p := resolveWindowDurations(cfg.WindowDuration, domain.AllowedCandleTimeframes)
	if p != nil {
		windowMs, _ = resolveWindowDurations(nil, domain.AllowedCandleTimeframes)
	}
	candles := ds.NewBoundedMap[domain.CandleKey, *domain.CandleV1](cfg.MaxCandles, cfg.CandleTTL, cfg.Clock)
	candles.SetSweepEveryOps(1024)
	candles.SetSweepMinInterval(time.Second)
	windows, p := domain.NewWatermarkWindowManager(domain.WatermarkWindowConfig{
		MaxOpenWindows:  cfg.WindowCap,
		LateToleranceMs: cfg.LateTolerance.Milliseconds(),
	})
	if p != nil {
		windows, _ = domain.NewWatermarkWindowManager(domain.WatermarkWindowConfig{
			MaxOpenWindows:  96,
			LateToleranceMs: 30_000,
		})
	}

	return &BuildCandleFromEvents{
		publisher:  pub,
		store:      store,
		candles:    candles,
		windows:    windows,
		windowMs:   windowMs,
		timeframes: []string{"1s", "5s", "1m", "5m", "15m", "30m", "1h", "4h", "1d"},
	}
}

// Execute applies one trade input and emits any finalized candles.
func (uc *BuildCandleFromEvents) Execute(ctx context.Context, req BuildCandleRequest) (BuildCandleResponse, *problem.Problem) {
	if p := uc.validateRequest(req); p != nil {
		return BuildCandleResponse{}, p
	}

	venue := naming.CanonicalVenue(req.Venue)
	instrument := naming.CanonicalInstrument(req.Instrument)
	var closed []domain.CandleClosed

	baseKey := domain.CandleKey{
		Venue:      venue,
		Instrument: instrument,
		Timeframe:  candleTimeframeBase,
	}
	baseDecision, forcedClosed, p := uc.observeWindow(ctx, baseKey, req.TsIngest, uc.windowMs[candleTimeframeBase])
	if p != nil {
		return BuildCandleResponse{}, p
	}
	if forcedClosed != nil {
		closed = append(closed, *forcedClosed)
	}
	if baseDecision.IsLate {
		metrics.IncMRWindowLateArrival(baseKey.Venue, baseKey.Instrument, baseKey.Timeframe)
		return BuildCandleResponse{
			Closed:        closed,
			ActiveCandles: uc.candles.Len(),
		}, nil
	}
	metrics.SetMRWindowOpen(baseKey.Venue, baseKey.Instrument, baseKey.Timeframe, 1)
	baseWindowStart := baseDecision.WindowStart
	baseClosedEvt, baseClosed, p := uc.closeIfWindowChanged(
		ctx,
		baseKey,
		baseWindowStart,
		uc.windowMs[candleTimeframeBase],
	)
	if p != nil {
		return BuildCandleResponse{}, p
	}
	if baseClosedEvt != nil {
		closed = append(closed, *baseClosedEvt)
	}

	baseCandle, p := uc.getOrCreateCandle(baseKey, baseWindowStart)
	if p != nil {
		return BuildCandleResponse{}, p
	}
	if p := baseCandle.ApplyTrade(req.Price, req.Quantity, req.IsBuy, req.Seq); p != nil {
		return BuildCandleResponse{}, p
	}
	uc.candles.Put(baseKey, baseCandle)

	if baseClosed != nil {
		cascadeClosed, p := uc.cascadeFromClosedBase(ctx, *baseClosed)
		if p != nil {
			return BuildCandleResponse{}, p
		}
		closed = append(closed, cascadeClosed...)
	}

	return BuildCandleResponse{
		Closed:        closed,
		ActiveCandles: uc.candles.Len(),
	}, nil
}

// ActiveCandles returns current bounded-map cardinality.
func (uc *BuildCandleFromEvents) ActiveCandles() int {
	return uc.candles.Len()
}

func (uc *BuildCandleFromEvents) validateRequest(req BuildCandleRequest) *problem.Problem {
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

func (uc *BuildCandleFromEvents) cascadeFromClosedBase(
	ctx context.Context,
	base domain.CandleV1,
) ([]domain.CandleClosed, *problem.Problem) {
	var closed []domain.CandleClosed
	for _, timeframe := range uc.timeframes {
		if timeframe == candleTimeframeBase {
			continue
		}
		key := domain.CandleKey{
			Venue:      base.Venue,
			Instrument: base.Instrument,
			Timeframe:  timeframe,
		}
		decision, forcedClosed, p := uc.observeWindow(ctx, key, base.WindowStartTs, uc.windowMs[timeframe])
		if p != nil {
			return nil, p
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
		closedEvt, _, p := uc.closeIfWindowChanged(ctx, key, windowStart, uc.windowMs[timeframe])
		if p != nil {
			return nil, p
		}
		if closedEvt != nil {
			closed = append(closed, *closedEvt)
		}
		candle, p := uc.getOrCreateCandle(key, windowStart)
		if p != nil {
			return nil, p
		}
		if p := candle.ApplyClosedCandle(base); p != nil {
			return nil, p
		}
		uc.candles.Put(key, candle)
	}
	return closed, nil
}

func (uc *BuildCandleFromEvents) closeIfWindowChanged(
	ctx context.Context,
	key domain.CandleKey,
	windowStart int64,
	windowDurationMs int64,
) (*domain.CandleClosed, *domain.CandleV1, *problem.Problem) {
	existing, ok := uc.candles.Get(key)
	if !ok {
		return nil, nil, nil
	}
	if existing.WindowStartTs == windowStart {
		return nil, nil, nil
	}
	if p := existing.Close(existing.WindowStartTs + windowDurationMs); p != nil {
		return nil, nil, p
	}
	closedEvt, p := uc.persistClosedCandle(ctx, *existing)
	if p != nil {
		return nil, nil, p
	}
	uc.candles.Delete(key)
	closedCopy := closedEvt.Candle
	return &closedEvt, &closedCopy, nil
}

func (uc *BuildCandleFromEvents) getOrCreateCandle(
	key domain.CandleKey,
	windowStart int64,
) (*domain.CandleV1, *problem.Problem) {
	if c, ok := uc.candles.Get(key); ok {
		if c.WindowStartTs != windowStart {
			return nil, problem.New(problem.IntegrityViolation, "candle window mismatch after window roll")
		}
		return c, nil
	}
	c, p := domain.NewCandleV1(key.Venue, key.Instrument, key.Timeframe, windowStart)
	if p != nil {
		return nil, p
	}
	uc.candles.Put(key, c)
	return c, nil
}

func (uc *BuildCandleFromEvents) persistClosedCandle(
	ctx context.Context,
	candle domain.CandleV1,
) (domain.CandleClosed, *problem.Problem) {
	evt := domain.NewCandleClosed(candle)
	if uc.store != nil {
		if p := uc.store.SaveCandle(ctx, evt); p != nil {
			return domain.CandleClosed{}, p
		}
	}
	if uc.publisher != nil {
		if p := uc.publisher.PublishCandleClosed(ctx, evt); p != nil {
			return domain.CandleClosed{}, p
		}
	}
	return evt, nil
}

func (uc *BuildCandleFromEvents) observeWindow(
	ctx context.Context,
	key domain.CandleKey,
	eventTsMs int64,
	windowDurationMs int64,
) (domain.WindowDecision, *domain.CandleClosed, *problem.Problem) {
	if uc.windows == nil {
		return domain.WindowDecision{WindowStart: bucketStart(eventTsMs, windowDurationMs)}, nil, nil
	}
	decision, p := uc.windows.Observe(domain.WindowKey(key), eventTsMs, windowDurationMs)
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

func (uc *BuildCandleFromEvents) forceCloseWindow(
	ctx context.Context,
	forced domain.ForcedWindowClose,
) (*domain.CandleClosed, *problem.Problem) {
	key := domain.CandleKey{
		Venue:      forced.Key.Venue,
		Instrument: forced.Key.Instrument,
		Timeframe:  forced.Key.Timeframe,
	}
	existing, ok := uc.candles.Get(key)
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
	evt, p := uc.persistClosedCandle(ctx, *existing)
	if p != nil {
		return nil, p
	}
	uc.candles.Delete(key)
	metrics.SetMRWindowOpen(key.Venue, key.Instrument, key.Timeframe, 0)
	return &evt, nil
}

func resolveWindowDurations(
	config map[string]time.Duration,
	allowed []string,
) (map[string]int64, *problem.Problem) {
	defaults := map[string]time.Duration{
		"1s":  time.Second,
		"5s":  5 * time.Second,
		"1m":  time.Minute,
		"5m":  5 * time.Minute,
		"15m": 15 * time.Minute,
		"30m": 30 * time.Minute,
		"1h":  time.Hour,
		"4h":  4 * time.Hour,
		"1d":  24 * time.Hour,
	}
	out := make(map[string]int64, len(allowed))
	for _, timeframe := range allowed {
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

func bucketStart(tsMs, windowMs int64) int64 {
	if windowMs <= 0 {
		return tsMs
	}
	return (tsMs / windowMs) * windowMs
}
