package httpserver

import (
	"encoding/json"
	"net/http"
	"time"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	executionports "github.com/market-raccoon/internal/core/execution/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

// WithControlPlane configures the execution control plane API endpoints.
func WithControlPlane(cp executionports.ControlPlane) Option {
	return func(s *Server) {
		s.controlPlane = cp
	}
}

// handleControlSnapshot serves GET /api/v1/control/snapshot.
func (s *Server) handleControlSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.controlPlane == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "control plane not configured",
		})
		return
	}
	snap := s.controlPlane.Snapshot()
	dto := controlSnapshotToDTO(snap)
	writeResponse(w, r, http.StatusOK, "execution.control.snapshot", dto)
}

// handleControlApply serves POST /api/v1/control.
func (s *Server) handleControlApply(w http.ResponseWriter, r *http.Request) {
	if s.controlPlane == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "control plane not configured",
		})
		return
	}
	if !supportedRequestContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	var req controlDirectiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON: " + err.Error(),
		})
		return
	}

	directive := executiondomain.ControlDirective{
		Command:    executiondomain.ControlCommand(req.Command),
		TargetID:   req.TargetID,
		Parameters: req.Parameters,
		Reason:     req.Reason,
		IssuedAtMs: req.IssuedAtMs,
		Issuer:     req.Issuer,
	}

	if directive.IssuedAtMs <= 0 {
		directive.IssuedAtMs = time.Now().UnixMilli()
	}

	if p := s.controlPlane.Apply(directive); p != nil {
		code := http.StatusBadRequest
		if p.Code == problem.Conflict {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]string{
			"error": p.Message,
			"code":  string(p.Code),
		})
		return
	}

	snap := s.controlPlane.Snapshot()
	dto := controlSnapshotToDTO(snap)
	writeResponse(w, r, http.StatusOK, "execution.control.applied", dto)
}

// ---------------------------------------------------------------------------
// DTOs
// ---------------------------------------------------------------------------

type controlDirectiveRequest struct {
	Command    string            `json:"command"`
	TargetID   string            `json:"target_id,omitempty"`
	Parameters map[string]string `json:"parameters,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	IssuedAtMs int64             `json:"issued_at_ms,omitempty"`
	Issuer     string            `json:"issuer"`
}

type controlSnapshotDTO struct {
	State              string            `json:"state"`
	DisabledStrategies []string          `json:"disabled_strategies"`
	DisabledAdapters   []string          `json:"disabled_adapters"`
	SimulationProfile  string            `json:"simulation_profile,omitempty"`
	AllowlistOverrides *allowlistDTO     `json:"allowlist_overrides,omitempty"`
	LastDirective      *lastDirectiveDTO `json:"last_directive,omitempty"`
	UpdatedAtMs        int64             `json:"updated_at_ms"`
}

type allowlistDTO struct {
	RestrictVenues  []string `json:"restrict_venues,omitempty"`
	RestrictSymbols []string `json:"restrict_symbols,omitempty"`
}

type lastDirectiveDTO struct {
	Command    string `json:"command"`
	TargetID   string `json:"target_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Issuer     string `json:"issuer"`
	IssuedAtMs int64  `json:"issued_at_ms"`
}

func controlSnapshotToDTO(snap executiondomain.ControlSnapshot) controlSnapshotDTO {
	strategies := make([]string, 0, len(snap.DisabledStrategies))
	for k := range snap.DisabledStrategies {
		strategies = append(strategies, k)
	}
	adapters := make([]string, 0, len(snap.DisabledAdapters))
	for k := range snap.DisabledAdapters {
		adapters = append(adapters, k)
	}

	dto := controlSnapshotDTO{
		State:              string(snap.State),
		DisabledStrategies: strategies,
		DisabledAdapters:   adapters,
		SimulationProfile:  snap.SimulationProfile,
		UpdatedAtMs:        snap.UpdatedAtMs,
	}

	if snap.AllowlistOverrides != nil {
		venues := make([]string, 0, len(snap.AllowlistOverrides.RestrictVenues))
		for k := range snap.AllowlistOverrides.RestrictVenues {
			venues = append(venues, k)
		}
		symbols := make([]string, 0, len(snap.AllowlistOverrides.RestrictSymbols))
		for k := range snap.AllowlistOverrides.RestrictSymbols {
			symbols = append(symbols, k)
		}
		dto.AllowlistOverrides = &allowlistDTO{
			RestrictVenues:  venues,
			RestrictSymbols: symbols,
		}
	}

	if snap.LastDirective.Issuer != "" {
		dto.LastDirective = &lastDirectiveDTO{
			Command:    string(snap.LastDirective.Command),
			TargetID:   snap.LastDirective.TargetID,
			Reason:     snap.LastDirective.Reason,
			Issuer:     snap.LastDirective.Issuer,
			IssuedAtMs: snap.LastDirective.IssuedAtMs,
		}
	}

	return dto
}
