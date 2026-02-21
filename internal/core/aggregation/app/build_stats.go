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
	WindowTTL      time.Duration
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
	windowMs   map[string]int64
	timeframes []string
}

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

	return &BuildStatsFromEvents{
		publisher:  pub,
		store:      store,
		windows:    windows,
		windowMs:   windowMs,
		timeframes: []string{"1m", "5m", "15m", "30m", "1h", "4h", "1d"},
	}
}

// Execute applies one event input and emits any finalized stats windows.
func (uc *BuildStatsFromEvents) Execute(ctx context.Context, req BuildStatsRequest) (BuildStatsResponse, *problem.Problem) {
	if p := uc.validateRequest(req); p != nil {
		return BuildStatsResponse{}, p
	}
	venue := naming.CanonicalVenue(req.Venue)
	instrument := naming.CanonicalInstrument(req.Instrument)
	var closed []domain.StatsWindowClosed

	for _, timeframe := range uc.timeframes {
		key := domain.StatsKey{
			Venue:      venue,
			Instrument: instrument,
			Timeframe:  timeframe,
		}
		windowStart := bucketStart(req.TsIngest, uc.windowMs[timeframe])
		closedEvt, p := uc.closeIfWindowChanged(ctx, key, windowStart, uc.windowMs[timeframe])
		if p != nil {
			return BuildStatsResponse{}, p
		}
		if closedEvt != nil {
			closed = append(closed, *closedEvt)
		}

		win, p := uc.getOrCreateWindow(key, windowStart)
		if p != nil {
			return BuildStatsResponse{}, p
		}
		if p := applyStatsUpdate(win, req); p != nil {
			return BuildStatsResponse{}, p
		}
		uc.windows.Put(key, win)
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
) (*domain.StatsWindowV1, *problem.Problem) {
	if w, ok := uc.windows.Get(key); ok {
		if w.WindowStartTs != windowStart {
			return nil, problem.New(problem.IntegrityViolation, "stats window mismatch after window roll")
		}
		return w, nil
	}
	w, p := domain.NewStatsWindowV1(key.Venue, key.Instrument, key.Timeframe, windowStart)
	if p != nil {
		return nil, p
	}
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
