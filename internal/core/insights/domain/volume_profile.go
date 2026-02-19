package domain

import (
	"math"
	"slices"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	VolumeProfileSnapshotType    = "insights.volume_profile_snapshot"
	VolumeProfileSnapshotVersion = 1
	VolumeProfileDeltaType       = "insights.volume_profile_delta"
	VolumeProfileDeltaVersion    = 1

	VPVRCapBucketsPerWindow  = 400
	VPVRCapLevelsPerPayload  = 400
	VPVRCapOpenWindowsPerKey = 96
	VPVRCapWSRangeWindows    = 200
)

var VPVRTimeframes = map[string]struct{}{
	"1m": {},
	"5m": {},
	"1h": {},
	"4h": {},
	"1d": {},
}

type VolumeProfileBucketV1 struct {
	PriceLow    float64 `json:"price_low"`
	PriceHigh   float64 `json:"price_high"`
	BuyVolume   float64 `json:"buy_volume"`
	SellVolume  float64 `json:"sell_volume"`
	TotalVolume float64 `json:"total_volume"`
	SeqMin      int64   `json:"seq_min"`
	SeqMax      int64   `json:"seq_max"`
}

type VolumeProfileSnapshotV1 struct {
	Venue         string                  `json:"venue"`
	Instrument    string                  `json:"instrument"`
	Timeframe     string                  `json:"timeframe"`
	WindowStartTs int64                   `json:"window_start_ts"`
	WindowEndTs   int64                   `json:"window_end_ts"`
	Buckets       []VolumeProfileBucketV1 `json:"buckets"`
	POCPrice      float64                 `json:"poc_price"`
	ValueAreaLow  float64                 `json:"value_area_low"`
	ValueAreaHigh float64                 `json:"value_area_high"`
}

func AssignVPVRBucket(price, tickSize float64) (float64, float64, *problem.Problem) {
	if !isFiniteFloat(price) || !isFiniteFloat(tickSize) || tickSize <= 0 {
		return 0, 0, problem.New(problem.ValidationFailed, "vpvr bucket assignment requires finite positive tick_size and price")
	}
	step := tickSize
	ticks := int64(math.Floor(price / step))
	low := float64(ticks) * step
	high := low + step
	return low, high, nil
}

func (s VolumeProfileSnapshotV1) Validate() *problem.Problem {
	if p := validateVPVRSnapshotMetadata(s); p != nil {
		return p
	}
	expectedPOC, p := validateVPVRSnapshotBuckets(s.Buckets)
	if p != nil {
		return p
	}
	if p := validateVPVRSnapshotDerivedFields(s, expectedPOC); p != nil {
		return p
	}
	return nil
}

func validateVPVRSnapshotMetadata(s VolumeProfileSnapshotV1) *problem.Problem {
	if strings.TrimSpace(s.Venue) == "" || strings.TrimSpace(s.Instrument) == "" {
		return problem.New(problem.ValidationFailed, "vpvr venue/instrument must not be empty")
	}
	if _, ok := VPVRTimeframes[strings.ToLower(strings.TrimSpace(s.Timeframe))]; !ok {
		return problem.New(problem.ValidationFailed, "vpvr timeframe is unsupported")
	}
	if s.WindowStartTs <= 0 || s.WindowEndTs <= s.WindowStartTs {
		return problem.New(problem.ValidationFailed, "vpvr window bounds are invalid")
	}
	if len(s.Buckets) == 0 {
		return problem.New(problem.ValidationFailed, "vpvr snapshot requires at least one bucket")
	}
	if len(s.Buckets) > VPVRCapBucketsPerWindow {
		return problem.New(problem.ValidationFailed, "vpvr bucket cardinality cap exceeded")
	}
	return nil
}

func validateVPVRSnapshotBuckets(buckets []VolumeProfileBucketV1) (float64, *problem.Problem) {
	if !slices.IsSortedFunc(buckets, compareVPVRBucketOrder) {
		return 0, problem.New(problem.ValidationFailed, "vpvr buckets must be sorted by price range")
	}
	seen := make(map[[2]float64]struct{}, len(buckets))
	maxTotal := -1.0
	expectedPOC := 0.0
	for _, b := range buckets {
		if p := validateVPVRBucket(b); p != nil {
			return 0, p
		}
		key := [2]float64{b.PriceLow, b.PriceHigh}
		if _, ok := seen[key]; ok {
			return 0, problem.New(problem.ValidationFailed, "vpvr buckets must be unique by price range")
		}
		seen[key] = struct{}{}
		if b.TotalVolume > maxTotal {
			maxTotal = b.TotalVolume
			expectedPOC = b.PriceLow
		}
	}
	return expectedPOC, nil
}

func validateVPVRSnapshotDerivedFields(s VolumeProfileSnapshotV1, expectedPOC float64) *problem.Problem {
	if !isFiniteFloat(s.POCPrice) || s.POCPrice != expectedPOC {
		return problem.New(problem.ValidationFailed, "vpvr poc_price must match highest-volume bucket")
	}
	if !isFiniteFloat(s.ValueAreaLow) || !isFiniteFloat(s.ValueAreaHigh) || s.ValueAreaHigh < s.ValueAreaLow {
		return problem.New(problem.ValidationFailed, "vpvr value area bounds are invalid")
	}
	return nil
}

func compareVPVRBucketOrder(a, b VolumeProfileBucketV1) int {
	if a.PriceLow < b.PriceLow {
		return -1
	}
	if a.PriceLow > b.PriceLow {
		return 1
	}
	if a.PriceHigh < b.PriceHigh {
		return -1
	}
	if a.PriceHigh > b.PriceHigh {
		return 1
	}
	return 0
}

func validateVPVRBucket(b VolumeProfileBucketV1) *problem.Problem {
	if !isFiniteFloat(b.PriceLow) || !isFiniteFloat(b.PriceHigh) {
		return problem.New(problem.ValidationFailed, "vpvr bucket prices must be finite")
	}
	if b.PriceHigh <= b.PriceLow {
		return problem.New(problem.ValidationFailed, "vpvr bucket price bounds are invalid")
	}
	if b.BuyVolume < 0 || b.SellVolume < 0 || b.TotalVolume < 0 {
		return problem.New(problem.ValidationFailed, "vpvr bucket volumes must be non-negative")
	}
	if b.TotalVolume != b.BuyVolume+b.SellVolume {
		return problem.New(problem.ValidationFailed, "vpvr total_volume must equal buy_volume + sell_volume")
	}
	if b.SeqMin <= 0 || b.SeqMax < b.SeqMin {
		return problem.New(problem.ValidationFailed, "vpvr bucket seq bounds are invalid")
	}
	return nil
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
