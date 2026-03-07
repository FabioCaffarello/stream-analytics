package domain

import (
	"slices"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	FusedHeatmapSnapshotType    = "insights.fused_heatmap_snapshot"
	FusedHeatmapSnapshotVersion = 1
	FusedHeatmapMaxCells        = 1000
)

// FusedHeatmapCellV1 is one cell in a fused heatmap.
type FusedHeatmapCellV1 struct {
	PriceBucketLow  float64             `json:"price_bucket_low"`
	PriceBucketHigh float64             `json:"price_bucket_high"`
	SizeBucket      string              `json:"size_bucket"`
	BidLiquidity    float64             `json:"bid_liquidity"`
	AskLiquidity    float64             `json:"ask_liquidity"`
	TradeVolume     float64             `json:"trade_volume"`
	Samples         int64               `json:"samples"`
	VenueMix        []VenueContribution `json:"venue_mix"`
}

// FusedHeatmapArtifactV1 is a deterministic merged heatmap across venues.
type FusedHeatmapArtifactV1 struct {
	Instrument    string               `json:"instrument"`
	Timeframe     string               `json:"timeframe"`
	WindowStartTs int64                `json:"window_start_ts"`
	WindowEndTs   int64                `json:"window_end_ts"`
	Mode          FusionMode           `json:"mode"`
	Cells         []FusedHeatmapCellV1 `json:"cells"`
	SourceVenues  []string             `json:"source_venues"`
	Meta          FusionMeta           `json:"meta"`
}

// Validate checks FusedHeatmapArtifactV1 invariants.
func (a FusedHeatmapArtifactV1) Validate() *problem.Problem {
	if strings.TrimSpace(a.Instrument) == "" {
		return problem.New(problem.ValidationFailed, "fused heatmap instrument must not be empty")
	}
	if strings.TrimSpace(a.Timeframe) == "" {
		return problem.New(problem.ValidationFailed, "fused heatmap timeframe must not be empty")
	}
	if a.WindowStartTs <= 0 || a.WindowEndTs <= a.WindowStartTs {
		return problem.New(problem.ValidationFailed, "fused heatmap window bounds are invalid")
	}
	if len(a.Cells) == 0 {
		return problem.New(problem.ValidationFailed, "fused heatmap requires at least one cell")
	}
	if len(a.Cells) > FusedHeatmapMaxCells {
		return problem.New(problem.ValidationFailed, "fused heatmap cell cardinality cap exceeded")
	}
	if !slices.IsSorted(a.SourceVenues) {
		return problem.New(problem.ValidationFailed, "fused heatmap source_venues must be sorted")
	}
	return nil
}

// FuseHeatmaps merges heatmap artifacts from multiple venues.
func FuseHeatmaps(
	instrument, timeframe string,
	heatmaps []HeatmapArtifactV1,
	mode FusionMode,
) (FusedHeatmapArtifactV1, *problem.Problem) {
	if strings.TrimSpace(instrument) == "" {
		return FusedHeatmapArtifactV1{}, problem.New(problem.ValidationFailed, "instrument must not be empty")
	}
	if len(heatmaps) == 0 {
		return FusedHeatmapArtifactV1{}, problem.New(problem.ValidationFailed, "at least one heatmap required")
	}

	cells, sourceVenues, windowStart, windowEnd := accumulateHeatmapCells(heatmaps)
	sourceMix := buildEqualSourceMix(sourceVenues)

	return FusedHeatmapArtifactV1{
		Instrument:    strings.TrimSpace(instrument),
		Timeframe:     timeframe,
		WindowStartTs: windowStart,
		WindowEndTs:   windowEnd,
		Mode:          mode,
		Cells:         cells,
		SourceVenues:  sourceVenues,
		Meta: FusionMeta{
			Reason:     fusionReasonStr(mode),
			Confidence: 1.0,
			SourceMix:  sourceMix,
			Staleness:  FusionStalenessReport{FreshCount: len(sourceVenues)},
		},
	}, nil
}

func accumulateHeatmapCells(heatmaps []HeatmapArtifactV1) ([]FusedHeatmapCellV1, []string, int64, int64) {
	type cellKey struct {
		low, high float64
		size      string
	}
	type cellAcc struct {
		bid, ask, trade float64
		samples         int64
		venueMix        []VenueContribution
	}
	index := make(map[cellKey]*cellAcc)
	var keys []cellKey
	var windowStart, windowEnd int64
	sourceVenues := make([]string, 0, len(heatmaps))

	for _, h := range heatmaps {
		sourceVenues = append(sourceVenues, h.Venue)
		windowStart, windowEnd = expandWindow(windowStart, windowEnd, h.WindowStartTs, h.WindowEndTs)
		for _, c := range h.Cells {
			k := cellKey{low: c.PriceBucketLow, high: c.PriceBucketHigh, size: strings.ToUpper(strings.TrimSpace(c.SizeBucket))}
			totalVol := c.BidLiquidity + c.AskLiquidity + c.TradeVolume
			if acc, ok := index[k]; ok {
				acc.bid += c.BidLiquidity
				acc.ask += c.AskLiquidity
				acc.trade += c.TradeVolume
				acc.samples += c.Samples
				acc.venueMix = append(acc.venueMix, VenueContribution{Venue: h.Venue, Volume: totalVol})
			} else {
				index[k] = &cellAcc{
					bid: c.BidLiquidity, ask: c.AskLiquidity, trade: c.TradeVolume,
					samples:  c.Samples,
					venueMix: []VenueContribution{{Venue: h.Venue, Volume: totalVol}},
				}
				keys = append(keys, k)
			}
		}
	}

	slices.Sort(sourceVenues)

	cells := make([]FusedHeatmapCellV1, 0, len(keys))
	for _, k := range keys {
		acc := index[k]
		totalAll := acc.bid + acc.ask + acc.trade
		assignVenueMixWeights(acc.venueMix, totalAll)
		cells = append(cells, FusedHeatmapCellV1{
			PriceBucketLow: k.low, PriceBucketHigh: k.high, SizeBucket: k.size,
			BidLiquidity: acc.bid, AskLiquidity: acc.ask, TradeVolume: acc.trade,
			Samples: acc.samples, VenueMix: acc.venueMix,
		})
	}

	slices.SortFunc(cells, sortHeatmapCells)
	if len(cells) > FusedHeatmapMaxCells {
		cells = cells[:FusedHeatmapMaxCells]
	}
	return cells, sourceVenues, windowStart, windowEnd
}

func sortHeatmapCells(a, b FusedHeatmapCellV1) int {
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
	return strings.Compare(
		strings.ToUpper(strings.TrimSpace(a.SizeBucket)),
		strings.ToUpper(strings.TrimSpace(b.SizeBucket)),
	)
}
