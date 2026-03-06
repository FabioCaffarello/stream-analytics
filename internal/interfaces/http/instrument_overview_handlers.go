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

const (
	overviewTimelineDefaultTimeframe = "1m"
	overviewMaxStreams               = 1024
)

// InstrumentOverviewResponse is a backend-owned composed read model for
// instrument-level client bootstrap and diagnostics.
type InstrumentOverviewResponse struct {
	Venue      string                      `json:"venue"`
	Instrument string                      `json:"instrument"`
	Status     string                      `json:"status"`
	CheckedAt  int64                       `json:"checked_at"`
	Readiness  InstrumentReadinessStatus   `json:"readiness"`
	Freshness  InstrumentFreshnessStatus   `json:"freshness"`
	Resync     InstrumentResyncDiagnostics `json:"resync"`
	Artifacts  []InstrumentArtifactSummary `json:"artifacts"`
}

// InstrumentReadinessStatus normalizes guardian readiness for client consumption.
type InstrumentReadinessStatus struct {
	Status string `json:"status"`
}

// InstrumentFreshnessStatus normalizes per-channel flow health.
type InstrumentFreshnessStatus struct {
	Status   string                      `json:"status"`
	Active   bool                        `json:"active"`
	Channels map[string]ChannelFreshness `json:"channels"`
}

// InstrumentResyncDiagnostics summarizes delivery recovery signals for one instrument.
type InstrumentResyncDiagnostics struct {
	Status      string `json:"status"`
	ResyncTotal uint64 `json:"resync_total"`
	DropsTotal  uint64 `json:"drops_total"`
	Streams     int    `json:"streams"`
	MaxLagMs    int64  `json:"max_lag_ms"`
}

// InstrumentArtifactSummary describes one artifact and its timeline availability.
type InstrumentArtifactSummary struct {
	Name       string                     `json:"name"`
	Endpoint   string                     `json:"endpoint"`
	Timeframes []string                   `json:"timeframes"`
	Timeline   InstrumentArtifactTimeline `json:"timeline"`
}

// InstrumentArtifactTimeline is a compact timeline readiness view.
type InstrumentArtifactTimeline struct {
	Timeframe string `json:"timeframe"`
	FirstTs   int64  `json:"first_ts"`
	LastTs    int64  `json:"last_ts"`
	Status    string `json:"status"`
}

// handleGetInstrumentOverview serves GET /api/v1/instrument/overview?venue=X&instrument=Y.
//
// It composes readiness, freshness, resync diagnostics, and artifact timeline
// summaries into a single widget-oriented payload without leaking internals.
func (s *Server) handleGetInstrumentOverview(w http.ResponseWriter, r *http.Request) {
	venue := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("venue")))
	instrument := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("instrument")))
	if venue == "" || instrument == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "venue and instrument are required"})
		return
	}

	nowMs := time.Now().UnixMilli()
	terminal := observability.SnapshotTerminalWSState(overviewMaxStreams)

	readinessStatus := "not_ready"
	if s.queryReadiness() {
		readinessStatus = "ready"
	}

	freshness := buildInstrumentFreshnessStatus(terminal, venue, instrument, nowMs)
	resync := buildInstrumentResyncDiagnostics(terminal, venue, instrument)

	resp := InstrumentOverviewResponse{
		Venue:      venue,
		Instrument: instrument,
		CheckedAt:  nowMs,
		Readiness:  InstrumentReadinessStatus{Status: readinessStatus},
		Freshness:  freshness,
		Resync:     resync,
		Artifacts:  s.buildInstrumentArtifacts(r.Context(), venue, instrument),
	}
	resp.Status = classifyInstrumentOverviewStatus(resp.Readiness.Status, resp.Freshness.Status, resp.Resync.Status)

	writeJSON(w, http.StatusOK, resp)
}

func buildInstrumentFreshnessStatus(snapshot observability.TerminalWSStateSnapshot, venue, instrument string, nowMs int64) InstrumentFreshnessStatus {
	channels := make(map[string]ChannelFreshness)
	for _, stream := range snapshot.Streams {
		if !matchesInstrument(stream.Venue, stream.Symbol, venue, instrument) {
			continue
		}
		if stream.Channel == "" {
			continue
		}

		existing, ok := channels[stream.Channel]
		if ok && existing.LastEventTs >= stream.LastTsServer {
			continue
		}

		flowing := stream.LastTsServer > 0 && (nowMs-stream.LastTsServer) < freshnessStaleThresholdMs
		channels[stream.Channel] = ChannelFreshness{
			LastEventTs: stream.LastTsServer,
			LagMs:       stream.LastLagMs,
			Flowing:     flowing,
		}
	}

	status := classifyFreshnessStatus(channels)
	return InstrumentFreshnessStatus{
		Status:   status,
		Active:   status == "flowing",
		Channels: channels,
	}
}

func classifyFreshnessStatus(channels map[string]ChannelFreshness) string {
	if len(channels) == 0 {
		return "inactive"
	}
	for _, ch := range channels {
		if ch.Flowing {
			return "flowing"
		}
	}
	return "stale"
}

func buildInstrumentResyncDiagnostics(snapshot observability.TerminalWSStateSnapshot, venue, instrument string) InstrumentResyncDiagnostics {
	var out InstrumentResyncDiagnostics
	for _, stream := range snapshot.Streams {
		if !matchesInstrument(stream.Venue, stream.Symbol, venue, instrument) {
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

func classifyInstrumentOverviewStatus(readinessStatus, freshnessStatus, resyncStatus string) string {
	if readinessStatus != "ready" {
		return "not_ready"
	}
	if freshnessStatus == "inactive" {
		return "inactive"
	}
	if freshnessStatus == "stale" || resyncStatus == "degraded" {
		return "degraded"
	}
	return "ready"
}

func (s *Server) buildInstrumentArtifacts(ctx context.Context, venue, instrument string) []InstrumentArtifactSummary {
	artifacts := []InstrumentArtifactSummary{
		{
			Name:       "candle",
			Endpoint:   "/api/v1/candles",
			Timeframes: aggdomain.AllowedCandleTimeframes,
			Timeline:   s.resolveCandleTimeline(ctx, venue, instrument, overviewTimelineDefaultTimeframe),
		},
		{
			Name:       "stats",
			Endpoint:   "/api/v1/stats",
			Timeframes: aggdomain.AllowedStatsTimeframes,
			Timeline:   s.resolveStatsTimeline(ctx, venue, instrument, overviewTimelineDefaultTimeframe),
		},
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })
	return artifacts
}

func (s *Server) resolveCandleTimeline(ctx context.Context, venue, instrument, timeframe string) InstrumentArtifactTimeline {
	timeline := InstrumentArtifactTimeline{Timeframe: timeframe, Status: "unavailable"}
	if s.coldReaders == nil || s.coldReaders.Candles == nil {
		return timeline
	}
	first, p := s.coldReaders.Candles.GetFirstCandle(ctx, venue, instrument, timeframe)
	if p != nil {
		return timeline
	}
	last, p := s.coldReaders.Candles.GetLastCandle(ctx, venue, instrument, timeframe)
	if p != nil {
		return timeline
	}
	if first != nil {
		timeline.FirstTs = first.WindowStartTs
	}
	if last != nil {
		timeline.LastTs = last.WindowStartTs
	}
	if timeline.FirstTs == 0 && timeline.LastTs == 0 {
		timeline.Status = "empty"
		return timeline
	}
	timeline.Status = "available"
	return timeline
}

func (s *Server) resolveStatsTimeline(ctx context.Context, venue, instrument, timeframe string) InstrumentArtifactTimeline {
	timeline := InstrumentArtifactTimeline{Timeframe: timeframe, Status: "unavailable"}
	if s.coldReaders == nil || s.coldReaders.Stats == nil {
		return timeline
	}
	first, p := s.coldReaders.Stats.GetFirstStats(ctx, venue, instrument, timeframe)
	if p != nil {
		return timeline
	}
	last, p := s.coldReaders.Stats.GetLastStats(ctx, venue, instrument, timeframe)
	if p != nil {
		return timeline
	}
	if first != nil {
		timeline.FirstTs = first.WindowStartTs
	}
	if last != nil {
		timeline.LastTs = last.WindowStartTs
	}
	if timeline.FirstTs == 0 && timeline.LastTs == 0 {
		timeline.Status = "empty"
		return timeline
	}
	timeline.Status = "available"
	return timeline
}
