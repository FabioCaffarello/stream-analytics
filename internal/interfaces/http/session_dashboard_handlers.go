package httpserver

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/observability"
)

// SessionDashboardResponse is a composed session-level client-readiness read model.
type SessionDashboardResponse struct {
	ServerTimeMs int64                            `json:"server_time_ms"`
	Status       string                           `json:"status"`
	Readiness    SessionDashboardReadinessStatus  `json:"readiness"`
	Freshness    SessionDashboardFreshnessStatus  `json:"freshness"`
	Resync       SessionDashboardResyncStatus     `json:"resync"`
	Artifacts    []SessionDashboardArtifactStatus `json:"artifacts"`
	Summary      SessionDashboardSummary          `json:"summary"`
}

// SessionDashboardReadinessStatus normalizes guardian readiness for dashboard usage.
type SessionDashboardReadinessStatus struct {
	Status string `json:"status"`
}

// SessionDashboardFreshnessStatus summarizes data-flow health across configured instruments.
type SessionDashboardFreshnessStatus struct {
	Status            string `json:"status"`
	ActiveInstruments int    `json:"active_instruments"`
	StaleInstruments  int    `json:"stale_instruments"`
	FlowingChannels   int    `json:"flowing_channels"`
	StaleChannels     int    `json:"stale_channels"`
	CheckedAt         int64  `json:"checked_at"`
}

// SessionDashboardResyncStatus summarizes delivery recovery signals across configured instruments.
type SessionDashboardResyncStatus struct {
	Status            string `json:"status"`
	ConnectionsActive int64  `json:"connections_active"`
	Streams           int    `json:"streams"`
	ResyncTotal       uint64 `json:"resync_total"`
	DropsTotal        uint64 `json:"drops_total"`
	MaxLagMs          int64  `json:"max_lag_ms"`
}

// SessionDashboardArtifactStatus is a compact artifact readiness matrix entry.
type SessionDashboardArtifactStatus struct {
	Name             string                           `json:"name"`
	Endpoint         string                           `json:"endpoint"`
	Timeframes       []string                         `json:"timeframes"`
	DefaultTimeframe string                           `json:"default_timeframe"`
	Coverage         SessionDashboardArtifactCoverage `json:"coverage"`
}

// SessionDashboardArtifactCoverage summarizes artifact availability over configured instruments.
type SessionDashboardArtifactCoverage struct {
	Status                 string `json:"status"`
	TotalInstruments       int    `json:"total_instruments"`
	AvailableInstruments   int    `json:"available_instruments"`
	EmptyInstruments       int    `json:"empty_instruments"`
	UnavailableInstruments int    `json:"unavailable_instruments"`
}

// SessionDashboardSummary provides compact configured market cardinalities.
type SessionDashboardSummary struct {
	Venues      int `json:"venues"`
	Instruments int `json:"instruments"`
}

type sessionInstrumentRef struct {
	venue      string
	instrument string
}

func (r sessionInstrumentRef) key() string {
	return r.venue + "|" + r.instrument
}

// handleGetSessionDashboard serves GET /api/v1/session/dashboard.
//
// It composes global client-readiness posture for configured markets using
// backend-owned status normalization and artifact coverage summaries.
func (s *Server) handleGetSessionDashboard(w http.ResponseWriter, r *http.Request) {
	nowMs := time.Now().UnixMilli()
	markets := s.buildSessionMarkets()
	instruments := flattenSessionInstruments(markets)

	readinessStatus := "not_ready"
	if s.queryReadiness() {
		readinessStatus = "ready"
	}

	snapshot := observability.SnapshotTerminalWSState(overviewMaxStreams)
	freshness := buildSessionDashboardFreshness(snapshot, instruments, nowMs)
	resync := buildSessionDashboardResync(snapshot, instruments)
	artifacts := s.buildSessionDashboardArtifacts(r.Context(), instruments)

	resp := SessionDashboardResponse{
		ServerTimeMs: nowMs,
		Readiness:    SessionDashboardReadinessStatus{Status: readinessStatus},
		Freshness:    freshness,
		Resync:       resync,
		Artifacts:    artifacts,
		Summary: SessionDashboardSummary{
			Venues:      len(markets),
			Instruments: len(instruments),
		},
	}
	resp.Status = classifySessionDashboardStatus(resp.Readiness.Status, resp.Freshness.Status, resp.Resync.Status)

	writeJSON(w, http.StatusOK, resp)
}

func flattenSessionInstruments(markets []SessionMarket) []sessionInstrumentRef {
	out := make([]sessionInstrumentRef, 0)
	seen := make(map[string]struct{})
	for _, market := range markets {
		for _, instrument := range market.Instruments {
			ref := sessionInstrumentRef{venue: market.Venue, instrument: instrument}
			if _, ok := seen[ref.key()]; ok {
				continue
			}
			seen[ref.key()] = struct{}{}
			out = append(out, ref)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].venue != out[j].venue {
			return out[i].venue < out[j].venue
		}
		return out[i].instrument < out[j].instrument
	})
	return out
}

func buildSessionDashboardFreshness(snapshot observability.TerminalWSStateSnapshot, instruments []sessionInstrumentRef, nowMs int64) SessionDashboardFreshnessStatus {
	byInstrument := make(map[string]sessionInstrumentRef, len(instruments))
	for _, ref := range instruments {
		byInstrument[ref.key()] = ref
	}

	hasStream := make(map[string]bool, len(instruments))
	isActive := make(map[string]bool, len(instruments))
	flowingChannels := make(map[string]struct{})
	staleChannels := make(map[string]struct{})

	for _, stream := range snapshot.Streams {
		ref := sessionInstrumentRef{
			venue:      strings.ToLower(strings.TrimSpace(stream.Venue)),
			instrument: strings.ToUpper(strings.TrimSpace(stream.Symbol)),
		}
		if _, ok := byInstrument[ref.key()]; !ok {
			continue
		}
		hasStream[ref.key()] = true

		if stream.Channel == "" {
			continue
		}

		flowing := stream.LastTsServer > 0 && (nowMs-stream.LastTsServer) < freshnessStaleThresholdMs
		if flowing {
			isActive[ref.key()] = true
			flowingChannels[stream.Channel] = struct{}{}
		} else if stream.LastTsServer > 0 {
			staleChannels[stream.Channel] = struct{}{}
		}
	}

	for channel := range flowingChannels {
		delete(staleChannels, channel)
	}

	active := 0
	stale := 0
	for _, ref := range instruments {
		if isActive[ref.key()] {
			active++
			continue
		}
		if hasStream[ref.key()] {
			stale++
		}
	}

	status := classifySessionFreshnessStatus(len(instruments), active, stale)
	return SessionDashboardFreshnessStatus{
		Status:            status,
		ActiveInstruments: active,
		StaleInstruments:  stale,
		FlowingChannels:   len(flowingChannels),
		StaleChannels:     len(staleChannels),
		CheckedAt:         nowMs,
	}
}

func buildSessionDashboardResync(snapshot observability.TerminalWSStateSnapshot, instruments []sessionInstrumentRef) SessionDashboardResyncStatus {
	byInstrument := make(map[string]struct{}, len(instruments))
	for _, ref := range instruments {
		byInstrument[ref.key()] = struct{}{}
	}

	var out SessionDashboardResyncStatus
	out.ConnectionsActive = snapshot.ConnectionsActive
	for _, stream := range snapshot.Streams {
		ref := sessionInstrumentRef{
			venue:      strings.ToLower(strings.TrimSpace(stream.Venue)),
			instrument: strings.ToUpper(strings.TrimSpace(stream.Symbol)),
		}
		if _, ok := byInstrument[ref.key()]; !ok {
			continue
		}
		out.Streams++
		out.ResyncTotal += stream.ResyncTotal
		out.DropsTotal += stream.DroppedTotal
		if stream.LastLagMs > out.MaxLagMs {
			out.MaxLagMs = stream.LastLagMs
		}
	}

	switch {
	case out.DropsTotal > 0:
		out.Status = "degraded"
	case out.ResyncTotal > 0:
		out.Status = "recovering"
	default:
		out.Status = "stable"
	}
	return out
}

func classifySessionFreshnessStatus(total, active, stale int) string {
	if total == 0 {
		return "inactive"
	}
	if active == total {
		return "flowing"
	}
	if active > 0 {
		return "partial"
	}
	if stale > 0 {
		return "stale"
	}
	return "inactive"
}

func classifySessionDashboardStatus(readinessStatus, freshnessStatus, resyncStatus string) string {
	if readinessStatus != "ready" {
		return "not_ready"
	}
	if freshnessStatus == "inactive" {
		return "inactive"
	}
	if freshnessStatus == "partial" || freshnessStatus == "stale" || resyncStatus == "degraded" {
		return "degraded"
	}
	return "ready"
}

func (s *Server) buildSessionDashboardArtifacts(ctx context.Context, instruments []sessionInstrumentRef) []SessionDashboardArtifactStatus {
	artifacts := []SessionDashboardArtifactStatus{
		{
			Name:             "candle",
			Endpoint:         "/api/v1/candles",
			Timeframes:       aggdomain.AllowedCandleTimeframes,
			DefaultTimeframe: overviewTimelineDefaultTimeframe,
			Coverage: s.evaluateArtifactCoverage(instruments, func(ref sessionInstrumentRef) string {
				return s.resolveCandleTimeline(ctx, ref.venue, ref.instrument, overviewTimelineDefaultTimeframe).Status
			}),
		},
		{
			Name:             "stats",
			Endpoint:         "/api/v1/stats",
			Timeframes:       aggdomain.AllowedStatsTimeframes,
			DefaultTimeframe: overviewTimelineDefaultTimeframe,
			Coverage: s.evaluateArtifactCoverage(instruments, func(ref sessionInstrumentRef) string {
				return s.resolveStatsTimeline(ctx, ref.venue, ref.instrument, overviewTimelineDefaultTimeframe).Status
			}),
		},
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })
	return artifacts
}

func (s *Server) evaluateArtifactCoverage(instruments []sessionInstrumentRef, resolveStatus func(ref sessionInstrumentRef) string) SessionDashboardArtifactCoverage {
	coverage := SessionDashboardArtifactCoverage{TotalInstruments: len(instruments)}
	for _, ref := range instruments {
		switch resolveStatus(ref) {
		case "available":
			coverage.AvailableInstruments++
		case "empty":
			coverage.EmptyInstruments++
		default:
			coverage.UnavailableInstruments++
		}
	}
	coverage.Status = classifyArtifactCoverageStatus(coverage)
	return coverage
}

func classifyArtifactCoverageStatus(c SessionDashboardArtifactCoverage) string {
	if c.TotalInstruments == 0 {
		return "unavailable"
	}
	if c.UnavailableInstruments == c.TotalInstruments {
		return "unavailable"
	}
	if c.AvailableInstruments == 0 && c.EmptyInstruments == c.TotalInstruments {
		return "empty"
	}
	if c.AvailableInstruments > 0 && c.EmptyInstruments > 0 {
		return "partial"
	}
	if c.UnavailableInstruments > 0 {
		return "partial"
	}
	return "available"
}
