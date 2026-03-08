package timescale_test

import (
	"context"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/timescale"
	domain "github.com/market-raccoon/internal/core/portfolio/domain"
)

func TestPgPortfolioStateWriter_Upsert_Success(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewPgPortfolioStateWriterWithExecutor(exec)

	if p := w.UpsertPortfolioState(context.Background(), testPortfolioState()); p != nil {
		t.Fatalf("upsert: %v", p)
	}
	if !strings.Contains(exec.lastQuery, "portfolio_state") {
		t.Fatalf("query=%q missing target table", exec.lastQuery)
	}
	if len(exec.lastArgs) != 14 {
		t.Fatalf("args len=%d want=14", len(exec.lastArgs))
	}
}

func TestPgPortfolioStateWriter_Upsert_DuplicateIdempotent(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 0}
	w := timescale.NewPgPortfolioStateWriterWithExecutor(exec)

	if p := w.UpsertPortfolioState(context.Background(), testPortfolioState()); p != nil {
		t.Fatalf("upsert: %v", p)
	}
}

func TestPgPortfolioStateWriter_Upsert_ValidationFails(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewPgPortfolioStateWriterWithExecutor(exec)

	s := testPortfolioState()
	s.StateID = "" // invalid

	if p := w.UpsertPortfolioState(context.Background(), s); p == nil {
		t.Fatal("expected validation error, got nil")
	}
	if exec.lastQuery != "" {
		t.Fatal("should not have executed any query")
	}
}

func TestPgPortfolioStateWriter_Upsert_NilWriter(t *testing.T) {
	var w *timescale.PgPortfolioStateWriter
	if p := w.UpsertPortfolioState(context.Background(), testPortfolioState()); p == nil {
		t.Fatal("expected problem, got nil")
	}
}

func TestPgPortfolioStateWriter_Upsert_ConnectionError(t *testing.T) {
	exec := &fakeSQLExecutor{
		p: testUnavailableProblem(),
	}
	w := timescale.NewPgPortfolioStateWriterWithExecutor(exec)

	p := w.UpsertPortfolioState(context.Background(), testPortfolioState())
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
}

func testPortfolioState() domain.PortfolioStateV1 {
	return domain.PortfolioStateV1{
		StateID:          "state-001",
		Scope:            domain.PortfolioScopeVenueAccount,
		AccountID:        "acct-1",
		Venue:            "binance",
		ProjectedAtMs:    1_710_000_000_000,
		EquityUSD:        50_000.0,
		RealizedPnlUSD:   1_000.0,
		UnrealizedPnlUSD: -200.0,
		Balances: []domain.BalanceV1{
			{Asset: "USDT", Total: 50000, Available: 48000, Locked: 2000},
		},
		Positions: []domain.PositionV1{
			{Venue: "binance", Symbol: "BTCUSDT", Quantity: 0.5, AvgEntryPrice: 60000, NotionalUSD: 30000, Side: "long", TradeCount: 3, VolumeTradedUSD: 90000, LastFillMs: 1_710_000_000_000},
		},
		Exposures: []domain.ExposureV1{
			{Symbol: "BTCUSDT", NetQty: 0.5, GrossNotionalUSD: 30000, Leverage: 1.67},
		},
		Risk: domain.RiskSnapshotV1{
			MarginUsedUSD: 2000, MarginAvailableUSD: 48000, MaintenanceMarginUSD: 1000,
		},
		FillSummary: domain.FillSummaryV1{
			TotalTradeCount: 3, TotalVolumeTradedUSD: 90000, WinCount: 2, LossCount: 1,
			LargestWinUSD: 500, LargestLossUSD: 200, TurnoverUSD: 90000,
		},
		Provenance: domain.ProjectionProvenanceV1{
			SourceExecutionEventID: "exec-001",
			SourceExecutionSeq:     42,
			CorrelationID:          "corr-001",
			TraceID:                "trace-001",
			ProjectorVersion:       "v1.0.0",
		},
	}
}
