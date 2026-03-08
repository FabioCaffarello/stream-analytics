package httpserver

import (
	"net/http"
	"strconv"
	"time"

	portfolioapp "github.com/market-raccoon/internal/core/portfolio/app"
	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	portfolioports "github.com/market-raccoon/internal/core/portfolio/ports"
)

// Payload budget limits — maximum allowed limit per endpoint.
const (
	MaxStatesLimit      = 500
	MaxSnapshotsLimit   = 200
	MaxSummariesLimit   = 200
	MaxEquityCurveLimit = 1000
)

// clampLimit enforces payload budget: returns the requested limit clamped
// to [1, maxVal], defaulting to defaultVal when requested <= 0.
func clampLimit(requested, defaultVal, maxVal int) int {
	if requested <= 0 {
		return defaultVal
	}
	if requested > maxVal {
		return maxVal
	}
	return requested
}

// PortfolioReaders groups the portfolio read model readers injected into
// the HTTP server.  Each field is optional — nil means the endpoint
// returns 503 Service Unavailable.
type PortfolioReaders struct {
	States    portfolioports.PortfolioStateReader
	Snapshots portfolioports.AccountSnapshotReader
	Summaries portfolioports.PortfolioSummaryReader
}

// WithPortfolioReaders configures optional portfolio read-model API routes.
func WithPortfolioReaders(readers *PortfolioReaders) Option {
	return func(s *Server) {
		s.portfolioReaders = readers
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/state/latest
// ---------------------------------------------------------------------------

// handleGetPortfolioStateLatest returns the most recent venue-scoped
// portfolio state for a given account+venue+symbol triple.
//
// Query parameters:
//
//	account_id (required)
//	venue      (required)
//	symbol     (required)
func (s *Server) handleGetPortfolioStateLatest(w http.ResponseWriter, r *http.Request) {
	if s.portfolioReaders == nil || s.portfolioReaders.States == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "portfolio state reader not available"})
		return
	}

	accountID := r.URL.Query().Get("account_id")
	venue := r.URL.Query().Get("venue")
	symbol := r.URL.Query().Get("symbol")
	if accountID == "" || venue == "" || symbol == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id, venue, and symbol are required"})
		return
	}

	start := time.Now()
	state, p := s.portfolioReaders.States.GetLatestPortfolioState(r.Context(), accountID, venue, symbol)
	elapsed := time.Since(start)
	if p != nil {
		s.logger.Warn("portfolio state latest query failed", "err", p.Message, "elapsed_ms", elapsed.Milliseconds())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}

	s.logger.Debug("portfolio state latest served", "account_id", accountID, "venue", venue, "symbol", symbol, "elapsed_ms", elapsed.Milliseconds())
	writeResponse(w, r, http.StatusOK, "portfolio.state.latest", state)
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/states
// ---------------------------------------------------------------------------

// handleGetPortfolioStates returns venue-scoped portfolio states filtered
// by account_id (required), with optional venue, symbol, and limit filters.
//
// Query parameters:
//
//	account_id (required)
//	venue      (optional)
//	symbol     (optional)
//	limit      (optional, default 100)
func (s *Server) handleGetPortfolioStates(w http.ResponseWriter, r *http.Request) {
	if s.portfolioReaders == nil || s.portfolioReaders.States == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "portfolio state reader not available"})
		return
	}

	accountID := r.URL.Query().Get("account_id")
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}
	limit = clampLimit(limit, 100, MaxStatesLimit)

	q := portfolioports.PortfolioStateQuery{
		AccountID: accountID,
		Venue:     r.URL.Query().Get("venue"),
		Symbol:    r.URL.Query().Get("symbol"),
		Limit:     limit,
	}

	start := time.Now()
	states, p := s.portfolioReaders.States.GetPortfolioStates(r.Context(), q)
	elapsed := time.Since(start)
	if p != nil {
		s.logger.Warn("portfolio states query failed", "err", p.Message, "elapsed_ms", elapsed.Milliseconds())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}

	s.logger.Debug("portfolio states served", "account_id", accountID, "count", len(states), "elapsed_ms", elapsed.Milliseconds())
	writeResponse(w, r, http.StatusOK, "portfolio.states", states)
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/account-snapshot/latest
// ---------------------------------------------------------------------------

// handleGetAccountSnapshotLatest returns the most recent account-level
// portfolio snapshot.
//
// Query parameters:
//
//	account_id (required)
func (s *Server) handleGetAccountSnapshotLatest(w http.ResponseWriter, r *http.Request) {
	if s.portfolioReaders == nil || s.portfolioReaders.Snapshots == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "account snapshot reader not available"})
		return
	}

	accountID := r.URL.Query().Get("account_id")
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
		return
	}

	start := time.Now()
	snap, p := s.portfolioReaders.Snapshots.GetLatestAccountSnapshot(r.Context(), accountID)
	elapsed := time.Since(start)
	if p != nil {
		s.logger.Warn("account snapshot latest query failed", "err", p.Message, "elapsed_ms", elapsed.Milliseconds())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}

	s.logger.Debug("account snapshot latest served", "account_id", accountID, "elapsed_ms", elapsed.Milliseconds())
	writeResponse(w, r, http.StatusOK, "portfolio.account_snapshot.latest", snap)
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/summary/latest
// ---------------------------------------------------------------------------

// handleGetPortfolioSummaryLatest returns the most recent global portfolio
// summary.  No parameters required — summaries are always global.
func (s *Server) handleGetPortfolioSummaryLatest(w http.ResponseWriter, r *http.Request) {
	if s.portfolioReaders == nil || s.portfolioReaders.Summaries == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "portfolio summary reader not available"})
		return
	}

	start := time.Now()
	summary, p := s.portfolioReaders.Summaries.GetLatestPortfolioSummary(r.Context())
	elapsed := time.Since(start)
	if p != nil {
		s.logger.Warn("portfolio summary latest query failed", "err", p.Message, "elapsed_ms", elapsed.Milliseconds())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}

	s.logger.Debug("portfolio summary latest served", "elapsed_ms", elapsed.Milliseconds())
	writeResponse(w, r, http.StatusOK, "portfolio.summary.latest", summary)
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/account-snapshots
// ---------------------------------------------------------------------------

// handleGetAccountSnapshots returns historical account snapshots with
// time-range filtering.
//
// Query parameters:
//
//	account_id (required)
//	from_ms    (optional)
//	to_ms      (optional)
//	limit      (optional, default 100)
func (s *Server) handleGetAccountSnapshots(w http.ResponseWriter, r *http.Request) {
	if s.portfolioReaders == nil || s.portfolioReaders.Snapshots == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "account snapshot reader not available"})
		return
	}

	accountID := r.URL.Query().Get("account_id")
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
		return
	}

	q := portfolioports.AccountSnapshotQuery{AccountID: accountID}
	if v := r.URL.Query().Get("from_ms"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.FromMs = ms
		}
	}
	if v := r.URL.Query().Get("to_ms"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.ToMs = ms
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Limit = n
		}
	}
	q.Limit = clampLimit(q.Limit, 100, MaxSnapshotsLimit)

	start := time.Now()
	snaps, p := s.portfolioReaders.Snapshots.GetAccountSnapshots(r.Context(), q)
	elapsed := time.Since(start)
	if p != nil {
		s.logger.Warn("account snapshots query failed", "err", p.Message, "elapsed_ms", elapsed.Milliseconds())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}

	s.logger.Debug("account snapshots served", "account_id", accountID, "count", len(snaps), "elapsed_ms", elapsed.Milliseconds())
	writeResponse(w, r, http.StatusOK, "portfolio.account_snapshots", snaps)
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/summaries
// ---------------------------------------------------------------------------

// handleGetPortfolioSummaries returns historical portfolio summaries with
// time-range filtering.
//
// Query parameters:
//
//	from_ms (optional)
//	to_ms   (optional)
//	limit   (optional, default 100)
func (s *Server) handleGetPortfolioSummaries(w http.ResponseWriter, r *http.Request) {
	if s.portfolioReaders == nil || s.portfolioReaders.Summaries == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "portfolio summary reader not available"})
		return
	}

	q := portfolioports.PortfolioSummaryQuery{}
	if v := r.URL.Query().Get("from_ms"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.FromMs = ms
		}
	}
	if v := r.URL.Query().Get("to_ms"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.ToMs = ms
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Limit = n
		}
	}
	q.Limit = clampLimit(q.Limit, 100, MaxSummariesLimit)

	start := time.Now()
	sums, p := s.portfolioReaders.Summaries.GetPortfolioSummaries(r.Context(), q)
	elapsed := time.Since(start)
	if p != nil {
		s.logger.Warn("portfolio summaries query failed", "err", p.Message, "elapsed_ms", elapsed.Milliseconds())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}

	s.logger.Debug("portfolio summaries served", "count", len(sums), "elapsed_ms", elapsed.Milliseconds())
	writeResponse(w, r, http.StatusOK, "portfolio.summaries", sums)
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/equity-curve
// ---------------------------------------------------------------------------

// handleGetEquityCurve returns the equity curve for an account (or global
// if no account_id is provided).
//
// Query parameters:
//
//	account_id (optional — omit for global curve from summaries)
//	from_ms    (optional)
//	to_ms      (optional)
//	limit      (optional, default 500)
func (s *Server) handleGetEquityCurve(w http.ResponseWriter, r *http.Request) {
	if s.portfolioReaders == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "portfolio readers not available"})
		return
	}

	accountID := r.URL.Query().Get("account_id")
	limit := 500
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	limit = clampLimit(limit, 500, MaxEquityCurveLimit)

	var fromMs, toMs int64
	if v := r.URL.Query().Get("from_ms"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			fromMs = ms
		}
	}
	if v := r.URL.Query().Get("to_ms"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			toMs = ms
		}
	}

	start := time.Now()

	if accountID != "" {
		if s.portfolioReaders.Snapshots == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "account snapshot reader not available"})
			return
		}
		q := portfolioports.AccountSnapshotQuery{
			AccountID: accountID,
			FromMs:    fromMs,
			ToMs:      toMs,
			Limit:     limit,
		}
		snaps, p := s.portfolioReaders.Snapshots.GetAccountSnapshots(r.Context(), q)
		elapsed := time.Since(start)
		if p != nil {
			s.logger.Warn("equity curve snapshot query failed", "err", p.Message, "elapsed_ms", elapsed.Milliseconds())
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
			return
		}
		curve := portfolioapp.BuildEquityCurve(snaps)
		s.logger.Debug("equity curve served", "account_id", accountID, "points", len(curve), "elapsed_ms", elapsed.Milliseconds())
		writeResponse(w, r, http.StatusOK, "portfolio.equity_curve", curve)
		return
	}

	// Global equity curve from summaries
	if s.portfolioReaders.Summaries == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "portfolio summary reader not available"})
		return
	}
	q := portfolioports.PortfolioSummaryQuery{
		FromMs: fromMs,
		ToMs:   toMs,
		Limit:  limit,
	}
	sums, p := s.portfolioReaders.Summaries.GetPortfolioSummaries(r.Context(), q)
	elapsed := time.Since(start)
	if p != nil {
		s.logger.Warn("equity curve summary query failed", "err", p.Message, "elapsed_ms", elapsed.Milliseconds())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	curve := portfolioapp.BuildEquityCurveFromSummaries(sums)
	s.logger.Debug("equity curve (global) served", "points", len(curve), "elapsed_ms", elapsed.Milliseconds())
	writeResponse(w, r, http.StatusOK, "portfolio.equity_curve", curve)
}

// ---------------------------------------------------------------------------
// GET /api/v1/portfolio/reconciliation
// ---------------------------------------------------------------------------

// handleGetReconciliation runs a reconciliation check for the given account.
//
// Query parameters:
//
//	account_id (required)
//	from_ms    (optional)
//	to_ms      (optional)
func (s *Server) handleGetReconciliation(w http.ResponseWriter, r *http.Request) {
	if s.portfolioReaders == nil || s.portfolioReaders.States == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "portfolio state reader not available"})
		return
	}

	accountID := r.URL.Query().Get("account_id")
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
		return
	}

	query := portfoliodomain.ReconciliationQuery{AccountID: accountID}
	if v := r.URL.Query().Get("from_ms"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			query.FromMs = ms
		}
	}
	if v := r.URL.Query().Get("to_ms"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			query.ToMs = ms
		}
	}

	start := time.Now()

	stateQ := portfolioports.PortfolioStateQuery{
		AccountID: accountID,
		Limit:     1000,
	}
	states, p := s.portfolioReaders.States.GetPortfolioStates(r.Context(), stateQ)
	if p != nil {
		s.logger.Warn("reconciliation state query failed", "err", p.Message)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}

	var snaps []portfoliodomain.AccountSnapshotV1
	if s.portfolioReaders.Snapshots != nil {
		snapQ := portfolioports.AccountSnapshotQuery{
			AccountID: accountID,
			FromMs:    query.FromMs,
			ToMs:      query.ToMs,
			Limit:     1000,
		}
		snaps, p = s.portfolioReaders.Snapshots.GetAccountSnapshots(r.Context(), snapQ)
		if p != nil {
			s.logger.Warn("reconciliation snapshot query failed", "err", p.Message)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
			return
		}
	}

	nowMs := time.Now().UnixMilli()
	report := portfolioapp.CheckReconciliation(states, snaps, query, nowMs)

	elapsed := time.Since(start)
	s.logger.Debug("reconciliation report served",
		"account_id", accountID,
		"findings", len(report.Findings),
		"healthy", report.Healthy,
		"elapsed_ms", elapsed.Milliseconds(),
	)
	writeResponse(w, r, http.StatusOK, "portfolio.reconciliation", report)
}
