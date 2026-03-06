package httpserver

import (
	"net/http"
	"strings"
	"time"

	"github.com/market-raccoon/internal/shared/observability"
)

// FreshnessResponse describes per-instrument data flow health.
// It composes terminal WS stream state into a client-friendly view
// without exposing internal stream IDs, observability internals, or
// hot/cold storage details.
type FreshnessResponse struct {
	Venue      string                      `json:"venue"`
	Instrument string                      `json:"instrument"`
	Active     bool                        `json:"active"`
	Channels   map[string]ChannelFreshness `json:"channels"`
	CheckedAt  int64                       `json:"checked_at"`
}

// ChannelFreshness describes the flow state of one data channel.
type ChannelFreshness struct {
	LastEventTs int64 `json:"last_event_ts"`
	LagMs       int64 `json:"lag_ms"`
	Flowing     bool  `json:"flowing"`
}

const freshnessStaleThresholdMs = 30_000 // 30 seconds without data = not flowing

// handleGetFreshness serves GET /api/v1/freshness?venue=X&instrument=Y.
//
// Returns per-channel data flow health for the requested instrument,
// derived from the terminal WS stream state. The client can use this to
// show green/yellow/red indicators per market without subscribing to WS.
//
// A channel is considered "flowing" if its last event is within 30 seconds.
// The response hides internal stream IDs and observability details.
func (s *Server) handleGetFreshness(w http.ResponseWriter, r *http.Request) {
	venue := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("venue")))
	instrument := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("instrument")))

	if venue == "" || instrument == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "venue and instrument are required",
		})
		return
	}

	nowMs := time.Now().UnixMilli()
	snapshot := observability.SnapshotTerminalWSState(terminalWSMaxStreamsForFreshness)

	channels := make(map[string]ChannelFreshness)
	for _, stream := range snapshot.Streams {
		if !matchesInstrument(stream.Venue, stream.Symbol, venue, instrument) {
			continue
		}
		channel := stream.Channel
		if channel == "" {
			continue
		}
		// Keep the most recent entry per channel.
		existing, ok := channels[channel]
		if ok && existing.LastEventTs >= stream.LastTsServer {
			continue
		}
		flowing := stream.LastTsServer > 0 && (nowMs-stream.LastTsServer) < freshnessStaleThresholdMs
		channels[channel] = ChannelFreshness{
			LastEventTs: stream.LastTsServer,
			LagMs:       stream.LastLagMs,
			Flowing:     flowing,
		}
	}

	active := false
	for _, ch := range channels {
		if ch.Flowing {
			active = true
			break
		}
	}

	writeJSON(w, http.StatusOK, FreshnessResponse{
		Venue:      venue,
		Instrument: instrument,
		Active:     active,
		Channels:   channels,
		CheckedAt:  nowMs,
	})
}

// matchesInstrument compares stream venue/symbol against the requested
// venue/instrument, handling case normalization.
func matchesInstrument(streamVenue, streamSymbol, venue, instrument string) bool {
	return strings.EqualFold(streamVenue, venue) &&
		strings.EqualFold(streamSymbol, instrument)
}

const terminalWSMaxStreamsForFreshness = 1024
