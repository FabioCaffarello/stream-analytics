package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

var testLogger = slog.Default()

// ── helpers ──────────────────────────────────────────────────────────────────

func testSnapshot(venue, instrument string, seq int64) aggdomain.SnapshotProduced {
	return aggdomain.SnapshotProduced{
		BookID: aggdomain.BookID{
			Venue:      venue,
			Instrument: instrument,
		},
		Seq: seq,
		Bids: []aggdomain.Level{{
			Price:    aggdomain.Price(100.5),
			Quantity: 1,
		}},
		Asks: []aggdomain.Level{{
			Price:    aggdomain.Price(101.0),
			Quantity: 1,
		}},
	}
}

func snapshotEnvelope(t *testing.T, venue, instrument string, seq int64) envelope.Envelope {
	t.Helper()
	snap := testSnapshot(venue, instrument, seq)
	payload, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return envelope.Envelope{
		Type:       "aggregation.snapshot",
		Version:    1,
		Venue:      venue,
		Instrument: instrument,
		Seq:        seq,
		Payload:    payload,
	}
}

// testBatcher creates a batcher with batch-size-1 (default, write-through).
func testBatcher(w *clickhouse.Writer) *StoreBatcher {
	return NewStoreBatcher(w, defaultBatchCfg())
}

// ── handleAggregationSnapshot ────────────────────────────────────────────────

func TestHandleAggregationSnapshot_CommitSucceeds_ReturnsNil(t *testing.T) {
	writer := clickhouse.NewWriter()
	b := testBatcher(writer)
	env := snapshotEnvelope(t, "binance", "BTCUSDT", 1)

	p := handleAggregationSnapshot(context.Background(), env, b, testLogger)
	if p != nil {
		t.Fatalf("expected nil, got %v", p)
	}
	if got := writer.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1", got)
	}
}

func TestHandleAggregationSnapshot_NilBatcher_ReturnsProblem(t *testing.T) {
	env := snapshotEnvelope(t, "binance", "BTCUSDT", 1)

	p := handleAggregationSnapshot(context.Background(), env, nil, testLogger)
	if p == nil {
		t.Fatal("expected problem for nil batcher, got nil")
	}
}

func TestHandleAggregationSnapshot_DecodeFailure_ReturnsProblem(t *testing.T) {
	writer := clickhouse.NewWriter()
	b := testBatcher(writer)
	env := envelope.Envelope{
		Type:    "aggregation.snapshot",
		Version: 1,
		Venue:   "binance",
		Payload: []byte(`{invalid json`),
	}

	p := handleAggregationSnapshot(context.Background(), env, b, testLogger)
	if p == nil {
		t.Fatal("expected problem for invalid payload, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%q want=%q", p.Code, problem.ValidationFailed)
	}
	if got := writer.CommitCount(); got != 0 {
		t.Fatalf("commit count=%d want=0 (nothing should be written)", got)
	}
}

func TestHandleAggregationSnapshot_DuplicateIdempotent(t *testing.T) {
	writer := clickhouse.NewWriter()
	b := testBatcher(writer)
	env := snapshotEnvelope(t, "binance", "BTCUSDT", 42)

	if p := handleAggregationSnapshot(context.Background(), env, b, testLogger); p != nil {
		t.Fatalf("first: %v", p)
	}
	if p := handleAggregationSnapshot(context.Background(), env, b, testLogger); p != nil {
		t.Fatalf("second (idempotent): %v", p)
	}
	if got := writer.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1 (idempotent dedup)", got)
	}
}

func TestHandleAggregationSnapshot_BookIDFallsBackToEnvelope(t *testing.T) {
	writer := clickhouse.NewWriter()
	b := testBatcher(writer)

	// Payload with empty BookID — handler should fill from envelope metadata.
	snap := aggdomain.SnapshotProduced{
		Seq:  7,
		Bids: []aggdomain.Level{{Price: 100, Quantity: 1}},
		Asks: []aggdomain.Level{{Price: 101, Quantity: 1}},
	}
	payload, _ := json.Marshal(snap)
	env := envelope.Envelope{
		Type:       "aggregation.snapshot",
		Version:    1,
		Venue:      "bybit",
		Instrument: "ETHUSDT",
		Seq:        7,
		Payload:    payload,
	}

	p := handleAggregationSnapshot(context.Background(), env, b, testLogger)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if got := writer.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1", got)
	}
}

// ── redelivery / idempotency (S3-D1) ─────────────────────────────────────────

func TestHandleAggregationSnapshot_RedeliveryWithSameIdempotencyKey_NoDuplicate(t *testing.T) {
	writer := clickhouse.NewWriter()
	b := testBatcher(writer)
	env := snapshotEnvelope(t, "binance", "BTCUSDT", 42)
	env.IdempotencyKey = "js-msg-id-001"

	if p := handleAggregationSnapshot(context.Background(), env, b, testLogger); p != nil {
		t.Fatalf("first delivery: %v", p)
	}
	// Simulate JetStream redelivery (NumDelivered>1) — same envelope, same key.
	if p := handleAggregationSnapshot(context.Background(), env, b, testLogger); p != nil {
		t.Fatalf("redelivery: %v", p)
	}
	if got := writer.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1 (redelivery must not duplicate)", got)
	}
}

func TestHandleAggregationSnapshot_DifferentIdempotencyKeys_BothStored(t *testing.T) {
	writer := clickhouse.NewWriter()
	b := testBatcher(writer)
	env1 := snapshotEnvelope(t, "binance", "BTCUSDT", 42)
	env1.IdempotencyKey = "key-a"
	env2 := snapshotEnvelope(t, "binance", "BTCUSDT", 42)
	env2.IdempotencyKey = "key-b"

	if p := handleAggregationSnapshot(context.Background(), env1, b, testLogger); p != nil {
		t.Fatalf("first: %v", p)
	}
	if p := handleAggregationSnapshot(context.Background(), env2, b, testLogger); p != nil {
		t.Fatalf("second: %v", p)
	}
	if got := writer.CommitCount(); got != 2 {
		t.Fatalf("commit count=%d want=2 (different idempotency keys are independent)", got)
	}
}

// ── handleStoreEnvelope routing ──────────────────────────────────────────────

func TestHandleStoreEnvelope_RoutesSnapshot(t *testing.T) {
	writer := clickhouse.NewWriter()
	b := testBatcher(writer)
	env := snapshotEnvelope(t, "binance", "BTCUSDT", 1)

	p := handleStoreEnvelope(context.Background(), env, b, testLogger)
	if p != nil {
		t.Fatalf("expected nil, got %v", p)
	}
	if got := writer.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1", got)
	}
}

func TestHandleStoreEnvelope_SkipsUnhandledEvent(t *testing.T) {
	writer := clickhouse.NewWriter()
	b := testBatcher(writer)
	env := envelope.Envelope{
		Type:    "aggregation.orderbook_inconsistency",
		Version: 1,
		Venue:   "binance",
		Payload: []byte(`{}`),
	}

	p := handleStoreEnvelope(context.Background(), env, b, testLogger)
	if p != nil {
		t.Fatalf("expected nil for skipped event, got %v", p)
	}
	if got := writer.CommitCount(); got != 0 {
		t.Fatalf("commit count=%d want=0 (skipped events must not write)", got)
	}
}

func TestHandleStoreEnvelope_SnapshotWrongVersion_Skipped(t *testing.T) {
	writer := clickhouse.NewWriter()
	b := testBatcher(writer)
	env := snapshotEnvelope(t, "binance", "BTCUSDT", 1)
	env.Version = 99

	p := handleStoreEnvelope(context.Background(), env, b, testLogger)
	if p != nil {
		t.Fatalf("expected nil for wrong version, got %v", p)
	}
	if got := writer.CommitCount(); got != 0 {
		t.Fatalf("commit count=%d want=0", got)
	}
}

// ── heartbeat counter ────────────────────────────────────────────────────────

func TestHeartbeat_IncrementsConcurrently(t *testing.T) {
	// Reset counter for deterministic test.
	storeConsumedCount.Store(0)

	writer := clickhouse.NewWriter()
	b := testBatcher(writer)
	for i := int64(0); i < 10; i++ {
		env := snapshotEnvelope(t, "binance", "BTCUSDT", i)
		_ = handleStoreEnvelope(context.Background(), env, b, testLogger)
	}

	if got := storeConsumedCount.Load(); got != 10 {
		t.Fatalf("consumed count=%d want=10", got)
	}
}
