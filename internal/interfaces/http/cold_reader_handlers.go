package httpserver

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/config"
)

// handleGetMarkets serves GET /api/v1/markets and returns all configured
// exchanges and symbols for client market discovery.
func (s *Server) handleGetMarkets(w http.ResponseWriter, _ *http.Request) {
	if s.markets == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "markets not configured"})
		return
	}
	writeJSON(w, http.StatusOK, normalizeMarketsConfig(s.markets))
}

func normalizeMarketsConfig(in *config.MarketsConfig) config.MarketsConfig {
	if in == nil {
		return config.MarketsConfig{}
	}
	type exchangeAccumulator struct {
		name    string
		symbols map[string]config.MarketsSymbolConfig
	}

	byExchange := make(map[string]*exchangeAccumulator, len(in.Exchanges))
	for _, ex := range in.Exchanges {
		name := strings.ToLower(strings.TrimSpace(ex.Name))
		if name == "" {
			continue
		}
		acc, ok := byExchange[name]
		if !ok {
			acc = &exchangeAccumulator{
				name:    name,
				symbols: make(map[string]config.MarketsSymbolConfig, len(ex.Symbols)),
			}
			byExchange[name] = acc
		}
		for _, sym := range ex.Symbols {
			ticker := strings.ToUpper(strings.TrimSpace(sym.Ticker))
			if ticker == "" {
				continue
			}
			marketType := strings.ToUpper(strings.TrimSpace(sym.MarketType))
			tickSize := sym.TickSize
			if tickSize < 0 {
				tickSize = 0
			}
			key := ticker + "|" + marketType
			if _, exists := acc.symbols[key]; exists {
				continue
			}
			acc.symbols[key] = config.MarketsSymbolConfig{
				Ticker:     ticker,
				TickSize:   tickSize,
				MarketType: marketType,
			}
		}
	}

	out := config.MarketsConfig{
		Exchanges: make([]config.MarketsExchangeConfig, 0, len(byExchange)),
	}
	exchangeNames := make([]string, 0, len(byExchange))
	for name := range byExchange {
		exchangeNames = append(exchangeNames, name)
	}
	sort.Strings(exchangeNames)

	for _, name := range exchangeNames {
		acc := byExchange[name]
		symbols := make([]config.MarketsSymbolConfig, 0, len(acc.symbols))
		for _, sym := range acc.symbols {
			symbols = append(symbols, sym)
		}
		sort.Slice(symbols, func(i, j int) bool {
			if symbols[i].Ticker == symbols[j].Ticker {
				return symbols[i].MarketType < symbols[j].MarketType
			}
			return symbols[i].Ticker < symbols[j].Ticker
		})
		out.Exchanges = append(out.Exchanges, config.MarketsExchangeConfig{
			Name:    acc.name,
			Symbols: symbols,
		})
	}
	return out
}

// ColdReaders holds optional ClickHouse-backed readers for cold data APIs.
// Each field may be nil if the corresponding reader is not wired.
type ColdReaders struct {
	Candles     aggports.CandleReader
	Stats       aggports.StatsReader
	Snapshots   aggports.SnapshotReader
	Tape        aggports.TapeReader
	OI          aggports.OIReader
	DeltaVolume aggports.DeltaVolumeReader
	CVD         aggports.CVDReader
	BarStats    aggports.BarStatsReader
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

// handleGetTape serves GET /api/v1/tape with query parameters:
//
//	venue, instrument, timeframe, fromMs, toMs, limit
func (s *Server) handleGetTape(w http.ResponseWriter, r *http.Request) {
	if s.coldReaders == nil || s.coldReaders.Tape == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "tape reader not available"})
		return
	}
	venue, instrument, timeframe, fromMs, toMs, limit, ok := parseWindowRangeParams(w, r)
	if !ok {
		return
	}
	rows, p := s.coldReaders.Tape.GetTapeRange(r.Context(), venue, instrument, timeframe, fromMs, toMs, limit)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// handleGetOI serves GET /api/v1/oi with query parameters:
//
//	venue, instrument, timeframe, fromMs, toMs, limit
func (s *Server) handleGetOI(w http.ResponseWriter, r *http.Request) {
	if s.coldReaders == nil || s.coldReaders.OI == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "oi reader not available"})
		return
	}
	venue, instrument, timeframe, fromMs, toMs, limit, ok := parseWindowRangeParams(w, r)
	if !ok {
		return
	}
	rows, p := s.coldReaders.OI.GetOIRange(r.Context(), venue, instrument, timeframe, fromMs, toMs, limit)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// handleGetDeltaVolume serves GET /api/v1/delta_volume with query parameters:
//
//	venue, instrument, timeframe, fromMs, toMs, limit
func (s *Server) handleGetDeltaVolume(w http.ResponseWriter, r *http.Request) {
	if s.coldReaders == nil || s.coldReaders.DeltaVolume == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "delta_volume reader not available"})
		return
	}
	venue, instrument, timeframe, fromMs, toMs, limit, ok := parseWindowRangeParams(w, r)
	if !ok {
		return
	}
	rows, p := s.coldReaders.DeltaVolume.GetDeltaVolumeRange(r.Context(), venue, instrument, timeframe, fromMs, toMs, limit)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// handleGetCVD serves GET /api/v1/cvd with query parameters:
//
//	venue, instrument, timeframe, fromMs, toMs, limit
func (s *Server) handleGetCVD(w http.ResponseWriter, r *http.Request) {
	if s.coldReaders == nil || s.coldReaders.CVD == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cvd reader not available"})
		return
	}
	venue, instrument, timeframe, fromMs, toMs, limit, ok := parseWindowRangeParams(w, r)
	if !ok {
		return
	}
	rows, p := s.coldReaders.CVD.GetCVDRange(r.Context(), venue, instrument, timeframe, fromMs, toMs, limit)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// handleGetBarStats serves GET /api/v1/bar_stats with query parameters:
//
//	venue, instrument, timeframe, fromMs, toMs, limit
func (s *Server) handleGetBarStats(w http.ResponseWriter, r *http.Request) {
	if s.coldReaders == nil || s.coldReaders.BarStats == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "bar_stats reader not available"})
		return
	}
	venue, instrument, timeframe, fromMs, toMs, limit, ok := parseWindowRangeParams(w, r)
	if !ok {
		return
	}
	rows, p := s.coldReaders.BarStats.GetBarStatsRange(r.Context(), venue, instrument, timeframe, fromMs, toMs, limit)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// handleConsistencyCheck serves GET /api/v1/consistency with query parameters:
//
//	artifact (candle|stats), venue, instrument, timeframe, fromMs, toMs
func (s *Server) handleConsistencyCheck(w http.ResponseWriter, r *http.Request) {
	artifact := r.URL.Query().Get("artifact")
	checkFn, ok := s.consistencyChecks[artifact]
	if !ok {
		available := make([]string, 0, len(s.consistencyChecks))
		for k := range s.consistencyChecks {
			available = append(available, k)
		}
		sort.Strings(available)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "unknown or missing artifact",
			"available": available,
		})
		return
	}

	venue, instrument, timeframe, fromMs, toMs, _, ok := parseWindowRangeParams(w, r)
	if !ok {
		return
	}
	result, err := checkFn(r.Context(), venue, instrument, timeframe, fromMs, toMs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// parseWindowRangeParams extracts common query parameters for windowed range queries.
// Returns false and writes a 400 response if validation fails.
func parseWindowRangeParams(w http.ResponseWriter, r *http.Request) (venue, instrument, timeframe string, fromMs, toMs int64, limit int, ok bool) {
	venue = r.URL.Query().Get("venue")
	instrument = r.URL.Query().Get("instrument")
	timeframe = r.URL.Query().Get("timeframe")

	if venue == "" || instrument == "" || timeframe == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "venue, instrument, and timeframe are required"})
		return "", "", "", 0, 0, 0, false
	}

	var err error
	fromMs, err = strconv.ParseInt(r.URL.Query().Get("fromMs"), 10, 64)
	if err != nil || fromMs < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid fromMs"})
		return "", "", "", 0, 0, 0, false
	}
	toMs, err = strconv.ParseInt(r.URL.Query().Get("toMs"), 10, 64)
	if err != nil || toMs < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid toMs"})
		return "", "", "", 0, 0, 0, false
	}
	limit, err = strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		limit = 1000
	}
	return venue, instrument, timeframe, fromMs, toMs, limit, true
}
