package domain

import (
	"slices"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	SessionVolumeProfileType    = "insights.session_volume_profile"
	SessionVolumeProfileVersion = 1

	SVPCapBuckets   = 400
	SVPValueAreaPct = 0.70
)

// SessionVolumeProfileV1 is a volume profile scoped to a session anchor.
// Reuses VolumeProfileBucketV1 for bucket representation.
type SessionVolumeProfileV1 struct {
	Venue         string                  `json:"venue"`
	Instrument    string                  `json:"instrument"`
	Anchor        SessionAnchor           `json:"anchor"`
	Buckets       []VolumeProfileBucketV1 `json:"buckets"`
	POCPrice      float64                 `json:"poc_price"`
	ValueAreaLow  float64                 `json:"value_area_low"`
	ValueAreaHigh float64                 `json:"value_area_high"`
	TotalVolume   float64                 `json:"total_volume"`
	BuyVolume     float64                 `json:"buy_volume"`
	SellVolume    float64                 `json:"sell_volume"`
	TradeCount    int64                   `json:"trade_count"`
	WindowStartTs int64                   `json:"window_start_ts"`
	WindowEndTs   int64                   `json:"window_end_ts"`
	SeqFirst      int64                   `json:"seq_first"`
	SeqLast       int64                   `json:"seq_last"`
}

func (s SessionVolumeProfileV1) Validate() *problem.Problem {
	if strings.TrimSpace(s.Venue) == "" || strings.TrimSpace(s.Instrument) == "" {
		return problem.New(problem.ValidationFailed, "session vp venue/instrument must not be empty")
	}
	if p := s.Anchor.Validate(); p != nil {
		return p
	}
	if s.WindowStartTs <= 0 || s.WindowEndTs <= s.WindowStartTs {
		return problem.New(problem.ValidationFailed, "session vp window bounds are invalid")
	}
	if len(s.Buckets) == 0 {
		return problem.New(problem.ValidationFailed, "session vp requires at least one bucket")
	}
	if len(s.Buckets) > SVPCapBuckets {
		return problem.New(problem.ValidationFailed, "session vp bucket cap exceeded")
	}
	if !slices.IsSortedFunc(s.Buckets, compareVPVRBucketOrder) {
		return problem.New(problem.ValidationFailed, "session vp buckets must be sorted by price")
	}
	expectedPOC, p := validateVPVRSnapshotBuckets(s.Buckets)
	if p != nil {
		return p
	}
	if !isFiniteFloat(s.POCPrice) || s.POCPrice != expectedPOC {
		return problem.New(problem.ValidationFailed, "session vp poc_price must match highest-volume bucket")
	}
	if !isFiniteFloat(s.ValueAreaLow) || !isFiniteFloat(s.ValueAreaHigh) || s.ValueAreaHigh < s.ValueAreaLow {
		return problem.New(problem.ValidationFailed, "session vp value area bounds are invalid")
	}
	return nil
}

// ComputePOC returns the PriceLow of the bucket with the highest TotalVolume.
func ComputePOC(buckets []VolumeProfileBucketV1) (float64, int) {
	if len(buckets) == 0 {
		return 0, -1
	}
	maxVol := -1.0
	pocIdx := 0
	for i, b := range buckets {
		if b.TotalVolume > maxVol {
			maxVol = b.TotalVolume
			pocIdx = i
		}
	}
	return buckets[pocIdx].PriceLow, pocIdx
}

// ComputeValueArea computes VAH and VAL by expanding outward from POC
// until pct of total volume is captured.
func ComputeValueArea(buckets []VolumeProfileBucketV1, pocIdx int, pct float64) (vah, val float64) {
	if len(buckets) == 0 {
		return 0, 0
	}
	totalVol := 0.0
	for _, b := range buckets {
		totalVol += b.TotalVolume
	}
	if totalVol <= 0 {
		return buckets[0].PriceLow, buckets[len(buckets)-1].PriceHigh
	}

	target := totalVol * pct
	lo := pocIdx
	hi := pocIdx
	accum := buckets[pocIdx].TotalVolume

	for accum < target && (lo > 0 || hi < len(buckets)-1) {
		volLo := 0.0
		volHi := 0.0
		if lo > 0 {
			volLo = buckets[lo-1].TotalVolume
		}
		if hi < len(buckets)-1 {
			volHi = buckets[hi+1].TotalVolume
		}
		if volLo >= volHi && lo > 0 {
			lo--
			accum += volLo
		} else if hi < len(buckets)-1 {
			hi++
			accum += volHi
		} else if lo > 0 {
			lo--
			accum += volLo
		} else {
			break
		}
	}
	return buckets[hi].PriceHigh, buckets[lo].PriceLow
}
