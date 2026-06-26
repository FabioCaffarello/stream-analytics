package clickhouse_test

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	aggapp "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/app"
	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type inMemoryColdStore struct {
	mu      sync.RWMutex
	candles []aggdomain.CandleV1
	stats   []aggdomain.StatsWindowV1
}

func (s *inMemoryColdStore) SaveCandle(_ context.Context, evt aggdomain.CandleClosed) *problem.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candles = append(s.candles, evt.Candle)
	return nil
}

func (s *inMemoryColdStore) SaveStats(_ context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats = append(s.stats, evt.Stats)
	return nil
}

func (s *inMemoryColdStore) GetCandleRange(_ context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.CandleV1, *problem.Problem) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]aggdomain.CandleV1, 0, len(s.candles))
	for _, c := range s.candles {
		if c.Venue != venue || c.Instrument != instrument || c.Timeframe != timeframe {
			continue
		}
		if c.WindowStartTs < fromMs || c.WindowStartTs > toMs {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WindowStartTs < out[j].WindowStartTs })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *inMemoryColdStore) GetCandleTimestamps(_ context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[int64]struct{}, 1024)
	out := make([]int64, 0, 1024)
	for _, c := range s.candles {
		if c.Venue != venue || c.Instrument != instrument || c.Timeframe != timeframe {
			continue
		}
		if c.WindowStartTs < fromMs || c.WindowStartTs > toMs {
			continue
		}
		if _, ok := seen[c.WindowStartTs]; ok {
			continue
		}
		seen[c.WindowStartTs] = struct{}{}
		out = append(out, c.WindowStartTs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (s *inMemoryColdStore) GetFirstCandle(_ context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	candles, p := s.GetCandleRange(context.Background(), venue, instrument, timeframe, -1<<62, 1<<62-1, 1)
	if p != nil || len(candles) == 0 {
		return nil, p
	}
	c := candles[0]
	return &c, nil
}

func (s *inMemoryColdStore) GetLastCandle(_ context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	candles, p := s.GetCandleRange(context.Background(), venue, instrument, timeframe, -1<<62, 1<<62-1, 0)
	if p != nil || len(candles) == 0 {
		return nil, p
	}
	c := candles[len(candles)-1]
	return &c, nil
}

func (s *inMemoryColdStore) GetStatsTimestamps(_ context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[int64]struct{}, 1024)
	out := make([]int64, 0, 1024)
	for _, st := range s.stats {
		if st.Venue != venue || st.Instrument != instrument || st.Timeframe != timeframe {
			continue
		}
		if st.WindowStartTs < fromMs || st.WindowStartTs > toMs {
			continue
		}
		if _, ok := seen[st.WindowStartTs]; ok {
			continue
		}
		seen[st.WindowStartTs] = struct{}{}
		out = append(out, st.WindowStartTs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (s *inMemoryColdStore) GetFirstStats(_ context.Context, venue, instrument, timeframe string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	all := s.statsRange(venue, instrument, timeframe, -1<<62, 1<<62-1)
	if len(all) == 0 {
		return nil, nil
	}
	v := all[0]
	return &v, nil
}

func (s *inMemoryColdStore) GetLastStats(_ context.Context, venue, instrument, timeframe string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	all := s.statsRange(venue, instrument, timeframe, -1<<62, 1<<62-1)
	if len(all) == 0 {
		return nil, nil
	}
	v := all[len(all)-1]
	return &v, nil
}

func (s *inMemoryColdStore) statsRange(venue, instrument, timeframe string, fromMs, toMs int64) []aggdomain.StatsWindowV1 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]aggdomain.StatsWindowV1, 0, len(s.stats))
	for _, st := range s.stats {
		if st.Venue != venue || st.Instrument != instrument || st.Timeframe != timeframe {
			continue
		}
		if st.WindowStartTs < fromMs || st.WindowStartTs > toMs {
			continue
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WindowStartTs < out[j].WindowStartTs })
	return out
}

//nolint:gocyclo // roundtrip soak test intentionally validates many invariants in one flow.
func TestColdPath_CandleWriteReadRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping roundtrip test in short mode")
	}

	ctx := context.Background()
	store := &inMemoryColdStore{}
	venue := "BINANCE"
	instruments := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}
	timeframes := []string{"1m", "5m", "15m"}

	written := make(map[string][]aggdomain.CandleV1, len(instruments)*len(timeframes))
	for i, instrument := range instruments {
		for j, timeframe := range timeframes {
			for k := 0; k < 67; k++ {
				windowStart := int64(k) * timeframeStepMs(timeframe)
				c := aggdomain.CandleV1{
					Venue:         venue,
					Instrument:    instrument,
					Timeframe:     timeframe,
					WindowStartTs: windowStart,
					WindowEndTs:   windowStart + timeframeStepMs(timeframe),
					Open:          100 + float64(i+j+k),
					High:          101 + float64(i+j+k),
					Low:           99 + float64(i+j+k),
					ClosePrice:    100.5 + float64(i+j+k),
					Volume:        10 + float64(k%7),
					BuyVolume:     6 + float64(k%5),
					SellVolume:    4 + float64(k%3),
					TradeCount:    int64((k % 9) + 1),
					SeqFirst:      int64(k*10 + 1),
					SeqLast:       int64(k*10 + 9),
					IsClosed:      true,
				}
				if p := store.SaveCandle(ctx, aggdomain.CandleClosed{Candle: c}); p != nil {
					t.Fatalf("SaveCandle: %v", p)
				}
				key := fmt.Sprintf("%s|%s|%s", venue, instrument, timeframe)
				written[key] = append(written[key], c)
			}
		}
	}

	for _, instrument := range instruments {
		for _, timeframe := range timeframes {
			key := fmt.Sprintf("%s|%s|%s", venue, instrument, timeframe)
			want := written[key]

			got, p := store.GetCandleRange(ctx, venue, instrument, timeframe, 0, 1<<62-1, 10_000)
			if p != nil {
				t.Fatalf("GetCandleRange %s: %v", key, p)
			}
			if len(got) != len(want) {
				t.Fatalf("%s read count=%d want=%d", key, len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("%s candle mismatch at idx=%d got=%+v want=%+v", key, i, got[i], want[i])
				}
			}

			ts, p := store.GetCandleTimestamps(ctx, venue, instrument, timeframe, 0, 1<<62-1)
			if p != nil {
				t.Fatalf("GetCandleTimestamps %s: %v", key, p)
			}
			if !sort.SliceIsSorted(ts, func(i, j int) bool { return ts[i] < ts[j] }) {
				t.Fatalf("timestamps not sorted for %s", key)
			}
			for i := 1; i < len(ts); i++ {
				if ts[i] == ts[i-1] {
					t.Fatalf("timestamps not unique for %s", key)
				}
			}

			first, p := store.GetFirstCandle(ctx, venue, instrument, timeframe)
			if p != nil {
				t.Fatalf("GetFirstCandle %s: %v", key, p)
			}
			last, p := store.GetLastCandle(ctx, venue, instrument, timeframe)
			if p != nil {
				t.Fatalf("GetLastCandle %s: %v", key, p)
			}
			if first == nil || last == nil {
				t.Fatalf("expected first/last candles for %s", key)
			}
			if first.WindowStartTs != want[0].WindowStartTs {
				t.Fatalf("first candle mismatch for %s", key)
			}
			if last.WindowStartTs != want[len(want)-1].WindowStartTs {
				t.Fatalf("last candle mismatch for %s", key)
			}

			gaps, p := aggapp.DetectCandleGaps(ctx, store, aggapp.GapDetectorConfig{
				Venue:          venue,
				Instrument:     instrument,
				Timeframe:      timeframe,
				ExpectedStepMs: timeframeStepMs(timeframe),
			})
			if p != nil {
				t.Fatalf("DetectCandleGaps %s: %v", key, p)
			}
			if len(gaps) != 0 {
				t.Fatalf("expected zero gaps for %s, got=%+v", key, gaps)
			}
		}
	}
}

//nolint:gocyclo // roundtrip soak test intentionally validates many invariants in one flow.
func TestColdPath_StatsWriteReadRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping roundtrip test in short mode")
	}

	ctx := context.Background()
	store := &inMemoryColdStore{}
	venue := "BYBIT"
	instruments := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}
	timeframes := []string{"1m", "5m", "15m"}

	written := make(map[string][]aggdomain.StatsWindowV1, len(instruments)*len(timeframes))
	for i, instrument := range instruments {
		for j, timeframe := range timeframes {
			for k := 0; k < 67; k++ {
				windowStart := int64(k) * timeframeStepMs(timeframe)
				mark := 0.0
				fund := 0.0
				if k%3 != 0 {
					mark = 200 + float64(i+j+k)
				}
				if k%4 == 0 {
					fund = 0.0001
				}
				st := aggdomain.StatsWindowV1{
					Venue:           venue,
					Instrument:      instrument,
					Timeframe:       timeframe,
					WindowStartTs:   windowStart,
					WindowEndTs:     windowStart + timeframeStepMs(timeframe),
					LiqBuyVolume:    2 + float64(k%3),
					LiqSellVolume:   1 + float64(k%2),
					LiqTotalVolume:  3 + float64(k%5),
					LiqCount:        int64((k % 7) + 1),
					MarkPriceOpen:   mark,
					MarkPriceHigh:   mark,
					MarkPriceLow:    mark,
					MarkPriceClose:  mark,
					FundingRateAvg:  fund,
					FundingRateLast: fund,
					SeqFirst:        int64(k*10 + 1),
					SeqLast:         int64(k*10 + 9),
					IsClosed:        true,
				}
				if p := store.SaveStats(ctx, aggdomain.StatsWindowClosed{Stats: st}); p != nil {
					t.Fatalf("SaveStats: %v", p)
				}
				key := fmt.Sprintf("%s|%s|%s", venue, instrument, timeframe)
				written[key] = append(written[key], st)
			}
		}
	}

	for _, instrument := range instruments {
		for _, timeframe := range timeframes {
			key := fmt.Sprintf("%s|%s|%s", venue, instrument, timeframe)
			want := written[key]
			got := store.statsRange(venue, instrument, timeframe, 0, 1<<62-1)
			if len(got) != len(want) {
				t.Fatalf("%s read count=%d want=%d", key, len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("%s stats mismatch at idx=%d got=%+v want=%+v", key, i, got[i], want[i])
				}
			}

			ts, p := store.GetStatsTimestamps(ctx, venue, instrument, timeframe, 0, 1<<62-1)
			if p != nil {
				t.Fatalf("GetStatsTimestamps %s: %v", key, p)
			}
			if !sort.SliceIsSorted(ts, func(i, j int) bool { return ts[i] < ts[j] }) {
				t.Fatalf("stats timestamps not sorted for %s", key)
			}

			first, p := store.GetFirstStats(ctx, venue, instrument, timeframe)
			if p != nil {
				t.Fatalf("GetFirstStats %s: %v", key, p)
			}
			last, p := store.GetLastStats(ctx, venue, instrument, timeframe)
			if p != nil {
				t.Fatalf("GetLastStats %s: %v", key, p)
			}
			if first == nil || last == nil {
				t.Fatalf("expected first/last stats for %s", key)
			}
			if first.WindowStartTs != want[0].WindowStartTs {
				t.Fatalf("first stats mismatch for %s", key)
			}
			if last.WindowStartTs != want[len(want)-1].WindowStartTs {
				t.Fatalf("last stats mismatch for %s", key)
			}
		}
	}
}

func timeframeStepMs(tf string) int64 {
	switch tf {
	case "1m":
		return 60_000
	case "5m":
		return 300_000
	case "15m":
		return 900_000
	default:
		return 60_000
	}
}
