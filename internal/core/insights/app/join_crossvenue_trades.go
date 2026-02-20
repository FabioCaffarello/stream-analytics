// Package app contains application use cases for the insights context.
package app

import (
	"context"
	"math"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/ds"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/result"
	"github.com/market-raccoon/internal/shared/validation"
)

// JoinCrossVenueTradesConfig controls bounded state for join processing.
type JoinCrossVenueTradesConfig struct {
	MaxInstruments int
	TTL            time.Duration
	// EnableSpreadSignal toggles optional spread-signal emission.
	EnableSpreadSignal bool
	// MinVenues is the minimum joined venue count required for spread-signal emission.
	MinVenues int
	// MinSpreadBPS is the minimum spread threshold for spread-signal emission.
	MinSpreadBPS float64
	// RoundingMode controls deterministic rounding for derived spread math.
	// Supported: "half_even" (default) | "floor".
	RoundingMode string
	// SweepEveryN triggers one explicit sweep every N updates.
	// When > 0, it takes precedence over SweepEvery.
	SweepEveryN int
	// SweepEvery triggers one explicit sweep based on elapsed wall-clock duration.
	// Used only when SweepEveryN == 0.
	SweepEvery time.Duration
	Clock      clock.Clock
}

// JoinCrossVenueTradesRequest is one normalized marketdata.trade input.
type JoinCrossVenueTradesRequest struct {
	Venue          string
	Instrument     string
	MarketType     string
	Price          float64
	Size           float64
	Side           string
	TradeID        string
	TsExchange     int64
	TsIngest       int64
	Seq            int64
	IdempotencyKey string
}

// JoinCrossVenueTradesResponse returns an optional emitted snapshot.
type JoinCrossVenueTradesResponse struct {
	Emitted       bool
	Snapshot      domain.CrossVenueTradeSnapshotV1
	SignalEmitted bool
	SpreadSignal  domain.CrossVenueSpreadSignalV1
}

type instrumentState struct {
	venues map[string]domain.LastTrade
}

type spreadRoundingMode uint8

const (
	spreadRoundingHalfEven spreadRoundingMode = iota
	spreadRoundingFloor
)

const (
	derivedPriceDecimals = 8
	derivedBPSDecimals   = 4
)

// JoinCrossVenueTrades maintains bounded per-instrument state and emits
// deterministic snapshots once at least two venues are present.
type JoinCrossVenueTrades struct {
	states       *ds.BoundedMap[domain.JoinKey, *instrumentState]
	clock        clock.Clock
	enableSignal bool
	minVenues    int
	minSpreadBPS float64
	roundingMode spreadRoundingMode
	sweepEveryN  uint64
	sweepEvery   time.Duration
	updates      atomic.Uint64
	lastSweepMs  atomic.Int64
	sweepCalls   atomic.Uint64
}

// NewJoinCrossVenueTrades constructs JoinCrossVenueTrades with defaults.
func NewJoinCrossVenueTrades() *JoinCrossVenueTrades {
	return NewJoinCrossVenueTradesWithConfig(JoinCrossVenueTradesConfig{
		MaxInstruments:     10_000,
		TTL:                time.Hour,
		EnableSpreadSignal: false,
		MinVenues:          2,
		MinSpreadBPS:       0,
		RoundingMode:       "half_even",
		SweepEveryN:        1024,
		SweepEvery:         30 * time.Second,
		Clock:              clock.NewSystemClock(),
	})
}

// NewJoinCrossVenueTradesWithConfig constructs JoinCrossVenueTrades with explicit bounds.
func NewJoinCrossVenueTradesWithConfig(cfg JoinCrossVenueTradesConfig) *JoinCrossVenueTrades {
	if cfg.MaxInstruments <= 0 {
		cfg.MaxInstruments = 10_000
	}
	if cfg.TTL <= 0 {
		cfg.TTL = time.Hour
	}
	if cfg.MinVenues < 2 {
		cfg.MinVenues = 2
	}
	if cfg.MinSpreadBPS < 0 || math.IsNaN(cfg.MinSpreadBPS) || math.IsInf(cfg.MinSpreadBPS, 0) {
		cfg.MinSpreadBPS = 0
	}
	if cfg.SweepEveryN < 0 {
		cfg.SweepEveryN = 0
	}
	if cfg.SweepEvery < 0 {
		cfg.SweepEvery = 0
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.NewSystemClock()
	}

	states := ds.NewBoundedMap[domain.JoinKey, *instrumentState](cfg.MaxInstruments, cfg.TTL, cfg.Clock)
	// Disable internal opportunistic sweeping so cadence is deterministic and explicit here.
	states.SetSweepEveryOps(0)
	states.SetSweepMinInterval(0)
	states.SetOnEvict(func(_ domain.JoinKey, _ *instrumentState, reason string) {
		metrics.IncInsightsStateEvictions(reason)
	})

	uc := &JoinCrossVenueTrades{
		states:       states,
		clock:        cfg.Clock,
		enableSignal: cfg.EnableSpreadSignal,
		minVenues:    cfg.MinVenues,
		minSpreadBPS: cfg.MinSpreadBPS,
		roundingMode: parseSpreadRoundingMode(cfg.RoundingMode),
		sweepEveryN:  uint64(cfg.SweepEveryN),
		sweepEvery:   cfg.SweepEvery,
	}
	uc.lastSweepMs.Store(cfg.Clock.NowUnixMilli())
	metrics.SetInsightsStateInstrumentsActive(float64(states.Len()))
	return uc
}

// Execute updates join state for one trade and optionally emits a snapshot.
func (uc *JoinCrossVenueTrades) Execute(_ context.Context, req JoinCrossVenueTradesRequest) result.Result[JoinCrossVenueTradesResponse] {
	if p := uc.validateRequest(req); p != nil {
		return result.FailProblem[JoinCrossVenueTradesResponse](p)
	}

	key, p := domain.NewJoinKey(req.Instrument, req.MarketType)
	if p != nil {
		return result.FailProblem[JoinCrossVenueTradesResponse](p)
	}

	state := uc.getOrCreateState(key)
	venue := naming.CanonicalVenue(req.Venue)
	next := domain.LastTrade{
		Venue:          venue,
		Price:          req.Price,
		Size:           req.Size,
		Side:           naming.NormalizeSide(req.Side),
		TradeID:        strings.TrimSpace(req.TradeID),
		TsExchange:     req.TsExchange,
		TsIngest:       req.TsIngest,
		Seq:            req.Seq,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
	}
	if prev, ok := state.venues[venue]; ok && compareLastTrade(next, prev) <= 0 {
		uc.states.Put(key, state)
		uc.afterUpdate(0)
		return result.Ok(JoinCrossVenueTradesResponse{Emitted: false})
	}

	state.venues[venue] = next
	uc.states.Put(key, state)

	if len(state.venues) < 2 {
		uc.afterUpdate(0)
		return result.Ok(JoinCrossVenueTradesResponse{Emitted: false})
	}

	snapshot := buildSnapshot(key, req.TsIngest, state.venues, uc.roundingMode)
	if p := snapshot.Validate(); p != nil {
		return result.FailProblem[JoinCrossVenueTradesResponse](p)
	}

	resp := JoinCrossVenueTradesResponse{
		Emitted:  true,
		Snapshot: snapshot,
	}
	if uc.enableSignal && len(snapshot.Venues) >= uc.minVenues && snapshot.SpreadBps >= uc.minSpreadBPS {
		signal := buildSpreadSignal(snapshot)
		if p := signal.Validate(); p != nil {
			return result.FailProblem[JoinCrossVenueTradesResponse](p)
		}
		resp.SignalEmitted = true
		resp.SpreadSignal = signal
	}
	uc.afterUpdate(len(snapshot.Venues))
	return result.Ok(resp)
}

// ActiveInstruments returns current bounded-map cardinality.
func (uc *JoinCrossVenueTrades) ActiveInstruments() int {
	return uc.states.Len()
}

func (uc *JoinCrossVenueTrades) afterUpdate(snapshotVenueCount int) {
	uc.maybeSweep()
	metrics.SetInsightsStateInstrumentsActive(float64(uc.states.Len()))
	if snapshotVenueCount >= 2 {
		metrics.IncInsightsSnapshots(snapshotVenueCount)
	}
}

func (uc *JoinCrossVenueTrades) maybeSweep() {
	if uc.sweepEveryN > 0 {
		updates := uc.updates.Add(1)
		if updates%uc.sweepEveryN != 0 {
			return
		}
		_ = uc.states.Sweep()
		uc.sweepCalls.Add(1)
		uc.lastSweepMs.Store(uc.clock.NowUnixMilli())
		return
	}

	if uc.sweepEvery <= 0 {
		return
	}

	nowMs := uc.clock.NowUnixMilli()
	lastMs := uc.lastSweepMs.Load()
	if nowMs-lastMs < uc.sweepEvery.Milliseconds() {
		return
	}
	if !uc.lastSweepMs.CompareAndSwap(lastMs, nowMs) {
		return
	}
	_ = uc.states.Sweep()
	uc.sweepCalls.Add(1)
}

func (uc *JoinCrossVenueTrades) getOrCreateState(key domain.JoinKey) *instrumentState {
	if existing, ok := uc.states.Get(key); ok {
		return existing
	}
	state := &instrumentState{
		venues: make(map[string]domain.LastTrade, 4),
	}
	uc.states.Put(key, state)
	return state
}

func (uc *JoinCrossVenueTrades) validateRequest(req JoinCrossVenueTradesRequest) *problem.Problem {
	if p := validation.Collect(
		validation.NonEmptyString("venue", req.Venue),
		validation.NonEmptyString("instrument", req.Instrument),
		validation.PositiveInt("ts_ingest", req.TsIngest),
	); p != nil {
		return p
	}
	if req.Seq < 0 {
		return problem.Newf(problem.ValidationFailed, "seq must be >= 0, got %d", req.Seq)
	}
	if math.IsNaN(req.Price) || math.IsInf(req.Price, 0) {
		return problem.New(problem.ValidationFailed, "price must be a finite number")
	}
	if math.IsNaN(req.Size) || math.IsInf(req.Size, 0) {
		return problem.New(problem.ValidationFailed, "size must be a finite number")
	}
	return nil
}

func buildSnapshot(
	key domain.JoinKey,
	watermarkTsIngest int64,
	byVenue map[string]domain.LastTrade,
	roundingMode spreadRoundingMode,
) domain.CrossVenueTradeSnapshotV1 {
	venues := make([]string, 0, len(byVenue))
	for venue := range byVenue {
		venues = append(venues, venue)
	}
	slices.Sort(venues)

	rows := make([]domain.SnapshotVenueTradeV1, 0, len(venues))
	for _, venue := range venues {
		trade := byVenue[venue]
		rows = append(rows, domain.SnapshotVenueTradeV1{
			Venue:      venue,
			Price:      trade.Price,
			Size:       trade.Size,
			Side:       trade.Side,
			TradeID:    trade.TradeID,
			TsExchange: trade.TsExchange,
			TsIngest:   trade.TsIngest,
			Seq:        trade.Seq,
		})
	}

	minPrice := rows[0].Price
	maxPrice := rows[0].Price
	minVenue := rows[0].Venue
	maxVenue := rows[0].Venue
	for _, row := range rows[1:] {
		if row.Price < minPrice {
			minPrice = row.Price
			minVenue = row.Venue
		}
		if row.Price > maxPrice {
			maxPrice = row.Price
			maxVenue = row.Venue
		}
	}
	minPrice = roundDeterministic(minPrice, derivedPriceDecimals, roundingMode)
	maxPrice = roundDeterministic(maxPrice, derivedPriceDecimals, roundingMode)
	spreadAbs := roundDeterministic(maxPrice-minPrice, derivedPriceDecimals, roundingMode)
	midPrice := roundDeterministic((maxPrice+minPrice)/2, derivedPriceDecimals, roundingMode)
	spreadBPS := float64(0)
	if midPrice > 0 {
		spreadBPS = roundDeterministic((spreadAbs/midPrice)*10_000, derivedBPSDecimals, roundingMode)
	}

	snap := domain.CrossVenueTradeSnapshotV1{
		Instrument:        key.Instrument,
		WatermarkTsIngest: watermarkTsIngest,
		Venues:            rows,
		MinPrice:          minPrice,
		MinPriceVenue:     minVenue,
		MaxPrice:          maxPrice,
		MaxPriceVenue:     maxVenue,
		SpreadAbs:         spreadAbs,
		SpreadBps:         spreadBPS,
		MidPrice:          midPrice,
	}
	if key.HasMarketType() {
		snap.MarketType = key.MarketType
	}
	return snap
}

func buildSpreadSignal(snapshot domain.CrossVenueTradeSnapshotV1) domain.CrossVenueSpreadSignalV1 {
	return domain.CrossVenueSpreadSignalV1{
		Instrument:        snapshot.Instrument,
		MarketType:        snapshot.MarketType,
		WatermarkTsIngest: snapshot.WatermarkTsIngest,
		MinPrice:          snapshot.MinPrice,
		MinPriceVenue:     snapshot.MinPriceVenue,
		MaxPrice:          snapshot.MaxPrice,
		MaxPriceVenue:     snapshot.MaxPriceVenue,
		SpreadAbs:         snapshot.SpreadAbs,
		SpreadBps:         snapshot.SpreadBps,
	}
}

func parseSpreadRoundingMode(raw string) spreadRoundingMode {
	switch naming.NormalizeSide(raw) {
	case "floor":
		return spreadRoundingFloor
	case "half_even", "bankers", "":
		return spreadRoundingHalfEven
	default:
		return spreadRoundingHalfEven
	}
}

func roundDeterministic(value float64, decimals int, mode spreadRoundingMode) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	scale := math.Pow10(decimals)
	scaled := value * scale
	switch mode {
	case spreadRoundingFloor:
		return math.Floor(scaled) / scale
	default:
		return math.RoundToEven(scaled) / scale
	}
}

func compareLastTrade(a, b domain.LastTrade) int {
	if a.Seq != b.Seq {
		return cmpInt64(a.Seq, b.Seq)
	}
	if a.TsIngest != b.TsIngest {
		return cmpInt64(a.TsIngest, b.TsIngest)
	}
	if a.TsExchange != b.TsExchange {
		return cmpInt64(a.TsExchange, b.TsExchange)
	}
	if c := strings.Compare(a.TradeID, b.TradeID); c != 0 {
		return c
	}
	if c := strings.Compare(a.Side, b.Side); c != 0 {
		return c
	}
	if c := cmpFloat64(a.Price, b.Price); c != 0 {
		return c
	}
	if c := cmpFloat64(a.Size, b.Size); c != 0 {
		return c
	}
	return strings.Compare(a.IdempotencyKey, b.IdempotencyKey)
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpFloat64(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func (uc *JoinCrossVenueTrades) sweepCallsForTest() uint64 {
	return uc.sweepCalls.Load()
}
