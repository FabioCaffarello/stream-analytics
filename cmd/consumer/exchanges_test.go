package main

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/market-raccoon/internal/actors/marketdata/ws"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/exchange/binance"
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
	if r.ManagerCfg.StreamsPerTicker != 2 {
		t.Fatalf("spot without extras: streams_per_ticker=%d want=2", r.ManagerCfg.StreamsPerTicker)
	}
	endpoint := r.ManagerCfg.EndpointBuilder([]string{"BTC-USDT"})
	if !strings.Contains(endpoint, "btcusdt@aggTrade") || !strings.Contains(endpoint, "btcusdt@depth@100ms") {
		t.Fatalf("expected trade/depth streams in endpoint: %s", endpoint)
	}
	if strings.Contains(endpoint, "btcusdt@markPrice") || strings.Contains(endpoint, "btcusdt@forceOrder") {
		t.Fatalf("unexpected markprice/liquidation streams when disabled: %s", endpoint)
	}

	cfg.Consumer.EnableMarkPriceLiquidation = true
	r = buildBinanceRuntime(cfg, slog.Default(), ex, actorruntime.SubsystemMarketData)
	if r.ManagerCfg.StreamsPerTicker != 4 {
		t.Fatalf("spot with extras: streams_per_ticker=%d want=4", r.ManagerCfg.StreamsPerTicker)
	}
	endpoint = r.ManagerCfg.EndpointBuilder([]string{"BTC-USDT"})
	if !strings.Contains(endpoint, "btcusdt@markPrice") || !strings.Contains(endpoint, "btcusdt@forceOrder") {
		t.Fatalf("expected markprice/liquidation streams when enabled: %s", endpoint)
	}
}

func TestBuildExchangeRuntimes_BinanceSpotFuturesSplit(t *testing.T) {
	cfg, p := config.Load("")
	if p != nil {
		t.Fatalf("Load defaults: %v", p)
	}
	cfg.Consumer.EnableMarkPriceLiquidation = false

	spot := config.ConsumerExchangeConfig{
		Name:       "binance-spot",
		Type:       "binance",
		Tickers:    []string{"BTCUSDT"},
		MarketType: "SPOT",
	}
	futures := config.ConsumerExchangeConfig{
		Name:       "binance-futures",
		Type:       "binance",
		Tickers:    []string{"BTCUSDT"},
		MarketType: "USD_M_FUTURES",
	}

	spotRuntime := buildBinanceRuntime(cfg, slog.Default(), spot, marketDataSubsystemKey(spot.Name, true))
	futuresRuntime := buildBinanceRuntime(cfg, slog.Default(), futures, marketDataSubsystemKey(futures.Name, true))

	assertBinanceSpotRuntime(t, spotRuntime)
	assertBinanceFuturesRuntime(t, futuresRuntime)

	msg := &ws.WsMessage{
		Data:   []byte(`{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1710000001000,"T":1710000002000,"s":"BTCUSDT","a":12345,"p":"42000.10","q":"0.200","m":true}}`),
		RecvAt: time.UnixMilli(1710000003000),
	}
	spotReq, spotSkip, _ := spotRuntime.ParseV2(msg)
	futuresReq, futuresSkip, _ := futuresRuntime.ParseV2(msg)
	if spotSkip || futuresSkip {
		t.Fatalf("expected both runtimes to parse aggTrade; spotSkip=%v futuresSkip=%v", spotSkip, futuresSkip)
	}
	if spotReq.EventType != futuresReq.EventType || spotReq.Instrument != futuresReq.Instrument {
		t.Fatalf("parser mismatch spot=%#v futures=%#v", spotReq, futuresReq)
	}
}

func assertBinanceSpotRuntime(t *testing.T, runtime consumerExchangeRuntime) {
	t.Helper()
	if runtime.ManagerCfg.StreamsPerTicker != 2 {
		t.Fatalf("spot streams_per_ticker=%d want=2", runtime.ManagerCfg.StreamsPerTicker)
	}
	spotEndpoint := runtime.ManagerCfg.EndpointBuilder([]string{"BTCUSDT"})
	if !strings.HasPrefix(spotEndpoint, binance.DefaultWSBaseURL) {
		t.Fatalf("spot endpoint=%q want prefix=%q", spotEndpoint, binance.DefaultWSBaseURL)
	}
	if strings.Contains(spotEndpoint, "@markPrice") || strings.Contains(spotEndpoint, "@forceOrder") {
		t.Fatalf("spot endpoint unexpectedly contains futures streams: %s", spotEndpoint)
	}
	if runtime.Subsystem != actorruntime.Subsystem("marketdata:binance-spot") {
		t.Fatalf("spot subsystem=%q", runtime.Subsystem)
	}
}

func assertBinanceFuturesRuntime(t *testing.T, runtime consumerExchangeRuntime) {
	t.Helper()
	if runtime.ManagerCfg.StreamsPerTicker != 4 {
		t.Fatalf("futures streams_per_ticker=%d want=4", runtime.ManagerCfg.StreamsPerTicker)
	}
	futuresEndpoint := runtime.ManagerCfg.EndpointBuilder([]string{"BTCUSDT"})
	if !strings.HasPrefix(futuresEndpoint, binance.DefaultFuturesWSBaseURL) {
		t.Fatalf("futures endpoint=%q want prefix=%q", futuresEndpoint, binance.DefaultFuturesWSBaseURL)
	}
	if !strings.Contains(futuresEndpoint, "@markPrice") || !strings.Contains(futuresEndpoint, "@forceOrder") {
		t.Fatalf("futures endpoint missing extras: %s", futuresEndpoint)
	}
	if runtime.Subsystem != actorruntime.Subsystem("marketdata:binance-futures") {
		t.Fatalf("futures subsystem=%q", runtime.Subsystem)
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
	if r.ManagerCfg.StreamsPerTicker != 2 {
		t.Fatalf("bybit without extras: streams_per_ticker=%d want=2", r.ManagerCfg.StreamsPerTicker)
	}
	msgs := r.ManagerCfg.SubscriptionBuilder([]string{"BTC-USDT"})
	if len(msgs) != 1 {
		t.Fatalf("subscriptions len=%d want 1", len(msgs))
	}
	body := string(msgs[0])
	if !strings.Contains(body, "publicTrade.BTCUSDT") || !strings.Contains(body, "orderbook.50.BTCUSDT") {
		t.Fatalf("expected trade/depth topics: %s", body)
	}
	if strings.Contains(body, "tickers.BTCUSDT") || strings.Contains(body, "allLiquidation.BTCUSDT") {
		t.Fatalf("unexpected markprice/liquidation topics when disabled: %s", body)
	}

	cfg.Consumer.EnableMarkPriceLiquidation = true
	r = buildBybitRuntime(cfg, slog.Default(), ex, actorruntime.SubsystemMarketData)
	if r.ManagerCfg.StreamsPerTicker != 4 {
		t.Fatalf("bybit with extras: streams_per_ticker=%d want=4", r.ManagerCfg.StreamsPerTicker)
	}
	msgs = r.ManagerCfg.SubscriptionBuilder([]string{"BTC-USDT"})
	if len(msgs) != 1 {
		t.Fatalf("subscriptions len=%d want 1", len(msgs))
	}
	body = string(msgs[0])
	if !strings.Contains(body, "tickers.BTCUSDT") {
		t.Fatalf("expected markprice topic when enabled: %s", body)
	}
	if !strings.Contains(body, "allLiquidation.BTCUSDT") {
		t.Fatalf("expected allLiquidation topic when enabled: %s", body)
	}

	hb := r.ManagerCfg.Heartbeat()
	if hb.Interval != 20*time.Second {
		t.Fatalf("heartbeat interval=%s want=%s", hb.Interval, 20*time.Second)
	}
	if got := string(hb.Message); got != `{"op":"ping"}` {
		t.Fatalf("heartbeat message=%q want=%q", got, `{"op":"ping"}`)
	}
}
