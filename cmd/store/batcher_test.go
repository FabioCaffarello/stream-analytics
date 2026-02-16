package main

import (
	"context"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/config"
)

func defaultBatchCfg() config.StoreBatchConfig {
	return config.StoreBatchConfig{
		MaxRows:       1,
		FlushInterval: "100ms",
	}
}

func batchSnap(seq int64) aggdomain.SnapshotProduced {
	return aggdomain.SnapshotProduced{
		BookID: aggdomain.BookID{Venue: "binance", Instrument: "BTCUSDT"},
		Seq:    seq,
		Bids:   []aggdomain.Level{{Price: 100, Quantity: 1}},
		Asks:   []aggdomain.Level{{Price: 101, Quantity: 1}},
	}
}

func TestBatcher_SingleItem_FlushesImmediately(t *testing.T) {
	w := clickhouse.NewWriter()
	b := NewStoreBatcher(w, defaultBatchCfg())

	if p := b.Write(context.Background(), batchSnap(1), "k1"); p != nil {
		t.Fatalf("write: %v", p)
	}
	if got := w.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1 (max_rows=1 flushes immediately)", got)
	}
}

func TestBatcher_MaxRows_TriggersFlush(t *testing.T) {
	w := clickhouse.NewWriter()
	cfg := defaultBatchCfg()
	cfg.MaxRows = 3
	cfg.FlushInterval = "1h" // disable time-based flush
	b := NewStoreBatcher(w, cfg)

	// First two items: below threshold, no flush.
	for i := int64(1); i <= 2; i++ {
		if p := b.Write(context.Background(), batchSnap(i), "k"); p != nil {
			t.Fatalf("write seq=%d: %v", i, p)
		}
	}
	if got := w.CommitCount(); got != 0 {
		t.Fatalf("commit count=%d want=0 (batch not full yet)", got)
	}

	// Third item triggers flush of all 3.
	if p := b.Write(context.Background(), batchSnap(3), "k"); p != nil {
		t.Fatalf("write seq=3: %v", p)
	}
	if got := w.CommitCount(); got != 3 {
		t.Fatalf("commit count=%d want=3 (batch flushed)", got)
	}
}

func TestBatcher_FlushError_ReturnsFirstProblem(t *testing.T) {
	b := NewStoreBatcher(nil, defaultBatchCfg()) // nil writer → error

	p := b.Write(context.Background(), batchSnap(1), "k")
	if p == nil {
		t.Fatal("expected problem for nil writer, got nil")
	}
}

func TestBatcher_Close_FlushesRemaining(t *testing.T) {
	w := clickhouse.NewWriter()
	cfg := defaultBatchCfg()
	cfg.MaxRows = 100
	cfg.FlushInterval = "1h"
	b := NewStoreBatcher(w, cfg)

	for i := int64(1); i <= 5; i++ {
		_ = b.Write(context.Background(), batchSnap(i), "k")
	}
	if got := w.CommitCount(); got != 0 {
		t.Fatalf("commit count=%d want=0 before close", got)
	}

	if p := b.Close(context.Background()); p != nil {
		t.Fatalf("close: %v", p)
	}
	if got := w.CommitCount(); got != 5 {
		t.Fatalf("commit count=%d want=5 after close", got)
	}
}

func TestBatcher_Close_EmptyBatch_Noop(t *testing.T) {
	w := clickhouse.NewWriter()
	b := NewStoreBatcher(w, defaultBatchCfg())

	if p := b.Close(context.Background()); p != nil {
		t.Fatalf("close: %v", p)
	}
	if got := w.CommitCount(); got != 0 {
		t.Fatalf("commit count=%d want=0", got)
	}
}

func TestBatcher_Idempotent_RedeliveryInSameBatch(t *testing.T) {
	w := clickhouse.NewWriter()
	cfg := defaultBatchCfg()
	cfg.MaxRows = 10
	cfg.FlushInterval = "1h"
	b := NewStoreBatcher(w, cfg)

	snap := batchSnap(42)
	// Enqueue same snapshot+key twice (simulates redelivery within same batch).
	_ = b.Write(context.Background(), snap, "same-key")
	_ = b.Write(context.Background(), snap, "same-key")

	if p := b.Close(context.Background()); p != nil {
		t.Fatalf("close: %v", p)
	}
	if got := w.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1 (idempotent dedup within batch)", got)
	}
}
