package httpserver

import (
	"net/http"
	"sort"
	"strings"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
)

// CatalogArtifact describes one available artifact type and its supported timeframes.
type CatalogArtifact struct {
	Name       string   `json:"name"`
	Endpoint   string   `json:"endpoint"`
	Timeframes []string `json:"timeframes"`
}

// CatalogEntry describes the available artifacts for one venue/instrument pair.
type CatalogEntry struct {
	Venue      string            `json:"venue"`
	Instrument string            `json:"instrument"`
	Artifacts  []CatalogArtifact `json:"artifacts"`
}

// CatalogResponse is the top-level response for GET /api/v1/catalog.
type CatalogResponse struct {
	Entries []CatalogEntry `json:"entries"`
}

// catalogArtifactDefinitions returns the fixed set of artifact types with their
// timeframes and HTTP endpoints. This is config-derived from domain constants,
// not from storage queries.
func catalogArtifactDefinitions() []CatalogArtifact {
	return []CatalogArtifact{
		{Name: "candle", Endpoint: "/api/v1/candles", Timeframes: aggdomain.AllowedCandleTimeframes},
		{Name: "stats", Endpoint: "/api/v1/stats", Timeframes: aggdomain.AllowedStatsTimeframes},
		{Name: "tape", Endpoint: "/api/v1/tape", Timeframes: aggdomain.AllowedTapeTimeframes},
		{Name: "oi", Endpoint: "/api/v1/oi", Timeframes: []string{"raw"}},
		{Name: "delta_volume", Endpoint: "/api/v1/delta_volume", Timeframes: aggdomain.AllowedTapeTimeframes},
		{Name: "cvd", Endpoint: "/api/v1/cvd", Timeframes: aggdomain.AllowedTapeTimeframes},
		{Name: "bar_stats", Endpoint: "/api/v1/bar_stats", Timeframes: aggdomain.AllowedTapeTimeframes},
		{Name: "snapshots", Endpoint: "/api/v1/snapshots", Timeframes: []string{"raw"}},
	}
}

// handleGetCatalog serves GET /api/v1/catalog with optional query parameters:
//
//	venue, instrument
//
// Returns available artifacts and their supported timeframes for configured markets.
// When venue and/or instrument are provided, the result is filtered.
// This is entirely config-derived — no storage queries are executed.
func (s *Server) handleGetCatalog(w http.ResponseWriter, r *http.Request) {
	if s.markets == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "markets not configured"})
		return
	}

	filterVenue := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("venue")))
	filterInstrument := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("instrument")))

	artifacts := catalogArtifactDefinitions()
	entries := make([]CatalogEntry, 0)

	for _, ex := range s.markets.Exchanges {
		venue := strings.ToLower(strings.TrimSpace(ex.Name))
		if venue == "" {
			continue
		}
		if filterVenue != "" && venue != filterVenue {
			continue
		}

		seen := make(map[string]struct{})
		for _, sym := range ex.Symbols {
			instrument := strings.ToUpper(strings.TrimSpace(sym.Ticker))
			if instrument == "" {
				continue
			}
			if filterInstrument != "" && instrument != filterInstrument {
				continue
			}
			key := venue + "|" + instrument
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}

			entries = append(entries, CatalogEntry{
				Venue:      venue,
				Instrument: instrument,
				Artifacts:  artifacts,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Venue != entries[j].Venue {
			return entries[i].Venue < entries[j].Venue
		}
		return entries[i].Instrument < entries[j].Instrument
	})

	writeJSON(w, http.StatusOK, CatalogResponse{Entries: entries})
}
