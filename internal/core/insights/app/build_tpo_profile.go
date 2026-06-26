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

const tpoDefaultEmitCadence = 5

type BuildTPOProfileConfig struct {
	MaxLevels   int
	EmitCadence int
}

type BuildTPOProfileRequest struct {
	Venue      string
	Instrument string
	Anchor     domain.SessionAnchor
	TickSize   float64
	High       float64
	Low        float64
	TsIngest   int64 // candle window start ms
	SeqLast    int64
}

type BuildTPOProfileResponse struct {
	Emitted        bool
	Snapshot       domain.TPOProfileV1
	IdempotencyKey string
}

type BuildTPOProfile struct {
	cfg    BuildTPOProfileConfig
	states map[string]*tpoPartitionState
}

type tpoPartitionState struct {
	periods     [domain.TPOMaxPeriods]*tpoPeriodState
	periodCount int
	levels      map[string]*tpoLevelState
	windowStart int64
	windowEnd   int64
	rangeHigh   float64
	rangeLow    float64
	eventCount  int
}

type tpoPeriodState struct {
	letter byte
	high   float64
	low    float64
}

type tpoLevelState struct {
	priceLow  float64
	priceHigh float64
	letters   map[byte]struct{}
}

func NewBuildTPOProfile() *BuildTPOProfile {
	return NewBuildTPOProfileWithConfig(BuildTPOProfileConfig{})
}

func NewBuildTPOProfileWithConfig(cfg BuildTPOProfileConfig) *BuildTPOProfile {
	if cfg.MaxLevels <= 0 {
		cfg.MaxLevels = domain.TPOMaxLevels
	}
	if cfg.EmitCadence <= 0 {
		cfg.EmitCadence = tpoDefaultEmitCadence
	}
	return &BuildTPOProfile{
		cfg:    cfg,
		states: make(map[string]*tpoPartitionState),
	}
}

// Snapshot returns the current in-memory TPO profile for a partition key.
func (uc *BuildTPOProfile) Snapshot(venue, instrument, anchorLabel string) (domain.TPOProfileV1, *problem.Problem) {
	v := naming.CanonicalVenue(venue)
	i := naming.CanonicalInstrument(instrument)
	key := v + "|" + i + "|" + anchorLabel
	ps, ok := uc.states[key]
	if !ok || len(ps.levels) == 0 {
		return domain.TPOProfileV1{}, problem.Newf(problem.NotFound, "tpo profile not found for %s/%s/%s", venue, instrument, anchorLabel)
	}
	anchor, aOk := domain.SessionPresets[anchorLabel]
	if !aOk {
		anchor = domain.SessionAnchor{Kind: domain.SessionAnchorCustom, Label: anchorLabel, Timezone: "UTC", DurationMs: ps.windowEnd - ps.windowStart}
	}
	return uc.buildSnapshot(v, i, anchor, ps)
}

func (uc *BuildTPOProfile) Execute(_ context.Context, req BuildTPOProfileRequest) result.Result[BuildTPOProfileResponse] {
	if p := validation.Collect(
		validation.NonEmptyString("venue", req.Venue),
		validation.NonEmptyString("instrument", req.Instrument),
		validation.Positive("tick_size", req.TickSize),
		validation.Positive("high", req.High),
		validation.Positive("low", req.Low),
		validation.PositiveInt("ts_ingest", req.TsIngest),
		validation.PositiveInt("seq_last", req.SeqLast),
	); p != nil {
		return result.FailProblem[BuildTPOProfileResponse](p)
	}
	if p := req.Anchor.Validate(); p != nil {
		return result.FailProblem[BuildTPOProfileResponse](p)
	}

	venue := naming.CanonicalVenue(req.Venue)
	instrument := naming.CanonicalInstrument(req.Instrument)

	sessionStart, sessionEnd, p := domain.ResolveSessionBounds(req.Anchor, req.TsIngest)
	if p != nil {
		return result.FailProblem[BuildTPOProfileResponse](p)
	}

	key := venue + "|" + instrument + "|" + req.Anchor.Label
	ps := uc.states[key]

	if ps != nil && sessionStart != ps.windowStart {
		delete(uc.states, key)
		ps = nil
	}
	if ps == nil {
		ps = &tpoPartitionState{
			levels:      make(map[string]*tpoLevelState),
			windowStart: sessionStart,
			windowEnd:   sessionEnd,
			rangeHigh:   req.High,
			rangeLow:    req.Low,
		}
		uc.states[key] = ps
	}

	uc.applyCandle(ps, req)
	ps.eventCount++

	if ps.eventCount%uc.cfg.EmitCadence != 0 {
		return result.Ok(BuildTPOProfileResponse{Emitted: false})
	}

	snap, p := uc.buildSnapshot(venue, instrument, req.Anchor, ps)
	if p != nil {
		return result.FailProblem[BuildTPOProfileResponse](p)
	}

	idemKey := hash.NewFieldHasher().
		String(venue).
		String(instrument).
		String(req.Anchor.Label).
		Int64(ps.windowStart).
		Int64(req.SeqLast).
		Int(domain.TPOProfileVersion).
		Hex()

	return result.Ok(BuildTPOProfileResponse{
		Emitted:        true,
		Snapshot:       snap,
		IdempotencyKey: idemKey,
	})
}

func (uc *BuildTPOProfile) applyCandle(ps *tpoPartitionState, req BuildTPOProfileRequest) {
	periodIdx := domain.PeriodIndex(ps.windowStart, req.TsIngest)
	letter := domain.PeriodLetter(periodIdx)

	// Ensure period exists.
	if ps.periods[periodIdx] == nil {
		ps.periods[periodIdx] = &tpoPeriodState{
			letter: letter,
			high:   req.High,
			low:    req.Low,
		}
		ps.periodCount++
	} else {
		pd := ps.periods[periodIdx]
		if req.High > pd.high {
			pd.high = req.High
		}
		if req.Low < pd.low {
			pd.low = req.Low
		}
	}

	// Update range.
	if req.High > ps.rangeHigh {
		ps.rangeHigh = req.High
	}
	if req.Low < ps.rangeLow {
		ps.rangeLow = req.Low
	}

	// Mark all price levels between Low and High with this period's letter.
	low, high := req.Low, req.High
	step := domain.CalculateVolumeBinSize(low, req.TickSize)
	if step <= 0 {
		return
	}

	for price := low; price < high; price += step {
		bucketLow, bucketHigh, bp := domain.AssignVPVRBucket(price, req.TickSize)
		if bp != nil {
			continue
		}
		bKey := vpvrBucketKey(bucketLow, bucketHigh)
		lv := ps.levels[bKey]
		if lv == nil {
			if len(ps.levels) >= uc.cfg.MaxLevels {
				break
			}
			lv = &tpoLevelState{
				priceLow:  bucketLow,
				priceHigh: bucketHigh,
				letters:   make(map[byte]struct{}),
			}
			ps.levels[bKey] = lv
		}
		lv.letters[letter] = struct{}{}
	}
}

func (uc *BuildTPOProfile) buildSnapshot(venue, instrument string, anchor domain.SessionAnchor, ps *tpoPartitionState) (domain.TPOProfileV1, *problem.Problem) {
	periods := collectTPOPeriods(ps)
	levels := collectTPOLevels(ps)

	if len(levels) == 0 || len(periods) == 0 {
		return domain.TPOProfileV1{}, problem.New(problem.ValidationFailed, "tpo profile has no data")
	}

	pocPrice, pocIdx := tpoPOC(levels)
	vah, val := tpoValueArea(levels, pocIdx)
	ibHigh, ibLow := tpoInitialBalance(ps)

	snap := domain.TPOProfileV1{
		Venue:         venue,
		Instrument:    instrument,
		Anchor:        anchor,
		Periods:       periods,
		Levels:        levels,
		POCPrice:      pocPrice,
		ValueAreaHigh: vah,
		ValueAreaLow:  val,
		IBHigh:        ibHigh,
		IBLow:         ibLow,
		RangeHigh:     ps.rangeHigh,
		RangeLow:      ps.rangeLow,
		WindowStartTs: ps.windowStart,
		WindowEndTs:   ps.windowEnd,
	}
	if p := snap.Validate(); p != nil {
		return domain.TPOProfileV1{}, p
	}
	return snap, nil
}

func collectTPOPeriods(ps *tpoPartitionState) []domain.TPOPeriod {
	periods := make([]domain.TPOPeriod, 0, ps.periodCount)
	for i := 0; i < domain.TPOMaxPeriods; i++ {
		pd := ps.periods[i]
		if pd == nil {
			continue
		}
		startMs := ps.windowStart + int64(i)*int64(domain.TPOPeriodDuration)
		periods = append(periods, domain.TPOPeriod{
			Letter:    pd.letter,
			StartMs:   startMs,
			EndMs:     startMs + int64(domain.TPOPeriodDuration),
			HighPrice: pd.high,
			LowPrice:  pd.low,
		})
	}
	return periods
}

func collectTPOLevels(ps *tpoPartitionState) []domain.TPOLevel {
	levels := make([]domain.TPOLevel, 0, len(ps.levels))
	for _, lv := range ps.levels {
		letters := make([]byte, 0, len(lv.letters))
		for l := range lv.letters {
			letters = append(letters, l)
		}
		slices.Sort(letters)
		levels = append(levels, domain.TPOLevel{
			PriceLow:  lv.priceLow,
			PriceHigh: lv.priceHigh,
			Letters:   letters,
			Count:     len(letters),
		})
	}
	slices.SortFunc(levels, func(a, b domain.TPOLevel) int {
		if a.PriceLow < b.PriceLow {
			return -1
		}
		if a.PriceLow > b.PriceLow {
			return 1
		}
		return 0
	})
	return levels
}

func tpoPOC(levels []domain.TPOLevel) (float64, int) {
	pocPrice := levels[0].PriceLow
	maxCount := levels[0].Count
	pocIdx := 0
	for i, lv := range levels {
		if lv.Count > maxCount {
			maxCount = lv.Count
			pocPrice = lv.PriceLow
			pocIdx = i
		}
	}
	return pocPrice, pocIdx
}

func tpoValueArea(levels []domain.TPOLevel, pocIdx int) (float64, float64) {
	buckets := make([]domain.VolumeProfileBucketV1, len(levels))
	for i, lv := range levels {
		buckets[i] = domain.VolumeProfileBucketV1{
			PriceLow:    lv.PriceLow,
			PriceHigh:   lv.PriceHigh,
			TotalVolume: float64(lv.Count),
		}
	}
	return domain.ComputeValueArea(buckets, pocIdx, domain.SVPValueAreaPct)
}

func tpoInitialBalance(ps *tpoPartitionState) (float64, float64) {
	ibHigh := 0.0
	ibLow := 0.0
	if ps.periods[0] != nil {
		ibHigh = ps.periods[0].high
		ibLow = ps.periods[0].low
	}
	if ps.periods[1] != nil {
		if ps.periods[1].high > ibHigh {
			ibHigh = ps.periods[1].high
		}
		if ibLow == 0 || ps.periods[1].low < ibLow {
			ibLow = ps.periods[1].low
		}
	}
	return ibHigh, ibLow
}
