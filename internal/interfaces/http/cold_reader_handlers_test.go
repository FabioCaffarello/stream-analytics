package httpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	"github.com/market-raccoon/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// stub readers
// ---------------------------------------------------------------------------

type stubCandleReader struct {
	candles []aggdomain.CandleV1
	prob    *problem.Problem
}

func (s *stubCandleReader) GetCandleRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.CandleV1, *problem.Problem) {
	return s.candles, s.prob
}

func (s *stubCandleReader) GetCandleTimestamps(_ context.Context, _, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return nil, nil
}

func (s *stubCandleReader) GetFirstCandle(_ context.Context, _, _, _ string) (*aggdomain.CandleV1, *problem.Problem) {
	return nil, nil
}

func (s *stubCandleReader) GetLastCandle(_ context.Context, _, _, _ string) (*aggdomain.CandleV1, *problem.Problem) {
	return nil, nil
}

type stubStatsReader struct {
	stats []aggdomain.StatsWindowV1
	prob  *problem.Problem
}

func (s *stubStatsReader) GetStatsRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.StatsWindowV1, *problem.Problem) {
	return s.stats, s.prob
}

func (s *stubStatsReader) GetStatsTimestamps(_ context.Context, _, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return nil, nil
}

func (s *stubStatsReader) GetFirstStats(_ context.Context, _, _, _ string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	return nil, nil
}

func (s *stubStatsReader) GetLastStats(_ context.Context, _, _, _ string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	return nil, nil
}

type stubSnapshotReader struct {
	timestamps []int64
	prob       *problem.Problem
}

func (s *stubSnapshotReader) GetSnapshotTimestamps(_ context.Context, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return s.timestamps, s.prob
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newColdServer(t *testing.T, readers *httpserver.ColdReaders) *httpserver.Server {
	t.Helper()
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	t.Cleanup(func() { e.Poison(guardianPID) })

	srv := httpserver.NewServer(e, guardianPID, ":0", false, nil,
		httpserver.WithColdReaders(readers),
	)
	srv.SetSnapshotTimeout(2 * time.Second)
	return srv
}

// ---------------------------------------------------------------------------
// GET /api/v1/candles
// ---------------------------------------------------------------------------

func TestColdReader_Candles_HappyPath(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &stubCandleReader{
			candles: []aggdomain.CandleV1{
				{Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m", WindowStartTs: 1000},
				{Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m", WindowStartTs: 2000},
			},
		},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/candles?venue=binance&instrument=BTCUSDT&timeframe=1m&fromMs=1000&toMs=3000&limit=100", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rec.Code, rec.Body.String())
	}

	var body []aggdomain.CandleV1
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode failed: %v\nbody: %s", err, rec.Body.String())
	}
	if len(body) != 2 {
		t.Fatalf("expected 2 candles, got %d", len(body))
	}
}

func TestColdReader_Candles_MissingRequiredParams(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &stubCandleReader{},
	})

	tests := []struct {
		name string
		path string
	}{
		{"missing venue", "/api/v1/candles?instrument=BTCUSDT&timeframe=1m&fromMs=0&toMs=1000"},
		{"missing instrument", "/api/v1/candles?venue=binance&timeframe=1m&fromMs=0&toMs=1000"},
		{"missing timeframe", "/api/v1/candles?venue=binance&instrument=BTCUSDT&fromMs=0&toMs=1000"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, srv, http.MethodGet, tc.path, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d\nbody: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestColdReader_Candles_InvalidFromMs(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &stubCandleReader{},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/candles?venue=binance&instrument=BTCUSDT&timeframe=1m&fromMs=abc&toMs=1000", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestColdReader_Candles_InvalidToMs(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &stubCandleReader{},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/candles?venue=binance&instrument=BTCUSDT&timeframe=1m&fromMs=0&toMs=xyz", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestColdReader_Candles_DefaultLimit(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &stubCandleReader{candles: []aggdomain.CandleV1{{Venue: "binance"}}},
	})

	// Omit limit → handler should default to 1000 and still succeed.
	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/candles?venue=binance&instrument=BTCUSDT&timeframe=1m&fromMs=0&toMs=1000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rec.Code, rec.Body.String())
	}
}

func TestColdReader_Candles_ReaderError(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &stubCandleReader{
			prob: problem.New(problem.Internal, "db connection lost"),
		},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/candles?venue=binance&instrument=BTCUSDT&timeframe=1m&fromMs=0&toMs=1000&limit=10", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d\nbody: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if body["error"] != "db connection lost" {
		t.Fatalf("expected error='db connection lost', got %q", body["error"])
	}
}

func TestColdReader_Candles_NilReader_Returns503(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: nil,
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/candles?venue=binance&instrument=BTCUSDT&timeframe=1m&fromMs=0&toMs=1000", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/stats
// ---------------------------------------------------------------------------

func TestColdReader_Stats_HappyPath(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Stats: &stubStatsReader{
			stats: []aggdomain.StatsWindowV1{
				{Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m", WindowStartTs: 1000},
			},
		},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/stats?venue=binance&instrument=BTCUSDT&timeframe=1m&fromMs=1000&toMs=3000&limit=50", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rec.Code, rec.Body.String())
	}

	var body []aggdomain.StatsWindowV1
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode failed: %v\nbody: %s", err, rec.Body.String())
	}
	if len(body) != 1 {
		t.Fatalf("expected 1 stats window, got %d", len(body))
	}
}

func TestColdReader_Stats_MissingRequiredParams(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Stats: &stubStatsReader{},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/stats?venue=binance&fromMs=0&toMs=1000", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestColdReader_Stats_ReaderError(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Stats: &stubStatsReader{
			prob: problem.New(problem.Internal, "query timeout"),
		},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/stats?venue=binance&instrument=BTCUSDT&timeframe=1m&fromMs=0&toMs=1000&limit=10", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestColdReader_Stats_NilReader_Returns503(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Stats: nil,
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/stats?venue=binance&instrument=BTCUSDT&timeframe=1m&fromMs=0&toMs=1000", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/snapshots
// ---------------------------------------------------------------------------

func TestColdReader_Snapshots_HappyPath(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Snapshots: &stubSnapshotReader{
			timestamps: []int64{1000, 2000, 3000},
		},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/snapshots?venue=binance&instrument=BTCUSDT&fromMs=0&toMs=5000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rec.Code, rec.Body.String())
	}

	var body []int64
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode failed: %v\nbody: %s", err, rec.Body.String())
	}
	if len(body) != 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(body))
	}
}

func TestColdReader_Snapshots_MissingVenue(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Snapshots: &stubSnapshotReader{},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/snapshots?instrument=BTCUSDT&fromMs=0&toMs=1000", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestColdReader_Snapshots_MissingInstrument(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Snapshots: &stubSnapshotReader{},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/snapshots?venue=binance&fromMs=0&toMs=1000", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestColdReader_Snapshots_InvalidFromMs(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Snapshots: &stubSnapshotReader{},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/snapshots?venue=binance&instrument=BTCUSDT&fromMs=abc&toMs=1000", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestColdReader_Snapshots_ReaderError(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Snapshots: &stubSnapshotReader{
			prob: problem.New(problem.Internal, "storage offline"),
		},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/snapshots?venue=binance&instrument=BTCUSDT&fromMs=0&toMs=1000", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestColdReader_Snapshots_NilReader_Returns503(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Snapshots: nil,
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/snapshots?venue=binance&instrument=BTCUSDT&fromMs=0&toMs=1000", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// No cold readers configured → 404 on cold API routes
// ---------------------------------------------------------------------------

func TestColdReader_NoColdReaders_RoutesNotRegistered(t *testing.T) {
	e := newEngine(t)
	guardianPID := newGuardian(t, e)
	defer e.Poison(guardianPID)

	// Server without WithColdReaders → routes should not exist.
	srv := newTestServer(e, guardianPID)

	for _, path := range []string{"/api/v1/candles", "/api/v1/stats", "/api/v1/snapshots"} {
		rec := doRequest(t, srv, http.MethodGet, path+"?venue=x&instrument=y&timeframe=z&fromMs=0&toMs=1", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("path=%s: expected 404, got %d", path, rec.Code)
		}
	}
}
