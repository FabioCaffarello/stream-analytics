package httpserver

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/market-raccoon/internal/actors/runtime"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
)

// SessionOverviewResponse is the composed read model a client uses to bootstrap.
// It combines server identity, readiness, available markets, and artifact
// capabilities into a single payload — replacing multiple startup HTTP calls.
type SessionOverviewResponse struct {
	ServerTimeMs int64               `json:"server_time_ms"`
	Ready        bool                `json:"ready"`
	Markets      []SessionMarket     `json:"markets"`
	Capabilities SessionCapabilities `json:"capabilities"`
}

// SessionMarket describes one venue and its available instruments.
type SessionMarket struct {
	Venue       string   `json:"venue"`
	Instruments []string `json:"instruments"`
}

// SessionCapabilities describes available artifacts, their timeframes, and endpoints.
type SessionCapabilities struct {
	Artifacts []SessionArtifact `json:"artifacts"`
}

// SessionArtifact describes one artifact type with its timeframes and endpoint.
type SessionArtifact struct {
	Name       string   `json:"name"`
	Endpoint   string   `json:"endpoint"`
	Timeframes []string `json:"timeframes"`
}

// sessionArtifactDefinitions returns the fixed set of artifact capabilities.
func sessionArtifactDefinitions() []SessionArtifact {
	return []SessionArtifact{
		{Name: "candle", Endpoint: "/api/v1/candles", Timeframes: aggdomain.AllowedCandleTimeframes},
		{Name: "stats", Endpoint: "/api/v1/stats", Timeframes: aggdomain.AllowedStatsTimeframes},
		{Name: "tape", Endpoint: "/api/v1/tape", Timeframes: aggdomain.AllowedTapeTimeframes},
		{Name: "oi", Endpoint: "/api/v1/oi", Timeframes: []string{"raw"}},
		{Name: "delta_volume", Endpoint: "/api/v1/delta_volume", Timeframes: aggdomain.AllowedTapeTimeframes},
		{Name: "cvd", Endpoint: "/api/v1/cvd", Timeframes: aggdomain.AllowedTapeTimeframes},
		{Name: "bar_stats", Endpoint: "/api/v1/bar_stats", Timeframes: aggdomain.AllowedTapeTimeframes},
		{Name: "snapshots", Endpoint: "/api/v1/snapshots", Timeframes: []string{"raw"}},
		{Name: "session_vp", Endpoint: "/api/v1/insights/session-vp", Timeframes: []string{"session"}},
		{Name: "tpo", Endpoint: "/api/v1/insights/tpo", Timeframes: []string{"session"}},
	}
}

// handleGetSession serves GET /api/v1/session.
//
// Returns a composed read model with server time, readiness, available
// markets, and artifact capabilities. This allows the client to bootstrap
// with a single HTTP call instead of calling /readyz + /markets + /catalog.
//
// The response hides all internal details (federation, hot/cold tiers,
// subsystem topology). The client sees a stable, flat structure.
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	resp := SessionOverviewResponse{
		ServerTimeMs: time.Now().UnixMilli(),
		Markets:      s.buildSessionMarkets(),
		Capabilities: SessionCapabilities{
			Artifacts: sessionArtifactDefinitions(),
		},
	}

	// Query guardian for readiness (best-effort — if unavailable, report not ready).
	resp.Ready = s.queryReadiness()

	writeJSON(w, http.StatusOK, resp)
}

// queryReadiness asks the Guardian if all subsystems are ready.
// Returns false on timeout or unexpected response.
func (s *Server) queryReadiness() bool {
	if s.engine == nil || s.guardianPID == nil {
		return false
	}
	resp := s.engine.Request(s.guardianPID, runtime.ReadyQuery{}, s.snapshotTimeout)
	result, err := resp.Result()
	if err != nil {
		return false
	}
	rr, ok := result.(runtime.ReadyResponse)
	if !ok {
		return false
	}
	return rr.Ready
}

// buildSessionMarkets extracts venue/instrument pairs from markets config.
func (s *Server) buildSessionMarkets() []SessionMarket {
	if s.markets == nil {
		return []SessionMarket{}
	}

	type accumulator struct {
		venue       string
		instruments map[string]struct{}
	}

	byVenue := make(map[string]*accumulator, len(s.markets.Exchanges))
	for _, ex := range s.markets.Exchanges {
		venue := strings.ToLower(strings.TrimSpace(ex.Name))
		if venue == "" {
			continue
		}
		acc, ok := byVenue[venue]
		if !ok {
			acc = &accumulator{venue: venue, instruments: make(map[string]struct{})}
			byVenue[venue] = acc
		}
		for _, sym := range ex.Symbols {
			ticker := strings.ToUpper(strings.TrimSpace(sym.Ticker))
			if ticker != "" {
				acc.instruments[ticker] = struct{}{}
			}
		}
	}

	venues := make([]string, 0, len(byVenue))
	for v := range byVenue {
		venues = append(venues, v)
	}
	sort.Strings(venues)

	markets := make([]SessionMarket, 0, len(venues))
	for _, v := range venues {
		acc := byVenue[v]
		instruments := make([]string, 0, len(acc.instruments))
		for inst := range acc.instruments {
			instruments = append(instruments, inst)
		}
		sort.Strings(instruments)
		markets = append(markets, SessionMarket{
			Venue:       v,
			Instruments: instruments,
		})
	}
	return markets
}
