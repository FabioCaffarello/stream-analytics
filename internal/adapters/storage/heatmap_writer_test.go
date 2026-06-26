package storage_test

import (
	"context"
	"testing"

	clickhouse "github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/clickhouse"
	timescale "github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/timescale"
	insightsdomain "github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
)

func TestHeatmapStorageHotColdIdempotent(t *testing.T) {
	artifact := insightsdomain.HeatmapArtifactV1{
		Venue:         "BINANCE",
		Instrument:    "BTCUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1_710_000_000_000,
		WindowEndTs:   1_710_000_060_000,
		Cells: []insightsdomain.HeatmapCellV1{
			{
				PriceBucketLow:  100,
				PriceBucketHigh: 100.5,
				SizeBucket:      "M",
				BidLiquidity:    1,
				AskLiquidity:    2,
				TradeVolume:     3,
				SeqMin:          1,
				SeqMax:          10,
				Samples:         4,
			},
		},
	}

	hot := timescale.NewHeatmapWriter()
	cold := clickhouse.NewHeatmapWriter()

	for i := 0; i < 2; i++ {
		if p := hot.Save(context.Background(), artifact, "src-1"); p != nil {
			t.Fatalf("hot save failed: %v", p)
		}
		if p := cold.Save(context.Background(), artifact, "src-1"); p != nil {
			t.Fatalf("cold save failed: %v", p)
		}
	}

	if got := hot.CommitCount(); got != 1 {
		t.Fatalf("hot commits=%d want=1", got)
	}
	if got := cold.CommitCount(); got != 1 {
		t.Fatalf("cold commits=%d want=1", got)
	}
}
