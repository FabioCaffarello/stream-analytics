package httpserver

import (
	"net/http"
	"strconv"

	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
)

// handleGetMarkets serves GET /api/v1/markets and returns all configured
// exchanges and symbols for client market discovery.
func (s *Server) handleGetMarkets(w http.ResponseWriter, _ *http.Request) {
	if s.markets == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "markets not configured"})
		return
	}
	writeJSON(w, http.StatusOK, s.markets)
}

// ColdReaders holds optional ClickHouse-backed readers for cold data APIs.
// Each field may be nil if the corresponding reader is not wired.
type ColdReaders struct {
	Candles   aggports.CandleReader
	Stats     aggports.StatsReader
	Snapshots aggports.SnapshotReader
}

// handleGetCandles serves GET /api/v1/candles with query parameters:
//
//	venue, instrument, timeframe, fromMs, toMs, limit
func (s *Server) handleGetCandles(w http.ResponseWriter, r *http.Request) {
	if s.coldReaders == nil || s.coldReaders.Candles == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "candle reader not available"})
		return
	}

	venue := r.URL.Query().Get("venue")
	instrument := r.URL.Query().Get("instrument")
	timeframe := r.URL.Query().Get("timeframe")
	fromMsStr := r.URL.Query().Get("fromMs")
	toMsStr := r.URL.Query().Get("toMs")
	limitStr := r.URL.Query().Get("limit")

	if venue == "" || instrument == "" || timeframe == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "venue, instrument, and timeframe are required"})
		return
	}

	fromMs, err := strconv.ParseInt(fromMsStr, 10, 64)
	if err != nil || fromMs < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid fromMs"})
		return
	}
	toMs, err := strconv.ParseInt(toMsStr, 10, 64)
	if err != nil || toMs < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid toMs"})
		return
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 1000
	}

	candles, p := s.coldReaders.Candles.GetCandleRange(r.Context(), venue, instrument, timeframe, fromMs, toMs, limit)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	writeJSON(w, http.StatusOK, candles)
}

// handleGetStats serves GET /api/v1/stats with query parameters:
//
//	venue, instrument, timeframe, fromMs, toMs, limit
func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	if s.coldReaders == nil || s.coldReaders.Stats == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "stats reader not available"})
		return
	}

	venue := r.URL.Query().Get("venue")
	instrument := r.URL.Query().Get("instrument")
	timeframe := r.URL.Query().Get("timeframe")
	fromMsStr := r.URL.Query().Get("fromMs")
	toMsStr := r.URL.Query().Get("toMs")
	limitStr := r.URL.Query().Get("limit")

	if venue == "" || instrument == "" || timeframe == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "venue, instrument, and timeframe are required"})
		return
	}

	fromMs, err := strconv.ParseInt(fromMsStr, 10, 64)
	if err != nil || fromMs < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid fromMs"})
		return
	}
	toMs, err := strconv.ParseInt(toMsStr, 10, 64)
	if err != nil || toMs < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid toMs"})
		return
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 1000
	}

	stats, p := s.coldReaders.Stats.GetStatsRange(r.Context(), venue, instrument, timeframe, fromMs, toMs, limit)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// handleGetSnapshots serves GET /api/v1/snapshots with query parameters:
//
//	venue, instrument, fromMs, toMs
func (s *Server) handleGetSnapshots(w http.ResponseWriter, r *http.Request) {
	if s.coldReaders == nil || s.coldReaders.Snapshots == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "snapshot reader not available"})
		return
	}

	venue := r.URL.Query().Get("venue")
	instrument := r.URL.Query().Get("instrument")
	fromMsStr := r.URL.Query().Get("fromMs")
	toMsStr := r.URL.Query().Get("toMs")

	if venue == "" || instrument == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "venue and instrument are required"})
		return
	}

	fromMs, err := strconv.ParseInt(fromMsStr, 10, 64)
	if err != nil || fromMs < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid fromMs"})
		return
	}
	toMs, err := strconv.ParseInt(toMsStr, 10, 64)
	if err != nil || toMs < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid toMs"})
		return
	}

	timestamps, p := s.coldReaders.Snapshots.GetSnapshotTimestamps(r.Context(), venue, instrument, fromMs, toMs)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	writeJSON(w, http.StatusOK, timestamps)
}
