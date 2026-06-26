package httpserver

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
)

// ArtifactSummaryResponse is a backend-owned artifact availability matrix
// oriented to client widget enablement.
type ArtifactSummaryResponse struct {
	Timeframe string                    `json:"timeframe"`
	Status    string                    `json:"status"`
	CheckedAt int64                     `json:"checked_at"`
	Filters   ArtifactSummaryFilters    `json:"filters"`
	Artifacts []ArtifactSummaryArtifact `json:"artifacts"`
	Entries   []ArtifactSummaryEntry    `json:"entries"`
	Summary   ArtifactSummaryOverview   `json:"summary"`
}

// ArtifactSummaryFilters echoes effective request filters.
type ArtifactSummaryFilters struct {
	Venue      string `json:"venue,omitempty"`
	Instrument string `json:"instrument,omitempty"`
	Artifact   string `json:"artifact,omitempty"`
}

// ArtifactSummaryArtifact is a compact artifact matrix header + coverage.
type ArtifactSummaryArtifact struct {
	Name             string                           `json:"name"`
	Endpoint         string                           `json:"endpoint"`
	Timeframes       []string                         `json:"timeframes"`
	DefaultTimeframe string                           `json:"default_timeframe"`
	Coverage         SessionDashboardArtifactCoverage `json:"coverage"`
}

// ArtifactSummaryEntry is one venue/instrument matrix row.
type ArtifactSummaryEntry struct {
	Venue      string            `json:"venue"`
	Instrument string            `json:"instrument"`
	Artifacts  map[string]string `json:"artifacts"`
}

// ArtifactSummaryOverview summarizes matrix cardinality.
type ArtifactSummaryOverview struct {
	Venues      int `json:"venues"`
	Instruments int `json:"instruments"`
	Entries     int `json:"entries"`
}

type artifactSummaryDefinition struct {
	name       string
	endpoint   string
	timeframes []string
	resolve    func(ctx context.Context, s *Server, ref sessionInstrumentRef, timeframe string) string
}

// handleGetArtifactSummary serves GET /api/v1/artifacts/summary.
//
// Optional query params:
//   - timeframe (default: 1m)
//   - venue
//   - instrument
//   - artifact (candle|stats)
func (s *Server) handleGetArtifactSummary(w http.ResponseWriter, r *http.Request) {
	timeframe := strings.TrimSpace(r.URL.Query().Get("timeframe"))
	if timeframe == "" {
		timeframe = overviewTimelineDefaultTimeframe
	}
	if !isAllowedSummaryTimeframe(timeframe) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "unsupported timeframe",
			"supported": aggdomain.AllowedCandleTimeframes,
		})
		return
	}

	filterVenue := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("venue")))
	filterInstrument := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("instrument")))
	filterArtifact := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("artifact")))
	if filterArtifact != "" && filterArtifact != "candle" && filterArtifact != "stats" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "unsupported artifact",
			"supported": []string{"candle", "stats"},
		})
		return
	}

	definitions := []artifactSummaryDefinition{
		{
			name:       "candle",
			endpoint:   "/api/v1/candles",
			timeframes: aggdomain.AllowedCandleTimeframes,
			resolve: func(ctx context.Context, s *Server, ref sessionInstrumentRef, timeframe string) string {
				return s.resolveCandleTimeline(ctx, ref.venue, ref.instrument, timeframe).Status
			},
		},
		{
			name:       "stats",
			endpoint:   "/api/v1/stats",
			timeframes: aggdomain.AllowedStatsTimeframes,
			resolve: func(ctx context.Context, s *Server, ref sessionInstrumentRef, timeframe string) string {
				return s.resolveStatsTimeline(ctx, ref.venue, ref.instrument, timeframe).Status
			},
		},
	}
	definitions = filterSummaryDefinitions(definitions, filterArtifact)

	refs := filterSessionInstrumentRefs(flattenSessionInstruments(s.buildSessionMarkets()), filterVenue, filterInstrument)
	entries := buildArtifactSummaryEntries(r.Context(), s, refs, definitions, timeframe)
	artifacts := buildArtifactSummaryArtifacts(definitions, entries, timeframe)

	resp := ArtifactSummaryResponse{
		Timeframe: timeframe,
		Status:    classifyArtifactSummaryStatus(entries, artifacts),
		CheckedAt: time.Now().UnixMilli(),
		Filters: ArtifactSummaryFilters{
			Venue:      filterVenue,
			Instrument: filterInstrument,
			Artifact:   filterArtifact,
		},
		Artifacts: artifacts,
		Entries:   entries,
		Summary: ArtifactSummaryOverview{
			Venues:      countSummaryVenues(entries),
			Instruments: len(refs),
			Entries:     len(entries),
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

func isAllowedSummaryTimeframe(timeframe string) bool {
	for _, tf := range aggdomain.AllowedCandleTimeframes {
		if tf == timeframe {
			return true
		}
	}
	return false
}

func filterSummaryDefinitions(definitions []artifactSummaryDefinition, artifact string) []artifactSummaryDefinition {
	if artifact == "" {
		return definitions
	}
	out := make([]artifactSummaryDefinition, 0, 1)
	for _, def := range definitions {
		if def.name == artifact {
			out = append(out, def)
			break
		}
	}
	return out
}

func filterSessionInstrumentRefs(refs []sessionInstrumentRef, venue, instrument string) []sessionInstrumentRef {
	out := make([]sessionInstrumentRef, 0, len(refs))
	for _, ref := range refs {
		if venue != "" && ref.venue != venue {
			continue
		}
		if instrument != "" && ref.instrument != instrument {
			continue
		}
		out = append(out, ref)
	}
	return out
}

func buildArtifactSummaryEntries(ctx context.Context, s *Server, refs []sessionInstrumentRef, definitions []artifactSummaryDefinition, timeframe string) []ArtifactSummaryEntry {
	entries := make([]ArtifactSummaryEntry, 0, len(refs))
	for _, ref := range refs {
		entry := ArtifactSummaryEntry{
			Venue:      ref.venue,
			Instrument: ref.instrument,
			Artifacts:  make(map[string]string, len(definitions)),
		}
		for _, def := range definitions {
			entry.Artifacts[def.name] = def.resolve(ctx, s, ref, timeframe)
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Venue != entries[j].Venue {
			return entries[i].Venue < entries[j].Venue
		}
		return entries[i].Instrument < entries[j].Instrument
	})
	return entries
}

func buildArtifactSummaryArtifacts(definitions []artifactSummaryDefinition, entries []ArtifactSummaryEntry, timeframe string) []ArtifactSummaryArtifact {
	artifacts := make([]ArtifactSummaryArtifact, 0, len(definitions))
	for _, def := range definitions {
		coverage := SessionDashboardArtifactCoverage{TotalInstruments: len(entries)}
		for _, entry := range entries {
			switch entry.Artifacts[def.name] {
			case "available":
				coverage.AvailableInstruments++
			case "empty":
				coverage.EmptyInstruments++
			default:
				coverage.UnavailableInstruments++
			}
		}
		coverage.Status = classifyArtifactCoverageStatus(coverage)
		artifacts = append(artifacts, ArtifactSummaryArtifact{
			Name:             def.name,
			Endpoint:         def.endpoint,
			Timeframes:       def.timeframes,
			DefaultTimeframe: timeframe,
			Coverage:         coverage,
		})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })
	return artifacts
}

func classifyArtifactSummaryStatus(entries []ArtifactSummaryEntry, artifacts []ArtifactSummaryArtifact) string {
	if len(entries) == 0 {
		return "empty"
	}
	if len(artifacts) == 0 {
		return "unavailable"
	}

	available := 0
	empty := 0
	unavailable := 0
	for _, artifact := range artifacts {
		switch artifact.Coverage.Status {
		case "available":
			available++
		case "empty":
			empty++
		case "unavailable":
			unavailable++
		default:
			return "partial"
		}
	}

	switch {
	case available == len(artifacts):
		return "available"
	case unavailable == len(artifacts):
		return "unavailable"
	case empty == len(artifacts):
		return "empty"
	default:
		return "partial"
	}
}

func countSummaryVenues(entries []ArtifactSummaryEntry) int {
	set := make(map[string]struct{})
	for _, entry := range entries {
		set[entry.Venue] = struct{}{}
	}
	return len(set)
}
