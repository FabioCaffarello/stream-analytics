package federation

import (
	"context"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// stubTimestampCandleReader returns fixed timestamps.
type stubTimestampCandleReader struct {
	stubCandleReader
	ts []int64
}

func (r *stubTimestampCandleReader) GetCandleTimestamps(_ context.Context, _, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return r.ts, nil
}

// stubTimestampStatsReader returns fixed timestamps.
type stubTimestampStatsReader struct {
	stubStatsReader
	ts []int64
}

func (r *stubTimestampStatsReader) GetStatsTimestamps(_ context.Context, _, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return r.ts, nil
}

func TestConsistencyChecker_Candles_FullOverlap(t *testing.T) {
	hot := &stubTimestampCandleReader{ts: []int64{100, 200, 300}}
	cold := &stubTimestampCandleReader{ts: []int64{100, 200, 300}}
	cc := NewConsistencyChecker(hot, cold, nil, nil)

	r, p := cc.CheckCandles(context.Background(), "binance", "BTCUSDT", "1m", 0, 1000)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if r.HotCount != 3 || r.ColdCount != 3 {
		t.Fatalf("counts: hot=%d cold=%d", r.HotCount, r.ColdCount)
	}
	if r.OverlapCount != 3 {
		t.Fatalf("overlap=%d want 3", r.OverlapCount)
	}
	if r.MissingInCold != 0 || r.MissingInHot != 0 {
		t.Fatalf("missing: inCold=%d inHot=%d", r.MissingInCold, r.MissingInHot)
	}
}

func TestConsistencyChecker_Candles_PartialOverlap(t *testing.T) {
	hot := &stubTimestampCandleReader{ts: []int64{100, 200, 300}}
	cold := &stubTimestampCandleReader{ts: []int64{200, 300, 400}}
	cc := NewConsistencyChecker(hot, cold, nil, nil)

	r, p := cc.CheckCandles(context.Background(), "binance", "BTCUSDT", "1m", 0, 1000)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if r.OverlapCount != 2 {
		t.Fatalf("overlap=%d want 2", r.OverlapCount)
	}
	if r.MissingInCold != 1 {
		t.Fatalf("missingInCold=%d want 1", r.MissingInCold)
	}
	if r.MissingInHot != 1 {
		t.Fatalf("missingInHot=%d want 1", r.MissingInHot)
	}
}

func TestConsistencyChecker_Candles_NilReaders(t *testing.T) {
	cc := NewConsistencyChecker(nil, nil, nil, nil)
	r, p := cc.CheckCandles(context.Background(), "binance", "BTCUSDT", "1m", 0, 1000)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if r.HotCount != 0 || r.ColdCount != 0 {
		t.Fatalf("expected zeros, got hot=%d cold=%d", r.HotCount, r.ColdCount)
	}
}

func TestConsistencyChecker_Stats_MissingInCold(t *testing.T) {
	hot := &stubTimestampStatsReader{ts: []int64{100, 200, 300}}
	cold := &stubTimestampStatsReader{ts: []int64{100}}
	cc := NewConsistencyChecker(nil, nil, hot, cold)

	r, p := cc.CheckStats(context.Background(), "binance", "BTCUSDT", "1m", 0, 1000)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if r.MissingInCold != 2 {
		t.Fatalf("missingInCold=%d want 2", r.MissingInCold)
	}
	if r.HotMinTs != 100 || r.HotMaxTs != 300 {
		t.Fatalf("hot bounds: min=%d max=%d", r.HotMinTs, r.HotMaxTs)
	}
}

// stubCandleReader and stubStatsReader are defined in federation_test.go
