package httpserver

import (
	"net/http"
)

// TimelineResponse describes the time range available for one artifact
// on a given venue/instrument/timeframe triple.
type TimelineResponse struct {
	Venue      string `json:"venue"`
	Instrument string `json:"instrument"`
	Timeframe  string `json:"timeframe"`
	Artifact   string `json:"artifact"`
	FirstTs    int64  `json:"first_ts"`
	LastTs     int64  `json:"last_ts"`
}

// handleGetTimeline serves GET /api/v1/timeline with query parameters:
//
//	venue, instrument, timeframe, artifact (candle|stats, default: candle)
//
// Returns the earliest and latest window_start timestamps for the requested
// artifact, enabling clients to discover available data ranges without
// transferring full payloads. The response hides hot/cold/federation
// details — the caller sees a single unified time range.
func (s *Server) handleGetTimeline(w http.ResponseWriter, r *http.Request) {
	if s.coldReaders == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "readers not available"})
		return
	}

	venue := r.URL.Query().Get("venue")
	instrument := r.URL.Query().Get("instrument")
	timeframe := r.URL.Query().Get("timeframe")
	artifact := r.URL.Query().Get("artifact")

	if venue == "" || instrument == "" || timeframe == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "venue, instrument, and timeframe are required"})
		return
	}
	if artifact == "" {
		artifact = "candle"
	}

	switch artifact {
	case "candle":
		s.handleCandleTimeline(w, r, venue, instrument, timeframe)
	case "stats":
		s.handleStatsTimeline(w, r, venue, instrument, timeframe)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "unsupported artifact",
			"supported": []string{"candle", "stats"},
		})
	}
}

func (s *Server) handleCandleTimeline(w http.ResponseWriter, r *http.Request, venue, instrument, timeframe string) {
	if s.coldReaders.Candles == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "candle reader not available"})
		return
	}
	first, p := s.coldReaders.Candles.GetFirstCandle(r.Context(), venue, instrument, timeframe)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	last, p := s.coldReaders.Candles.GetLastCandle(r.Context(), venue, instrument, timeframe)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	resp := TimelineResponse{
		Venue:      venue,
		Instrument: instrument,
		Timeframe:  timeframe,
		Artifact:   "candle",
	}
	if first != nil {
		resp.FirstTs = first.WindowStartTs
	}
	if last != nil {
		resp.LastTs = last.WindowStartTs
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleStatsTimeline(w http.ResponseWriter, r *http.Request, venue, instrument, timeframe string) {
	if s.coldReaders.Stats == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "stats reader not available"})
		return
	}
	first, p := s.coldReaders.Stats.GetFirstStats(r.Context(), venue, instrument, timeframe)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	last, p := s.coldReaders.Stats.GetLastStats(r.Context(), venue, instrument, timeframe)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	resp := TimelineResponse{
		Venue:      venue,
		Instrument: instrument,
		Timeframe:  timeframe,
		Artifact:   "stats",
	}
	if first != nil {
		resp.FirstTs = first.WindowStartTs
	}
	if last != nil {
		resp.LastTs = last.WindowStartTs
	}
	writeJSON(w, http.StatusOK, resp)
}
