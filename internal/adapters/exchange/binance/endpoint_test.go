package binance_test

import (
	"strings"
	"testing"

	"github.com/market-raccoon/internal/adapters/exchange/binance"
)

func TestBuildEndpoint(t *testing.T) {
	endpoint, p := binance.BuildEndpoint("", []string{"BTC-USDT", "ethusdt"})
	if p != nil {
		t.Fatalf("BuildEndpoint: %v", p)
	}
	if !strings.Contains(endpoint, "btcusdt@aggTrade") || !strings.Contains(endpoint, "ethusdt@depth@100ms") {
		t.Fatalf("unexpected endpoint: %s", endpoint)
	}
}

func TestBuildEndpoint_requiresTicker(t *testing.T) {
	_, p := binance.BuildEndpoint("", nil)
	if p == nil {
		t.Fatal("expected problem")
	}
}
