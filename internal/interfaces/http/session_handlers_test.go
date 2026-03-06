package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/market-raccoon/internal/shared/config"
)

func TestHandleGetSession_HappyPath(t *testing.T) {
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
	srv := NewServer(nil, nil, ":0", false, nil, WithMarkets(markets))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	w := httptest.NewRecorder()
	srv.handleGetSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp SessionOverviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.ServerTimeMs <= 0 {
		t.Error("server_time_ms should be positive")
	}

	// readiness is false because no engine/guardian wired
	if resp.Ready {
		t.Error("expected ready=false with nil engine")
	}

	if len(resp.Markets) != 2 {
		t.Fatalf("expected 2 markets, got %d", len(resp.Markets))
	}
	if resp.Markets[0].Venue != "binance" {
		t.Errorf("expected first venue=binance, got %s", resp.Markets[0].Venue)
	}
	if len(resp.Markets[0].Instruments) != 2 {
		t.Errorf("expected 2 instruments for binance, got %d", len(resp.Markets[0].Instruments))
	}
	if resp.Markets[1].Venue != "bybit" {
		t.Errorf("expected second venue=bybit, got %s", resp.Markets[1].Venue)
	}

	if len(resp.Capabilities.Artifacts) != 8 {
		t.Errorf("expected 8 artifacts, got %d", len(resp.Capabilities.Artifacts))
	}
}

func TestHandleGetSession_NoMarkets(t *testing.T) {
	srv := NewServer(nil, nil, ":0", false, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	w := httptest.NewRecorder()
	srv.handleGetSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp SessionOverviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Markets) != 0 {
		t.Errorf("expected 0 markets, got %d", len(resp.Markets))
	}
	if len(resp.Capabilities.Artifacts) != 8 {
		t.Errorf("expected 8 artifacts even without markets, got %d", len(resp.Capabilities.Artifacts))
	}
}

func TestHandleGetSession_DedupInstruments(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{
				{Ticker: "BTCUSDT", MarketType: "SPOT"},
				{Ticker: "BTCUSDT", MarketType: "FUTURES"},
			}},
		},
	}
	srv := NewServer(nil, nil, ":0", false, nil, WithMarkets(markets))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	w := httptest.NewRecorder()
	srv.handleGetSession(w, req)

	var resp SessionOverviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Markets) != 1 {
		t.Fatalf("expected 1 market, got %d", len(resp.Markets))
	}
	// BTCUSDT should appear only once (deduped across market types)
	if len(resp.Markets[0].Instruments) != 1 {
		t.Errorf("expected 1 instrument (deduped), got %d", len(resp.Markets[0].Instruments))
	}
}

func TestHandleGetSession_SortedOutput(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "kraken", Symbols: []config.MarketsSymbolConfig{{Ticker: "ETHUSDT"}}},
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{
				{Ticker: "ETHUSDT"},
				{Ticker: "BTCUSDT"},
			}},
		},
	}
	srv := NewServer(nil, nil, ":0", false, nil, WithMarkets(markets))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	w := httptest.NewRecorder()
	srv.handleGetSession(w, req)

	var resp SessionOverviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Markets[0].Venue != "binance" {
		t.Errorf("expected first venue=binance (sorted), got %s", resp.Markets[0].Venue)
	}
	if resp.Markets[1].Venue != "kraken" {
		t.Errorf("expected second venue=kraken (sorted), got %s", resp.Markets[1].Venue)
	}
	if resp.Markets[0].Instruments[0] != "BTCUSDT" {
		t.Errorf("expected first instrument=BTCUSDT (sorted), got %s", resp.Markets[0].Instruments[0])
	}
}

func TestHandleGetSession_EmptyExchangeNameSkipped(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "", Symbols: []config.MarketsSymbolConfig{{Ticker: "BTCUSDT"}}},
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{{Ticker: "BTCUSDT"}}},
		},
	}
	srv := NewServer(nil, nil, ":0", false, nil, WithMarkets(markets))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	w := httptest.NewRecorder()
	srv.handleGetSession(w, req)

	var resp SessionOverviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Markets) != 1 {
		t.Errorf("expected 1 market (empty name skipped), got %d", len(resp.Markets))
	}
}

func TestHandleGetSession_JSONShape(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{
			{Name: "binance", Symbols: []config.MarketsSymbolConfig{{Ticker: "BTCUSDT"}}},
		},
	}
	srv := NewServer(nil, nil, ":0", false, nil, WithMarkets(markets))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	w := httptest.NewRecorder()
	srv.handleGetSession(w, req)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}

	expectedKeys := []string{"server_time_ms", "ready", "markets", "capabilities"}
	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing top-level key: %s", key)
		}
	}
}
