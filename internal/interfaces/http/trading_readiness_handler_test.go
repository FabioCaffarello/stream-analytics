package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	portfolioports "github.com/market-raccoon/internal/core/portfolio/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

func testLogger() *slog.Logger { return slog.Default() }

// --- fakes ---

type fakeControlPlaneForReadiness struct {
	snap executiondomain.ControlSnapshot
}

func (f *fakeControlPlaneForReadiness) Snapshot() executiondomain.ControlSnapshot { return f.snap }
func (f *fakeControlPlaneForReadiness) Apply(_ executiondomain.ControlDirective) *problem.Problem {
	return nil
}

type fakeSummaryReaderForReadiness struct {
	summary portfoliodomain.PortfolioSummaryV1
	err     *problem.Problem
}

func (f *fakeSummaryReaderForReadiness) GetLatestPortfolioSummary(_ context.Context) (portfoliodomain.PortfolioSummaryV1, *problem.Problem) {
	return f.summary, f.err
}

func (f *fakeSummaryReaderForReadiness) GetPortfolioSummaries(_ context.Context, _ portfolioports.PortfolioSummaryQuery) ([]portfoliodomain.PortfolioSummaryV1, *problem.Problem) {
	return nil, nil
}

type fakeSnapshotReaderForReadiness struct {
	snaps map[string]portfoliodomain.AccountSnapshotV1
}

func (f *fakeSnapshotReaderForReadiness) GetLatestAccountSnapshot(_ context.Context, accountID string) (portfoliodomain.AccountSnapshotV1, *problem.Problem) {
	if snap, ok := f.snaps[accountID]; ok {
		return snap, nil
	}
	return portfoliodomain.AccountSnapshotV1{}, problem.New(problem.NotFound, "not found")
}

func (f *fakeSnapshotReaderForReadiness) GetAccountSnapshots(_ context.Context, _ portfolioports.AccountSnapshotQuery) ([]portfoliodomain.AccountSnapshotV1, *problem.Problem) {
	return nil, nil
}

// --- tests ---

func TestHandleGetTradingReadiness_Active(t *testing.T) {
	cp := &fakeControlPlaneForReadiness{
		snap: executiondomain.ControlSnapshot{
			State:       executiondomain.ControlStateActive,
			UpdatedAtMs: 1700000000000,
		},
	}
	summaryReader := &fakeSummaryReaderForReadiness{
		summary: portfoliodomain.PortfolioSummaryV1{
			SummaryID: "sum-1",
			Accounts: []portfoliodomain.AccountSummaryV1{
				{AccountID: "acc-1", VenueCount: 1, PositionCount: 3, EquityUSD: 10000},
			},
		},
	}
	snapshotReader := &fakeSnapshotReaderForReadiness{
		snaps: map[string]portfoliodomain.AccountSnapshotV1{
			"acc-1": {
				SnapshotID:    "snap-1",
				AccountID:     "acc-1",
				ProjectedAtMs: 1700000000000,
				Venues: []portfoliodomain.VenuePositionV1{
					{Venue: "binance", EquityUSD: 10000, Positions: []portfoliodomain.PositionV1{
						{Venue: "binance", Symbol: "BTCUSDT", Side: "long"},
					}},
				},
			},
		},
	}

	s := &Server{
		logger:       testLogger(),
		controlPlane: cp,
		portfolioReaders: &PortfolioReaders{
			Summaries: summaryReader,
			Snapshots: snapshotReader,
		},
	}

	req := httptest.NewRequest("GET", "/api/v1/trading/readiness", nil)
	w := httptest.NewRecorder()
	s.handleGetTradingReadiness(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result executiondomain.TradingReadinessV1
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if result.ControlPlane.State != "active" {
		t.Errorf("expected state=active, got %s", result.ControlPlane.State)
	}
	if len(result.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(result.Accounts))
	}
	if result.Accounts[0].AccountID != "acc-1" {
		t.Errorf("expected acc-1, got %s", result.Accounts[0].AccountID)
	}
	if len(result.Accounts[0].Venues) != 1 {
		t.Fatalf("expected 1 venue, got %d", len(result.Accounts[0].Venues))
	}
	vr := result.Accounts[0].Venues[0]
	if vr.TradingStatus != executiondomain.TradingStatusEnabled {
		t.Errorf("expected enabled, got %s", vr.TradingStatus)
	}
	if vr.Venue != "binance" {
		t.Errorf("expected binance, got %s", vr.Venue)
	}
}

func TestHandleGetTradingReadiness_Halted(t *testing.T) {
	cp := &fakeControlPlaneForReadiness{
		snap: executiondomain.ControlSnapshot{
			State:       executiondomain.ControlStateHalted,
			UpdatedAtMs: 1700000000000,
		},
	}

	s := &Server{
		logger:       testLogger(),
		controlPlane: cp,
	}

	req := httptest.NewRequest("GET", "/api/v1/trading/readiness", nil)
	w := httptest.NewRecorder()
	s.handleGetTradingReadiness(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result executiondomain.TradingReadinessV1
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result.ControlPlane.State != "halted" {
		t.Errorf("expected halted, got %s", result.ControlPlane.State)
	}
	foundHalted := false
	for _, f := range result.SafetyFlags {
		if f == "halted" {
			foundHalted = true
		}
	}
	if !foundHalted {
		t.Error("expected halted in safety flags")
	}
}

func TestHandleGetTradingReadiness_NoControlPlane(t *testing.T) {
	s := &Server{
		logger: testLogger(),
		portfolioReaders: &PortfolioReaders{
			Summaries: &fakeSummaryReaderForReadiness{
				summary: portfoliodomain.PortfolioSummaryV1{
					SummaryID: "sum-1",
					Accounts:  []portfoliodomain.AccountSummaryV1{},
				},
			},
		},
	}

	req := httptest.NewRequest("GET", "/api/v1/trading/readiness", nil)
	w := httptest.NewRecorder()
	s.handleGetTradingReadiness(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result executiondomain.TradingReadinessV1
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result.ControlPlane.State != "unknown" {
		t.Errorf("expected unknown, got %s", result.ControlPlane.State)
	}
	foundFlag := false
	for _, f := range result.SafetyFlags {
		if f == "control_plane_unavailable" {
			foundFlag = true
		}
	}
	if !foundFlag {
		t.Error("expected control_plane_unavailable in safety flags")
	}
}

func TestHandleGetTradingReadiness_VenueRestricted(t *testing.T) {
	cp := &fakeControlPlaneForReadiness{
		snap: executiondomain.ControlSnapshot{
			State: executiondomain.ControlStateActive,
			AllowlistOverrides: &executiondomain.AllowlistOverride{
				RestrictVenues: map[string]struct{}{"binance": {}},
			},
			UpdatedAtMs: 1700000000000,
		},
	}
	snapshotReader := &fakeSnapshotReaderForReadiness{
		snaps: map[string]portfoliodomain.AccountSnapshotV1{
			"acc-1": {
				SnapshotID:    "snap-1",
				AccountID:     "acc-1",
				ProjectedAtMs: 1700000000000,
				Venues: []portfoliodomain.VenuePositionV1{
					{Venue: "binance", EquityUSD: 8000},
					{Venue: "bybit", EquityUSD: 5000},
				},
			},
		},
	}
	summaryReader := &fakeSummaryReaderForReadiness{
		summary: portfoliodomain.PortfolioSummaryV1{
			SummaryID: "sum-1",
			Accounts: []portfoliodomain.AccountSummaryV1{
				{AccountID: "acc-1", EquityUSD: 13000, PositionCount: 4},
			},
		},
	}

	s := &Server{
		logger:       testLogger(),
		controlPlane: cp,
		portfolioReaders: &PortfolioReaders{
			Summaries: summaryReader,
			Snapshots: snapshotReader,
		},
	}

	req := httptest.NewRequest("GET", "/api/v1/trading/readiness", nil)
	w := httptest.NewRecorder()
	s.handleGetTradingReadiness(w, req)

	var result executiondomain.TradingReadinessV1
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(result.Accounts) != 1 || len(result.Accounts[0].Venues) != 2 {
		t.Fatalf("expected 1 account with 2 venues, got %d accounts", len(result.Accounts))
	}

	// binance is in allowlist → not restricted
	binance := result.Accounts[0].Venues[0]
	if binance.Restricted {
		t.Error("binance should NOT be restricted (it's in the allowlist)")
	}

	// bybit is NOT in allowlist → restricted
	bybit := result.Accounts[0].Venues[1]
	if !bybit.Restricted {
		t.Error("bybit should be restricted (not in allowlist)")
	}
	if bybit.TradingStatus != executiondomain.TradingStatusDegraded {
		t.Errorf("bybit should be degraded, got %s", bybit.TradingStatus)
	}
}

func TestHandleGetTradingReadiness_StalenessThresholdIncluded(t *testing.T) {
	cp := &fakeControlPlaneForReadiness{
		snap: executiondomain.ControlSnapshot{
			State:       executiondomain.ControlStateActive,
			UpdatedAtMs: 1700000000000,
		},
	}

	s := &Server{
		logger:       testLogger(),
		controlPlane: cp,
	}

	req := httptest.NewRequest("GET", "/api/v1/trading/readiness", nil)
	w := httptest.NewRecorder()
	s.handleGetTradingReadiness(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result executiondomain.TradingReadinessV1
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result.StalenessThresholdMs != StalenessThresholdMs {
		t.Errorf("expected staleness_threshold_ms=%d, got %d", StalenessThresholdMs, result.StalenessThresholdMs)
	}
}
