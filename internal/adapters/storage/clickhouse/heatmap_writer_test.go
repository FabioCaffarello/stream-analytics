package clickhouse_test

import (
	"context"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestChHeatmapWriter_Save_Success(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChHeatmapWriterWithPreparer(&fakePreparer{batch: batch})

	if p := w.Save(context.Background(), testHeatmapArtifact(), "src-1"); p != nil {
		t.Fatalf("save: %v", p)
	}
	if got, want := len(batch.rows), 2; got != want {
		t.Fatalf("rows=%d want=%d", got, want)
	}
	if got, want := batch.flushes, 1; got != want {
		t.Fatalf("flushes=%d want=%d", got, want)
	}
}

func TestChHeatmapWriter_Save_Validation(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChHeatmapWriterWithPreparer(&fakePreparer{batch: batch})

	p := w.Save(context.Background(), insightsdomain.HeatmapArtifactV1{}, "src-1")
	if p == nil {
		t.Fatal("expected validation problem")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%q want=%q", p.Code, problem.ValidationFailed)
	}
}

func TestChHeatmapWriter_Save_RequiresSourceIdempotencyKey(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChHeatmapWriterWithPreparer(&fakePreparer{batch: batch})

	p := w.Save(context.Background(), testHeatmapArtifact(), "")
	if p == nil {
		t.Fatal("expected validation problem")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%q want=%q", p.Code, problem.ValidationFailed)
	}
}

func testHeatmapArtifact() insightsdomain.HeatmapArtifactV1 {
	return insightsdomain.HeatmapArtifactV1{
		Venue:         "BINANCE",
		Instrument:    "BTCUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1_700_000_000_000,
		WindowEndTs:   1_700_000_060_000,
		Cells: []insightsdomain.HeatmapCellV1{
			{
				PriceBucketLow:  100.0,
				PriceBucketHigh: 100.5,
				SizeBucket:      "S",
				BidLiquidity:    1.2,
				AskLiquidity:    2.3,
				TradeVolume:     3.4,
				SeqMin:          10,
				SeqMax:          20,
				Samples:         5,
			},
			{
				PriceBucketLow:  100.5,
				PriceBucketHigh: 101.0,
				SizeBucket:      "M",
				BidLiquidity:    4.5,
				AskLiquidity:    5.6,
				TradeVolume:     6.7,
				SeqMin:          21,
				SeqMax:          30,
				Samples:         7,
			},
		},
	}
}
