package httpserver

import (
	"context"
	"net/http"

	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/result"
)

// InsightsSnapshotter abstracts the snapshot query surface of the insights
// bounded context. The HTTP layer depends on this interface rather than
// importing the concrete InsightsService so that tests stay lightweight.
type InsightsSnapshotter interface {
	SnapshotSessionVolumeProfile(ctx context.Context, venue, instrument, anchorLabel string) result.Result[insightsdomain.SessionVolumeProfileV1]
	SnapshotTPOProfile(ctx context.Context, venue, instrument, anchorLabel string) result.Result[insightsdomain.TPOProfileV1]
}

// WithInsightsSnapshotter configures optional insights session profile API routes.
func WithInsightsSnapshotter(snap InsightsSnapshotter) Option {
	return func(s *Server) {
		s.insightsSnapshotter = snap
	}
}

// SessionVPResponse wraps the domain SVP with an is_active flag indicating
// whether the session window is still open.
type SessionVPResponse struct {
	insightsdomain.SessionVolumeProfileV1
	IsActive bool `json:"is_active"`
}

// TPOProfileResponse wraps the domain TPO profile with an is_active flag.
type TPOProfileResponse struct {
	insightsdomain.TPOProfileV1
	IsActive bool `json:"is_active"`
}

// handleGetSessionVolumeProfile serves GET /api/v1/insights/session-vp.
//
// Query parameters:
//
//	venue      (required)
//	instrument (required)
//	anchor     (optional, default "current")
func (s *Server) handleGetSessionVolumeProfile(w http.ResponseWriter, r *http.Request) {
	if s.insightsSnapshotter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "insights snapshotter not available"})
		return
	}

	venue := r.URL.Query().Get("venue")
	instrument := r.URL.Query().Get("instrument")
	if venue == "" || instrument == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "venue and instrument are required"})
		return
	}

	anchor := r.URL.Query().Get("anchor")
	if anchor == "" {
		anchor = "current"
	}

	res := s.insightsSnapshotter.SnapshotSessionVolumeProfile(r.Context(), venue, instrument, anchor)
	if res.IsFail() {
		p := res.Problem()
		code := httpStatusFromProblem(p)
		writeJSON(w, code, map[string]string{"error": p.Message})
		return
	}

	svp := res.Value()
	resp := SessionVPResponse{
		SessionVolumeProfileV1: svp,
		IsActive:               svp.WindowEndTs == 0 || anchor == "current",
	}
	writeResponse(w, r, http.StatusOK, "insights.session_vp", resp)
}

// handleGetTPOProfile serves GET /api/v1/insights/tpo.
//
// Query parameters:
//
//	venue      (required)
//	instrument (required)
//	anchor     (optional, default "current")
func (s *Server) handleGetTPOProfile(w http.ResponseWriter, r *http.Request) {
	if s.insightsSnapshotter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "insights snapshotter not available"})
		return
	}

	venue := r.URL.Query().Get("venue")
	instrument := r.URL.Query().Get("instrument")
	if venue == "" || instrument == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "venue and instrument are required"})
		return
	}

	anchor := r.URL.Query().Get("anchor")
	if anchor == "" {
		anchor = "current"
	}

	res := s.insightsSnapshotter.SnapshotTPOProfile(r.Context(), venue, instrument, anchor)
	if res.IsFail() {
		p := res.Problem()
		code := httpStatusFromProblem(p)
		writeJSON(w, code, map[string]string{"error": p.Message})
		return
	}

	tpo := res.Value()
	resp := TPOProfileResponse{
		TPOProfileV1: tpo,
		IsActive:     tpo.WindowEndTs == 0 || anchor == "current",
	}
	writeResponse(w, r, http.StatusOK, "insights.tpo", resp)
}

// httpStatusFromProblem maps a problem code to an HTTP status.
func httpStatusFromProblem(p *problem.Problem) int {
	if p == nil {
		return http.StatusInternalServerError
	}
	switch p.Code {
	case problem.NotFound:
		return http.StatusNotFound
	case problem.ValidationFailed:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
