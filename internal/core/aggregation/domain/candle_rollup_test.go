package domain_test

import (
	"math"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// makeClosedCandle creates a closed 1m candle for testing.
func makeClosedCandle(t *testing.T, venue, instrument string, windowStart int64, trades []testTrade) domain.CandleV1 {
	t.Helper()
	c, p := domain.NewCandleV1(venue, instrument, "1m", windowStart)
	if p != nil {
		t.Fatalf("NewCandleV1: %v", p)
	}
	for i, tr := range trades {
		if p := c.ApplyTrade(tr.price, tr.qty, tr.isBuy, tr.seq); p != nil {
			t.Fatalf("ApplyTrade[%d]: %v", i, p)
		}
	}
	if p := c.Close(windowStart + 60_000); p != nil {
		t.Fatalf("Close: %v", p)
	}
	return *c
}

type testTrade struct {
	price float64
	qty   float64
	isBuy bool
	seq   int64
}

func TestRollupCandle_5mFrom1m(t *testing.T) {
	candles := make([]domain.CandleV1, 5)
	for i := range candles {
		candles[i] = makeClosedCandle(t, "BINANCE", "BTCUSDT", int64(i)*60_000, []testTrade{
			{price: 100 + float64(i), qty: 1.0, isBuy: true, seq: int64(i*2 + 1)},
			{price: 100.5 + float64(i), qty: 0.5, isBuy: false, seq: int64(i*2 + 2)},
		})
	}

	result, p := domain.RollupCandle(candles, "5m")
	if p != nil {
		t.Fatalf("RollupCandle: %v", p)
	}

	if result.Venue != "BINANCE" {
		t.Fatalf("venue=%q want=BINANCE", result.Venue)
	}
	if result.Instrument != "BTCUSDT" {
		t.Fatalf("instrument=%q want=BTCUSDT", result.Instrument)
	}
	if result.Timeframe != "5m" {
		t.Fatalf("timeframe=%q want=5m", result.Timeframe)
	}
	if !result.IsClosed {
		t.Fatal("expected closed candle")
	}
	if result.WindowStartTs != 0 {
		t.Fatalf("window_start=%d want=0", result.WindowStartTs)
	}
	if result.WindowEndTs != 300_000 {
		t.Fatalf("window_end=%d want=300000", result.WindowEndTs)
	}
	if result.Open != 100 {
		t.Fatalf("open=%f want=100", result.Open)
	}
	if result.ClosePrice != 104.5 {
		t.Fatalf("close=%f want=104.5", result.ClosePrice)
	}
	if result.High != 104.5 {
		t.Fatalf("high=%f want=104.5", result.High)
	}
	if result.Low != 100 {
		t.Fatalf("low=%f want=100", result.Low)
	}
	if result.TradeCount != 10 {
		t.Fatalf("trade_count=%d want=10", result.TradeCount)
	}
	if result.Volume != result.BuyVolume+result.SellVolume {
		t.Fatalf("volume invariant: total=%f buy+sell=%f", result.Volume, result.BuyVolume+result.SellVolume)
	}
}

func TestRollupCandle_15mFrom1m(t *testing.T) {
	candles := make([]domain.CandleV1, 15)
	for i := range candles {
		candles[i] = makeClosedCandle(t, "BINANCE", "BTCUSDT", int64(i)*60_000, []testTrade{
			{price: 50000 + float64(i)*10, qty: 0.1, isBuy: i%2 == 0, seq: int64(i + 1)},
		})
	}

	result, p := domain.RollupCandle(candles, "15m")
	if p != nil {
		t.Fatalf("RollupCandle: %v", p)
	}
	if result.Timeframe != "15m" {
		t.Fatalf("timeframe=%q want=15m", result.Timeframe)
	}
	if result.TradeCount != 15 {
		t.Fatalf("trade_count=%d want=15", result.TradeCount)
	}
	if result.Open != 50000 {
		t.Fatalf("open=%f want=50000", result.Open)
	}
	if result.ClosePrice != 50140 {
		t.Fatalf("close=%f want=50140", result.ClosePrice)
	}
}

func TestRollupCandle_1hFrom1m(t *testing.T) {
	candles := make([]domain.CandleV1, 60)
	for i := range candles {
		candles[i] = makeClosedCandle(t, "BINANCE", "ETHUSDT", int64(i)*60_000, []testTrade{
			{price: 3000 + float64(i), qty: 1.0, isBuy: true, seq: int64(i + 1)},
		})
	}

	result, p := domain.RollupCandle(candles, "1h")
	if p != nil {
		t.Fatalf("RollupCandle: %v", p)
	}
	if result.Timeframe != "1h" {
		t.Fatalf("timeframe=%q want=1h", result.Timeframe)
	}
	if result.WindowEndTs != 3_600_000 {
		t.Fatalf("window_end=%d want=3600000", result.WindowEndTs)
	}
	if result.TradeCount != 60 {
		t.Fatalf("trade_count=%d want=60", result.TradeCount)
	}
	if result.Open != 3000 {
		t.Fatalf("open=%f want=3000", result.Open)
	}
	if result.ClosePrice != 3059 {
		t.Fatalf("close=%f want=3059", result.ClosePrice)
	}
	if result.High != 3059 {
		t.Fatalf("high=%f want=3059", result.High)
	}
	if result.Low != 3000 {
		t.Fatalf("low=%f want=3000", result.Low)
	}
}

func TestRollupCandle_4hFrom1m(t *testing.T) {
	candles := make([]domain.CandleV1, 5)
	for i := range candles {
		candles[i] = makeClosedCandle(t, "BINANCE", "BTCUSDT", int64(i)*60_000, []testTrade{
			{price: 40000 + float64(i)*100, qty: 2.0, isBuy: true, seq: int64(i + 1)},
		})
	}

	result, p := domain.RollupCandle(candles, "4h")
	if p != nil {
		t.Fatalf("RollupCandle: %v", p)
	}
	if result.Timeframe != "4h" {
		t.Fatalf("timeframe=%q want=4h", result.Timeframe)
	}
	if result.WindowStartTs != 0 {
		t.Fatalf("window_start=%d want=0", result.WindowStartTs)
	}
	if result.WindowEndTs != 14_400_000 {
		t.Fatalf("window_end=%d want=14400000", result.WindowEndTs)
	}
	if result.Open != 40000 {
		t.Fatalf("open=%f want=40000", result.Open)
	}
	if result.High != 40400 {
		t.Fatalf("high=%f want=40400", result.High)
	}
}

func TestRollupCandle_1dFrom1m(t *testing.T) {
	candles := make([]domain.CandleV1, 3)
	for i := range candles {
		candles[i] = makeClosedCandle(t, "BINANCE", "BTCUSDT", int64(i)*60_000, []testTrade{
			{price: 60000 - float64(i)*500, qty: 0.5, isBuy: false, seq: int64(i + 1)},
		})
	}

	result, p := domain.RollupCandle(candles, "1d")
	if p != nil {
		t.Fatalf("RollupCandle: %v", p)
	}
	if result.Timeframe != "1d" {
		t.Fatalf("timeframe=%q want=1d", result.Timeframe)
	}
	if result.WindowEndTs != 86_400_000 {
		t.Fatalf("window_end=%d want=86400000", result.WindowEndTs)
	}
	if result.Open != 60000 {
		t.Fatalf("open=%f want=60000", result.Open)
	}
	if result.ClosePrice != 59000 {
		t.Fatalf("close=%f want=59000", result.ClosePrice)
	}
	if result.Low != 59000 {
		t.Fatalf("low=%f want=59000", result.Low)
	}
	if result.High != 60000 {
		t.Fatalf("high=%f want=60000", result.High)
	}
}

func TestRollupCandle_EmptySource_Fails(t *testing.T) {
	_, p := domain.RollupCandle(nil, "5m")
	if p == nil {
		t.Fatal("expected error for nil source")
	}
	_, p = domain.RollupCandle([]domain.CandleV1{}, "5m")
	if p == nil {
		t.Fatal("expected error for empty source")
	}
}

func TestRollupCandle_UnclosedCandle_Fails(t *testing.T) {
	c, p := domain.NewCandleV1("BINANCE", "BTCUSDT", "1m", 0)
	if p != nil {
		t.Fatalf("NewCandleV1: %v", p)
	}
	if p := c.ApplyTrade(100, 1, true, 1); p != nil {
		t.Fatalf("ApplyTrade: %v", p)
	}
	// Not closed
	_, p = domain.RollupCandle([]domain.CandleV1{*c}, "5m")
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected validation failure for unclosed candle, got=%v", p)
	}
}

func TestRollupCandle_MismatchedVenue_Fails(t *testing.T) {
	c1 := makeClosedCandle(t, "BINANCE", "BTCUSDT", 0, []testTrade{
		{price: 100, qty: 1, isBuy: true, seq: 1},
	})
	c2 := makeClosedCandle(t, "BYBIT", "BTCUSDT", 60_000, []testTrade{
		{price: 101, qty: 1, isBuy: true, seq: 2},
	})
	_, p := domain.RollupCandle([]domain.CandleV1{c1, c2}, "5m")
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected validation failure for mismatched venue, got=%v", p)
	}
}

func TestRollupCandle_MismatchedInstrument_Fails(t *testing.T) {
	c1 := makeClosedCandle(t, "BINANCE", "BTCUSDT", 0, []testTrade{
		{price: 100, qty: 1, isBuy: true, seq: 1},
	})
	c2 := makeClosedCandle(t, "BINANCE", "ETHUSDT", 60_000, []testTrade{
		{price: 101, qty: 1, isBuy: true, seq: 2},
	})
	_, p := domain.RollupCandle([]domain.CandleV1{c1, c2}, "5m")
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected validation failure for mismatched instrument, got=%v", p)
	}
}

func TestRollupCandle_CrossWindow_Fails(t *testing.T) {
	c1 := makeClosedCandle(t, "BINANCE", "BTCUSDT", 0, []testTrade{
		{price: 100, qty: 1, isBuy: true, seq: 1},
	})
	// Second candle starts in the next 5m window (at 300_000ms = 5min).
	c2 := makeClosedCandle(t, "BINANCE", "BTCUSDT", 300_000, []testTrade{
		{price: 101, qty: 1, isBuy: true, seq: 2},
	})
	_, p := domain.RollupCandle([]domain.CandleV1{c1, c2}, "5m")
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected validation failure for cross-window candles, got=%v", p)
	}
}

func TestRollupCandle_InvalidTimeframe_Fails(t *testing.T) {
	c := makeClosedCandle(t, "BINANCE", "BTCUSDT", 0, []testTrade{
		{price: 100, qty: 1, isBuy: true, seq: 1},
	})
	_, p := domain.RollupCandle([]domain.CandleV1{c}, "2m")
	if p == nil {
		t.Fatal("expected error for invalid timeframe")
	}
}

func TestRollupCandle_SingleCandle(t *testing.T) {
	c := makeClosedCandle(t, "BINANCE", "BTCUSDT", 0, []testTrade{
		{price: 42000.50, qty: 1.5, isBuy: true, seq: 1},
		{price: 42100.00, qty: 0.5, isBuy: false, seq: 2},
	})

	result, p := domain.RollupCandle([]domain.CandleV1{c}, "5m")
	if p != nil {
		t.Fatalf("RollupCandle: %v", p)
	}
	if result.Open != 42000.50 {
		t.Fatalf("open=%f want=42000.50", result.Open)
	}
	if result.ClosePrice != 42100.00 {
		t.Fatalf("close=%f want=42100.00", result.ClosePrice)
	}
	if result.TradeCount != 2 {
		t.Fatalf("trade_count=%d want=2", result.TradeCount)
	}
}

func TestRollupCandle_VolumeInvariant(t *testing.T) {
	candles := make([]domain.CandleV1, 10)
	for i := range candles {
		candles[i] = makeClosedCandle(t, "BINANCE", "BTCUSDT", int64(i)*60_000, []testTrade{
			{price: 100.0, qty: 1.5, isBuy: true, seq: int64(i*3 + 1)},
			{price: 100.5, qty: 0.75, isBuy: false, seq: int64(i*3 + 2)},
			{price: 99.5, qty: 2.0, isBuy: true, seq: int64(i*3 + 3)},
		})
	}

	result, p := domain.RollupCandle(candles, "15m")
	if p != nil {
		t.Fatalf("RollupCandle: %v", p)
	}

	if math.Abs(result.Volume-(result.BuyVolume+result.SellVolume)) > 1e-9 {
		t.Fatalf("volume invariant: total=%f buy+sell=%f", result.Volume, result.BuyVolume+result.SellVolume)
	}
	// Each candle: buy=3.5, sell=0.75 → total buy=35, sell=7.5
	if math.Abs(result.BuyVolume-35.0) > 1e-9 {
		t.Fatalf("buy_volume=%f want=35.0", result.BuyVolume)
	}
	if math.Abs(result.SellVolume-7.5) > 1e-9 {
		t.Fatalf("sell_volume=%f want=7.5", result.SellVolume)
	}
}

func TestRollupCandle_Deterministic(t *testing.T) {
	candles := make([]domain.CandleV1, 5)
	for i := range candles {
		candles[i] = makeClosedCandle(t, "BINANCE", "BTCUSDT", int64(i)*60_000, []testTrade{
			{price: 100 + float64(i)*0.1, qty: 1.0 + float64(i)*0.01, isBuy: i%2 == 0, seq: int64(i + 1)},
		})
	}

	r1, p := domain.RollupCandle(candles, "5m")
	if p != nil {
		t.Fatalf("first RollupCandle: %v", p)
	}
	r2, p := domain.RollupCandle(candles, "5m")
	if p != nil {
		t.Fatalf("second RollupCandle: %v", p)
	}

	if r1.Open != r2.Open || r1.High != r2.High || r1.Low != r2.Low || r1.ClosePrice != r2.ClosePrice {
		t.Fatalf("non-deterministic OHLC: r1=%+v r2=%+v", r1, r2)
	}
	if r1.Volume != r2.Volume || r1.BuyVolume != r2.BuyVolume || r1.SellVolume != r2.SellVolume {
		t.Fatalf("non-deterministic volume: r1=%+v r2=%+v", r1, r2)
	}
	if r1.TradeCount != r2.TradeCount || r1.SeqFirst != r2.SeqFirst || r1.SeqLast != r2.SeqLast {
		t.Fatalf("non-deterministic seq: r1=%+v r2=%+v", r1, r2)
	}
}

// TestRollupCandle_GoldenOHLCV validates exact OHLCV output for a known fixture.
// These values are computed deterministically and serve as regression anchors.
func TestRollupCandle_GoldenOHLCV(t *testing.T) {
	// 5 x 1m candles forming one 5m window [0, 300000).
	// Prices: minute0=50000/50100, minute1=50050/49900, minute2=49950/50200,
	//         minute3=50150/50000, minute4=50100/50300
	candles := []domain.CandleV1{
		makeClosedCandle(t, "BINANCE", "BTCUSDT", 0, []testTrade{
			{price: 50000, qty: 1.0, isBuy: true, seq: 1},
			{price: 50100, qty: 0.5, isBuy: false, seq: 2},
		}),
		makeClosedCandle(t, "BINANCE", "BTCUSDT", 60_000, []testTrade{
			{price: 50050, qty: 0.8, isBuy: true, seq: 3},
			{price: 49900, qty: 1.2, isBuy: false, seq: 4},
		}),
		makeClosedCandle(t, "BINANCE", "BTCUSDT", 120_000, []testTrade{
			{price: 49950, qty: 0.3, isBuy: true, seq: 5},
			{price: 50200, qty: 0.7, isBuy: false, seq: 6},
		}),
		makeClosedCandle(t, "BINANCE", "BTCUSDT", 180_000, []testTrade{
			{price: 50150, qty: 1.5, isBuy: true, seq: 7},
			{price: 50000, qty: 0.5, isBuy: false, seq: 8},
		}),
		makeClosedCandle(t, "BINANCE", "BTCUSDT", 240_000, []testTrade{
			{price: 50100, qty: 0.4, isBuy: true, seq: 9},
			{price: 50300, qty: 0.6, isBuy: false, seq: 10},
		}),
	}

	result, p := domain.RollupCandle(candles, "5m")
	if p != nil {
		t.Fatalf("RollupCandle: %v", p)
	}

	// Golden values: Open=first candle open, Close=last candle close,
	// High=max of all highs, Low=min of all lows.
	assertFloat(t, "open", result.Open, 50000)
	assertFloat(t, "close", result.ClosePrice, 50300)
	assertFloat(t, "high", result.High, 50300)
	assertFloat(t, "low", result.Low, 49900)

	// Total buy volume: 1.0 + 0.8 + 0.3 + 1.5 + 0.4 = 4.0
	assertFloat(t, "buy_volume", result.BuyVolume, 4.0)
	// Total sell volume: 0.5 + 1.2 + 0.7 + 0.5 + 0.6 = 3.5
	assertFloat(t, "sell_volume", result.SellVolume, 3.5)
	assertFloat(t, "volume", result.Volume, 7.5)

	if result.TradeCount != 10 {
		t.Fatalf("trade_count=%d want=10", result.TradeCount)
	}
	if result.SeqFirst != 1 {
		t.Fatalf("seq_first=%d want=1", result.SeqFirst)
	}
	if result.SeqLast != 10 {
		t.Fatalf("seq_last=%d want=10", result.SeqLast)
	}
}

func TestRollupCandle_UnsortedInput_ProducesCorrectOHLC(t *testing.T) {
	// Provide candles out of order; RollupCandle should sort by WindowStartTs.
	c0 := makeClosedCandle(t, "BINANCE", "BTCUSDT", 120_000, []testTrade{
		{price: 102, qty: 1, isBuy: true, seq: 3},
	})
	c1 := makeClosedCandle(t, "BINANCE", "BTCUSDT", 0, []testTrade{
		{price: 100, qty: 1, isBuy: true, seq: 1},
	})
	c2 := makeClosedCandle(t, "BINANCE", "BTCUSDT", 60_000, []testTrade{
		{price: 101, qty: 1, isBuy: false, seq: 2},
	})

	result, p := domain.RollupCandle([]domain.CandleV1{c0, c1, c2}, "5m")
	if p != nil {
		t.Fatalf("RollupCandle: %v", p)
	}
	if result.Open != 100 {
		t.Fatalf("open=%f want=100 (should be from earliest candle)", result.Open)
	}
	if result.ClosePrice != 102 {
		t.Fatalf("close=%f want=102 (should be from latest candle)", result.ClosePrice)
	}
}

func assertFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s=%f want=%f", name, got, want)
	}
}
