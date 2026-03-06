package httpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
)

type dashboardCandleReader struct {
	first map[string]*aggdomain.CandleV1
	last  map[string]*aggdomain.CandleV1
	prob  map[string]*problem.Problem
}

func (r *dashboardCandleReader) key(venue, instrument, timeframe string) string {
	return venue + "|" + instrument + "|" + timeframe
}

func (r *dashboardCandleReader) GetCandleRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.CandleV1, *problem.Problem) {
	return nil, nil
}
func (r *dashboardCandleReader) GetCandleTimestamps(_ context.Context, _, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return nil, nil
}
func (r *dashboardCandleReader) GetFirstCandle(_ context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	if p := r.prob[r.key(venue, instrument, timeframe)]; p != nil {
		return nil, p
	}
	return r.first[r.key(venue, instrument, timeframe)], nil
}
func (r *dashboardCandleReader) GetLastCandle(_ context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	if p := r.prob[r.key(venue, instrument, timeframe)]; p != nil {
		return nil, p
	}
	return r.last[r.key(venue, instrument, timeframe)], nil
}

type dashboardStatsReader struct {
	first map[string]*aggdomain.StatsWindowV1
	last  map[string]*aggdomain.StatsWindowV1
	prob  map[string]*problem.Problem
}

func (r *dashboardStatsReader) key(venue, instrument, timeframe string) string {
	return venue + "|" + instrument + "|" + timeframe
}

func (r *dashboardStatsReader) GetStatsRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.StatsWindowV1, *problem.Problem) {
	return nil, nil
}
func (r *dashboardStatsReader) GetStatsTimestamps(_ context.Context, _, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return nil, nil
}
func (r *dashboardStatsReader) GetFirstStats(_ context.Context, venue, instrument, timeframe string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	if p := r.prob[r.key(venue, instrument, timeframe)]; p != nil {
		return nil, p
	}
	return r.first[r.key(venue, instrument, timeframe)], nil
}
func (r *dashboardStatsReader) GetLastStats(_ context.Context, venue, instrument, timeframe string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	if p := r.prob[r.key(venue, instrument, timeframe)]; p != nil {
		return nil, p
	}
	return r.last[r.key(venue, instrument, timeframe)], nil
}

func newDashboardServer(t *testing.T, markets *config.MarketsConfig, readers *httpserver.ColdReaders) *httpserver.Server {
	t.Helper()
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	t.Cleanup(func() { e.Poison(guardianPID) })

	opts := make([]httpserver.Option, 0, 2)
	if markets != nil {
		opts = append(opts, httpserver.WithMarkets(markets))
	}
	if readers != nil {
		opts = append(opts, httpserver.WithColdReaders(readers))
	}

	srv := httpserver.NewServer(e, guardianPID, ":0", false, nil, opts...)
	srv.SetSnapshotTimeout(2 * time.Second)
	return srv
}

func TestSessionDashboard_ComposesGlobalStatusesAndArtifactMatrix(t *testing.T) {
	instA := "BTCUSDT_S19_DASH_A"
	instB := "ETHUSDT_S19_DASH_B"
	nowMs := time.Now().UnixMilli()
	staleTs := time.Now().Add(-2 * time.Minute).UnixMilli()

	observability.RecordTerminalWSDelivery("stage19-dash-a-candle", "binance", instA, "candle", 100, nowMs, nowMs-15, 15)
	observability.RecordTerminalWSDelivery("stage19-dash-a-stats", "binance", instA, "stats", 101, nowMs, nowMs-12, 12)
	observability.IncTerminalWSResync("stage19-dash-a-candle")
	observability.RecordTerminalWSDelivery("stage19-dash-b-candle", "binance", instB, "candle", 88, staleTs, staleTs, 0)
	observability.RecordTerminalWSDrop("stage19-dash-b-candle", "binance", instB, "candle", "slow_client")

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

	srv := newDashboardServer(t, markets, &httpserver.ColdReaders{
		Candles: candleReader,
		Stats:   statsReader,
	})

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/session/dashboard", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp httpserver.SessionDashboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v body=%s", err, rec.Body.String())
	}

	if resp.Readiness.Status != "ready" {
		t.Fatalf("readiness.status=%q want=ready", resp.Readiness.Status)
	}
	if resp.Freshness.Status != "partial" {
		t.Fatalf("freshness.status=%q want=partial", resp.Freshness.Status)
	}
	if resp.Freshness.ActiveInstruments != 1 || resp.Freshness.StaleInstruments != 1 {
		t.Fatalf("freshness instruments active=%d stale=%d want=1/1", resp.Freshness.ActiveInstruments, resp.Freshness.StaleInstruments)
	}
	if resp.Resync.Status != "degraded" {
		t.Fatalf("resync.status=%q want=degraded", resp.Resync.Status)
	}
	if resp.Status != "degraded" {
		t.Fatalf("status=%q want=degraded", resp.Status)
	}
	if resp.Summary.Venues != 1 || resp.Summary.Instruments != 2 {
		t.Fatalf("summary venues=%d instruments=%d want=1/2", resp.Summary.Venues, resp.Summary.Instruments)
	}

	if len(resp.Artifacts) != 2 {
		t.Fatalf("artifacts=%d want=2", len(resp.Artifacts))
	}

	artifact := map[string]httpserver.SessionDashboardArtifactStatus{}
	for _, item := range resp.Artifacts {
		artifact[item.Name] = item
	}
	if artifact["candle"].Coverage.Status != "available" || artifact["candle"].Coverage.AvailableInstruments != 2 {
		t.Fatalf("candle coverage=%+v want status=available available=2", artifact["candle"].Coverage)
	}
	if artifact["stats"].Coverage.Status != "partial" || artifact["stats"].Coverage.AvailableInstruments != 1 || artifact["stats"].Coverage.UnavailableInstruments != 1 {
		t.Fatalf("stats coverage=%+v want status=partial available=1 unavailable=1", artifact["stats"].Coverage)
	}
}

func TestSessionDashboard_RouteNotRegisteredWithoutMarkets(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)
	srv := newTestServer(e, guardianPID)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/session/dashboard", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestSessionDashboard_ArtifactsUnavailableWithoutReaders(t *testing.T) {
	inst := "SOLUSDT_S19_DASH_UNAVAILABLE"
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{{
			Name:    "binance",
			Symbols: []config.MarketsSymbolConfig{{Ticker: inst}},
		}},
	}

	srv := newDashboardServer(t, markets, nil)
	rec := doRequest(t, srv, http.MethodGet, "/api/v1/session/dashboard", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp httpserver.SessionDashboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v body=%s", err, rec.Body.String())
	}

	for _, artifact := range resp.Artifacts {
		if artifact.Coverage.Status != "unavailable" {
			t.Fatalf("artifact %q coverage.status=%q want=unavailable", artifact.Name, artifact.Coverage.Status)
		}
	}
}

func TestSessionDashboard_JSONShape(t *testing.T) {
	markets := &config.MarketsConfig{
		Exchanges: []config.MarketsExchangeConfig{{
			Name:    "binance",
			Symbols: []config.MarketsSymbolConfig{{Ticker: "BTCUSDT_S19_DASH_SHAPE"}},
		}},
	}
	srv := newDashboardServer(t, markets, nil)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/session/dashboard", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("json decode: %v", err)
	}

	for _, key := range []string{"server_time_ms", "status", "readiness", "freshness", "resync", "artifacts", "summary"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing key %q", key)
		}
	}
}
