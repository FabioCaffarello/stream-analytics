package app_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type fakeCandleStore struct {
	events  []domain.CandleClosed
	saveErr *problem.Problem
}

func (f *fakeCandleStore) SaveCandle(_ context.Context, evt domain.CandleClosed) *problem.Problem {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.events = append(f.events, evt)
	return nil
}

func newCandleUC(maxCandles int) (*app.BuildCandleFromEvents, *fakePublisher, *fakeCandleStore) {
	pub := &fakePublisher{}
	store := &fakeCandleStore{}
	uc := app.NewBuildCandleFromEvents(pub, store, app.BuildCandleConfig{
		MaxCandles: maxCandles,
		WindowCap:  96,
		CandleTTL:  time.Hour,
		Clock:      clock.NewFakeClock(time.Unix(0, 0)),
	})
	return uc, pub, store
}

func TestBuildCandle_SingleTrade_CreatesOpenCandle(t *testing.T) {
	uc, _, _ := newCandleUC(100)
	resp, p := uc.Execute(context.Background(), app.BuildCandleRequest{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Price:      100.5,
		Quantity:   1.25,
		IsBuy:      true,
		Seq:        1,
		TsIngest:   1,
	})
	if p != nil {
		t.Fatalf("Execute: %v", p)
	}
	if len(resp.Closed) != 0 {
		t.Fatalf("closed events=%d want=0", len(resp.Closed))
	}
	if resp.ActiveCandles != 1 {
		t.Fatalf("active candles=%d want=1", resp.ActiveCandles)
	}
}

func TestBuildCandle_WindowClose_EmitsCandleClosed(t *testing.T) {
	uc, pub, store := newCandleUC(100)
	if _, p := uc.Execute(context.Background(), app.BuildCandleRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 100, Quantity: 1, IsBuy: true, Seq: 1, TsIngest: 1,
	}); p != nil {
		t.Fatalf("Execute #1: %v", p)
	}
	resp, p := uc.Execute(context.Background(), app.BuildCandleRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 101, Quantity: 1, IsBuy: false, Seq: 2, TsIngest: 1_001,
	})
	if p != nil {
		t.Fatalf("Execute #2: %v", p)
	}
	if len(resp.Closed) == 0 {
		t.Fatal("expected at least one closed candle event")
	}
	first := resp.Closed[0].Candle
	if first.Timeframe != "1s" {
		t.Fatalf("closed timeframe=%s want=1s", first.Timeframe)
	}
	if !first.IsClosed {
		t.Fatal("closed candle must have is_closed=true")
	}
	if len(pub.candles) == 0 || len(store.events) == 0 {
		t.Fatalf("expected publish+store side effects, got pub=%d store=%d", len(pub.candles), len(store.events))
	}
}

func TestBuildCandle_MultiTimeframe_BaseCascades(t *testing.T) {
	uc, _, _ := newCandleUC(1_000)
	var allClosed []domain.CandleClosed
	for i := 0; i < 7; i++ {
		resp, p := uc.Execute(context.Background(), app.BuildCandleRequest{
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Price:      100 + float64(i),
			Quantity:   1,
			IsBuy:      true,
			Seq:        int64(i + 1),
			TsIngest:   int64(i*60_000 + 1),
		})
		if p != nil {
			t.Fatalf("Execute[%d]: %v", i, p)
		}
		allClosed = append(allClosed, resp.Closed...)
	}

	var closed5m *domain.CandleClosed
	for i := range allClosed {
		if allClosed[i].Candle.Timeframe == "5m" {
			closed5m = &allClosed[i]
			break
		}
	}
	if closed5m == nil {
		t.Fatal("expected one 5m closed candle from base cascade")
	}
	if closed5m.Candle.TradeCount != 5 {
		t.Fatalf("5m candle trade_count=%d want=5", closed5m.Candle.TradeCount)
	}
	if closed5m.Candle.Open != 100 || closed5m.Candle.ClosePrice != 104 {
		t.Fatalf("unexpected 5m o/c values: open=%v close=%v", closed5m.Candle.Open, closed5m.Candle.ClosePrice)
	}
}

func TestBuildCandle_Deterministic_SameInputSameOutput(t *testing.T) {
	uc1, _, _ := newCandleUC(1_000)
	uc2, _, _ := newCandleUC(1_000)
	sequence := []app.BuildCandleRequest{
		{Venue: "binance", Instrument: "BTCUSDT", Price: 100.1, Quantity: 1, IsBuy: true, Seq: 1, TsIngest: 1},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 100.4, Quantity: 0.5, IsBuy: false, Seq: 2, TsIngest: 10_000},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 100.0, Quantity: 2, IsBuy: true, Seq: 3, TsIngest: 60_001},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 99.8, Quantity: 1, IsBuy: false, Seq: 4, TsIngest: 120_001},
	}

	var left []domain.CandleClosed
	var right []domain.CandleClosed
	for i := range sequence {
		resp1, p := uc1.Execute(context.Background(), sequence[i])
		if p != nil {
			t.Fatalf("uc1 Execute[%d]: %v", i, p)
		}
		resp2, p := uc2.Execute(context.Background(), sequence[i])
		if p != nil {
			t.Fatalf("uc2 Execute[%d]: %v", i, p)
		}
		left = append(left, resp1.Closed...)
		right = append(right, resp2.Closed...)
	}
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("non-deterministic closed output:\nleft=%+v\nright=%+v", left, right)
	}
}

func TestBuildCandle_BoundedMap_EvictsOldest(t *testing.T) {
	uc, _, _ := newCandleUC(1)
	if _, p := uc.Execute(context.Background(), app.BuildCandleRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 100, Quantity: 1, IsBuy: true, Seq: 1, TsIngest: 1,
	}); p != nil {
		t.Fatalf("Execute BTC: %v", p)
	}
	if _, p := uc.Execute(context.Background(), app.BuildCandleRequest{
		Venue: "binance", Instrument: "ETHUSDT", Price: 200, Quantity: 1, IsBuy: true, Seq: 1, TsIngest: 1,
	}); p != nil {
		t.Fatalf("Execute ETH: %v", p)
	}
	if got := uc.ActiveCandles(); got != 1 {
		t.Fatalf("active candles=%d want=1", got)
	}
	if _, p := uc.Execute(context.Background(), app.BuildCandleRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 101, Quantity: 1, IsBuy: true, Seq: 1, TsIngest: 1,
	}); p != nil {
		t.Fatalf("expected BTC seq=1 accepted after eviction, got=%v", p)
	}
}

func TestBuildCandle_GapEventDriven_NoSyntheticBaseClosures(t *testing.T) {
	uc, _, _ := newCandleUC(1_000)
	if _, p := uc.Execute(context.Background(), app.BuildCandleRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 100, Quantity: 1, IsBuy: true, Seq: 1, TsIngest: 1,
	}); p != nil {
		t.Fatalf("Execute #1: %v", p)
	}

	resp, p := uc.Execute(context.Background(), app.BuildCandleRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 101, Quantity: 1, IsBuy: true, Seq: 2, TsIngest: 60_001,
	})
	if p != nil {
		t.Fatalf("Execute #2: %v", p)
	}

	if got := countClosedCandlesByTimeframe(resp.Closed, "1s"); got != 1 {
		t.Fatalf("1s close count=%d want=1 (event-driven, no synthetic empty-window closes)", got)
	}
	if got := countClosedCandlesByTimeframe(resp.Closed, "1m"); got != 0 {
		t.Fatalf("1m close count=%d want=0 (first 1m close occurs only after a second base close)", got)
	}
}

func TestBuildCandle_LateArrivalDropped(t *testing.T) {
	uc, _, _ := newCandleUC(1_000)

	if _, p := uc.Execute(context.Background(), app.BuildCandleRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 100, Quantity: 1, IsBuy: true, Seq: 1, TsIngest: 60_000,
	}); p != nil {
		t.Fatalf("Execute #1: %v", p)
	}
	if _, p := uc.Execute(context.Background(), app.BuildCandleRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 101, Quantity: 1, IsBuy: true, Seq: 2, TsIngest: 120_000,
	}); p != nil {
		t.Fatalf("Execute #2: %v", p)
	}
	resp, p := uc.Execute(context.Background(), app.BuildCandleRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 99, Quantity: 1, IsBuy: true, Seq: 3, TsIngest: 60_000,
	})
	if p != nil {
		t.Fatalf("Execute late: %v", p)
	}
	if len(resp.Closed) != 0 {
		t.Fatalf("late arrival should not close windows, got %d closed", len(resp.Closed))
	}
}

func countClosedCandlesByTimeframe(closed []domain.CandleClosed, timeframe string) int {
	count := 0
	for i := range closed {
		if closed[i].Candle.Timeframe == timeframe {
			count++
		}
	}
	return count
}
