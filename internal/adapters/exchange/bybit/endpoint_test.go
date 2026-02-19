package bybit_test

import (
	"strings"
	"testing"

	"github.com/market-raccoon/internal/adapters/exchange/bybit"
)

func TestBuildEndpoint_DefaultSpot(t *testing.T) {
	endpoint, p := bybit.BuildEndpoint("", []string{"BTC-USDT"}, "SPOT")
	if p != nil {
		t.Fatalf("BuildEndpoint: %v", p)
	}
	if endpoint != bybit.DefaultSpotWSBaseURL {
		t.Fatalf("endpoint = %q, want %q", endpoint, bybit.DefaultSpotWSBaseURL)
	}
}

func TestBuildEndpoint_TrimsTrailingSlash(t *testing.T) {
	endpoint, p := bybit.BuildEndpoint("wss://stream.bybit.com/v5/public/spot/", []string{"BTC-USDT"}, "SPOT")
	if p != nil {
		t.Fatalf("BuildEndpoint: %v", p)
	}
	if strings.HasSuffix(endpoint, "/") {
		t.Fatalf("unexpected trailing slash endpoint: %s", endpoint)
	}
}

func TestBuildEndpoint_RequiresTicker(t *testing.T) {
	_, p := bybit.BuildEndpoint("", nil, "SPOT")
	if p == nil {
		t.Fatal("expected problem")
	}
}

func TestBuildEndpoint_RejectsInvalidMarketType(t *testing.T) {
	_, p := bybit.BuildEndpoint("", []string{"BTC-USDT"}, "OPTIONS")
	if p == nil {
		t.Fatal("expected problem")
	}
}

func TestBuildSubscriptions(t *testing.T) {
	msgs, p := bybit.BuildSubscriptions([]string{"BTC-USDT", "ethusdt"}, false)
	if p != nil {
		t.Fatalf("BuildSubscriptions: %v", p)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d, want 1", len(msgs))
	}
	body := string(msgs[0])
	if !strings.Contains(body, "publicTrade.BTCUSDT") || !strings.Contains(body, "orderbook.50.ETHUSDT") {
		t.Fatalf("unexpected subscription body: %s", body)
	}
}

func TestBuildSubscriptions_IncludesMarkPrice(t *testing.T) {
	msgs, p := bybit.BuildSubscriptions([]string{"BTC-USDT"}, true)
	if p != nil {
		t.Fatalf("BuildSubscriptions: %v", p)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d, want 1", len(msgs))
	}
	body := string(msgs[0])
	if !strings.Contains(body, "tickers.BTCUSDT") {
		t.Fatalf("unexpected subscription body: %s", body)
	}
	if !strings.Contains(body, "allLiquidation.BTCUSDT") {
		t.Fatalf("expected allLiquidation topic in bybit subscriptions: %s", body)
	}
}
