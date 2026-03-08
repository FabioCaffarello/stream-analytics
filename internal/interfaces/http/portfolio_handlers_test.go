package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/core/portfolio/domain"
	portfolioports "github.com/market-raccoon/internal/core/portfolio/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// Stub readers
// ---------------------------------------------------------------------------

type stubPortfolioStateReader struct {
	states  []domain.PortfolioStateV1
	latest  domain.PortfolioStateV1
	err     *problem.Problem
	lastQ   portfolioports.PortfolioStateQuery
	lastAcc string
	lastVen string
	lastSym string
}

func (s *stubPortfolioStateReader) GetPortfolioStates(_ context.Context, q portfolioports.PortfolioStateQuery) ([]domain.PortfolioStateV1, *problem.Problem) {
	s.lastQ = q
	return s.states, s.err
}
func (s *stubPortfolioStateReader) GetLatestPortfolioState(_ context.Context, accountID, venue, sym string) (domain.PortfolioStateV1, *problem.Problem) {
	s.lastAcc = accountID
	s.lastVen = venue
	s.lastSym = sym
	return s.latest, s.err
}

type stubAccountSnapshotReader struct {
	snaps   []domain.AccountSnapshotV1
	latest  domain.AccountSnapshotV1
	err     *problem.Problem
	lastAcc string
}

func (s *stubAccountSnapshotReader) GetAccountSnapshots(_ context.Context, _ portfolioports.AccountSnapshotQuery) ([]domain.AccountSnapshotV1, *problem.Problem) {
	return s.snaps, s.err
}
func (s *stubAccountSnapshotReader) GetLatestAccountSnapshot(_ context.Context, accountID string) (domain.AccountSnapshotV1, *problem.Problem) {
	s.lastAcc = accountID
	return s.latest, s.err
}

type stubPortfolioSummaryReader struct {
	sums   []domain.PortfolioSummaryV1
	latest domain.PortfolioSummaryV1
	err    *problem.Problem
}

func (s *stubPortfolioSummaryReader) GetPortfolioSummaries(_ context.Context, _ portfolioports.PortfolioSummaryQuery) ([]domain.PortfolioSummaryV1, *problem.Problem) {
	return s.sums, s.err
}
func (s *stubPortfolioSummaryReader) GetLatestPortfolioSummary(_ context.Context) (domain.PortfolioSummaryV1, *problem.Problem) {
	return s.latest, s.err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestServerWithPortfolio(readers *PortfolioReaders) *Server {
	s := &Server{
		portfolioReaders: readers,
		logger:           slog.Default(),
	}
	mux := http.NewServeMux()
	if readers != nil {
		mux.HandleFunc("GET /api/v1/portfolio/state/latest", s.handleGetPortfolioStateLatest)
		mux.HandleFunc("GET /api/v1/portfolio/states", s.handleGetPortfolioStates)
		mux.HandleFunc("GET /api/v1/portfolio/account-snapshot/latest", s.handleGetAccountSnapshotLatest)
		mux.HandleFunc("GET /api/v1/portfolio/summary/latest", s.handleGetPortfolioSummaryLatest)
		mux.HandleFunc("GET /api/v1/portfolio/account-snapshots", s.handleGetAccountSnapshots)
		mux.HandleFunc("GET /api/v1/portfolio/summaries", s.handleGetPortfolioSummaries)
		mux.HandleFunc("GET /api/v1/portfolio/equity-curve", s.handleGetEquityCurve)
		mux.HandleFunc("GET /api/v1/portfolio/reconciliation", s.handleGetReconciliation)
	}
	s.mux = mux
	s.httpServer = &http.Server{Handler: mux}
	return s
}

func doPortfolioRequest(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, strings.NewReader(""))
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func doPortfolioRequestProto(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, strings.NewReader(""))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Accept", "application/x-protobuf")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/state/latest
// ---------------------------------------------------------------------------

func TestHandleGetPortfolioStateLatest_Success(t *testing.T) {
	stub := &stubPortfolioStateReader{
		latest: domain.PortfolioStateV1{
			StateID:   "state-1",
			AccountID: "acc-1",
			Venue:     "binance",
			EquityUSD: 10000.0,
		},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/state/latest?account_id=acc-1&venue=binance&symbol=BTCUSDT")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if stub.lastAcc != "acc-1" || stub.lastVen != "binance" || stub.lastSym != "BTCUSDT" {
		t.Fatalf("unexpected reader args: acc=%s ven=%s sym=%s", stub.lastAcc, stub.lastVen, stub.lastSym)
	}

	var got domain.PortfolioStateV1
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.StateID != "state-1" || got.EquityUSD != 10000.0 {
		t.Errorf("unexpected state: %+v", got)
	}
}

func TestHandleGetPortfolioStateLatest_MissingParams(t *testing.T) {
	stub := &stubPortfolioStateReader{}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stub})

	for _, path := range []string{
		"/api/v1/portfolio/state/latest",
		"/api/v1/portfolio/state/latest?account_id=acc-1",
		"/api/v1/portfolio/state/latest?account_id=acc-1&venue=binance",
		"/api/v1/portfolio/state/latest?venue=binance&symbol=BTCUSDT",
	} {
		rec := doPortfolioRequest(t, srv, path)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("path=%s: expected 400, got %d", path, rec.Code)
		}
	}
}

func TestHandleGetPortfolioStateLatest_ReaderUnavailable(t *testing.T) {
	srv := newTestServerWithPortfolio(&PortfolioReaders{})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/state/latest?account_id=acc-1&venue=binance&symbol=BTCUSDT")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestHandleGetPortfolioStateLatest_ReaderError(t *testing.T) {
	stub := &stubPortfolioStateReader{
		err: problem.New(problem.Unavailable, "db down"),
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/state/latest?account_id=acc-1&venue=binance&symbol=BTCUSDT")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleGetPortfolioStateLatest_ProtoNegotiation(t *testing.T) {
	stub := &stubPortfolioStateReader{
		latest: domain.PortfolioStateV1{StateID: "state-proto"},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stub})
	rec := doPortfolioRequestProto(t, srv, "/api/v1/portfolio/state/latest?account_id=a&venue=b&symbol=C")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/x-protobuf" {
		t.Errorf("expected proto content-type, got %s", ct)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/states
// ---------------------------------------------------------------------------

func TestHandleGetPortfolioStates_Success(t *testing.T) {
	stub := &stubPortfolioStateReader{
		states: []domain.PortfolioStateV1{
			{StateID: "s1", Venue: "binance"},
			{StateID: "s2", Venue: "bybit"},
		},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/states?account_id=acc-1&venue=binance&limit=50")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if stub.lastQ.AccountID != "acc-1" {
		t.Errorf("expected account_id=acc-1, got %s", stub.lastQ.AccountID)
	}
	if stub.lastQ.Venue != "binance" {
		t.Errorf("expected venue=binance, got %s", stub.lastQ.Venue)
	}
	if stub.lastQ.Limit != 50 {
		t.Errorf("expected limit=50, got %d", stub.lastQ.Limit)
	}

	var got []domain.PortfolioStateV1
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 states, got %d", len(got))
	}
}

func TestHandleGetPortfolioStates_DefaultLimit(t *testing.T) {
	stub := &stubPortfolioStateReader{}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stub})
	_ = doPortfolioRequest(t, srv, "/api/v1/portfolio/states?account_id=acc-1")
	if stub.lastQ.Limit != 100 {
		t.Errorf("expected default limit=100, got %d", stub.lastQ.Limit)
	}
}

func TestHandleGetPortfolioStates_MissingAccountID(t *testing.T) {
	stub := &stubPortfolioStateReader{}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/states")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleGetPortfolioStates_ReaderError(t *testing.T) {
	stub := &stubPortfolioStateReader{err: problem.New(problem.Unavailable, "timeout")}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/states?account_id=acc-1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/account-snapshot/latest
// ---------------------------------------------------------------------------

func TestHandleGetAccountSnapshotLatest_Success(t *testing.T) {
	stub := &stubAccountSnapshotReader{
		latest: domain.AccountSnapshotV1{
			SnapshotID:     "snap-1",
			AccountID:      "acc-1",
			TotalEquityUSD: 50000.0,
		},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Snapshots: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/account-snapshot/latest?account_id=acc-1")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if stub.lastAcc != "acc-1" {
		t.Errorf("expected account_id=acc-1, got %s", stub.lastAcc)
	}

	var got domain.AccountSnapshotV1
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SnapshotID != "snap-1" || got.TotalEquityUSD != 50000.0 {
		t.Errorf("unexpected snapshot: %+v", got)
	}
}

func TestHandleGetAccountSnapshotLatest_MissingAccountID(t *testing.T) {
	stub := &stubAccountSnapshotReader{}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Snapshots: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/account-snapshot/latest")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleGetAccountSnapshotLatest_ReaderUnavailable(t *testing.T) {
	srv := newTestServerWithPortfolio(&PortfolioReaders{})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/account-snapshot/latest?account_id=acc-1")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestHandleGetAccountSnapshotLatest_ReaderError(t *testing.T) {
	stub := &stubAccountSnapshotReader{err: problem.New(problem.Unavailable, "conn refused")}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Snapshots: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/account-snapshot/latest?account_id=acc-1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleGetAccountSnapshotLatest_ProtoNegotiation(t *testing.T) {
	stub := &stubAccountSnapshotReader{
		latest: domain.AccountSnapshotV1{SnapshotID: "snap-proto"},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Snapshots: stub})
	rec := doPortfolioRequestProto(t, srv, "/api/v1/portfolio/account-snapshot/latest?account_id=a")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/x-protobuf" {
		t.Errorf("expected proto content-type, got %s", ct)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/summary/latest
// ---------------------------------------------------------------------------

func TestHandleGetPortfolioSummaryLatest_Success(t *testing.T) {
	stub := &stubPortfolioSummaryReader{
		latest: domain.PortfolioSummaryV1{
			SummaryID:       "sum-1",
			GlobalEquityUSD: 100000.0,
		},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Summaries: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/summary/latest")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got domain.PortfolioSummaryV1
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SummaryID != "sum-1" || got.GlobalEquityUSD != 100000.0 {
		t.Errorf("unexpected summary: %+v", got)
	}
}

func TestHandleGetPortfolioSummaryLatest_ReaderUnavailable(t *testing.T) {
	srv := newTestServerWithPortfolio(&PortfolioReaders{})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/summary/latest")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestHandleGetPortfolioSummaryLatest_ReaderError(t *testing.T) {
	stub := &stubPortfolioSummaryReader{err: problem.New(problem.Unavailable, "shutdown")}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Summaries: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/summary/latest")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleGetPortfolioSummaryLatest_ProtoNegotiation(t *testing.T) {
	stub := &stubPortfolioSummaryReader{
		latest: domain.PortfolioSummaryV1{SummaryID: "sum-proto"},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Summaries: stub})
	rec := doPortfolioRequestProto(t, srv, "/api/v1/portfolio/summary/latest")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/x-protobuf" {
		t.Errorf("expected proto content-type, got %s", ct)
	}
}

// ---------------------------------------------------------------------------
// Nil readers (no PortfolioReaders configured at all)
// ---------------------------------------------------------------------------

func TestPortfolioHandlers_NilReaders(t *testing.T) {
	srv := newTestServerWithPortfolio(nil)
	// Routes not registered when readers=nil; should 404
	for _, path := range []string{
		"/api/v1/portfolio/state/latest?account_id=a&venue=b&symbol=c",
		"/api/v1/portfolio/states?account_id=a",
		"/api/v1/portfolio/account-snapshot/latest?account_id=a",
		"/api/v1/portfolio/summary/latest",
		"/api/v1/portfolio/account-snapshots?account_id=a",
		"/api/v1/portfolio/summaries",
		"/api/v1/portfolio/equity-curve",
		"/api/v1/portfolio/reconciliation?account_id=a",
	} {
		rec := doPortfolioRequest(t, srv, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("path=%s: expected 404 when routes not registered, got %d", path, rec.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/account-snapshots
// ---------------------------------------------------------------------------

func TestHandleGetAccountSnapshots_Success(t *testing.T) {
	stub := &stubAccountSnapshotReader{
		snaps: []domain.AccountSnapshotV1{
			{SnapshotID: "snap-1", AccountID: "acc-1", TotalEquityUSD: 10000},
			{SnapshotID: "snap-2", AccountID: "acc-1", TotalEquityUSD: 12000},
		},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Snapshots: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/account-snapshots?account_id=acc-1&from_ms=1000&to_ms=5000&limit=50")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []domain.AccountSnapshotV1
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 snapshots, got %d", len(got))
	}
}

func TestHandleGetAccountSnapshots_MissingAccountID(t *testing.T) {
	stub := &stubAccountSnapshotReader{}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Snapshots: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/account-snapshots")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleGetAccountSnapshots_ReaderError(t *testing.T) {
	stub := &stubAccountSnapshotReader{err: problem.New(problem.Unavailable, "timeout")}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Snapshots: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/account-snapshots?account_id=acc-1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/summaries
// ---------------------------------------------------------------------------

func TestHandleGetPortfolioSummaries_Success(t *testing.T) {
	stub := &stubPortfolioSummaryReader{
		sums: []domain.PortfolioSummaryV1{
			{SummaryID: "sum-1", GlobalEquityUSD: 50000},
			{SummaryID: "sum-2", GlobalEquityUSD: 55000},
		},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Summaries: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/summaries?from_ms=1000&limit=50")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []domain.PortfolioSummaryV1
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 summaries, got %d", len(got))
	}
}

func TestHandleGetPortfolioSummaries_ReaderError(t *testing.T) {
	stub := &stubPortfolioSummaryReader{err: problem.New(problem.Unavailable, "shutdown")}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Summaries: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/summaries")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/equity-curve
// ---------------------------------------------------------------------------

func TestHandleGetEquityCurve_AccountScope(t *testing.T) {
	stub := &stubAccountSnapshotReader{
		snaps: []domain.AccountSnapshotV1{
			{SnapshotID: "snap-1", AccountID: "acc-1", ProjectedAtMs: 1000, TotalEquityUSD: 10000},
			{SnapshotID: "snap-2", AccountID: "acc-1", ProjectedAtMs: 2000, TotalEquityUSD: 12000},
		},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Snapshots: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/equity-curve?account_id=acc-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []domain.EquityCurvePointV1
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 points, got %d", len(got))
	}
}

func TestHandleGetEquityCurve_GlobalScope(t *testing.T) {
	stub := &stubPortfolioSummaryReader{
		sums: []domain.PortfolioSummaryV1{
			{SummaryID: "sum-1", ProjectedAtMs: 1000, GlobalEquityUSD: 50000},
		},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{Summaries: stub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/equity-curve")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []domain.EquityCurvePointV1
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 point, got %d", len(got))
	}
}

func TestHandleGetEquityCurve_ReaderUnavailable(t *testing.T) {
	srv := newTestServerWithPortfolio(&PortfolioReaders{})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/equity-curve?account_id=acc-1")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/reconciliation
// ---------------------------------------------------------------------------

func TestHandleGetReconciliation_Success(t *testing.T) {
	stateStub := &stubPortfolioStateReader{
		states: []domain.PortfolioStateV1{
			{StateID: "s1", AccountID: "acc-1", Venue: "binance", ProjectedAtMs: 1000, RealizedPnlUSD: 100,
				Provenance: domain.ProjectionProvenanceV1{SourceExecutionSeq: 1}},
		},
	}
	snapStub := &stubAccountSnapshotReader{
		snaps: []domain.AccountSnapshotV1{
			{SnapshotID: "snap-1", AccountID: "acc-1", ProjectedAtMs: 1000, TotalRealizedUSD: 100},
		},
	}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stateStub, Snapshots: snapStub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/reconciliation?account_id=acc-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got domain.ReconciliationReportV1
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Healthy {
		t.Errorf("expected healthy report, got findings: %+v", got.Findings)
	}
}

func TestHandleGetReconciliation_MissingAccountID(t *testing.T) {
	stateStub := &stubPortfolioStateReader{}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stateStub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/reconciliation")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleGetReconciliation_ReaderUnavailable(t *testing.T) {
	srv := newTestServerWithPortfolio(&PortfolioReaders{})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/reconciliation?account_id=acc-1")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestHandleGetReconciliation_StateReaderError(t *testing.T) {
	stateStub := &stubPortfolioStateReader{err: problem.New(problem.Unavailable, "db down")}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stateStub})
	rec := doPortfolioRequest(t, srv, "/api/v1/portfolio/reconciliation?account_id=acc-1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Payload Budget Enforcement
// ---------------------------------------------------------------------------

func TestPayloadBudget_ClampLimit(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		defVal    int
		maxVal    int
		want      int
	}{
		{"zero uses default", 0, 100, 500, 100},
		{"negative uses default", -5, 100, 500, 100},
		{"within range", 50, 100, 500, 50},
		{"at max", 500, 100, 500, 500},
		{"exceeds max clamped", 9999, 100, 500, 500},
		{"default equals max", 0, 200, 200, 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := clampLimit(tc.requested, tc.defVal, tc.maxVal)
			if got != tc.want {
				t.Errorf("clampLimit(%d, %d, %d) = %d, want %d", tc.requested, tc.defVal, tc.maxVal, got, tc.want)
			}
		})
	}
}

func TestPayloadBudget_StatesEndpoint(t *testing.T) {
	stub := &stubPortfolioStateReader{}
	srv := newTestServerWithPortfolio(&PortfolioReaders{States: stub})

	// Request limit exceeding budget → should be clamped to MaxStatesLimit.
	_ = doPortfolioRequest(t, srv, "/api/v1/portfolio/states?account_id=acc-1&limit=9999")
	if stub.lastQ.Limit != MaxStatesLimit {
		t.Errorf("expected clamped limit=%d, got %d", MaxStatesLimit, stub.lastQ.Limit)
	}

	// Request within budget → should pass through.
	_ = doPortfolioRequest(t, srv, "/api/v1/portfolio/states?account_id=acc-1&limit=50")
	if stub.lastQ.Limit != 50 {
		t.Errorf("expected limit=50, got %d", stub.lastQ.Limit)
	}
}
