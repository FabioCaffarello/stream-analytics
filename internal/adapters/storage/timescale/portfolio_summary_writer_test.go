package timescale_test

import (
	"context"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/timescale"
	domain "github.com/market-raccoon/internal/core/portfolio/domain"
)

func TestPgPortfolioSummaryWriter_Upsert_Success(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewPgPortfolioSummaryWriterWithExecutor(exec)

	if p := w.UpsertPortfolioSummary(context.Background(), testPortfolioSummary()); p != nil {
		t.Fatalf("upsert: %v", p)
	}
	if !strings.Contains(exec.lastQuery, "portfolio_summary") {
		t.Fatalf("query=%q missing target table", exec.lastQuery)
	}
	if len(exec.lastArgs) != 11 {
		t.Fatalf("args len=%d want=11", len(exec.lastArgs))
	}
}

func TestPgPortfolioSummaryWriter_Upsert_DuplicateIdempotent(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 0}
	w := timescale.NewPgPortfolioSummaryWriterWithExecutor(exec)

	if p := w.UpsertPortfolioSummary(context.Background(), testPortfolioSummary()); p != nil {
		t.Fatalf("upsert: %v", p)
	}
}

func TestPgPortfolioSummaryWriter_Upsert_ValidationFails(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewPgPortfolioSummaryWriterWithExecutor(exec)

	s := testPortfolioSummary()
	s.SummaryID = ""

	if p := w.UpsertPortfolioSummary(context.Background(), s); p == nil {
		t.Fatal("expected validation error, got nil")
	}
	if exec.lastQuery != "" {
		t.Fatal("should not have executed any query")
	}
}

func TestPgPortfolioSummaryWriter_Upsert_NilWriter(t *testing.T) {
	var w *timescale.PgPortfolioSummaryWriter
	if p := w.UpsertPortfolioSummary(context.Background(), testPortfolioSummary()); p == nil {
		t.Fatal("expected problem, got nil")
	}
}

func TestPgPortfolioSummaryWriter_Upsert_ConnectionError(t *testing.T) {
	exec := &fakeSQLExecutor{
		p: testUnavailableProblem(),
	}
	w := timescale.NewPgPortfolioSummaryWriterWithExecutor(exec)

	p := w.UpsertPortfolioSummary(context.Background(), testPortfolioSummary())
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
}

func testPortfolioSummary() domain.PortfolioSummaryV1 {
	return domain.PortfolioSummaryV1{
		SummaryID:           "sum-001",
		ProjectedAtMs:       1_710_000_000_000,
		GlobalEquityUSD:     100_000.0,
		GlobalRealizedUSD:   2_000.0,
		GlobalUnrealizedUSD: -500.0,
		GlobalMarginUsedUSD: 4_000.0,
		GlobalLeverage:      1.5,
		TotalPositionCount:  5,
		TotalOpenOrders:     2,
		Accounts: []domain.AccountSummaryV1{
			{AccountID: "acct-1", VenueCount: 2, PositionCount: 3, EquityUSD: 50_000, RealizedPnlUSD: 1_000, UnrealizedPnlUSD: -200},
			{AccountID: "acct-2", VenueCount: 1, PositionCount: 2, EquityUSD: 50_000, RealizedPnlUSD: 1_000, UnrealizedPnlUSD: -300},
		},
		FillSummary: domain.FillSummaryV1{
			TotalTradeCount: 10, TotalVolumeTradedUSD: 200_000,
		},
	}
}
