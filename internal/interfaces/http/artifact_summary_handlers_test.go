package httpserver_test

import (
	"encoding/json"
	"net/http"
	"testing"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	httpserver "github.com/FabioCaffarello/stream-analytics/internal/interfaces/http"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/config"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func TestArtifactSummary_HappyPath(t *testing.T) {
	instA := "BTCUSDT_S19_ART_A"
	instB := "ETHUSDT_S19_ART_B"

	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{{
			Name: "binance",
			Symbols: []config.MarketsSymbolConfig{
				{Ticker: instA},
				{Ticker: instB},
			},
		}},
	}

	candleReader := &dashboardCandleReader{
		first: map[string]*aggdomain.CandleV1{
			"binance|" + instA + "|1m": {WindowStartTs: 1710000000000},
			"binance|" + instB + "|1m": {WindowStartTs: 1710000000000},
		},
		last: map[string]*aggdomain.CandleV1{
			"binance|" + instA + "|1m": {WindowStartTs: 1710003600000},
			"binance|" + instB + "|1m": {WindowStartTs: 1710003600000},
		},
		prob: map[string]*problem.Problem{},
	}
	statsReader := &dashboardStatsReader{
		first: map[string]*aggdomain.StatsWindowV1{
			"binance|" + instA + "|1m": {WindowStartTs: 1710000000000},
		},
		last: map[string]*aggdomain.StatsWindowV1{
			"binance|" + instA + "|1m": {WindowStartTs: 1710003600000},
		},
		prob: map[string]*problem.Problem{},
	}

	srv := newDashboardServer(t, markets, &httpserver.ColdReaders{
		Candles: candleReader,
		Stats:   statsReader,
	})

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/artifacts/summary", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp httpserver.ArtifactSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v body=%s", err, rec.Body.String())
	}

	if resp.Timeframe != "1m" {
		t.Fatalf("timeframe=%q want=1m", resp.Timeframe)
	}
	if resp.Status != "partial" {
		t.Fatalf("status=%q want=partial", resp.Status)
	}
	if resp.Summary.Venues != 1 || resp.Summary.Instruments != 2 || resp.Summary.Entries != 2 {
		t.Fatalf("summary=%+v want venues=1 instruments=2 entries=2", resp.Summary)
	}
	if len(resp.Artifacts) != 2 {
		t.Fatalf("artifacts=%d want=2", len(resp.Artifacts))
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("entries=%d want=2", len(resp.Entries))
	}

	artifactMap := map[string]httpserver.ArtifactSummaryArtifact{}
	for _, a := range resp.Artifacts {
		artifactMap[a.Name] = a
	}
	if artifactMap["candle"].Coverage.Status != "available" {
		t.Fatalf("candle coverage=%+v want status=available", artifactMap["candle"].Coverage)
	}
	if artifactMap["stats"].Coverage.Status != "partial" || artifactMap["stats"].Coverage.AvailableInstruments != 1 || artifactMap["stats"].Coverage.EmptyInstruments != 1 {
		t.Fatalf("stats coverage=%+v want partial with available=1 empty=1", artifactMap["stats"].Coverage)
	}

	for _, entry := range resp.Entries {
		if entry.Artifacts["candle"] != "available" {
			t.Fatalf("entry candle status=%q want=available", entry.Artifacts["candle"])
		}
	}
}

func TestArtifactSummary_FilterByVenueInstrumentArtifactAndTimeframe(t *testing.T) {
	inst := "BTCUSDT_S19_ART_FILTER"
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{{
			Name:    "binance",
			Symbols: []config.MarketsSymbolConfig{{Ticker: inst}},
		}},
	}

	candleReader := &dashboardCandleReader{
		first: map[string]*aggdomain.CandleV1{
			"binance|" + inst + "|5m": {WindowStartTs: 1710000000000},
		},
		last: map[string]*aggdomain.CandleV1{
			"binance|" + inst + "|5m": {WindowStartTs: 1710003600000},
		},
		prob: map[string]*problem.Problem{},
	}

	srv := newDashboardServer(t, markets, &httpserver.ColdReaders{Candles: candleReader})
	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/artifacts/summary?venue=binance&instrument="+inst+"&artifact=candle&timeframe=5m", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp httpserver.ArtifactSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v body=%s", err, rec.Body.String())
	}

	if resp.Timeframe != "5m" {
		t.Fatalf("timeframe=%q want=5m", resp.Timeframe)
	}
	if resp.Status != "available" {
		t.Fatalf("status=%q want=available", resp.Status)
	}
	if len(resp.Artifacts) != 1 || resp.Artifacts[0].Name != "candle" {
		t.Fatalf("artifacts=%+v want single candle", resp.Artifacts)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("entries=%d want=1", len(resp.Entries))
	}
	if len(resp.Entries[0].Artifacts) != 1 || resp.Entries[0].Artifacts["candle"] != "available" {
		t.Fatalf("entry artifacts=%+v want single candle=available", resp.Entries[0].Artifacts)
	}
}

func TestArtifactSummary_InvalidTimeframe(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{{Name: "binance", Symbols: []config.MarketsSymbolConfig{{Ticker: "BTCUSDT_S19_ART_INVALID_TF"}}}},
	}
	srv := newDashboardServer(t, markets, nil)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/artifacts/summary?timeframe=raw", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestArtifactSummary_InvalidArtifact(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{{Name: "binance", Symbols: []config.MarketsSymbolConfig{{Ticker: "BTCUSDT_S19_ART_INVALID_ART"}}}},
	}
	srv := newDashboardServer(t, markets, nil)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/artifacts/summary?artifact=heatmap", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestArtifactSummary_RouteNotRegisteredWithoutMarkets(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)
	srv := newTestServer(e, guardianPID)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/artifacts/summary", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestArtifactSummary_JSONShape(t *testing.T) {
	inst := "BTCUSDT_S19_ART_SHAPE"
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{{Name: "binance", Symbols: []config.MarketsSymbolConfig{{Ticker: inst}}}},
	}
	srv := newDashboardServer(t, markets, nil)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/artifacts/summary", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	for _, key := range []string{"timeframe", "status", "checked_at", "filters", "artifacts", "entries", "summary"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing key %q", key)
		}
	}
}
