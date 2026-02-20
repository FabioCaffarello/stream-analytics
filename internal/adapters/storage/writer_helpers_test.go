package storage

import (
	"testing"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
)

func TestNullableMarkPrice_AllPositive(t *testing.T) {
	s := aggdomain.StatsWindowV1{
		MarkPriceOpen: 100, MarkPriceHigh: 200, MarkPriceLow: 50, MarkPriceClose: 150,
	}
	o, h, l, c := NullableMarkPrice(s)
	if o == nil || h == nil || l == nil || c == nil {
		t.Fatal("expected non-nil values for all-positive mark prices")
	}
	if o.(float64) != 100 {
		t.Fatalf("open=%v want=100", o)
	}
}

func TestNullableMarkPrice_ZeroOpen(t *testing.T) {
	s := aggdomain.StatsWindowV1{
		MarkPriceOpen: 0, MarkPriceHigh: 200, MarkPriceLow: 50, MarkPriceClose: 150,
	}
	o, h, l, c := NullableMarkPrice(s)
	if o != nil || h != nil || l != nil || c != nil {
		t.Fatalf("expected all nil for zero open, got %v %v %v %v", o, h, l, c)
	}
}

func TestNullableMarkPrice_NegativeLow(t *testing.T) {
	s := aggdomain.StatsWindowV1{
		MarkPriceOpen: 100, MarkPriceHigh: 200, MarkPriceLow: -1, MarkPriceClose: 150,
	}
	o, h, l, c := NullableMarkPrice(s)
	if o != nil || h != nil || l != nil || c != nil {
		t.Fatalf("expected all nil for negative low, got %v %v %v %v", o, h, l, c)
	}
}

func TestNullableFundingRate_BothZero(t *testing.T) {
	s := aggdomain.StatsWindowV1{FundingRateAvg: 0, FundingRateLast: 0}
	avg, last := NullableFundingRate(s)
	if avg != nil || last != nil {
		t.Fatalf("expected nil for both-zero funding, got %v %v", avg, last)
	}
}

func TestNullableFundingRate_NonZeroAvg(t *testing.T) {
	s := aggdomain.StatsWindowV1{FundingRateAvg: 0.001, FundingRateLast: 0}
	avg, last := NullableFundingRate(s)
	if avg == nil || last == nil {
		t.Fatal("expected non-nil when avg is non-zero")
	}
	if avg.(float64) != 0.001 {
		t.Fatalf("avg=%v want=0.001", avg)
	}
}

func TestNullableFundingRate_NonZeroLast(t *testing.T) {
	s := aggdomain.StatsWindowV1{FundingRateAvg: 0, FundingRateLast: -0.005}
	avg, last := NullableFundingRate(s)
	if avg == nil || last == nil {
		t.Fatal("expected non-nil when last is non-zero")
	}
}

func TestWindowIdempotencyKey_Deterministic(t *testing.T) {
	k1 := WindowIdempotencyKey("binance", "BTC-USDT", "1m", 1700000000000)
	k2 := WindowIdempotencyKey("binance", "BTC-USDT", "1m", 1700000000000)
	if k1 != k2 {
		t.Fatalf("expected deterministic key, got %q != %q", k1, k2)
	}
	if k1 == "" {
		t.Fatal("expected non-empty key")
	}
}

func TestWindowIdempotencyKey_DifferentInputs(t *testing.T) {
	k1 := WindowIdempotencyKey("binance", "BTC-USDT", "1m", 1700000000000)
	k2 := WindowIdempotencyKey("bybit", "BTC-USDT", "1m", 1700000000000)
	if k1 == k2 {
		t.Fatal("expected different keys for different venues")
	}
}

func TestHeatmapBaseIdempotencyKey_Deterministic(t *testing.T) {
	k1 := HeatmapBaseIdempotencyKey("binance", "BTC-USDT", "1m", 1700000000000, "src-key-1")
	k2 := HeatmapBaseIdempotencyKey("binance", "BTC-USDT", "1m", 1700000000000, "src-key-1")
	if k1 != k2 {
		t.Fatalf("expected deterministic key, got %q != %q", k1, k2)
	}
}

func TestHeatmapCellIdempotencyKey_Deterministic(t *testing.T) {
	base := HeatmapBaseIdempotencyKey("binance", "BTC-USDT", "1m", 1700000000000, "src-key-1")
	k1 := HeatmapCellIdempotencyKey(base, 100.0, 200.0, "SMALL")
	k2 := HeatmapCellIdempotencyKey(base, 100.0, 200.0, "SMALL")
	if k1 != k2 {
		t.Fatalf("expected deterministic key, got %q != %q", k1, k2)
	}
}

func TestHeatmapCellIdempotencyKey_NormalizesSizeBucket(t *testing.T) {
	base := HeatmapBaseIdempotencyKey("binance", "BTC-USDT", "1m", 1700000000000, "src-key-1")
	k1 := HeatmapCellIdempotencyKey(base, 100.0, 200.0, "small")
	k2 := HeatmapCellIdempotencyKey(base, 100.0, 200.0, "  SMALL  ")
	if k1 != k2 {
		t.Fatalf("expected normalized size bucket, got %q != %q", k1, k2)
	}
}

func TestHeatmapCellIdempotencyKey_DifferentCells(t *testing.T) {
	base := HeatmapBaseIdempotencyKey("binance", "BTC-USDT", "1m", 1700000000000, "src-key-1")
	k1 := HeatmapCellIdempotencyKey(base, 100.0, 200.0, "SMALL")
	k2 := HeatmapCellIdempotencyKey(base, 200.0, 300.0, "SMALL")
	if k1 == k2 {
		t.Fatal("expected different keys for different price buckets")
	}
}
