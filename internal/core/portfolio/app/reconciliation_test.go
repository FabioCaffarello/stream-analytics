package app

import (
	"math"
	"testing"

	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
)

func TestBuildEquityCurve_Empty(t *testing.T) {
	curve := BuildEquityCurve(nil)
	if curve != nil {
		t.Errorf("expected nil for empty input, got %d points", len(curve))
	}
}

func TestBuildEquityCurve_SinglePoint(t *testing.T) {
	snaps := []portfoliodomain.AccountSnapshotV1{
		{ProjectedAtMs: 1000, TotalEquityUSD: 10000, TotalRealizedUSD: 100, TotalUnrealizedUSD: 50, TotalMarginUsedUSD: 500,
			Venues: []portfoliodomain.VenuePositionV1{{Positions: []portfoliodomain.PositionV1{{Symbol: "BTCUSDT"}}}}},
	}
	curve := BuildEquityCurve(snaps)
	if len(curve) != 1 {
		t.Fatalf("expected 1 point, got %d", len(curve))
	}
	if curve[0].TimestampMs != 1000 {
		t.Errorf("expected ts=1000, got %d", curve[0].TimestampMs)
	}
	if curve[0].EquityUSD != 10000 {
		t.Errorf("expected equity=10000, got %f", curve[0].EquityUSD)
	}
	if curve[0].DrawdownPct != 0 {
		t.Errorf("expected drawdown=0, got %f", curve[0].DrawdownPct)
	}
	if curve[0].PositionCount != 1 {
		t.Errorf("expected position_count=1, got %d", curve[0].PositionCount)
	}
}

func TestBuildEquityCurve_Drawdown(t *testing.T) {
	snaps := []portfoliodomain.AccountSnapshotV1{
		{ProjectedAtMs: 1000, TotalEquityUSD: 10000},
		{ProjectedAtMs: 2000, TotalEquityUSD: 12000},
		{ProjectedAtMs: 3000, TotalEquityUSD: 9000},
	}
	curve := BuildEquityCurve(snaps)
	if len(curve) != 3 {
		t.Fatalf("expected 3 points, got %d", len(curve))
	}
	// Peak is 12000 at t=2000; drawdown at t=3000 is (12000-9000)/12000 = 25%
	if curve[1].DrawdownPct != 0 {
		t.Errorf("expected 0 drawdown at peak, got %f", curve[1].DrawdownPct)
	}
	if math.Abs(curve[2].DrawdownPct-25.0) > 0.01 {
		t.Errorf("expected ~25%% drawdown, got %f", curve[2].DrawdownPct)
	}
}

func TestBuildEquityCurve_SortsInput(t *testing.T) {
	snaps := []portfoliodomain.AccountSnapshotV1{
		{ProjectedAtMs: 3000, TotalEquityUSD: 9000},
		{ProjectedAtMs: 1000, TotalEquityUSD: 10000},
		{ProjectedAtMs: 2000, TotalEquityUSD: 12000},
	}
	curve := BuildEquityCurve(snaps)
	if curve[0].TimestampMs != 1000 || curve[1].TimestampMs != 2000 || curve[2].TimestampMs != 3000 {
		t.Errorf("curve not sorted: %d, %d, %d", curve[0].TimestampMs, curve[1].TimestampMs, curve[2].TimestampMs)
	}
}

func TestBuildEquityCurveFromSummaries_Empty(t *testing.T) {
	curve := BuildEquityCurveFromSummaries(nil)
	if curve != nil {
		t.Errorf("expected nil for empty input, got %d points", len(curve))
	}
}

func TestBuildEquityCurveFromSummaries_Basic(t *testing.T) {
	sums := []portfoliodomain.PortfolioSummaryV1{
		{ProjectedAtMs: 1000, GlobalEquityUSD: 50000, TotalPositionCount: 3},
		{ProjectedAtMs: 2000, GlobalEquityUSD: 48000, TotalPositionCount: 2},
	}
	curve := BuildEquityCurveFromSummaries(sums)
	if len(curve) != 2 {
		t.Fatalf("expected 2 points, got %d", len(curve))
	}
	if curve[0].PositionCount != 3 {
		t.Errorf("expected 3 positions, got %d", curve[0].PositionCount)
	}
	// Drawdown at t=2000: (50000-48000)/50000 = 4%
	if math.Abs(curve[1].DrawdownPct-4.0) > 0.01 {
		t.Errorf("expected ~4%% drawdown, got %f", curve[1].DrawdownPct)
	}
}

func TestCheckReconciliation_Healthy(t *testing.T) {
	states := []portfoliodomain.PortfolioStateV1{
		{
			StateID: "s1", AccountID: "acc-1", Venue: "binance",
			ProjectedAtMs: 1000, RealizedPnlUSD: 100,
			Provenance: portfoliodomain.ProjectionProvenanceV1{SourceExecutionSeq: 1},
		},
		{
			StateID: "s2", AccountID: "acc-1", Venue: "binance",
			ProjectedAtMs: 2000, RealizedPnlUSD: 200,
			Provenance: portfoliodomain.ProjectionProvenanceV1{SourceExecutionSeq: 2},
		},
	}
	snaps := []portfoliodomain.AccountSnapshotV1{
		{SnapshotID: "snap-1", AccountID: "acc-1", ProjectedAtMs: 2000, TotalEquityUSD: 10000, TotalRealizedUSD: 300},
	}
	query := portfoliodomain.ReconciliationQuery{AccountID: "acc-1"}
	report := CheckReconciliation(states, snaps, query, 3000)

	if !report.Healthy {
		t.Errorf("expected healthy report, got findings: %+v", report.Findings)
	}
	if report.TotalStates != 2 {
		t.Errorf("expected 2 states, got %d", report.TotalStates)
	}
}

func TestCheckReconciliation_SeqGap(t *testing.T) {
	states := []portfoliodomain.PortfolioStateV1{
		{
			StateID: "s1", AccountID: "acc-1", Venue: "binance",
			ProjectedAtMs: 1000,
			Provenance:    portfoliodomain.ProjectionProvenanceV1{SourceExecutionSeq: 1},
		},
		{
			StateID: "s2", AccountID: "acc-1", Venue: "binance",
			ProjectedAtMs: 2000,
			Provenance:    portfoliodomain.ProjectionProvenanceV1{SourceExecutionSeq: 5},
		},
	}
	query := portfoliodomain.ReconciliationQuery{AccountID: "acc-1"}
	report := CheckReconciliation(states, nil, query, 3000)

	found := false
	for _, f := range report.Findings {
		if f.Kind == portfoliodomain.FindingSeqGap {
			found = true
			if f.ExpectedSeq != 2 || f.ActualSeq != 5 {
				t.Errorf("expected gap 2→5, got %d→%d", f.ExpectedSeq, f.ActualSeq)
			}
		}
	}
	if !found {
		t.Errorf("expected seq_gap finding, got: %+v", report.Findings)
	}
}

func TestCheckReconciliation_EquityDrift(t *testing.T) {
	snaps := []portfoliodomain.AccountSnapshotV1{
		{SnapshotID: "snap-1", AccountID: "acc-1", ProjectedAtMs: 1000, TotalEquityUSD: 10000},
		{SnapshotID: "snap-2", AccountID: "acc-1", ProjectedAtMs: 2000, TotalEquityUSD: 4000},
	}
	query := portfoliodomain.ReconciliationQuery{AccountID: "acc-1"}
	report := CheckReconciliation(nil, snaps, query, 3000)

	found := false
	for _, f := range report.Findings {
		if f.Kind == portfoliodomain.FindingEquityDrift {
			found = true
			if f.Severity != portfoliodomain.SeverityError {
				t.Errorf("expected error severity for >50%% drift, got %s", f.Severity)
			}
		}
	}
	if !found {
		t.Errorf("expected equity_drift finding, got: %+v", report.Findings)
	}
}

func TestCheckReconciliation_StaleProjection(t *testing.T) {
	nowMs := int64(1_000_000)
	states := []portfoliodomain.PortfolioStateV1{
		{
			StateID: "s1", AccountID: "acc-1", Venue: "binance",
			ProjectedAtMs: nowMs,
			Provenance:    portfoliodomain.ProjectionProvenanceV1{SourceExecutionSeq: 2},
		},
		{
			StateID: "s2", AccountID: "acc-1", Venue: "bybit",
			ProjectedAtMs: nowMs - 400_000, // 400s old, threshold is 300s
			Provenance:    portfoliodomain.ProjectionProvenanceV1{SourceExecutionSeq: 1},
		},
	}
	query := portfoliodomain.ReconciliationQuery{AccountID: "acc-1"}
	report := CheckReconciliation(states, nil, query, nowMs)

	found := false
	for _, f := range report.Findings {
		if f.Kind == portfoliodomain.FindingStaleProjection {
			found = true
			if f.Venue != "bybit" {
				t.Errorf("expected stale venue=bybit, got %s", f.Venue)
			}
		}
	}
	if !found {
		t.Errorf("expected stale_projection finding, got: %+v", report.Findings)
	}
}

func TestCheckReconciliation_PnLMismatch(t *testing.T) {
	states := []portfoliodomain.PortfolioStateV1{
		{
			StateID: "s1", AccountID: "acc-1", Venue: "binance",
			ProjectedAtMs: 2000, RealizedPnlUSD: 500,
			Provenance: portfoliodomain.ProjectionProvenanceV1{SourceExecutionSeq: 1},
		},
	}
	snaps := []portfoliodomain.AccountSnapshotV1{
		{SnapshotID: "snap-1", AccountID: "acc-1", ProjectedAtMs: 2000,
			TotalEquityUSD: 10000, TotalRealizedUSD: 100},
	}
	query := portfoliodomain.ReconciliationQuery{AccountID: "acc-1"}
	report := CheckReconciliation(states, snaps, query, 3000)

	found := false
	for _, f := range report.Findings {
		if f.Kind == portfoliodomain.FindingPnLMismatch {
			found = true
			if math.Abs(f.DriftUSD-400) > 0.01 {
				t.Errorf("expected drift=400, got %f", f.DriftUSD)
			}
		}
	}
	if !found {
		t.Errorf("expected pnl_mismatch finding, got: %+v", report.Findings)
	}
	if report.Healthy {
		t.Errorf("expected unhealthy report for PnL mismatch > 1.0 USD")
	}
}

func TestCheckReconciliation_EmptyInputs(t *testing.T) {
	query := portfoliodomain.ReconciliationQuery{AccountID: "acc-1"}
	report := CheckReconciliation(nil, nil, query, 1000)

	if !report.Healthy {
		t.Errorf("expected healthy with no data")
	}
	if report.TotalStates != 0 || report.TotalSnapshots != 0 {
		t.Errorf("expected 0 totals, got states=%d snapshots=%d", report.TotalStates, report.TotalSnapshots)
	}
	if report.ReportID == "" {
		t.Errorf("expected non-empty report ID")
	}
}
