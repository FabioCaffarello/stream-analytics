package clickhouse_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/clickhouse"
	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func TestWriter_IdempotentSamePayloadCommitsOnce(t *testing.T) {
	w := clickhouse.NewWriter()
	snap := testSnapshot(42, 100, 101)

	if p := w.Save(context.Background(), snap); p != nil {
		t.Fatalf("first save failed: %v", p)
	}
	if p := w.Save(context.Background(), snap); p != nil {
		t.Fatalf("second save failed: %v", p)
	}
	if got := w.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1", got)
	}
}

func TestWriter_DuplicateKeyConflictReturnsProblem(t *testing.T) {
	w := clickhouse.NewWriter()
	first := testSnapshot(42, 100, 101)
	conflict := testSnapshot(42, 99, 101)

	if p := w.Save(context.Background(), first); p != nil {
		t.Fatalf("first save failed: %v", p)
	}
	p := w.Save(context.Background(), conflict)
	if p == nil {
		t.Fatal("expected conflict problem, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code=%q want=%q", p.Code, problem.ValidationFailed)
	}
	if got := w.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1", got)
	}
}

func TestWriter_ClickHouseSchemaContractMatchesUpsertKey(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "..", "..", "sql", "clickhouse", "migrations", "0001_m1_writer_skeleton.sql"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", path, err)
	}
	ddl := string(raw)
	if !strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS aggregation_snapshots_v1") {
		t.Fatalf("schema must define aggregation_snapshots_v1 table")
	}
	if !strings.Contains(ddl, "ORDER BY (venue, instrument, seq)") {
		t.Fatalf("schema must preserve idempotent key order by (venue, instrument, seq)")
	}
}

func TestWriter_ClickHouseSchemaContractW2HasCanonicalSubjectKey(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "..", "..", "sql", "clickhouse", "migrations", "0002_w2_cold_correctness.sql"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", path, err)
	}
	ddl := string(raw)
	if !strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS aggregation_snapshots_v2") {
		t.Fatalf("schema must define aggregation_snapshots_v2 table")
	}
	if !strings.Contains(ddl, "source_idempotency_key String") {
		t.Fatalf("schema must persist source_idempotency_key for traceability")
	}
	if !strings.Contains(ddl, "ORDER BY (subject, venue, instrument, seq, source_idempotency_key)") {
		t.Fatalf("schema must preserve canonical key order by (subject, venue, instrument, seq, source_idempotency_key)")
	}
}

func TestWriter_ClickHouseSchemaContractW4HasTTLAndPartition(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "..", "..", "sql", "clickhouse", "migrations", "0003_w4_ttl_partition.sql"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", path, err)
	}
	ddl := string(raw)
	if !strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS aggregation_snapshots_v3") {
		t.Fatalf("schema must define aggregation_snapshots_v3 table")
	}
	if !strings.Contains(ddl, "ts") {
		t.Fatalf("schema must include ts column for TTL and partitioning")
	}
	if !strings.Contains(ddl, "PARTITION BY toYYYYMM(ts)") {
		t.Fatalf("schema must partition by toYYYYMM(ts)")
	}
	if !strings.Contains(ddl, "TTL toDateTime(ts) + INTERVAL 90 DAY") {
		t.Fatalf("schema must have TTL of 90 days on ts column")
	}
}

func TestWriter_ClickHouseSchemaContractM2HasSubMinuteTTLPolicy(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "..", "..", "sql", "clickhouse", "migrations", "0007_m2_subminute_ttl_policy.sql"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", path, err)
	}
	ddl := string(raw)
	if !strings.Contains(ddl, "ALTER TABLE aggregation_candle_cold") {
		t.Fatalf("migration must alter aggregation_candle_cold")
	}
	if !strings.Contains(ddl, "ALTER TABLE aggregation_stats_cold") {
		t.Fatalf("migration must alter aggregation_stats_cold")
	}
	if !strings.Contains(ddl, "timeframe IN ('1s', '5s')") {
		t.Fatalf("migration must define sub-minute timeframe policy")
	}
	if !strings.Contains(ddl, "INTERVAL 14 DAY") {
		t.Fatalf("migration must set shorter TTL for sub-minute timeframes")
	}
	if !strings.Contains(ddl, "INTERVAL 90 DAY") {
		t.Fatalf("migration must preserve default TTL for non-sub-minute timeframes")
	}
}

// ── SaveIdempotent (S3-D1) ───────────────────────────────────────────────────

func TestWriter_SaveIdempotent_RedeliverySameKey_NoDuplicate(t *testing.T) {
	w := clickhouse.NewWriter()
	snap := testSnapshot(42, 100, 101)
	key := "envelope-abc-123"

	if p := w.SaveIdempotent(context.Background(), snap, key); p != nil {
		t.Fatalf("first: %v", p)
	}
	if p := w.SaveIdempotent(context.Background(), snap, key); p != nil {
		t.Fatalf("redelivery: %v", p)
	}
	if got := w.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1 (redelivery must not duplicate)", got)
	}
}

func TestWriter_SaveIdempotent_DifferentKey_BothStored(t *testing.T) {
	w := clickhouse.NewWriter()
	snap := testSnapshot(42, 100, 101)

	if p := w.SaveIdempotent(context.Background(), snap, "key-a"); p != nil {
		t.Fatalf("first: %v", p)
	}
	if p := w.SaveIdempotent(context.Background(), snap, "key-b"); p != nil {
		t.Fatalf("second: %v", p)
	}
	if got := w.CommitCount(); got != 2 {
		t.Fatalf("commit count=%d want=2 (different keys must both store)", got)
	}
}

func TestWriter_SaveIdempotent_ConflictDetection(t *testing.T) {
	w := clickhouse.NewWriter()
	first := testSnapshot(42, 100, 101)
	conflict := testSnapshot(42, 99, 101) // same seq, different bid

	if p := w.SaveIdempotent(context.Background(), first, "same-key"); p != nil {
		t.Fatalf("first: %v", p)
	}
	p := w.SaveIdempotent(context.Background(), conflict, "same-key")
	if p == nil {
		t.Fatal("expected conflict problem, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%q want=%q", p.Code, problem.ValidationFailed)
	}
	if got := w.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1", got)
	}
}

func TestWriter_SaveIdempotent_NilWriter_ReturnsProblem(t *testing.T) {
	var w *clickhouse.Writer
	snap := testSnapshot(1, 100, 101)
	p := w.SaveIdempotent(context.Background(), snap, "key")
	if p == nil {
		t.Fatal("expected problem for nil writer")
	}
}

func testSnapshot(seq int64, bid, ask float64) aggdomain.SnapshotProduced {
	return aggdomain.SnapshotProduced{
		BookID: aggdomain.BookID{
			Venue:      "binance",
			Instrument: "BTCUSDT",
		},
		Seq: seq,
		Bids: []aggdomain.Level{{
			Price:    aggdomain.Price(bid),
			Quantity: 1,
		}},
		Asks: []aggdomain.Level{{
			Price:    aggdomain.Price(ask),
			Quantity: 1,
		}},
	}
}
