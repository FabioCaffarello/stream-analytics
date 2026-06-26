package app_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
)

func TestBuildMarkPriceDedupKey_CanonicalAndStable(t *testing.T) {
	p := domain.MarkPriceTickV1{
		MarkPrice:   50000,
		IndexPrice:  49990,
		FundingRate: 0.0001,
		Timestamp:   1_710_000_000_001,
	}

	k1 := app.BuildMarkPriceDedupKey(" binance ", "btc/usdt", 1, 1_710_000_000_001, 1_710_000_000_100, p, "")
	k2 := app.BuildMarkPriceDedupKey("BINANCE", "BTC-USDT", 1, 1_710_000_000_001, 1_710_000_000_100, p, "")
	if k1 == "" || k2 == "" {
		t.Fatal("dedup key must not be empty")
	}
	if k1 != k2 {
		t.Fatalf("canonical key mismatch: %q != %q", k1, k2)
	}
}

func TestBuildLiquidationDedupKey_SourceIdempotencyOverridesPayload(t *testing.T) {
	p1 := domain.LiquidationTickV1{
		Side:      "sell",
		Price:     50000,
		Size:      2,
		Timestamp: 1_710_000_100_001,
	}
	p2 := domain.LiquidationTickV1{
		Side:      "buy",
		Price:     51000,
		Size:      3,
		Timestamp: 1_710_000_100_002,
	}

	k1 := app.BuildLiquidationDedupKey("bybit", "ETH-USDT", 1, 1_710_000_100_001, 1_710_000_100_100, p1, "source-123")
	k2 := app.BuildLiquidationDedupKey("BYBIT", "eth/usdt", 1, 1_710_000_100_999, 1_710_000_100_888, p2, "source-123")
	if k1 != k2 {
		t.Fatalf("keys must match when source idempotency matches: %q != %q", k1, k2)
	}
}

func TestBuildMarkPriceDedupKey_PayloadChangeChangesKey(t *testing.T) {
	base := domain.MarkPriceTickV1{
		MarkPrice:   50000,
		IndexPrice:  49990,
		FundingRate: 0.0001,
		Timestamp:   1_710_000_200_001,
	}
	changed := base
	changed.MarkPrice = 50001

	k1 := app.BuildMarkPriceDedupKey("binance", "BTCUSDT", 1, 1_710_000_200_001, 1_710_000_200_100, base, "")
	k2 := app.BuildMarkPriceDedupKey("binance", "BTCUSDT", 1, 1_710_000_200_001, 1_710_000_200_100, changed, "")
	if k1 == k2 {
		t.Fatal("expected different dedup keys for different payloads")
	}
}
