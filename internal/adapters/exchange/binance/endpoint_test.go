package binance_test

import (
	"strings"
	"testing"

	"github.com/market-raccoon/internal/adapters/exchange/binance"
)

func TestBuildEndpoint(t *testing.T) {
	endpoint, p := binance.BuildEndpoint("", []string{"BTC-USDT", "ethusdt"}, false)
	if p != nil {
		t.Fatalf("BuildEndpoint: %v", p)
	}
	if !strings.Contains(endpoint, "btcusdt@aggTrade") || !strings.Contains(endpoint, "ethusdt@depth@100ms") {
		t.Fatalf("unexpected endpoint: %s", endpoint)
	}
}

func TestBuildEndpoint_requiresTicker(t *testing.T) {
	_, p := binance.BuildEndpoint("", nil, false)
	if p == nil {
		t.Fatal("expected problem")
	}
}

func TestBuildEndpoint_TrimsTrailingSlash(t *testing.T) {
	endpoint, p := binance.BuildEndpoint("wss://stream.binance.com:9443/stream/", []string{"BTC-USDT"}, false)
	if p != nil {
		t.Fatalf("BuildEndpoint: %v", p)
	}
	if strings.Contains(endpoint, "//?streams=") {
		t.Fatalf("unexpected endpoint with double slash: %s", endpoint)
	}
}

func TestBuildEndpoint_InvalidTicker(t *testing.T) {
	_, p := binance.BuildEndpoint("", []string{""}, false)
	if p == nil {
		t.Fatal("expected problem for invalid ticker")
	}
}

func TestBuildEndpoint_IncludesMarkPriceLiquidation(t *testing.T) {
	endpoint, p := binance.BuildEndpoint("", []string{"BTC-USDT"}, true)
	if p != nil {
		t.Fatalf("BuildEndpoint: %v", p)
	}
	if !strings.Contains(endpoint, "btcusdt@markPrice") || !strings.Contains(endpoint, "btcusdt@forceOrder") {
		t.Fatalf("expected markprice/liquidation streams in endpoint: %s", endpoint)
	}
}
