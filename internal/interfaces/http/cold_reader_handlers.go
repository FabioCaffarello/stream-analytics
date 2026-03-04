package httpserver

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/config"
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
