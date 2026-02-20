package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/insights/domain"
)

func validHeatmapArtifact() domain.HeatmapArtifactV1 {
	return domain.HeatmapArtifactV1{
		Venue:         "BINANCE",
		Instrument:    "BTCUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1710000000000,
		WindowEndTs:   1710000060000,
		Cells: []domain.HeatmapCellV1{
			{
				PriceBucketLow:  100,
				PriceBucketHigh: 101,
				SizeBucket:      "S",
				BidLiquidity:    10,
				AskLiquidity:    9,
				TradeVolume:     8,
				SeqMin:          1,
				SeqMax:          2,
				Samples:         2,
			},
			{
				PriceBucketLow:  101,
				PriceBucketHigh: 102,
				SizeBucket:      "M",
				BidLiquidity:    6,
				AskLiquidity:    7,
				TradeVolume:     5,
				SeqMin:          3,
				SeqMax:          5,
				Samples:         3,
			},
		},
	}
}

func TestHeatmapArtifactValidate_SucceedsForValidArtifact(t *testing.T) {
	artifact := validHeatmapArtifact()
	if p := artifact.Validate(); p != nil {
		t.Fatalf("Validate() unexpected problem: %v", p)
	}
}

func TestHeatmapArtifactValidate_FailsWhenCellsDuplicateByBucket(t *testing.T) {
	artifact := validHeatmapArtifact()
	artifact.Cells = []domain.HeatmapCellV1{
		artifact.Cells[0],
		{
			PriceBucketLow:  artifact.Cells[0].PriceBucketLow,
			PriceBucketHigh: artifact.Cells[0].PriceBucketHigh,
			SizeBucket:      "s", // case-insensitive duplicate bucket key
			BidLiquidity:    1,
			AskLiquidity:    1,
			TradeVolume:     1,
			SeqMin:          10,
			SeqMax:          10,
			Samples:         1,
		},
	}
	if p := artifact.Validate(); p == nil {
		t.Fatal("expected validation problem for duplicate cell bucket")
	}
}

func TestHeatmapArtifactValidate_FailsWhenCellsUnsorted(t *testing.T) {
	artifact := validHeatmapArtifact()
	artifact.Cells[0], artifact.Cells[1] = artifact.Cells[1], artifact.Cells[0]
	if p := artifact.Validate(); p == nil {
		t.Fatal("expected validation problem for unsorted cells")
	}
}

func TestHeatmapArtifactValidate_FailsWhenWindowBoundsInvalid(t *testing.T) {
	artifact := validHeatmapArtifact()
	artifact.WindowEndTs = artifact.WindowStartTs
	if p := artifact.Validate(); p == nil {
		t.Fatal("expected validation problem for invalid window bounds")
	}
}

func TestHeatmapArtifactValidate_FailsWhenCellValuesInvalid(t *testing.T) {
	artifact := validHeatmapArtifact()
	artifact.Cells[0].SeqMin = 0
	if p := artifact.Validate(); p == nil {
		t.Fatal("expected validation problem for invalid seq bounds")
	}
}
