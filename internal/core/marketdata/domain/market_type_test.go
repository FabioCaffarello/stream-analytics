package domain_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
)

func TestNewMarketType(t *testing.T) {
	mt, p := domain.NewMarketType("spot")
	if p != nil {
		t.Fatalf("NewMarketType failed: %v", p)
	}
	if mt != domain.MarketTypeSpot {
		t.Fatalf("market type = %q, want %q", mt, domain.MarketTypeSpot)
	}
}

func TestNewMarketType_Invalid(t *testing.T) {
	if _, p := domain.NewMarketType("options"); p == nil {
		t.Fatal("expected validation problem")
	}
}
