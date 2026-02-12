package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/marketdata/domain"
)

func TestParseCanonicalPair(t *testing.T) {
	base, quote, p := domain.ParseCanonicalPair("BTC-USDT")
	if p != nil {
		t.Fatalf("ParseCanonicalPair failed: %v", p)
	}
	if base != "BTC" || quote != "USDT" {
		t.Fatalf("unexpected pair: base=%s quote=%s", base, quote)
	}
}

func TestParseCanonicalPair_Invalid(t *testing.T) {
	_, _, p := domain.ParseCanonicalPair("BTCUSDT")
	if p == nil {
		t.Fatal("expected validation problem")
	}
}

func TestNewInstrumentIdentity(t *testing.T) {
	id, p := domain.NewInstrumentIdentity("eth/usdt", "ETHUSDT", "SPOT")
	if p != nil {
		t.Fatalf("NewInstrumentIdentity failed: %v", p)
	}
	if id.Canonical != "ETH-USDT" {
		t.Fatalf("canonical = %s, want ETH-USDT", id.Canonical)
	}
	if id.Base != "ETH" || id.Quote != "USDT" {
		t.Fatalf("pair mismatch: %#v", id)
	}
	if id.VenueSymbol != "ETHUSDT" || id.MarketType != domain.MarketTypeSpot {
		t.Fatalf("identity mismatch: %#v", id)
	}
}
