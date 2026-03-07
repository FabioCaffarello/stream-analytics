package domain

import (
	"slices"
	"strings"

	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	FusedVolumeProfileSnapshotType    = "insights.fused_volume_profile_snapshot"
	FusedVolumeProfileSnapshotVersion = 1
)

// FusionMode controls how multi-venue data is combined.
type FusionMode string

const (
	FusionSingleVenue FusionMode = "single"
	FusionWeighted    FusionMode = "weighted"
	FusionMerge       FusionMode = "merge"
)

// VenueContribution describes one venue's volume in a fused bucket.
type VenueContribution struct {
	Venue     string  `json:"venue"`
	Volume    float64 `json:"volume"`
	WeightPct float64 `json:"weight_pct"`
}

// FusionSourceEntry describes one venue's contribution to a fused result.
type FusionSourceEntry struct {
	Venue      string  `json:"venue"`
	WeightPct  float64 `json:"weight_pct"`
	LastSeenMs int64   `json:"last_seen_ms"`
	IsStale    bool    `json:"is_stale"`
}

// FusionStalenessReport summarizes freshness across sources.
type FusionStalenessReport struct {
	FreshCount int   `json:"fresh_count"`
	StaleCount int   `json:"stale_count"`
	OldestMs   int64 `json:"oldest_ms"`
	NewestMs   int64 `json:"newest_ms"`
}

// FusionMeta carries evidence metadata for any fused payload.
type FusionMeta struct {
	Reason      string                `json:"reason"`
	Confidence  float64               `json:"confidence"`
	SourceMix   []FusionSourceEntry   `json:"source_mix"`
	Staleness   FusionStalenessReport `json:"staleness"`
	FeatureTags []string              `json:"feature_tags"`
}

// FusedVolumeProfileBucketV1 is one price bucket in a fused volume profile.
type FusedVolumeProfileBucketV1 struct {
	PriceLow    float64             `json:"price_low"`
	PriceHigh   float64             `json:"price_high"`
	BuyVolume   float64             `json:"buy_volume"`
	SellVolume  float64             `json:"sell_volume"`
	TotalVolume float64             `json:"total_volume"`
	VenueMix    []VenueContribution `json:"venue_mix"`
}

// FusedVolumeProfileSnapshotV1 is a deterministic merged volume profile across venues.
type FusedVolumeProfileSnapshotV1 struct {
	Instrument    string                       `json:"instrument"`
	Timeframe     string                       `json:"timeframe"`
	WindowStartTs int64                        `json:"window_start_ts"`
	WindowEndTs   int64                        `json:"window_end_ts"`
	Mode          FusionMode                   `json:"mode"`
	Buckets       []FusedVolumeProfileBucketV1 `json:"buckets"`
	POCPrice      float64                      `json:"poc_price"`
	ValueAreaLow  float64                      `json:"value_area_low"`
	ValueAreaHigh float64                      `json:"value_area_high"`
	SourceVenues  []string                     `json:"source_venues"`
	Meta          FusionMeta                   `json:"meta"`
}

// Validate checks FusedVolumeProfileSnapshotV1 invariants.
func (s FusedVolumeProfileSnapshotV1) Validate() *problem.Problem {
	if strings.TrimSpace(s.Instrument) == "" {
		return problem.New(problem.ValidationFailed, "fused vpvr instrument must not be empty")
	}
	if _, ok := VPVRTimeframes[naming.NormalizeTimeframe(s.Timeframe)]; !ok {
		return problem.New(problem.ValidationFailed, "fused vpvr timeframe is unsupported")
	}
	if s.WindowStartTs <= 0 || s.WindowEndTs <= s.WindowStartTs {
		return problem.New(problem.ValidationFailed, "fused vpvr window bounds are invalid")
	}
	if len(s.Buckets) == 0 {
		return problem.New(problem.ValidationFailed, "fused vpvr requires at least one bucket")
	}
	if len(s.Buckets) > VPVRCapBucketsPerWindow {
		return problem.New(problem.ValidationFailed, "fused vpvr bucket cardinality cap exceeded")
	}
	if !slices.IsSorted(s.SourceVenues) {
		return problem.New(problem.ValidationFailed, "fused vpvr source_venues must be sorted")
	}
	return nil
}

// FuseVolumeProfiles merges volume profiles from multiple venues.
func FuseVolumeProfiles(
	instrument, timeframe string,
	profiles []VolumeProfileSnapshotV1,
	mode FusionMode,
) (FusedVolumeProfileSnapshotV1, *problem.Problem) {
	if strings.TrimSpace(instrument) == "" {
		return FusedVolumeProfileSnapshotV1{}, problem.New(problem.ValidationFailed, "instrument must not be empty")
	}
	if len(profiles) == 0 {
		return FusedVolumeProfileSnapshotV1{}, problem.New(problem.ValidationFailed, "at least one profile required")
	}

	buckets, sourceVenues, windowStart, windowEnd := accumulateProfileBuckets(profiles)
	pocPrice, vaLow, vaHigh := computeFusedPOCAndVA(buckets)
	sourceMix := buildEqualSourceMix(sourceVenues)

	return FusedVolumeProfileSnapshotV1{
		Instrument:    strings.TrimSpace(instrument),
		Timeframe:     timeframe,
		WindowStartTs: windowStart,
		WindowEndTs:   windowEnd,
		Mode:          mode,
		Buckets:       buckets,
		POCPrice:      pocPrice,
		ValueAreaLow:  vaLow,
		ValueAreaHigh: vaHigh,
		SourceVenues:  sourceVenues,
		Meta: FusionMeta{
			Reason:     fusionReasonStr(mode),
			Confidence: 1.0,
			SourceMix:  sourceMix,
			Staleness:  FusionStalenessReport{FreshCount: len(sourceVenues)},
		},
	}, nil
}

func accumulateProfileBuckets(profiles []VolumeProfileSnapshotV1) ([]FusedVolumeProfileBucketV1, []string, int64, int64) {
	type bucketKey struct{ low, high float64 }
	type bucketAcc struct {
		buy, sell float64
		venueMix  []VenueContribution
	}
	index := make(map[bucketKey]*bucketAcc)
	var keys []bucketKey
	var windowStart, windowEnd int64
	sourceVenues := make([]string, 0, len(profiles))

	for _, p := range profiles {
		sourceVenues = append(sourceVenues, p.Venue)
		windowStart, windowEnd = expandWindow(windowStart, windowEnd, p.WindowStartTs, p.WindowEndTs)
		for _, b := range p.Buckets {
			k := bucketKey{low: b.PriceLow, high: b.PriceHigh}
			if acc, ok := index[k]; ok {
				acc.buy += b.BuyVolume
				acc.sell += b.SellVolume
				acc.venueMix = append(acc.venueMix, VenueContribution{Venue: p.Venue, Volume: b.TotalVolume})
			} else {
				index[k] = &bucketAcc{
					buy: b.BuyVolume, sell: b.SellVolume,
					venueMix: []VenueContribution{{Venue: p.Venue, Volume: b.TotalVolume}},
				}
				keys = append(keys, k)
			}
		}
	}

	slices.Sort(sourceVenues)

	buckets := make([]FusedVolumeProfileBucketV1, 0, len(keys))
	for _, k := range keys {
		acc := index[k]
		total := acc.buy + acc.sell
		assignVenueMixWeights(acc.venueMix, total)
		buckets = append(buckets, FusedVolumeProfileBucketV1{
			PriceLow: k.low, PriceHigh: k.high,
			BuyVolume: acc.buy, SellVolume: acc.sell, TotalVolume: total,
			VenueMix: acc.venueMix,
		})
	}
	slices.SortFunc(buckets, sortByPriceLow)
	if len(buckets) > VPVRCapBucketsPerWindow {
		buckets = buckets[:VPVRCapBucketsPerWindow]
	}
	return buckets, sourceVenues, windowStart, windowEnd
}

func expandWindow(curStart, curEnd, newStart, newEnd int64) (int64, int64) {
	if curStart == 0 || newStart < curStart {
		curStart = newStart
	}
	if newEnd > curEnd {
		curEnd = newEnd
	}
	return curStart, curEnd
}

func assignVenueMixWeights(mix []VenueContribution, total float64) {
	for i := range mix {
		if total > 0 {
			mix[i].WeightPct = (mix[i].Volume / total) * 100
		}
	}
	slices.SortFunc(mix, func(a, b VenueContribution) int {
		return strings.Compare(a.Venue, b.Venue)
	})
}

func sortByPriceLow(a, b FusedVolumeProfileBucketV1) int {
	if a.PriceLow < b.PriceLow {
		return -1
	}
	if a.PriceLow > b.PriceLow {
		return 1
	}
	return 0
}

func computeFusedPOCAndVA(buckets []FusedVolumeProfileBucketV1) (float64, float64, float64) {
	pocPrice := 0.0
	maxVol := -1.0
	for _, b := range buckets {
		if b.TotalVolume > maxVol {
			maxVol = b.TotalVolume
			pocPrice = b.PriceLow
		}
	}
	if len(buckets) > 0 {
		vaLow, vaHigh := computeFusedValueArea(buckets, pocPrice)
		return pocPrice, vaLow, vaHigh
	}
	return pocPrice, 0, 0
}

func buildEqualSourceMix(sourceVenues []string) []FusionSourceEntry {
	sourceMix := make([]FusionSourceEntry, len(sourceVenues))
	w := 100.0 / float64(len(sourceVenues))
	for i, v := range sourceVenues {
		sourceMix[i] = FusionSourceEntry{Venue: v, WeightPct: w}
	}
	return sourceMix
}

func computeFusedValueArea(buckets []FusedVolumeProfileBucketV1, pocPrice float64) (float64, float64) {
	var totalVol float64
	for _, b := range buckets {
		totalVol += b.TotalVolume
	}
	if totalVol <= 0 {
		return pocPrice, pocPrice
	}

	type ranked struct {
		low, high, vol float64
	}
	sorted := make([]ranked, len(buckets))
	for i, b := range buckets {
		sorted[i] = ranked{low: b.PriceLow, high: b.PriceHigh, vol: b.TotalVolume}
	}
	slices.SortFunc(sorted, func(a, b ranked) int {
		if a.vol > b.vol {
			return -1
		}
		if a.vol < b.vol {
			return 1
		}
		return 0
	})

	target := totalVol * 0.70
	var accum float64
	vaLow := sorted[0].low
	vaHigh := sorted[0].high
	for _, r := range sorted {
		accum += r.vol
		if r.low < vaLow {
			vaLow = r.low
		}
		if r.high > vaHigh {
			vaHigh = r.high
		}
		if accum >= target {
			break
		}
	}
	return vaLow, vaHigh
}

func fusionReasonStr(mode FusionMode) string {
	switch mode {
	case FusionMerge:
		return "cross_venue_merge"
	case FusionWeighted:
		return "weighted_fusion"
	default:
		return "single_venue"
	}
}
