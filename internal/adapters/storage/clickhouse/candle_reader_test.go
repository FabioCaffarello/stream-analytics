package clickhouse_test

import (
	"context"
	"sort"
	"testing"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type inMemoryCandleReader struct {
	candles []aggdomain.CandleV1
}

func (r *inMemoryCandleReader) GetCandleRange(_ context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.CandleV1, *problem.Problem) {
	filtered := make([]aggdomain.CandleV1, 0, len(r.candles))
	for _, c := range r.candles {
		if c.Venue != venue || c.Instrument != instrument || c.Timeframe != timeframe {
			continue
		}
		if c.WindowStartTs < fromMs || c.WindowStartTs > toMs {
			continue
		}
		filtered = append(filtered, c)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].WindowStartTs < filtered[j].WindowStartTs
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (r *inMemoryCandleReader) GetCandleTimestamps(_ context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem) {
	out := make([]int64, 0, len(r.candles))
	for _, c := range r.candles {
		if c.Venue != venue || c.Instrument != instrument || c.Timeframe != timeframe {
			continue
		}
		if c.WindowStartTs < fromMs || c.WindowStartTs > toMs {
			continue
		}
		out = append(out, c.WindowStartTs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (r *inMemoryCandleReader) GetFirstCandle(_ context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	all, _ := r.GetCandleRange(context.Background(), venue, instrument, timeframe, -1<<62, 1<<62-1, 1)
	if len(all) == 0 {
		return nil, nil
	}
	c := all[0]
	return &c, nil
}

func (r *inMemoryCandleReader) GetLastCandle(_ context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	all, _ := r.GetCandleRange(context.Background(), venue, instrument, timeframe, -1<<62, 1<<62-1, 0)
	if len(all) == 0 {
		return nil, nil
	}
	c := all[len(all)-1]
	return &c, nil
}

func TestInMemoryCandleReader_GetCandleTimestamps_SortedInRange(t *testing.T) {
	r := &inMemoryCandleReader{candles: []aggdomain.CandleV1{
		{Venue: "BINANCE", Instrument: "BTCUSDT", Timeframe: "1m", WindowStartTs: 300_000},
		{Venue: "BINANCE", Instrument: "BTCUSDT", Timeframe: "1m", WindowStartTs: 60_000},
		{Venue: "BINANCE", Instrument: "BTCUSDT", Timeframe: "1m", WindowStartTs: 120_000},
		{Venue: "BINANCE", Instrument: "BTCUSDT", Timeframe: "5m", WindowStartTs: 300_000},
	}}

	ts, p := r.GetCandleTimestamps(context.Background(), "BINANCE", "BTCUSDT", "1m", 60_000, 180_000)
	if p != nil {
		t.Fatalf("GetCandleTimestamps: %v", p)
	}
	want := []int64{60_000, 120_000}
	if len(ts) != len(want) {
		t.Fatalf("len=%d want=%d", len(ts), len(want))
	}
	for i := range want {
		if ts[i] != want[i] {
			t.Fatalf("ts[%d]=%d want=%d", i, ts[i], want[i])
		}
	}
}

func TestInMemoryCandleReader_GetFirstLast_Boundaries(t *testing.T) {
	r := &inMemoryCandleReader{candles: []aggdomain.CandleV1{
		{Venue: "BINANCE", Instrument: "ETHUSDT", Timeframe: "1m", WindowStartTs: 180_000},
		{Venue: "BINANCE", Instrument: "ETHUSDT", Timeframe: "1m", WindowStartTs: 60_000},
		{Venue: "BINANCE", Instrument: "ETHUSDT", Timeframe: "1m", WindowStartTs: 120_000},
	}}

	first, p := r.GetFirstCandle(context.Background(), "BINANCE", "ETHUSDT", "1m")
	if p != nil {
		t.Fatalf("GetFirstCandle: %v", p)
	}
	if first == nil || first.WindowStartTs != 60_000 {
		t.Fatalf("first=%v", first)
	}

	last, p := r.GetLastCandle(context.Background(), "BINANCE", "ETHUSDT", "1m")
	if p != nil {
		t.Fatalf("GetLastCandle: %v", p)
	}
	if last == nil || last.WindowStartTs != 180_000 {
		t.Fatalf("last=%v", last)
	}
}

func TestInMemoryCandleReader_EmptyResults(t *testing.T) {
	r := &inMemoryCandleReader{}

	ts, p := r.GetCandleTimestamps(context.Background(), "BINANCE", "BTCUSDT", "1m", 0, 60_000)
	if p != nil {
		t.Fatalf("GetCandleTimestamps: %v", p)
	}
	if len(ts) != 0 {
		t.Fatalf("timestamps len=%d want=0", len(ts))
	}

	first, p := r.GetFirstCandle(context.Background(), "BINANCE", "BTCUSDT", "1m")
	if p != nil {
		t.Fatalf("GetFirstCandle: %v", p)
	}
	if first != nil {
		t.Fatalf("first=%+v want=nil", first)
	}

	last, p := r.GetLastCandle(context.Background(), "BINANCE", "BTCUSDT", "1m")
	if p != nil {
		t.Fatalf("GetLastCandle: %v", p)
	}
	if last != nil {
		t.Fatalf("last=%+v want=nil", last)
	}
}
