package clickhouse_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
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
