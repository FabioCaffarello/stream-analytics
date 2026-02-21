//go:build integration

package aggruntime_test

import (
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	aggruntime "github.com/market-raccoon/internal/actors/aggregation/runtime"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestProcessorE2E_TradeToCandle_WindowClose(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)
	ch := make(chan envelope.Envelope, 16)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- makeTradeEnvelope("BINANCE", "BTCUSDT", 1, 1, 100.0, "buy", "trade-1")
	ch <- makeTradeEnvelope("BINANCE", "BTCUSDT", 2, 60_001, 101.0, "sell", "trade-2")

	waitFor(t, 2*time.Second, func() bool { return pub.candleCount() == 1 })
	<-e.Poison(pid).Done()
}

func TestProcessorE2E_LiquidationToStats_WindowClose(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)
	ch := make(chan envelope.Envelope, 16)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- makeLiquidationEnvelope("BINANCE", "BTCUSDT", 1, 1, 2.0, "buy")
	ch <- makeLiquidationEnvelope("BINANCE", "BTCUSDT", 2, 60_001, 1.0, "sell")

	waitFor(t, 2*time.Second, func() bool { return pub.statsCount() > 0 })
	<-e.Poison(pid).Done()
}

func TestProcessorE2E_MarkPriceWithFunding_DualRouting(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)
	ch := make(chan envelope.Envelope, 16)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- makeMarkPriceEnvelope("BINANCE", "BTCUSDT", 1, 1, 100.0, 0.0002)
	ch <- makeMarkPriceEnvelope("BINANCE", "BTCUSDT", 2, 60_001, 101.0, 0.0003)

	waitFor(t, 2*time.Second, func() bool { return pub.statsCount() > 0 })
	closed := pub.lastStats().Stats
	if closed.MarkPriceOpen == 0 || closed.MarkPriceClose == 0 {
		t.Fatalf("expected markprice fields to be populated, got open=%f close=%f", closed.MarkPriceOpen, closed.MarkPriceClose)
	}
	if closed.FundingRateLast == 0 {
		t.Fatalf("expected non-zero funding_rate_last, got %f", closed.FundingRateLast)
	}

	<-e.Poison(pid).Done()
}
