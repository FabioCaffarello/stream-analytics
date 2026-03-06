package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/market-raccoon/internal/shared/config"
)

func TestCatalog_HappyPath(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{
				{Ticker: "BTCUSDT", MarketType: "FUTURES"},
				{Ticker: "ETHUSDT", MarketType: "FUTURES"},
			}},
		},
	}
	srv := newTestServerWithMarkets(markets)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/catalog", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp CatalogResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resp.Entries))
	}
	// Sorted by venue+instrument
	if resp.Entries[0].Instrument != "BTCUSDT" {
		t.Errorf("expected BTCUSDT first, got %s", resp.Entries[0].Instrument)
	}
	if resp.Entries[1].Instrument != "ETHUSDT" {
		t.Errorf("expected ETHUSDT second, got %s", resp.Entries[1].Instrument)
	}
	if len(resp.Entries[0].Artifacts) != 8 {
		t.Errorf("expected 8 artifacts, got %d", len(resp.Entries[0].Artifacts))
	}
}

func TestCatalog_FilterByVenue(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{{Ticker: "BTCUSDT"}}},
			{Name: "bybit", Symbols: []config.MarketsSymbolConfig{{Ticker: "BTCUSDT"}}},
		},
	}
	srv := newTestServerWithMarkets(markets)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/catalog?venue=bybit", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp CatalogResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(resp.Entries))
	}
	if resp.Entries[0].Venue != "bybit" {
		t.Errorf("expected bybit, got %s", resp.Entries[0].Venue)
	}
}

func TestCatalog_FilterByInstrument(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{
				{Ticker: "BTCUSDT"},
				{Ticker: "ETHUSDT"},
			}},
		},
	}
	srv := newTestServerWithMarkets(markets)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/catalog?instrument=ETHUSDT", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp CatalogResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(resp.Entries))
	}
	if resp.Entries[0].Instrument != "ETHUSDT" {
		t.Errorf("expected ETHUSDT, got %s", resp.Entries[0].Instrument)
	}
}

func TestCatalog_FilterBothVenueAndInstrument(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{
				{Ticker: "BTCUSDT"},
				{Ticker: "ETHUSDT"},
			}},
			{Name: "bybit", Symbols: []config.MarketsSymbolConfig{
				{Ticker: "BTCUSDT"},
			}},
		},
	}
	srv := newTestServerWithMarkets(markets)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/catalog?venue=binance&instrument=BTCUSDT", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp CatalogResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(resp.Entries))
	}
	if resp.Entries[0].Venue != "binance" || resp.Entries[0].Instrument != "BTCUSDT" {
		t.Errorf("expected binance/BTCUSDT, got %s/%s", resp.Entries[0].Venue, resp.Entries[0].Instrument)
	}
}

func TestCatalog_NoMatch(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{{Ticker: "BTCUSDT"}}},
		},
	}
	srv := newTestServerWithMarkets(markets)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/catalog?venue=kraken", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp CatalogResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(resp.Entries))
	}
}

func TestCatalog_MarketsNotConfigured(t *testing.T) {
	srv := newTestServerWithMarkets(nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/catalog", nil)
	srv.mux.ServeHTTP(rr, req)

	// Route is not registered when markets is nil, so 404 is expected.
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (route not registered), got %d", rr.Code)
	}
}

func TestCatalog_DeduplicatesSymbols(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{
				{Ticker: "BTCUSDT", MarketType: "SPOT"},
				{Ticker: "BTCUSDT", MarketType: "FUTURES"},
			}},
		},
	}
	srv := newTestServerWithMarkets(markets)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/catalog", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp CatalogResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry (deduped), got %d", len(resp.Entries))
	}
}

func TestCatalog_ArtifactShape(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{{Ticker: "BTCUSDT"}}},
		},
	}
	srv := newTestServerWithMarkets(markets)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/catalog?venue=binance&instrument=BTCUSDT", nil)
	srv.mux.ServeHTTP(rr, req)

	var resp CatalogResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(resp.Entries))
	}

	byName := make(map[string]CatalogArtifact)
	for _, a := range resp.Entries[0].Artifacts {
		byName[a.Name] = a
	}

	// candle has 9 timeframes
	candle, ok := byName["candle"]
	if !ok {
		t.Fatal("candle artifact missing")
	}
	if len(candle.Timeframes) != 9 {
		t.Errorf("candle: expected 9 timeframes, got %d", len(candle.Timeframes))
	}
	if candle.Endpoint != "/api/v1/candles" {
		t.Errorf("candle: expected /api/v1/candles, got %s", candle.Endpoint)
	}

	// tape has 3 timeframes
	tape, ok := byName["tape"]
	if !ok {
		t.Fatal("tape artifact missing")
	}
	if len(tape.Timeframes) != 3 {
		t.Errorf("tape: expected 3 timeframes, got %d", len(tape.Timeframes))
	}

	// oi has 1 timeframe (raw)
	oi, ok := byName["oi"]
	if !ok {
		t.Fatal("oi artifact missing")
	}
	if len(oi.Timeframes) != 1 || oi.Timeframes[0] != "raw" {
		t.Errorf("oi: expected [raw], got %v", oi.Timeframes)
	}
}

func TestCatalog_MultiExchangeSorted(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "bybit", Symbols: []config.MarketsSymbolConfig{{Ticker: "ETHUSDT"}}},
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{{Ticker: "BTCUSDT"}}},
		},
	}
	srv := newTestServerWithMarkets(markets)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/catalog", nil)
	srv.mux.ServeHTTP(rr, req)

	var resp CatalogResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resp.Entries))
	}
	if resp.Entries[0].Venue != "binance" {
		t.Errorf("expected binance first (sorted), got %s", resp.Entries[0].Venue)
	}
	if resp.Entries[1].Venue != "bybit" {
		t.Errorf("expected bybit second (sorted), got %s", resp.Entries[1].Venue)
	}
}

// newTestServerWithMarkets creates a minimal Server with markets config for testing.
func newTestServerWithMarkets(markets *config.MarketsConfig) *Server {
	s := &Server{
		markets: markets,
	}
	mux := http.NewServeMux()
	if s.markets != nil {
		mux.HandleFunc("GET /api/v1/catalog", s.handleGetCatalog)
	}
	s.mux = mux
	return s
}
