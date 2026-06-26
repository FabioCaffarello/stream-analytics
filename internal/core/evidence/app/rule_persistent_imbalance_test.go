package app

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

func TestPersistentImbalanceNoEmitBalanced(t *testing.T) {
	rule := NewPersistentImbalanceRule(DefaultRuleConfig())
	for i := range 20 {
		events := rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BidDepth: 500, AskDepth: 500, // imbalance = 0
		})
		if len(events) > 0 {
			t.Fatalf("event %d: expected no emission for balanced book", i)
		}
	}
}

func TestPersistentImbalanceEmitOnStreak(t *testing.T) {
	rule := NewPersistentImbalanceRule(DefaultRuleConfig())
	// 10 consecutive bid-heavy observations (imbalance > 0.3)
	var lastEvents []domain.EvidenceEvent
	for i := range 15 {
		lastEvents = rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BidDepth: 800, AskDepth: 200, // imbalance = 0.6
		})
	}
	if len(lastEvents) == 0 {
		t.Fatal("expected emission after 10+ consecutive imbalanced observations")
	}
	if lastEvents[0].Type != domain.PersistentImbalance {
		t.Errorf("kind = %s, want persistent_imbalance", lastEvents[0].Type)
	}
	if lastEvents[0].Seq <= 0 || lastEvents[0].TsServer <= 0 {
		t.Fatalf("expected positive seq/ts, got seq=%d ts=%d", lastEvents[0].Seq, lastEvents[0].TsServer)
	}
}

func TestPersistentImbalanceResetOnFlip(t *testing.T) {
	rule := NewPersistentImbalanceRule(DefaultRuleConfig())
	// 8 bid-heavy, then flip to ask-heavy — counter should reset
	for i := range 8 {
		rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BidDepth: 800, AskDepth: 200,
		})
	}
	// Flip direction
	for i := range 8 {
		events := rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i+8) * 1000, Seq: int64(i + 8),
			BidDepth: 200, AskDepth: 800,
		})
		if i < 9 && len(events) > 0 {
			t.Fatalf("event %d after flip: expected no emission before 10 consecutive", i)
		}
	}
}

func TestPersistentImbalanceCooldownRespected(t *testing.T) {
	cfg := DefaultRuleConfig()
	cfg.CooldownMs = 10000
	rule := NewPersistentImbalanceRule(cfg)
	// Build up 12 consecutive to trigger
	for i := range 12 {
		rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BidDepth: 800, AskDepth: 200,
		})
	}
	// Next event within cooldown
	events := rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
		TsServer: 12500, Seq: 13,
		BidDepth: 800, AskDepth: 200,
	})
	if len(events) != 0 {
		t.Fatal("expected no emission during cooldown")
	}
}
