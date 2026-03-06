package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/observability"
)

func TestHandleGetFreshness_MissingParams(t *testing.T) {
	srv := NewServer(nil, nil, ":0", false, nil)

	tests := []struct {
		name string
		url  string
	}{
		{"no params", "/api/v1/freshness"},
		{"venue only", "/api/v1/freshness?venue=binance"},
		{"instrument only", "/api/v1/freshness?instrument=BTCUSDT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()
			srv.handleGetFreshness(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestHandleGetFreshness_NoStreams(t *testing.T) {
	srv := NewServer(nil, nil, ":0", false, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/freshness?venue=binance&instrument=BTCUSDT", nil)
	w := httptest.NewRecorder()
	srv.handleGetFreshness(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp FreshnessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Venue != "binance" {
		t.Errorf("expected venue=binance, got %s", resp.Venue)
	}
	if resp.Instrument != "BTCUSDT" {
		t.Errorf("expected instrument=BTCUSDT, got %s", resp.Instrument)
	}
	if resp.Active {
		t.Error("expected active=false with no streams")
	}
	if len(resp.Channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(resp.Channels))
	}
	if resp.CheckedAt <= 0 {
		t.Error("expected checked_at > 0")
	}
}

func TestHandleGetFreshness_WithFlowingStream(t *testing.T) {
	// Seed a recent delivery into the global terminal WS store.
	nowMs := currentTimeMs()
	observability.RecordTerminalWSDelivery(
		"test-stream-freshness-1",
		"binance", "BTCUSDT", "candle",
		100, nowMs, nowMs-50, 50,
	)

	srv := NewServer(nil, nil, ":0", false, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/freshness?venue=binance&instrument=BTCUSDT", nil)
	w := httptest.NewRecorder()
	srv.handleGetFreshness(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp FreshnessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !resp.Active {
		t.Error("expected active=true with recent stream")
	}

	ch, ok := resp.Channels["candle"]
	if !ok {
		t.Fatal("expected candle channel in response")
	}
	if !ch.Flowing {
		t.Error("expected candle channel flowing=true")
	}
	if ch.LagMs != 50 {
		t.Errorf("expected lag_ms=50, got %d", ch.LagMs)
	}
}

func TestHandleGetFreshness_CaseInsensitiveMatch(t *testing.T) {
	nowMs := currentTimeMs()
	observability.RecordTerminalWSDelivery(
		"test-stream-freshness-ci",
		"Binance", "btcusdt", "stats",
		200, nowMs, nowMs-10, 10,
	)

	srv := NewServer(nil, nil, ":0", false, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/freshness?venue=BINANCE&instrument=btcusdt", nil)
	w := httptest.NewRecorder()
	srv.handleGetFreshness(w, req)

	var resp FreshnessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := resp.Channels["stats"]; !ok {
		t.Error("expected stats channel matched case-insensitively")
	}
}

func TestHandleGetFreshness_NoMatchDifferentInstrument(t *testing.T) {
	nowMs := currentTimeMs()
	observability.RecordTerminalWSDelivery(
		"test-stream-freshness-nomatch",
		"binance", "ETHUSDT", "candle",
		300, nowMs, nowMs-10, 10,
	)

	srv := NewServer(nil, nil, ":0", false, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/freshness?venue=binance&instrument=SOLUSDT", nil)
	w := httptest.NewRecorder()
	srv.handleGetFreshness(w, req)

	var resp FreshnessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Active {
		t.Error("expected active=false for unmatched instrument")
	}
	if len(resp.Channels) != 0 {
		t.Errorf("expected 0 channels for unmatched instrument, got %d", len(resp.Channels))
	}
}

func TestHandleGetFreshness_JSONShape(t *testing.T) {
	srv := NewServer(nil, nil, ":0", false, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/freshness?venue=x&instrument=Y", nil)
	w := httptest.NewRecorder()
	srv.handleGetFreshness(w, req)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}

	expectedKeys := []string{"venue", "instrument", "active", "channels", "checked_at"}
	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing top-level key: %s", key)
		}
	}
}

func currentTimeMs() int64 {
	return time.Now().UnixMilli()
}
