package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

func newStatsWindow(t *testing.T) *domain.StatsWindowV1 {
	t.Helper()
	w, p := domain.NewStatsWindowV1("BINANCE", "BTCUSDT", "1m", 60_000)
	if p != nil {
		t.Fatalf("NewStatsWindowV1: %v", p)
	}
	return w
}

func TestStatsWindowV1_NewValidation(t *testing.T) {
	if _, p := domain.NewStatsWindowV1("", "BTCUSDT", "1m", 1); p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected validation failure for empty venue, got=%v", p)
	}
	if _, p := domain.NewStatsWindowV1("BINANCE", "BTCUSDT", "2m", 1); p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected validation failure for invalid timeframe, got=%v", p)
	}
}

func TestStatsWindowV1_ApplyLiquidation_ST1NonNegativeAdditive(t *testing.T) {
	w := newStatsWindow(t)
	if p := w.ApplyLiquidation("buy", 2.5, 1); p != nil {
		t.Fatalf("ApplyLiquidation #1: %v", p)
	}
	if p := w.ApplyLiquidation("sell", 1.0, 2); p != nil {
		t.Fatalf("ApplyLiquidation #2: %v", p)
	}
	if w.LiqBuyVolume < 0 || w.LiqSellVolume < 0 || w.LiqTotalVolume < 0 {
		t.Fatalf("negative liquidation volume found: %+v", w)
	}
	if w.LiqTotalVolume != w.LiqBuyVolume+w.LiqSellVolume {
		t.Fatalf("liq total invariant broken: total=%v buy+sell=%v", w.LiqTotalVolume, w.LiqBuyVolume+w.LiqSellVolume)
	}
}

func TestStatsWindowV1_Close_Immutability(t *testing.T) {
	w := newStatsWindow(t)
	if p := w.ApplyMarkPrice(100, 1); p != nil {
		t.Fatalf("ApplyMarkPrice: %v", p)
	}
	if p := w.Close(120_000); p != nil {
		t.Fatalf("Close: %v", p)
	}
	if p := w.ApplyFundingRate(0.001, 2); p == nil || p.Code != problem.Conflict {
		t.Fatalf("expected conflict after close, got=%v", p)
	}
}

func TestStatsWindowV1_PartialInputsAllowed_ST6(t *testing.T) {
	w := newStatsWindow(t)
	if p := w.ApplyMarkPrice(100.5, 1); p != nil {
		t.Fatalf("ApplyMarkPrice: %v", p)
	}
	if p := w.Close(120_000); p != nil {
		t.Fatalf("Close: %v", p)
	}
	if w.FundingRateAvg != 0 || w.FundingRateLast != 0 {
		t.Fatalf("expected zero funding fields for partial stats, got avg=%v last=%v", w.FundingRateAvg, w.FundingRateLast)
	}
}

func TestStatsWindowV1_Deterministic(t *testing.T) {
	left := newStatsWindow(t)
	right := newStatsWindow(t)
	apply := func(t *testing.T, w *domain.StatsWindowV1) {
		t.Helper()
		if p := w.ApplyLiquidation("buy", 1.25, 1); p != nil {
			t.Fatalf("ApplyLiquidation #1: %v", p)
		}
		if p := w.ApplyMarkPrice(100.25, 2); p != nil {
			t.Fatalf("ApplyMarkPrice #1: %v", p)
		}
		if p := w.ApplyFundingRate(-0.0005, 3); p != nil {
			t.Fatalf("ApplyFundingRate #1: %v", p)
		}
		if p := w.ApplyMarkPrice(101.75, 4); p != nil {
			t.Fatalf("ApplyMarkPrice #2: %v", p)
		}
	}

	apply(t, left)
	apply(t, right)

	if left.LiqBuyVolume != right.LiqBuyVolume || left.LiqSellVolume != right.LiqSellVolume || left.LiqTotalVolume != right.LiqTotalVolume {
		t.Fatalf("determinism failed for liquidation fields: left=%+v right=%+v", left, right)
	}
	if left.MarkPriceOpen != right.MarkPriceOpen || left.MarkPriceHigh != right.MarkPriceHigh ||
		left.MarkPriceLow != right.MarkPriceLow || left.MarkPriceClose != right.MarkPriceClose {
		t.Fatalf("determinism failed for markprice fields: left=%+v right=%+v", left, right)
	}
	if left.FundingRateAvg != right.FundingRateAvg || left.FundingRateLast != right.FundingRateLast {
		t.Fatalf("determinism failed for funding fields: left=%+v right=%+v", left, right)
	}
}
