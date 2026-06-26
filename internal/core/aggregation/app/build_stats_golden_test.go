package app_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
)

func runStatsSequence(t *testing.T, uc *app.BuildStatsFromEvents, seq []app.BuildStatsRequest) []domain.StatsWindowClosed {
	t.Helper()
	out := make([]domain.StatsWindowClosed, 0, len(seq))
	for i := range seq {
		resp, p := uc.Execute(context.Background(), seq[i])
		if p != nil {
			t.Fatalf("Execute[%d]: %v", i, p)
		}
		out = append(out, resp.Closed...)
	}
	return out
}

func TestBuildStats_GoldenDeterminism_Liquidation(t *testing.T) {
	seq := []app.BuildStatsRequest{
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputLiquidation, LiquidationSide: "buy", LiquidationQty: 2.0, Seq: 1, TsIngest: 1},
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputLiquidation, LiquidationSide: "sell", LiquidationQty: 1.0, Seq: 2, TsIngest: 20_000},
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputLiquidation, LiquidationSide: "buy", LiquidationQty: 3.0, Seq: 3, TsIngest: 60_001},
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputLiquidation, LiquidationSide: "sell", LiquidationQty: 4.0, Seq: 4, TsIngest: 120_001},
	}

	uc1, _, _ := newStatsUC(1_000)
	uc2, _, _ := newStatsUC(1_000)
	left := runStatsSequence(t, uc1, seq)
	right := runStatsSequence(t, uc2, seq)
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("non-deterministic output:\nleft=%+v\nright=%+v", left, right)
	}
}

func TestBuildStats_GoldenDeterminism_MixedInputs(t *testing.T) {
	seq := []app.BuildStatsRequest{
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputLiquidation, LiquidationSide: "buy", LiquidationQty: 2.0, Seq: 1, TsIngest: 1},
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 100.0, Seq: 2, TsIngest: 5_000},
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputFundingRate, FundingRate: 0.0002, Seq: 3, TsIngest: 10_000},
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 101.0, Seq: 4, TsIngest: 60_001},
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputLiquidation, LiquidationSide: "sell", LiquidationQty: 1.5, Seq: 5, TsIngest: 70_000},
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputFundingRate, FundingRate: 0.0003, Seq: 6, TsIngest: 120_001},
	}

	uc1, _, _ := newStatsUC(1_000)
	uc2, _, _ := newStatsUC(1_000)
	left := runStatsSequence(t, uc1, seq)
	right := runStatsSequence(t, uc2, seq)
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("non-deterministic output:\nleft=%+v\nright=%+v", left, right)
	}
	if len(left) == 0 {
		t.Fatal("expected at least one closed stats window")
	}
}

func TestBuildStats_GoldenPartialInputs(t *testing.T) {
	uc, _, _ := newStatsUC(1_000)
	if _, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 100.0, Seq: 1, TsIngest: 1,
	}); p != nil {
		t.Fatalf("Execute #1: %v", p)
	}
	resp, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 101.0, Seq: 2, TsIngest: 60_001,
	})
	if p != nil {
		t.Fatalf("Execute #2: %v", p)
	}
	if len(resp.Closed) == 0 {
		t.Fatal("expected closed stats window")
	}
	closed := resp.Closed[0].Stats
	if closed.LiqCount != 0 || closed.LiqBuyVolume != 0 || closed.LiqSellVolume != 0 || closed.LiqTotalVolume != 0 {
		t.Fatalf("expected zero liquidation fields, got count=%d buy=%f sell=%f total=%f", closed.LiqCount, closed.LiqBuyVolume, closed.LiqSellVolume, closed.LiqTotalVolume)
	}
	if closed.MarkPriceOpen == 0 || closed.MarkPriceClose == 0 {
		t.Fatalf("expected markprice fields, got open=%f close=%f", closed.MarkPriceOpen, closed.MarkPriceClose)
	}
	if closed.FundingRateAvg != 0 || closed.FundingRateLast != 0 {
		t.Fatalf("expected zero funding fields, got avg=%f last=%f", closed.FundingRateAvg, closed.FundingRateLast)
	}
}
