package timescale_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/timescale"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

func testHeatmapArtifact() insightsdomain.HeatmapArtifactV1 {
	return insightsdomain.HeatmapArtifactV1{
		Venue:         "BINANCE",
		Instrument:    "BTCUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1_710_000_000_000,
		WindowEndTs:   1_710_000_060_000,
		Cells: []insightsdomain.HeatmapCellV1{
			{
				PriceBucketLow:  42000.0,
				PriceBucketHigh: 42100.0,
				SizeBucket:      "SMALL",
				BidLiquidity:    10.5,
				AskLiquidity:    8.3,
				TradeVolume:     5.0,
				SeqMin:          1,
				SeqMax:          10,
				Samples:         10,
			},
			{
				PriceBucketLow:  42100.0,
				PriceBucketHigh: 42200.0,
				SizeBucket:      "SMALL",
				BidLiquidity:    7.2,
				AskLiquidity:    9.1,
				TradeVolume:     3.0,
				SeqMin:          11,
				SeqMax:          20,
				Samples:         10,
			},
		},
	}
}

func TestPgHeatmapWriter_Save_Success(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewHeatmapWriterWithExecutor(exec)

	if p := w.Save(context.Background(), testHeatmapArtifact(), "source-key-1"); p != nil {
		t.Fatalf("save heatmap: %v", p)
	}
	if !strings.Contains(exec.lastQuery, "aggregation_heatmap") {
		t.Fatalf("query=%q missing target table", exec.lastQuery)
	}
	// 16 args per cell: venue, instrument, timeframe, window_start, window_end,
	// price_bucket_low, price_bucket_high, size_bucket, bid_liquidity,
	// ask_liquidity, trade_volume, seq_min, seq_max, samples,
	// source_idempotency_key, idempotency_key
	if len(exec.lastArgs) != 16 {
		t.Fatalf("args len=%d want=16", len(exec.lastArgs))
	}
}

func TestPgHeatmapWriter_Save_EmptySourceKey(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewHeatmapWriterWithExecutor(exec)

	p := w.Save(context.Background(), testHeatmapArtifact(), "")
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed for empty source key, got=%v", p)
	}
}

func TestPgHeatmapWriter_Save_InvalidArtifact(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewHeatmapWriterWithExecutor(exec)

	invalid := insightsdomain.HeatmapArtifactV1{
		Venue:         "",
		Instrument:    "BTCUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1,
		WindowEndTs:   2,
	}
	p := w.Save(context.Background(), invalid, "source-key-1")
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed for invalid artifact, got=%v", p)
	}
}

func TestPgHeatmapWriter_Save_IdempotencyKeyDeterministic(t *testing.T) {
	exec1 := &fakeSQLExecutor{rows: 1}
	w1 := timescale.NewHeatmapWriterWithExecutor(exec1)
	if p := w1.Save(context.Background(), testHeatmapArtifact(), "source-key-1"); p != nil {
		t.Fatalf("save #1: %v", p)
	}

	exec2 := &fakeSQLExecutor{rows: 1}
	w2 := timescale.NewHeatmapWriterWithExecutor(exec2)
	if p := w2.Save(context.Background(), testHeatmapArtifact(), "source-key-1"); p != nil {
		t.Fatalf("save #2: %v", p)
	}

	// Idempotency key is the last arg (index 15).
	key1, ok1 := exec1.lastArgs[15].(string)
	key2, ok2 := exec2.lastArgs[15].(string)
	if !ok1 || !ok2 {
		t.Fatalf("idempotency key type: ok1=%v ok2=%v", ok1, ok2)
	}
	if key1 != key2 {
		t.Fatalf("idempotency keys differ: %q vs %q", key1, key2)
	}
	if key1 == "" {
		t.Fatal("idempotency key must not be empty")
	}
}

func TestPgHeatmapWriter_Save_ConnectionError(t *testing.T) {
	exec := &fakeSQLExecutor{
		p: problem.Wrap(errors.New("db down"), problem.Unavailable, "timescale exec failed"),
	}
	w := timescale.NewHeatmapWriterWithExecutor(exec)

	p := w.Save(context.Background(), testHeatmapArtifact(), "source-key-1")
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("code=%q want=%q", p.Code, problem.Unavailable)
	}
}
