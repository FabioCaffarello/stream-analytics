package domain

import (
	"math"
	"slices"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

const (
	HeatmapSnapshotType    = "insights.heatmap_snapshot"
	HeatmapSnapshotVersion = 1
	HeatmapDeltaType       = "insights.heatmap_delta"
	HeatmapDeltaVersion    = 1
)

// HeatmapCellV1 is one bounded price/size bucket inside a heatmap window.
type HeatmapCellV1 struct {
	PriceBucketLow  float64 `json:"price_bucket_low"`
	PriceBucketHigh float64 `json:"price_bucket_high"`
	SizeBucket      string  `json:"size_bucket"`
	BidLiquidity    float64 `json:"bid_liquidity"`
	AskLiquidity    float64 `json:"ask_liquidity"`
	TradeVolume     float64 `json:"trade_volume"`
	SeqMin          int64   `json:"seq_min"`
	SeqMax          int64   `json:"seq_max"`
	Samples         int64   `json:"samples"`
}

// HeatmapArtifactV1 is the deterministic payload emitted by the heatmap builder.
type HeatmapArtifactV1 struct {
	Venue         string          `json:"venue"`
	Instrument    string          `json:"instrument"`
	Timeframe     string          `json:"timeframe"`
	WindowStartTs int64           `json:"window_start_ts"`
	WindowEndTs   int64           `json:"window_end_ts"`
	Cells         []HeatmapCellV1 `json:"cells"`
}

func (a HeatmapArtifactV1) Validate() *problem.Problem {
	if p := validateHeatmapArtifactMetadata(a); p != nil {
		return p
	}
	type key struct {
		low  float64
		high float64
		size string
	}
	seen := make(map[key]struct{}, len(a.Cells))
	for _, c := range a.Cells {
		if p := validateHeatmapCell(c); p != nil {
			return p
		}
		k := key{low: c.PriceBucketLow, high: c.PriceBucketHigh, size: strings.ToUpper(strings.TrimSpace(c.SizeBucket))}
		if _, ok := seen[k]; ok {
			return problem.New(problem.ValidationFailed, "heatmap cells must be unique by price bucket + size bucket")
		}
		seen[k] = struct{}{}
	}
	if !slices.IsSortedFunc(a.Cells, compareCellOrder) {
		return problem.New(problem.ValidationFailed, "heatmap cells must be sorted by price bucket then size bucket")
	}
	return nil
}

func validateHeatmapArtifactMetadata(a HeatmapArtifactV1) *problem.Problem {
	if strings.TrimSpace(a.Venue) == "" {
		return problem.New(problem.ValidationFailed, "heatmap venue must not be empty")
	}
	if strings.TrimSpace(a.Instrument) == "" {
		return problem.New(problem.ValidationFailed, "heatmap instrument must not be empty")
	}
	if strings.TrimSpace(a.Timeframe) == "" {
		return problem.New(problem.ValidationFailed, "heatmap timeframe must not be empty")
	}
	if a.WindowStartTs <= 0 || a.WindowEndTs <= a.WindowStartTs {
		return problem.New(problem.ValidationFailed, "heatmap window bounds are invalid")
	}
	if len(a.Cells) == 0 {
		return problem.New(problem.ValidationFailed, "heatmap artifact requires at least one cell")
	}
	return nil
}

func validateHeatmapCell(c HeatmapCellV1) *problem.Problem {
	if strings.TrimSpace(c.SizeBucket) == "" {
		return problem.New(problem.ValidationFailed, "heatmap size_bucket must not be empty")
	}
	if !isFinite(c.PriceBucketLow) || !isFinite(c.PriceBucketHigh) {
		return problem.New(problem.ValidationFailed, "heatmap price buckets must be finite")
	}
	if c.PriceBucketHigh < c.PriceBucketLow {
		return problem.New(problem.ValidationFailed, "heatmap price_bucket_high must be >= price_bucket_low")
	}
	if c.BidLiquidity < 0 || c.AskLiquidity < 0 || c.TradeVolume < 0 {
		return problem.New(problem.ValidationFailed, "heatmap cell volumes must be >= 0")
	}
	if c.SeqMin <= 0 || c.SeqMax < c.SeqMin {
		return problem.New(problem.ValidationFailed, "heatmap seq bounds are invalid")
	}
	if c.Samples <= 0 {
		return problem.New(problem.ValidationFailed, "heatmap samples must be > 0")
	}
	return nil
}

func compareCellOrder(a, b HeatmapCellV1) int {
	if a.PriceBucketLow < b.PriceBucketLow {
		return -1
	}
	if a.PriceBucketLow > b.PriceBucketLow {
		return 1
	}
	if a.PriceBucketHigh < b.PriceBucketHigh {
		return -1
	}
	if a.PriceBucketHigh > b.PriceBucketHigh {
		return 1
	}
	as := strings.ToUpper(strings.TrimSpace(a.SizeBucket))
	bs := strings.ToUpper(strings.TrimSpace(b.SizeBucket))
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
