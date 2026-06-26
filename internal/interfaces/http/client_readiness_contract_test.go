package httpserver_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	httpserver "github.com/FabioCaffarello/stream-analytics/internal/interfaces/http"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/config"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/observability"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func TestClientReadinessContracts_ReadyPathConsistent(t *testing.T) {
	inst := "BTCUSDT_S19_CONTRACT_READY"
	nowMs := time.Now().UnixMilli()

	observability.RecordTerminalWSDelivery("stage19-contract-ready-candle", "binance", inst, "candle", 1001, nowMs, nowMs-10, 10)
	observability.RecordTerminalWSDelivery("stage19-contract-ready-stats", "binance", inst, "stats", 1002, nowMs, nowMs-12, 12)

	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{{
			Name:    "binance",
			Symbols: []config.MarketsSymbolConfig{{Ticker: inst}},
		}},
	}

	candleReader := &dashboardCandleReader{
		first: map[string]*aggdomain.CandleV1{"binance|" + inst + "|1m": {WindowStartTs: 1710000000000}},
		last:  map[string]*aggdomain.CandleV1{"binance|" + inst + "|1m": {WindowStartTs: 1710003600000}},
		prob:  map[string]*problem.Problem{},
	}
	statsReader := &dashboardStatsReader{
		first: map[string]*aggdomain.StatsWindowV1{"binance|" + inst + "|1m": {WindowStartTs: 1710000000000}},
		last:  map[string]*aggdomain.StatsWindowV1{"binance|" + inst + "|1m": {WindowStartTs: 1710003600000}},
		prob:  map[string]*problem.Problem{},
	}

	srv := newDashboardServer(t, markets, &httpserver.ColdReaders{Candles: candleReader, Stats: statsReader})

	overview := mustGetInstrumentOverview(t, srv, "binance", inst)
	dashboard := mustGetSessionDashboard(t, srv)
	summary := mustGetArtifactSummary(t, srv, "/api/v1/artifacts/summary?venue=binance&instrument="+inst)

	assertOneOf(t, "overview.status", overview.Status, "ready", "degraded", "inactive", "not_ready")
	assertOneOf(t, "overview.readiness.status", overview.Readiness.Status, "ready", "not_ready")
	assertOneOf(t, "overview.freshness.status", overview.Freshness.Status, "flowing", "stale", "inactive")
	assertOneOf(t, "overview.resync.status", overview.Resync.Status, "stable", "recovering", "degraded")

	assertOneOf(t, "dashboard.status", dashboard.Status, "ready", "degraded", "inactive", "not_ready")
	assertOneOf(t, "dashboard.readiness.status", dashboard.Readiness.Status, "ready", "not_ready")
	assertOneOf(t, "dashboard.freshness.status", dashboard.Freshness.Status, "flowing", "partial", "stale", "inactive")
	assertOneOf(t, "dashboard.resync.status", dashboard.Resync.Status, "stable", "recovering", "degraded")

	assertOneOf(t, "summary.status", summary.Status, "available", "partial", "empty", "unavailable")
	for i, art := range summary.Artifacts {
		assertOneOf(t, "summary.artifacts.coverage.status", art.Coverage.Status, "available", "partial", "empty", "unavailable")
		if art.DefaultTimeframe != "1m" {
			t.Fatalf("artifact[%d].default_timeframe=%q want=1m", i, art.DefaultTimeframe)
		}
	}
	for _, entry := range summary.Entries {
		for name, status := range entry.Artifacts {
			assertOneOf(t, "summary.entries.artifacts["+name+"]", status, "available", "empty", "unavailable")
		}
	}

	if overview.Readiness.Status != "ready" || dashboard.Readiness.Status != "ready" {
		t.Fatalf("readiness drift: overview=%q dashboard=%q", overview.Readiness.Status, dashboard.Readiness.Status)
	}
	if overview.Status != "ready" {
		t.Fatalf("overview.status=%q want=ready", overview.Status)
	}
	if dashboard.Status != "ready" {
		t.Fatalf("dashboard.status=%q want=ready", dashboard.Status)
	}
	if summary.Status != "available" {
		t.Fatalf("summary.status=%q want=available", summary.Status)
	}
}

func TestClientReadinessContracts_DegradedPathConsistent(t *testing.T) {
	instA := "BTCUSDT_S19_CONTRACT_DEG_A"
	instB := "ETHUSDT_S19_CONTRACT_DEG_B"
	nowMs := time.Now().UnixMilli()
	staleTs := time.Now().Add(-2 * time.Minute).UnixMilli()

	observability.RecordTerminalWSDelivery("stage19-contract-deg-a-candle", "binance", instA, "candle", 2001, nowMs, nowMs-8, 8)
	observability.RecordTerminalWSDelivery("stage19-contract-deg-a-stats", "binance", instA, "stats", 2002, nowMs, nowMs-11, 11)

	observability.RecordTerminalWSDelivery("stage19-contract-deg-b-candle", "binance", instB, "candle", 2101, staleTs, staleTs, 0)
	observability.RecordTerminalWSDrop("stage19-contract-deg-b-candle", "binance", instB, "candle", "slow_client")
	observability.IncTerminalWSResync("stage19-contract-deg-b-candle")

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
		prob: map[string]*problem.Problem{
			"binance|" + instB + "|1m": problem.New(problem.Internal, "stats unavailable"),
		},
	}

	srv := newDashboardServer(t, markets, &httpserver.ColdReaders{Candles: candleReader, Stats: statsReader})

	overviewB := mustGetInstrumentOverview(t, srv, "binance", instB)
	dashboard := mustGetSessionDashboard(t, srv)
	summary := mustGetArtifactSummary(t, srv, "/api/v1/artifacts/summary")

	if overviewB.Status != "degraded" {
		t.Fatalf("overviewB.status=%q want=degraded", overviewB.Status)
	}
	if overviewB.Freshness.Status != "stale" {
		t.Fatalf("overviewB.freshness.status=%q want=stale", overviewB.Freshness.Status)
	}
	if overviewB.Resync.Status != "degraded" {
		t.Fatalf("overviewB.resync.status=%q want=degraded", overviewB.Resync.Status)
	}

	if dashboard.Status != "degraded" {
		t.Fatalf("dashboard.status=%q want=degraded", dashboard.Status)
	}
	if dashboard.Freshness.Status != "partial" {
		t.Fatalf("dashboard.freshness.status=%q want=partial", dashboard.Freshness.Status)
	}
	if dashboard.Resync.Status != "degraded" {
		t.Fatalf("dashboard.resync.status=%q want=degraded", dashboard.Resync.Status)
	}

	if summary.Status != "partial" {
		t.Fatalf("summary.status=%q want=partial", summary.Status)
	}
	statsCoverage := ""
	for _, art := range summary.Artifacts {
		if art.Name == "stats" {
			statsCoverage = art.Coverage.Status
			break
		}
	}
	if statsCoverage != "partial" {
		t.Fatalf("summary stats coverage=%q want=partial", statsCoverage)
	}
}

func mustGetInstrumentOverview(t *testing.T, srv *httpserver.Server, venue, instrument string) httpserver.InstrumentOverviewResponse {
	t.Helper()
	rec := doRequest(t, srv, http.MethodGet, "/api/v1/instrument/overview?venue="+venue+"&instrument="+instrument, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("instrument overview code=%d body=%s", rec.Code, rec.Body.String())
	}
	var out httpserver.InstrumentOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("instrument overview decode: %v body=%s", err, rec.Body.String())
	}
	return out
}

func mustGetSessionDashboard(t *testing.T, srv *httpserver.Server) httpserver.SessionDashboardResponse {
	t.Helper()
	rec := doRequest(t, srv, http.MethodGet, "/api/v1/session/dashboard", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("session dashboard code=%d body=%s", rec.Code, rec.Body.String())
	}
	var out httpserver.SessionDashboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("session dashboard decode: %v body=%s", err, rec.Body.String())
	}
	return out
}

func mustGetArtifactSummary(t *testing.T, srv *httpserver.Server, path string) httpserver.ArtifactSummaryResponse {
	t.Helper()
	rec := doRequest(t, srv, http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("artifact summary code=%d body=%s", rec.Code, rec.Body.String())
	}
	var out httpserver.ArtifactSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("artifact summary decode: %v body=%s", err, rec.Body.String())
	}
	return out
}

func assertOneOf(t *testing.T, field, got string, allowed ...string) {
	t.Helper()
	for _, v := range allowed {
		if got == v {
			return
		}
	}
	t.Fatalf("%s=%q not in allowed set %v", field, got, allowed)
}
