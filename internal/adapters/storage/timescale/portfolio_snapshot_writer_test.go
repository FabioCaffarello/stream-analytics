package timescale_test

import (
	"context"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/timescale"
	domain "github.com/market-raccoon/internal/core/portfolio/domain"
)

func TestPgAccountSnapshotWriter_Upsert_Success(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewPgAccountSnapshotWriterWithExecutor(exec)

	if p := w.UpsertAccountSnapshot(context.Background(), testAccountSnapshot()); p != nil {
		t.Fatalf("upsert: %v", p)
	}
	if !strings.Contains(exec.lastQuery, "portfolio_account_snapshot") {
		t.Fatalf("query=%q missing target table", exec.lastQuery)
	}
	if len(exec.lastArgs) != 10 {
		t.Fatalf("args len=%d want=10", len(exec.lastArgs))
	}
}

func TestPgAccountSnapshotWriter_Upsert_DuplicateIdempotent(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 0}
	w := timescale.NewPgAccountSnapshotWriterWithExecutor(exec)

	if p := w.UpsertAccountSnapshot(context.Background(), testAccountSnapshot()); p != nil {
		t.Fatalf("upsert: %v", p)
	}
}

func TestPgAccountSnapshotWriter_Upsert_ValidationFails(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewPgAccountSnapshotWriterWithExecutor(exec)

	s := testAccountSnapshot()
	s.SnapshotID = ""

	if p := w.UpsertAccountSnapshot(context.Background(), s); p == nil {
		t.Fatal("expected validation error, got nil")
	}
	if exec.lastQuery != "" {
		t.Fatal("should not have executed any query")
	}
}

func TestPgAccountSnapshotWriter_Upsert_NilWriter(t *testing.T) {
	var w *timescale.PgAccountSnapshotWriter
	if p := w.UpsertAccountSnapshot(context.Background(), testAccountSnapshot()); p == nil {
		t.Fatal("expected problem, got nil")
	}
}

func TestPgAccountSnapshotWriter_Upsert_ConnectionError(t *testing.T) {
	exec := &fakeSQLExecutor{
		p: testUnavailableProblem(),
	}
	w := timescale.NewPgAccountSnapshotWriterWithExecutor(exec)

	p := w.UpsertAccountSnapshot(context.Background(), testAccountSnapshot())
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
}

func testAccountSnapshot() domain.AccountSnapshotV1 {
	return domain.AccountSnapshotV1{
		SnapshotID:         "snap-001",
		AccountID:          "acct-1",
		ProjectedAtMs:      1_710_000_000_000,
		TotalEquityUSD:     50_000.0,
		TotalRealizedUSD:   1_000.0,
		TotalUnrealizedUSD: -200.0,
		TotalMarginUsedUSD: 2_000.0,
		TotalLeverage:      1.67,
		Venues: []domain.VenuePositionV1{
			{
				Venue:            "binance",
				EquityUSD:        50_000.0,
				RealizedPnlUSD:   1_000.0,
				UnrealizedPnlUSD: -200.0,
				MarginUsedUSD:    2_000.0,
				Positions: []domain.PositionV1{
					{Venue: "binance", Symbol: "BTCUSDT", Quantity: 0.5, AvgEntryPrice: 60000, Side: "long"},
				},
				Balances: []domain.BalanceV1{
					{Asset: "USDT", Total: 50000, Available: 48000, Locked: 2000},
				},
			},
		},
		FillSummary: domain.FillSummaryV1{
			TotalTradeCount: 3, TotalVolumeTradedUSD: 90000,
		},
	}
}
