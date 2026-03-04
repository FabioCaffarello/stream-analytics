package app

import (
	"testing"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

func TestAbsorptionNoEmitNormal(t *testing.T) {
	rule := NewAbsorptionRule(DefaultRuleConfig())
	for i := range 20 {
		events := rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindTrade, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			TradePrice: 50000 + float64(i)*10, // price moves
			TradeSize:  1.0,
			TradeSide:  "buy",
		})
		if len(events) > 0 {
			t.Fatalf("event %d: expected no emission for normal trades", i)
		}
	}
}

func TestAbsorptionEmitOnAbsorption(t *testing.T) {
	rule := NewAbsorptionRule(DefaultRuleConfig())
	// Build baseline with small trades
	for i := range 15 {
		rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindTrade, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			TradePrice: 50000.0, // stable price
			TradeSize:  0.1,
			TradeSide:  "buy",
		})
	}
	// Flood with large volume, stable price
	var emitted bool
	for i := 15; i < 200; i++ {
		events := rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindTrade, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 100, Seq: int64(i),
			TradePrice: 50000.0, // no price movement
			TradeSize:  5.0,     // 50x normal
			TradeSide:  "buy",
		})
		if len(events) > 0 {
			emitted = true
			if events[0].Type != domain.Absorption {
				t.Errorf("kind = %s, want absorption", events[0].Type)
			}
			if events[0].Seq <= 0 || events[0].TsServer <= 0 {
				t.Fatalf("expected positive seq/ts, got seq=%d ts=%d", events[0].Seq, events[0].TsServer)
			}
			break
		}
	}
	if !emitted {
		t.Fatal("expected at least one absorption emission")
	}
}

func TestAbsorptionCooldownRespected(t *testing.T) {
	rule := NewAbsorptionRule(DefaultRuleConfig())
	// Build baseline
	for i := range 15 {
		rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindTrade, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			TradePrice: 50000.0, TradeSize: 0.1, TradeSide: "buy",
		})
	}
	// Find first emission
	firstEmitTs := int64(0)
	for i := 15; i < 500; i++ {
		events := rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindTrade, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 100, Seq: int64(i),
			TradePrice: 50000.0, TradeSize: 5.0, TradeSide: "buy",
		})
		if len(events) > 0 {
			firstEmitTs = events[0].TsServer
			break
		}
	}
	if firstEmitTs == 0 {
		t.Fatal("expected first emission")
	}

	// Immediately after — should be in cooldown
	events := rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindTrade, Venue: "binance", Symbol: "BTC-USDT",
		TsServer: firstEmitTs + 100, Seq: 999,
		TradePrice: 50000.0, TradeSize: 100.0, TradeSide: "buy",
	})
	if len(events) != 0 {
		t.Fatal("expected no emission during cooldown")
	}
}

func TestAbsorptionMultiStream(t *testing.T) {
	rule := NewAbsorptionRule(DefaultRuleConfig())
	rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindTrade, Venue: "binance", Symbol: "BTC-USDT",
		TsServer: 1000, Seq: 1, TradePrice: 50000, TradeSize: 1, TradeSide: "buy",
	})
	rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindTrade, Venue: "coinbase", Symbol: "ETH-USD",
		TsServer: 1000, Seq: 2, TradePrice: 3000, TradeSize: 1, TradeSide: "sell",
	})
	if rule.StreamCount() != 2 {
		t.Errorf("StreamCount = %d, want 2", rule.StreamCount())
	}
}

func TestAbsorptionIgnoresNonTrade(t *testing.T) {
	rule := NewAbsorptionRule(DefaultRuleConfig())
	events := rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
		TsServer: 1000, Seq: 1, BestBid: 50000, BestAsk: 50001,
	})
	if len(events) != 0 {
		t.Fatal("expected no emission for book event")
	}
}
