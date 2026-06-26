package app

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

func TestLiquidityThinningNoEmitNormal(t *testing.T) {
	rule := NewLiquidityThinningRule(DefaultRuleConfig())
	for i := range 20 {
		events := rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BidDepth: 500, AskDepth: 500,
		})
		if len(events) > 0 {
			t.Fatalf("event %d: expected no emission for stable depth", i)
		}
	}
}

func TestLiquidityThinningEmitOnDrop(t *testing.T) {
	rule := NewLiquidityThinningRule(DefaultRuleConfig())
	// Build baseline with consistent depth
	for i := range 15 {
		rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BidDepth: 1000, AskDepth: 1000, // total = 2000
		})
	}
	// Drop to 30% of mean (total = 600, mean ≈ 2000, drop ≈ 70%)
	events := rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
		TsServer: 20000, Seq: 20,
		BidDepth: 300, AskDepth: 300,
	})
	if len(events) == 0 {
		t.Fatal("expected emission on depth drop")
	}
	if events[0].Type != domain.LiquidityThinning {
		t.Errorf("kind = %s, want liquidity_thinning", events[0].Type)
	}
	if events[0].Seq != 20 || events[0].TsServer != 20000 {
		t.Fatalf("unexpected seq/ts: seq=%d ts=%d", events[0].Seq, events[0].TsServer)
	}
}

func TestLiquidityThinningCooldownRespected(t *testing.T) {
	rule := NewLiquidityThinningRule(DefaultRuleConfig())
	for i := range 15 {
		rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BidDepth: 1000, AskDepth: 1000,
		})
	}
	// First drop emits
	events := rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
		TsServer: 20000, Seq: 20, BidDepth: 300, AskDepth: 300,
	})
	if len(events) == 0 {
		t.Fatal("expected first emission")
	}
	// Second drop within cooldown
	events = rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
		TsServer: 21000, Seq: 21, BidDepth: 200, AskDepth: 200,
	})
	if len(events) != 0 {
		t.Fatal("expected no emission during cooldown")
	}
}

func TestLiquidityThinningMultiStream(t *testing.T) {
	rule := NewLiquidityThinningRule(DefaultRuleConfig())
	for i := range 15 {
		rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BidDepth: 1000, AskDepth: 1000,
		})
		rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "coinbase", Symbol: "ETH-USD",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BidDepth: 500, AskDepth: 500,
		})
	}
	if rule.StreamCount() != 2 {
		t.Errorf("StreamCount = %d, want 2", rule.StreamCount())
	}
}
