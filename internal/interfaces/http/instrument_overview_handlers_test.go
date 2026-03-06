package httpserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	"github.com/market-raccoon/internal/shared/observability"
)

func TestInstrumentOverview_MissingParams(t *testing.T) {
	srv := httpserver.NewServer(nil, nil, ":0", false, nil)

	tests := []struct {
		name string
		url  string
	}{
		{name: "no params", url: "/api/v1/instrument/overview"},
		{name: "venue only", url: "/api/v1/instrument/overview?venue=binance"},
		{name: "instrument only", url: "/api/v1/instrument/overview?instrument=BTCUSDT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestInstrumentOverview_ComposesStatusesAndArtifacts(t *testing.T) {
	inst := "BTCUSDT_S19_OVERVIEW"
	streamCandle := "stage19-ov-candle"
	streamStats := "stage19-ov-stats"
	nowMs := time.Now().UnixMilli()

	observability.RecordTerminalWSDelivery(streamCandle, "binance", inst, "candle", 120, nowMs, nowMs-25, 25)
	observability.RecordTerminalWSDelivery(streamStats, "binance", inst, "stats", 35, nowMs, nowMs-12, 12)
	observability.IncTerminalWSResync(streamCandle)

	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &timelineCandleReader{
			first: &aggdomain.CandleV1{WindowStartTs: 1_710_000_000_000},
			last:  &aggdomain.CandleV1{WindowStartTs: 1_710_003_600_000},
		},
		Stats: &timelineStatsReader{
			first: &aggdomain.StatsWindowV1{WindowStartTs: 1_710_000_000_000},
			last:  &aggdomain.StatsWindowV1{WindowStartTs: 1_710_003_600_000},
		},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/instrument/overview?venue=binance&instrument="+inst, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp httpserver.InstrumentOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v body=%s", err, rec.Body.String())
	}

	if resp.Readiness.Status != "ready" {
		t.Fatalf("readiness.status=%q want=ready", resp.Readiness.Status)
	}
	if resp.Freshness.Status != "flowing" {
		t.Fatalf("freshness.status=%q want=flowing", resp.Freshness.Status)
	}
	if !resp.Freshness.Active {
		t.Fatal("freshness.active=false want=true")
	}
	if resp.Resync.Status != "recovering" {
		t.Fatalf("resync.status=%q want=recovering", resp.Resync.Status)
	}
	if resp.Resync.ResyncTotal < 1 {
		t.Fatalf("resync_total=%d want>=1", resp.Resync.ResyncTotal)
	}
	if resp.Status != "ready" {
		t.Fatalf("status=%q want=ready", resp.Status)
	}
	if len(resp.Artifacts) != 2 {
		t.Fatalf("artifacts=%d want=2", len(resp.Artifacts))
	}

	for _, art := range resp.Artifacts {
		if art.Timeline.Status != "available" {
			t.Fatalf("artifact %q timeline.status=%q want=available", art.Name, art.Timeline.Status)
		}
		if art.Timeline.Timeframe != "1m" {
			t.Fatalf("artifact %q timeline.timeframe=%q want=1m", art.Name, art.Timeline.Timeframe)
		}
	}
}

func TestInstrumentOverview_DegradedOnDropsAndStaleFreshness(t *testing.T) {
	inst := "ETHUSDT_S19_OVERVIEW"
	stream := "stage19-ov-degraded"
	staleTs := time.Now().Add(-2 * time.Minute).UnixMilli()

	observability.RecordTerminalWSDelivery(stream, "binance", inst, "candle", 10, staleTs, staleTs, 0)
	observability.RecordTerminalWSDrop(stream, "binance", inst, "candle", "slow_client")

	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &timelineCandleReader{first: nil, last: nil},
		Stats:   &timelineStatsReader{first: nil, last: nil},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/instrument/overview?venue=binance&instrument="+inst, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp httpserver.InstrumentOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v body=%s", err, rec.Body.String())
	}

	if resp.Freshness.Status != "stale" {
		t.Fatalf("freshness.status=%q want=stale", resp.Freshness.Status)
	}
	if resp.Resync.Status != "degraded" {
		t.Fatalf("resync.status=%q want=degraded", resp.Resync.Status)
	}
	if resp.Status != "degraded" {
		t.Fatalf("status=%q want=degraded", resp.Status)
	}
}

func TestInstrumentOverview_TimelineUnavailableWithoutReaders(t *testing.T) {
	inst := "SOLUSDT_S19_OVERVIEW"
	nowMs := time.Now().UnixMilli()
	observability.RecordTerminalWSDelivery("stage19-ov-no-readers", "binance", inst, "candle", 1, nowMs, nowMs, 0)

	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)
	srv := httpserver.NewServer(e, guardianPID, ":0", false, nil)

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/instrument/overview?venue=binance&instrument="+inst, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp httpserver.InstrumentOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v body=%s", err, rec.Body.String())
	}
	if len(resp.Artifacts) != 2 {
		t.Fatalf("artifacts=%d want=2", len(resp.Artifacts))
	}
	for _, art := range resp.Artifacts {
		if art.Timeline.Status != "unavailable" {
			t.Fatalf("artifact %q timeline.status=%q want=unavailable", art.Name, art.Timeline.Status)
		}
	}
}

func TestInstrumentOverview_JSONShape(t *testing.T) {
	srv := httpserver.NewServer(nil, nil, ":0", false, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instrument/overview?venue=binance&instrument=BTCUSDT_S19_SHAPE", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"venue", "instrument", "status", "checked_at", "readiness", "freshness", "resync", "artifacts"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing key: %s", key)
		}
	}
}
