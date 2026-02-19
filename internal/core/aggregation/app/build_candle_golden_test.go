package app_test

import (
	"context"
	"math"
	"reflect"
	"testing"

	"github.com/market-raccoon/internal/core/aggregation/app"
	"github.com/market-raccoon/internal/core/aggregation/domain"
)

func runCandleSequence(t *testing.T, uc *app.BuildCandleFromEvents, seq []app.BuildCandleRequest) []domain.CandleClosed {
	t.Helper()
	out := make([]domain.CandleClosed, 0, len(seq))
	for i := range seq {
		resp, p := uc.Execute(context.Background(), seq[i])
		if p != nil {
			t.Fatalf("Execute[%d]: %v", i, p)
		}
		out = append(out, resp.Closed...)
	}
	return out
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestBuildCandle_GoldenDeterminism(t *testing.T) {
	seq := []app.BuildCandleRequest{
		{Venue: "binance", Instrument: "BTCUSDT", Price: 100.0, Quantity: 1.2, IsBuy: true, Seq: 1, TsIngest: 1},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 101.0, Quantity: 0.8, IsBuy: false, Seq: 2, TsIngest: 10_000},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 99.5, Quantity: 0.4, IsBuy: true, Seq: 3, TsIngest: 20_000},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 102.0, Quantity: 1.0, IsBuy: true, Seq: 4, TsIngest: 60_001},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 101.5, Quantity: 0.5, IsBuy: false, Seq: 5, TsIngest: 120_001},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 103.0, Quantity: 0.7, IsBuy: true, Seq: 6, TsIngest: 180_001},
	}

	uc1, _, _ := newCandleUC(1_000)
	uc2, _, _ := newCandleUC(1_000)
	left := runCandleSequence(t, uc1, seq)
	right := runCandleSequence(t, uc2, seq)

	if !reflect.DeepEqual(left, right) {
		t.Fatalf("non-deterministic output:\nleft=%+v\nright=%+v", left, right)
	}
	if len(left) == 0 {
		t.Fatal("expected closed candle output")
	}

	for i := range left {
		c := left[i].Candle
		if !almostEqual(c.Volume, c.BuyVolume+c.SellVolume) {
			t.Fatalf("volume invariant failed at %d: volume=%f buy+sell=%f", i, c.Volume, c.BuyVolume+c.SellVolume)
		}
		if c.High < c.Low {
			t.Fatalf("high/low invariant failed at %d: high=%f low=%f", i, c.High, c.Low)
		}
		if c.High < c.Open || c.High < c.ClosePrice {
			t.Fatalf("high bound invariant failed at %d: high=%f open=%f close=%f", i, c.High, c.Open, c.ClosePrice)
		}
	}
}

func TestBuildCandle_GoldenCascade_5mFrom1m(t *testing.T) {
	seq := []app.BuildCandleRequest{
		{Venue: "binance", Instrument: "BTCUSDT", Price: 100, Quantity: 1, IsBuy: true, Seq: 1, TsIngest: 1},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 101, Quantity: 1, IsBuy: true, Seq: 2, TsIngest: 60_001},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 102, Quantity: 1, IsBuy: true, Seq: 3, TsIngest: 120_001},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 103, Quantity: 1, IsBuy: true, Seq: 4, TsIngest: 180_001},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 104, Quantity: 1, IsBuy: true, Seq: 5, TsIngest: 240_001},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 105, Quantity: 1, IsBuy: true, Seq: 6, TsIngest: 300_001},
		{Venue: "binance", Instrument: "BTCUSDT", Price: 106, Quantity: 1, IsBuy: true, Seq: 7, TsIngest: 360_001},
	}

	uc1, _, _ := newCandleUC(1_000)
	uc2, _, _ := newCandleUC(1_000)
	left := runCandleSequence(t, uc1, seq)
	right := runCandleSequence(t, uc2, seq)
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("non-deterministic output:\nleft=%+v\nright=%+v", left, right)
	}

	count1m := 0
	count5m := 0
	var closed5m domain.CandleClosed
	for i := range left {
		switch left[i].Candle.Timeframe {
		case "1m":
			count1m++
		case "5m":
			count5m++
			closed5m = left[i]
		}
	}
	if count5m != 1 {
		t.Fatalf("5m close count=%d want=1", count5m)
	}
	if count1m != 6 {
		t.Fatalf("1m close count=%d want=6", count1m)
	}
	if closed5m.Candle.Open != 100 || closed5m.Candle.ClosePrice != 104 {
		t.Fatalf("5m o/c mismatch: open=%f close=%f", closed5m.Candle.Open, closed5m.Candle.ClosePrice)
	}
	if closed5m.Candle.High != 104 || closed5m.Candle.Low != 100 {
		t.Fatalf("5m h/l mismatch: high=%f low=%f", closed5m.Candle.High, closed5m.Candle.Low)
	}
	if closed5m.Candle.TradeCount != 5 {
		t.Fatalf("5m trade_count=%d want=5", closed5m.Candle.TradeCount)
	}
}
