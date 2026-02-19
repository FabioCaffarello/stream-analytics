package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	ws "github.com/market-raccoon/internal/actors/marketdata/ws"
	mdapp "github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

type seqStub struct {
	byKey map[string]int64
}

func (s *seqStub) Next(venue, instrument string) (int64, *problem.Problem) {
	if s.byKey == nil {
		s.byKey = make(map[string]int64)
	}
	key := venue + "|" + instrument
	s.byKey[key]++
	return s.byKey[key], nil
}

type pubStub struct {
	envs []envelope.Envelope
}

func (p *pubStub) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	p.envs = append(p.envs, env)
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildExchangeRuntimes_LegacySingleExchange(t *testing.T) {
	cfg, prob := config.Load("")
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}

	runtimes, p := buildExchangeRuntimes(cfg, testLogger())
	if p != nil {
		t.Fatalf("buildExchangeRuntimes failed: %v", p)
	}
	if len(runtimes) != 1 {
		t.Fatalf("runtimes len=%d want=1", len(runtimes))
	}
	if runtimes[0].Subsystem != "marketdata" {
		t.Fatalf("subsystem=%q want marketdata", runtimes[0].Subsystem)
	}

	endpoint := runtimes[0].ManagerCfg.EndpointBuilder([]string{"BTC-USDT"})
	if !strings.Contains(endpoint, "btcusdt@aggTrade") {
		t.Fatalf("unexpected endpoint=%q", endpoint)
	}
}

func TestBuildExchangeRuntimes_MultiExchange(t *testing.T) {
	cfg, prob := config.Load("")
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}
	cfg.Consumer.Exchanges = []config.ConsumerExchangeConfig{
		{
			Name:       "binance",
			Type:       "binance",
			BaseURL:    "wss://stream.binance.com:9443/stream",
			Tickers:    []string{"BTC-USDT"},
			MarketType: "SPOT",
		},
		{
			Name:       "bybit",
			Type:       "bybit",
			BaseURL:    "wss://stream.bybit.com/v5/public/spot",
			Tickers:    []string{"BTC-USDT"},
			MarketType: "SPOT",
		},
	}

	runtimes, p := buildExchangeRuntimes(cfg, testLogger())
	if p != nil {
		t.Fatalf("buildExchangeRuntimes failed: %v", p)
	}
	if len(runtimes) != 2 {
		t.Fatalf("runtimes len=%d want=2", len(runtimes))
	}
	if runtimes[0].Subsystem != "marketdata:binance" || runtimes[1].Subsystem != "marketdata:bybit" {
		t.Fatalf("unexpected subsystems: %q, %q", runtimes[0].Subsystem, runtimes[1].Subsystem)
	}

	bybitSubMsgs := runtimes[1].ManagerCfg.SubscriptionBuilder([]string{"BTC-USDT"})
	if len(bybitSubMsgs) != 1 || !strings.Contains(string(bybitSubMsgs[0]), "publicTrade.BTCUSDT") {
		t.Fatalf("unexpected bybit subscriptions: %q", bybitSubMsgs)
	}
}

func TestMultiExchangeParserIntegration_SameProcess(t *testing.T) {
	cfg, prob := config.Load("")
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}
	cfg.Consumer.Exchanges = []config.ConsumerExchangeConfig{
		{
			Name:       "binance",
			Type:       "binance",
			BaseURL:    "wss://stream.binance.com:9443/stream",
			Tickers:    []string{"BTC-USDT"},
			MarketType: "SPOT",
		},
		{
			Name:       "bybit",
			Type:       "bybit",
			BaseURL:    "wss://stream.bybit.com/v5/public/spot",
			Tickers:    []string{"BTC-USDT"},
			MarketType: "SPOT",
		},
	}

	runtimes, p := buildExchangeRuntimes(cfg, testLogger())
	if p != nil {
		t.Fatalf("buildExchangeRuntimes failed: %v", p)
	}

	byName := make(map[string]consumerExchangeRuntime, len(runtimes))
	for _, rt := range runtimes {
		byName[rt.Exchange.Name] = rt
	}

	clk := clock.NewFakeClock(time.UnixMilli(1710000005000))
	seq := &seqStub{}
	pub := &pubStub{}
	uc := mdapp.NewIngestMarketData(clk, seq, pub)

	binanceMsg := &ws.WsMessage{
		Exchange: "binance",
		Data:     []byte(`{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1710000001000,"T":1710000002000,"s":"BTCUSDT","a":12345,"p":"42000.10","q":"0.200","m":true}}`),
		RecvAt:   time.UnixMilli(1710000003000),
	}
	req, skip := byName["binance"].ParseV1(binanceMsg)
	if skip {
		t.Fatal("binance parser unexpectedly skipped")
	}
	if res := uc.Execute(context.Background(), req); res.IsFail() {
		t.Fatalf("binance ingest failed: %v", res.Problem())
	}

	bybitMsg := &ws.WsMessage{
		Exchange: "bybit",
		Data:     []byte(`{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1710000001000,"data":[{"T":1710000001001,"s":"BTCUSDT","S":"Buy","v":"0.010","p":"65000.50","i":"123456"}]}`),
		RecvAt:   time.UnixMilli(1710000003000),
	}
	req, skip = byName["bybit"].ParseV1(bybitMsg)
	if skip {
		t.Fatal("bybit parser unexpectedly skipped")
	}
	if res := uc.Execute(context.Background(), req); res.IsFail() {
		t.Fatalf("bybit ingest failed: %v", res.Problem())
	}

	if len(pub.envs) != 2 {
		t.Fatalf("published envelopes=%d want=2", len(pub.envs))
	}
	if pub.envs[0].Venue != "BINANCE" || pub.envs[1].Venue != "BYBIT" {
		t.Fatalf("venues=%q,%q want BINANCE,BYBIT", pub.envs[0].Venue, pub.envs[1].Venue)
	}
	if seq.byKey["BINANCE|BTCUSDT:SPOT"] != 1 || seq.byKey["BYBIT|BTCUSDT:SPOT"] != 1 {
		t.Fatalf("unexpected sequencer keys: %+v", seq.byKey)
	}
}

func TestMarketDataSubsystemKey(t *testing.T) {
	if got := marketDataSubsystemKey("binance", false); got != "marketdata" {
		t.Fatalf("key=%q want marketdata", got)
	}
	if got := marketDataSubsystemKey("ByBit", true); got != "marketdata:bybit" {
		t.Fatalf("key=%q want marketdata:bybit", got)
	}
}

func TestBuildExchangeRuntimes_MarketTypePropagation(t *testing.T) {
	cfg, prob := config.Load("")
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}
	cfg.Consumer.Exchanges = []config.ConsumerExchangeConfig{
		{
			Name:       "bybit",
			Type:       "bybit",
			BaseURL:    "wss://stream.bybit.com/v5/public/linear",
			Tickers:    []string{"BTC-USDT"},
			MarketType: "USD_M_FUTURES",
		},
	}

	runtimes, p := buildExchangeRuntimes(cfg, testLogger())
	if p != nil {
		t.Fatalf("buildExchangeRuntimes failed: %v", p)
	}
	msg := &ws.WsMessage{
		Exchange: "bybit",
		Data:     []byte(`{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1710000001000,"data":[{"T":1710000001001,"s":"BTCUSDT","S":"Buy","v":"0.010","p":"65000.50","i":"123456"}]}`),
		RecvAt:   time.UnixMilli(1710000003000),
	}
	req, skip := runtimes[0].ParseV1(msg)
	if skip {
		t.Fatal("parser unexpectedly skipped")
	}
	if req.MarketType != domain.MarketTypeUSDMFutures.String() {
		t.Fatalf("req.MarketType=%q want %q", req.MarketType, domain.MarketTypeUSDMFutures.String())
	}
}

func TestShutdown_SlowPublisherDoesNotStarveGuardian(t *testing.T) {
	const (
		publisherFlushTimeout   = 40 * time.Millisecond
		guardianShutdownTimeout = 200 * time.Millisecond
		tolerance               = 35 * time.Millisecond
	)

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	var (
		mu                  sync.Mutex
		sequence            []string
		stopCalled          bool
		waitCalled          bool
		observedGuardBudget time.Duration
	)

	shutdownConsumerRuntime(
		logger,
		consumerShutdownHooks{
			shutdownE2E: func(context.Context) *problem.Problem {
				mu.Lock()
				sequence = append(sequence, "e2e")
				mu.Unlock()
				return nil
			},
			closePublisher: func(ctx context.Context) *problem.Problem {
				mu.Lock()
				sequence = append(sequence, "publisher")
				mu.Unlock()

				start := time.Now()
				<-ctx.Done() // Simula flush lento que estoura o timeout dedicado do publisher.
				elapsed := time.Since(start)
				if elapsed < publisherFlushTimeout-tolerance {
					t.Fatalf("publisher close elapsed=%s want >= %s", elapsed, publisherFlushTimeout-tolerance)
				}
				if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
					t.Fatalf("publisher close ctx err=%v want deadline exceeded", ctx.Err())
				}
				return nil
			},
			stopGuardian: func() {
				mu.Lock()
				sequence = append(sequence, "stop")
				stopCalled = true
				mu.Unlock()
			},
			waitGuardianStopped: func(ctx context.Context) bool {
				mu.Lock()
				sequence = append(sequence, "wait")
				waitCalled = true
				if deadline, ok := ctx.Deadline(); ok {
					observedGuardBudget = time.Until(deadline)
				}
				mu.Unlock()
				return true
			},
		},
		publisherFlushTimeout,
		guardianShutdownTimeout,
	)

	mu.Lock()
	defer mu.Unlock()
	if !stopCalled {
		t.Fatal("guardian stop was not executed")
	}
	if !waitCalled {
		t.Fatal("guardian poison/wait was not executed")
	}
	if observedGuardBudget < guardianShutdownTimeout-tolerance {
		t.Fatalf("guardian budget observed=%s want >= %s", observedGuardBudget, guardianShutdownTimeout-tolerance)
	}
	if got, want := strings.Join(sequence, ","), "e2e,publisher,stop,wait"; got != want {
		t.Fatalf("shutdown sequence=%q want %q", got, want)
	}
	if strings.Contains(logs.String(), "guardian did not stop in time") {
		t.Fatalf("unexpected guardian timeout log: %s", logs.String())
	}
}
