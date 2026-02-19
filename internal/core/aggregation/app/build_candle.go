package app

import (
	"context"
	"math"
	"time"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/ds"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

const candleTimeframe1m = "1m"

// BuildCandleConfig controls bounded state and window durations for candle build.
type BuildCandleConfig struct {
	MaxCandles     int
	CandleTTL      time.Duration
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

	return &BuildCandleFromEvents{
		publisher:  pub,
		store:      store,
		candles:    candles,
		windowMs:   windowMs,
		timeframes: []string{"1m", "5m", "15m", "30m", "1h"},
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

	minuteKey := domain.CandleKey{
		Venue:      venue,
		Instrument: instrument,
		Timeframe:  candleTimeframe1m,
	}
	minuteWindowStart := bucketStart(req.TsIngest, uc.windowMs[candleTimeframe1m])
	minuteClosedEvt, minuteClosed, p := uc.closeIfWindowChanged(
		ctx,
		minuteKey,
		minuteWindowStart,
		uc.windowMs[candleTimeframe1m],
	)
	if p != nil {
		return BuildCandleResponse{}, p
	}
	if minuteClosedEvt != nil {
		closed = append(closed, *minuteClosedEvt)
	}

	minuteCandle, p := uc.getOrCreateCandle(minuteKey, minuteWindowStart)
	if p != nil {
		return BuildCandleResponse{}, p
	}
	if p := minuteCandle.ApplyTrade(req.Price, req.Quantity, req.IsBuy, req.Seq); p != nil {
		return BuildCandleResponse{}, p
	}
	uc.candles.Put(minuteKey, minuteCandle)

	if minuteClosed != nil {
		cascadeClosed, p := uc.cascadeFromClosedMinute(ctx, *minuteClosed)
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

func (uc *BuildCandleFromEvents) cascadeFromClosedMinute(
	ctx context.Context,
	minute domain.CandleV1,
) ([]domain.CandleClosed, *problem.Problem) {
	var closed []domain.CandleClosed
	for _, timeframe := range uc.timeframes {
		if timeframe == candleTimeframe1m {
			continue
		}
		key := domain.CandleKey{
			Venue:      minute.Venue,
			Instrument: minute.Instrument,
			Timeframe:  timeframe,
		}
		windowStart := bucketStart(minute.WindowStartTs, uc.windowMs[timeframe])
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
		if p := candle.ApplyClosedCandle(minute); p != nil {
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

func resolveWindowDurations(
	config map[string]time.Duration,
	allowed []string,
) (map[string]int64, *problem.Problem) {
	defaults := map[string]time.Duration{
		"1m":  time.Minute,
		"5m":  5 * time.Minute,
		"15m": 15 * time.Minute,
		"30m": 30 * time.Minute,
		"1h":  time.Hour,
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
