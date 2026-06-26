package app

import (
	"context"
	"slices"

	"github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/hash"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/result"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/validation"
)

const svpDefaultEmitCadence = 5

type BuildSessionVolumeProfileConfig struct {
	MaxBuckets  int
	EmitCadence int // emit snapshot every N candle events
}

// BuildSessionVolumeProfileRequest represents a closed candle used to build the SVP.
type BuildSessionVolumeProfileRequest struct {
	Venue      string
	Instrument string
	Anchor     domain.SessionAnchor
	TickSize   float64
	// Candle OHLCV fields.
	Open       float64
	High       float64
	Low        float64
	Close      float64
	BuyVolume  float64
	SellVolume float64
	TradeCount int64
	TsIngest   int64 // candle window start ms
	SeqFirst   int64
	SeqLast    int64
}

type BuildSessionVolumeProfileResponse struct {
	Emitted        bool
	Snapshot       domain.SessionVolumeProfileV1
	IdempotencyKey string
}

type BuildSessionVolumeProfile struct {
	cfg    BuildSessionVolumeProfileConfig
	states map[string]*svpPartitionState
}

type svpPartitionState struct {
	buckets     map[string]*vpvrBucketState
	totalBuy    float64
	totalSell   float64
	tradeCount  int64
	seqFirst    int64
	seqLast     int64
	windowStart int64
	windowEnd   int64
	eventCount  int
	dirty       bool
}

func NewBuildSessionVolumeProfile() *BuildSessionVolumeProfile {
	return NewBuildSessionVolumeProfileWithConfig(BuildSessionVolumeProfileConfig{})
}

func NewBuildSessionVolumeProfileWithConfig(cfg BuildSessionVolumeProfileConfig) *BuildSessionVolumeProfile {
	if cfg.MaxBuckets <= 0 {
		cfg.MaxBuckets = domain.SVPCapBuckets
	}
	if cfg.EmitCadence <= 0 {
		cfg.EmitCadence = svpDefaultEmitCadence
	}
	return &BuildSessionVolumeProfile{
		cfg:    cfg,
		states: make(map[string]*svpPartitionState),
	}
}

func (uc *BuildSessionVolumeProfile) Execute(_ context.Context, req BuildSessionVolumeProfileRequest) result.Result[BuildSessionVolumeProfileResponse] {
	if p := validation.Collect(
		validation.NonEmptyString("venue", req.Venue),
		validation.NonEmptyString("instrument", req.Instrument),
		validation.Positive("tick_size", req.TickSize),
		validation.Positive("high", req.High),
		validation.PositiveInt("ts_ingest", req.TsIngest),
		validation.PositiveInt("seq_last", req.SeqLast),
	); p != nil {
		return result.FailProblem[BuildSessionVolumeProfileResponse](p)
	}
	if p := req.Anchor.Validate(); p != nil {
		return result.FailProblem[BuildSessionVolumeProfileResponse](p)
	}

	venue := naming.CanonicalVenue(req.Venue)
	instrument := naming.CanonicalInstrument(req.Instrument)

	sessionStart, sessionEnd, p := domain.ResolveSessionBounds(req.Anchor, req.TsIngest)
	if p != nil {
		return result.FailProblem[BuildSessionVolumeProfileResponse](p)
	}

	key := venue + "|" + instrument + "|" + req.Anchor.Label
	ps := uc.states[key]

	// Session rollover: if candle is outside current session, close old and start new.
	if ps != nil && (sessionStart != ps.windowStart) {
		delete(uc.states, key)
		ps = nil
	}
	if ps == nil {
		ps = &svpPartitionState{
			buckets:     make(map[string]*vpvrBucketState),
			windowStart: sessionStart,
			windowEnd:   sessionEnd,
			seqFirst:    req.SeqFirst,
		}
		uc.states[key] = ps
	}

	// Apply candle price range: distribute volume across buckets from Low to High.
	uc.applyCandleToBuckets(ps, req)
	ps.eventCount++
	ps.dirty = true

	// Emit on cadence or session boundary.
	if ps.eventCount%uc.cfg.EmitCadence != 0 {
		return result.Ok(BuildSessionVolumeProfileResponse{Emitted: false})
	}

	snap, p := uc.buildSnapshot(venue, instrument, req.Anchor, ps)
	if p != nil {
		return result.FailProblem[BuildSessionVolumeProfileResponse](p)
	}
	ps.dirty = false

	idemKey := hash.NewFieldHasher().
		String(venue).
		String(instrument).
		String(req.Anchor.Label).
		Int64(ps.windowStart).
		Int64(ps.seqLast).
		Int(domain.SessionVolumeProfileVersion).
		Hex()

	return result.Ok(BuildSessionVolumeProfileResponse{
		Emitted:        true,
		Snapshot:       snap,
		IdempotencyKey: idemKey,
	})
}

// Snapshot returns the current in-memory SVP for a partition key.
func (uc *BuildSessionVolumeProfile) Snapshot(venue, instrument, anchorLabel string) (domain.SessionVolumeProfileV1, *problem.Problem) {
	v := naming.CanonicalVenue(venue)
	i := naming.CanonicalInstrument(instrument)
	key := v + "|" + i + "|" + anchorLabel
	ps, ok := uc.states[key]
	if !ok || len(ps.buckets) == 0 {
		return domain.SessionVolumeProfileV1{}, problem.Newf(problem.NotFound, "session vp not found for %s/%s/%s", venue, instrument, anchorLabel)
	}
	anchor, aOk := domain.SessionPresets[anchorLabel]
	if !aOk {
		anchor = domain.SessionAnchor{Kind: domain.SessionAnchorCustom, Label: anchorLabel, Timezone: "UTC", DurationMs: ps.windowEnd - ps.windowStart}
	}
	return uc.buildSnapshot(v, i, anchor, ps)
}

func (uc *BuildSessionVolumeProfile) applyCandleToBuckets(ps *svpPartitionState, req BuildSessionVolumeProfileRequest) {
	// Distribute the candle's volume across all price levels between Low and High.
	// Use the midpoint (HLCC/4 typical price) for single-bucket assignment
	// when the range is within one bin, otherwise spread across range.
	typicalPrice := (req.High + req.Low + req.Close + req.Close) / 4
	low, high, p := domain.AssignVPVRBucket(typicalPrice, req.TickSize)
	if p != nil {
		return
	}

	bKey := vpvrBucketKey(low, high)
	b := ps.buckets[bKey]
	if b == nil {
		if len(ps.buckets) >= uc.cfg.MaxBuckets {
			return // cap reached
		}
		b = &vpvrBucketState{
			low:    low,
			high:   high,
			seqMin: req.SeqFirst,
			seqMax: req.SeqLast,
		}
		ps.buckets[bKey] = b
	}
	b.buy += req.BuyVolume
	b.sell += req.SellVolume
	b.total = b.buy + b.sell
	if req.SeqFirst > 0 && (b.seqMin == 0 || req.SeqFirst < b.seqMin) {
		b.seqMin = req.SeqFirst
	}
	if req.SeqLast > b.seqMax {
		b.seqMax = req.SeqLast
	}

	ps.totalBuy += req.BuyVolume
	ps.totalSell += req.SellVolume
	ps.tradeCount += req.TradeCount
	if req.SeqFirst > 0 && (ps.seqFirst == 0 || req.SeqFirst < ps.seqFirst) {
		ps.seqFirst = req.SeqFirst
	}
	if req.SeqLast > ps.seqLast {
		ps.seqLast = req.SeqLast
	}
}

func (uc *BuildSessionVolumeProfile) buildSnapshot(venue, instrument string, anchor domain.SessionAnchor, ps *svpPartitionState) (domain.SessionVolumeProfileV1, *problem.Problem) {
	buckets := make([]domain.VolumeProfileBucketV1, 0, len(ps.buckets))
	for _, b := range ps.buckets {
		buckets = append(buckets, domain.VolumeProfileBucketV1{
			PriceLow:    b.low,
			PriceHigh:   b.high,
			BuyVolume:   b.buy,
			SellVolume:  b.sell,
			TotalVolume: b.total,
			SeqMin:      b.seqMin,
			SeqMax:      b.seqMax,
		})
	}
	buckets = capVPVRLevelsByVolume(buckets, uc.cfg.MaxBuckets)
	if len(buckets) == 0 {
		return domain.SessionVolumeProfileV1{}, problem.New(problem.ValidationFailed, "session vp has no buckets")
	}
	slices.SortFunc(buckets, func(a, b domain.VolumeProfileBucketV1) int {
		if a.PriceLow < b.PriceLow {
			return -1
		}
		if a.PriceLow > b.PriceLow {
			return 1
		}
		return 0
	})

	pocPrice, pocIdx := domain.ComputePOC(buckets)
	vah, val := domain.ComputeValueArea(buckets, pocIdx, domain.SVPValueAreaPct)

	snap := domain.SessionVolumeProfileV1{
		Venue:         venue,
		Instrument:    instrument,
		Anchor:        anchor,
		Buckets:       buckets,
		POCPrice:      pocPrice,
		ValueAreaHigh: vah,
		ValueAreaLow:  val,
		TotalVolume:   ps.totalBuy + ps.totalSell,
		BuyVolume:     ps.totalBuy,
		SellVolume:    ps.totalSell,
		TradeCount:    ps.tradeCount,
		WindowStartTs: ps.windowStart,
		WindowEndTs:   ps.windowEnd,
		SeqFirst:      ps.seqFirst,
		SeqLast:       ps.seqLast,
	}
	if p := snap.Validate(); p != nil {
		return domain.SessionVolumeProfileV1{}, p
	}
	return snap, nil
}
