package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/marketdata/domain"
)

func TestNewInstrumentMetadata(t *testing.T) {
	meta, p := domain.NewInstrumentMetadata("btc/usdt", "btcusdt", "SPOT")
	if p != nil {
		t.Fatalf("NewInstrumentMetadata failed: %v", p)
	}
	if meta.VenueSymbol != "BTCUSDT" {
		t.Fatalf("venue symbol = %q, want BTCUSDT", meta.VenueSymbol)
	}
	if meta.CanonicalSymbol != "BTC-USDT" {
		t.Fatalf("canonical symbol = %q, want BTC-USDT", meta.CanonicalSymbol)
	}
	if meta.BaseAsset != "BTC" || meta.QuoteAsset != "USDT" {
		t.Fatalf("unexpected pair: %#v", meta)
	}
	if meta.MarketType != domain.MarketTypeSpot {
		t.Fatalf("market type = %q, want %q", meta.MarketType, domain.MarketTypeSpot)
	}
}

func TestNewInstrumentMetadata_InvalidMarket(t *testing.T) {
	if _, p := domain.NewInstrumentMetadata("BTC-USDT", "BTCUSDT", "invalid"); p == nil {
		t.Fatal("expected validation problem")
	}
}
