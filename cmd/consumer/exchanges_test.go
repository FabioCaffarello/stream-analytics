package main

import (
	"log/slog"
	"strings"
	"testing"

	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/shared/config"
)

func TestBuildBinanceRuntime_StreamToggle(t *testing.T) {
	ex := config.ConsumerExchangeConfig{
		Name:       "binance",
		Type:       "binance",
		BaseURL:    "wss://stream.binance.com:9443/stream",
		Tickers:    []string{"BTC-USDT"},
		MarketType: "SPOT",
	}

	cfg, p := config.Load("")
	if p != nil {
		t.Fatalf("Load defaults: %v", p)
	}
	cfg.Consumer.EnableMarkPriceLiquidation = false
	r := buildBinanceRuntime(cfg, slog.Default(), ex, actorruntime.SubsystemMarketData)
	endpoint := r.ManagerCfg.EndpointBuilder([]string{"BTC-USDT"})
	if !strings.Contains(endpoint, "btcusdt@aggTrade") || !strings.Contains(endpoint, "btcusdt@depth@100ms") {
		t.Fatalf("expected trade/depth streams in endpoint: %s", endpoint)
	}
	if strings.Contains(endpoint, "btcusdt@markPrice") || strings.Contains(endpoint, "btcusdt@forceOrder") {
		t.Fatalf("unexpected markprice/liquidation streams when disabled: %s", endpoint)
	}

	cfg.Consumer.EnableMarkPriceLiquidation = true
	r = buildBinanceRuntime(cfg, slog.Default(), ex, actorruntime.SubsystemMarketData)
	endpoint = r.ManagerCfg.EndpointBuilder([]string{"BTC-USDT"})
	if !strings.Contains(endpoint, "btcusdt@markPrice") || !strings.Contains(endpoint, "btcusdt@forceOrder") {
		t.Fatalf("expected markprice/liquidation streams when enabled: %s", endpoint)
	}
}

func TestBuildBybitRuntime_SubscriptionToggle(t *testing.T) {
	ex := config.ConsumerExchangeConfig{
		Name:       "bybit",
		Type:       "bybit",
		BaseURL:    "wss://stream.bybit.com/v5/public/spot",
		Tickers:    []string{"BTC-USDT"},
		MarketType: "SPOT",
	}

	cfg, p := config.Load("")
	if p != nil {
		t.Fatalf("Load defaults: %v", p)
	}
	cfg.Consumer.EnableMarkPriceLiquidation = false
	r := buildBybitRuntime(cfg, slog.Default(), ex, actorruntime.SubsystemMarketData)
	msgs := r.ManagerCfg.SubscriptionBuilder([]string{"BTC-USDT"})
	if len(msgs) != 1 {
		t.Fatalf("subscriptions len=%d want 1", len(msgs))
	}
	body := string(msgs[0])
	if !strings.Contains(body, "publicTrade.BTCUSDT") || !strings.Contains(body, "orderbook.50.BTCUSDT") {
		t.Fatalf("expected trade/depth topics: %s", body)
	}
	if strings.Contains(body, "tickers.BTCUSDT") || strings.Contains(body, "liquidation.BTCUSDT") {
		t.Fatalf("unexpected markprice/liquidation topics when disabled: %s", body)
	}

	cfg.Consumer.EnableMarkPriceLiquidation = true
	r = buildBybitRuntime(cfg, slog.Default(), ex, actorruntime.SubsystemMarketData)
	msgs = r.ManagerCfg.SubscriptionBuilder([]string{"BTC-USDT"})
	if len(msgs) != 1 {
		t.Fatalf("subscriptions len=%d want 1", len(msgs))
	}
	body = string(msgs[0])
	if !strings.Contains(body, "tickers.BTCUSDT") || !strings.Contains(body, "liquidation.BTCUSDT") {
		t.Fatalf("expected markprice/liquidation topics when enabled: %s", body)
	}
}
