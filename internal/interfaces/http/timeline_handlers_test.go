package httpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	httpserver "github.com/FabioCaffarello/stream-analytics/internal/interfaces/http"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// stub readers with timeline support
// ---------------------------------------------------------------------------

type timelineCandleReader struct {
	first *aggdomain.CandleV1
	last  *aggdomain.CandleV1
	prob  *problem.Problem
}

func (r *timelineCandleReader) GetCandleRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.CandleV1, *problem.Problem) {
	return nil, nil
}
func (r *timelineCandleReader) GetCandleTimestamps(_ context.Context, _, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return nil, nil
}
func (r *timelineCandleReader) GetFirstCandle(_ context.Context, _, _, _ string) (*aggdomain.CandleV1, *problem.Problem) {
	return r.first, r.prob
}
func (r *timelineCandleReader) GetLastCandle(_ context.Context, _, _, _ string) (*aggdomain.CandleV1, *problem.Problem) {
	return r.last, r.prob
}

type timelineStatsReader struct {
	first *aggdomain.StatsWindowV1
	last  *aggdomain.StatsWindowV1
	prob  *problem.Problem
}

func (r *timelineStatsReader) GetStatsRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.StatsWindowV1, *problem.Problem) {
	return nil, nil
}
func (r *timelineStatsReader) GetStatsTimestamps(_ context.Context, _, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return nil, nil
}
func (r *timelineStatsReader) GetFirstStats(_ context.Context, _, _, _ string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	return r.first, r.prob
}
func (r *timelineStatsReader) GetLastStats(_ context.Context, _, _, _ string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	return r.last, r.prob
}

// ---------------------------------------------------------------------------
// GET /api/v1/timeline — candle
// ---------------------------------------------------------------------------

func TestTimeline_Candle_HappyPath(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &timelineCandleReader{
			first: &aggdomain.CandleV1{WindowStartTs: 1000},
			last:  &aggdomain.CandleV1{WindowStartTs: 9000},
		},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/timeline?venue=binance&instrument=BTCUSDT&timeframe=1m&artifact=candle", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rec.Code, rec.Body.String())
	}

	var resp httpserver.TimelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp.FirstTs != 1000 {
		t.Errorf("first_ts: want 1000, got %d", resp.FirstTs)
	}
	if resp.LastTs != 9000 {
		t.Errorf("last_ts: want 9000, got %d", resp.LastTs)
	}
	if resp.Artifact != "candle" {
		t.Errorf("artifact: want candle, got %s", resp.Artifact)
	}
}

func TestTimeline_Candle_DefaultArtifact(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &timelineCandleReader{
			first: &aggdomain.CandleV1{WindowStartTs: 500},
			last:  &aggdomain.CandleV1{WindowStartTs: 5000},
		},
	})

	// Omit artifact → defaults to candle
	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/timeline?venue=binance&instrument=BTCUSDT&timeframe=1m", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rec.Code, rec.Body.String())
	}

	var resp httpserver.TimelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp.Artifact != "candle" {
		t.Errorf("artifact: want candle, got %s", resp.Artifact)
	}
}

func TestTimeline_Candle_NoData(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &timelineCandleReader{first: nil, last: nil},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/timeline?venue=binance&instrument=ETHUSDT&timeframe=1m", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp httpserver.TimelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp.FirstTs != 0 || resp.LastTs != 0 {
		t.Errorf("expected zero timestamps for empty data, got first=%d last=%d", resp.FirstTs, resp.LastTs)
	}
}

func TestTimeline_Candle_ReaderError(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &timelineCandleReader{
			prob: problem.New(problem.Internal, "db down"),
		},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/timeline?venue=binance&instrument=BTCUSDT&timeframe=1m", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/timeline — stats
// ---------------------------------------------------------------------------

func TestTimeline_Stats_HappyPath(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Stats: &timelineStatsReader{
			first: &aggdomain.StatsWindowV1{WindowStartTs: 2000},
			last:  &aggdomain.StatsWindowV1{WindowStartTs: 8000},
		},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/timeline?venue=bybit&instrument=ETHUSDT&timeframe=raw&artifact=stats", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rec.Code, rec.Body.String())
	}

	var resp httpserver.TimelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp.FirstTs != 2000 {
		t.Errorf("first_ts: want 2000, got %d", resp.FirstTs)
	}
	if resp.LastTs != 8000 {
		t.Errorf("last_ts: want 8000, got %d", resp.LastTs)
	}
	if resp.Artifact != "stats" {
		t.Errorf("artifact: want stats, got %s", resp.Artifact)
	}
}

func TestTimeline_Stats_ReaderNotAvailable(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		// Stats is nil
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/timeline?venue=binance&instrument=BTCUSDT&timeframe=raw&artifact=stats", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/timeline — validation
// ---------------------------------------------------------------------------

func TestTimeline_MissingRequiredParams(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &timelineCandleReader{},
	})

	tests := []struct {
		name string
		path string
	}{
		{"missing venue", "/api/v1/timeline?instrument=BTCUSDT&timeframe=1m"},
		{"missing instrument", "/api/v1/timeline?venue=binance&timeframe=1m"},
		{"missing timeframe", "/api/v1/timeline?venue=binance&instrument=BTCUSDT"},
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

func TestTimeline_UnsupportedArtifact(t *testing.T) {
	srv := newColdServer(t, &httpserver.ColdReaders{
		Candles: &timelineCandleReader{},
	})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/timeline?venue=binance&instrument=BTCUSDT&timeframe=1m&artifact=heatmap", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTimeline_NoReadersConfigured(t *testing.T) {
	// When coldReaders is nil, the route is not even registered.
	// Test with coldReaders present but Candles nil.
	srv := newColdServer(t, &httpserver.ColdReaders{})

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/timeline?venue=binance&instrument=BTCUSDT&timeframe=1m&artifact=candle", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
