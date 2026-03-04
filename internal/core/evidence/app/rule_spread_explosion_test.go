package app

import (
	"testing"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

func TestSpreadExplosionNoEmitNormal(t *testing.T) {
	rule := NewSpreadExplosionRule(DefaultRuleConfig())
	// Feed 20 normal events with tight spread (~5 bps)
	for i := range 20 {
		ev := domain.RuleEvent{
			Kind:     domain.EventKindBook,
			Venue:    "binance",
			Symbol:   "BTC-USDT",
			TsServer: int64(i) * 1000,
			Seq:      int64(i),
			BestBid:  50000.0,
			BestAsk:  50002.5, // ~5 bps
		}
		got := rule.OnEvent(ev)
		if len(got) > 0 {
			t.Fatalf("event %d: expected no emission for normal spread", i)
		}
	}
}

func TestSpreadExplosionEmitOnSpike(t *testing.T) {
	rule := NewSpreadExplosionRule(DefaultRuleConfig())
	// Build baseline with tight spread
	for i := range 15 {
		rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BestBid: 50000, BestAsk: 50002, // ~4 bps
		})
	}
	// Spike: wide spread
	events := rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
		TsServer: 20000, Seq: 20,
		BestBid: 50000, BestAsk: 50100, // ~200 bps
	})
	if len(events) == 0 {
		t.Fatal("expected emission on spread spike")
	}
	if events[0].Type != domain.SpreadExplosion {
		t.Errorf("kind = %s, want spread_explosion", events[0].Type)
	}
	if events[0].Venue != "binance" {
		t.Errorf("venue = %s, want binance", events[0].Venue)
	}
	if events[0].Seq != 20 || events[0].TsServer != 20000 {
		t.Fatalf("unexpected seq/ts: seq=%d ts=%d", events[0].Seq, events[0].TsServer)
	}
}

func TestSpreadExplosionCooldownRespected(t *testing.T) {
	rule := NewSpreadExplosionRule(DefaultRuleConfig())
	for i := range 15 {
		rule.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BestBid: 50000, BestAsk: 50002,
		})
	}
	// First spike emits
	events := rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
		TsServer: 20000, Seq: 20, BestBid: 50000, BestAsk: 50100,
	})
	if len(events) == 0 {
		t.Fatal("expected first emission")
	}
	// Second spike within cooldown — no emit
	events = rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT",
		TsServer: 21000, Seq: 21, BestBid: 50000, BestAsk: 50100,
	})
	if len(events) != 0 {
		t.Fatal("expected no emission during cooldown")
	}
}

func TestSpreadExplosionMultiStream(t *testing.T) {
	rule := NewSpreadExplosionRule(DefaultRuleConfig())
	streams := []string{"BTC-USDT", "ETH-USDT"}
	for _, sym := range streams {
		for i := range 15 {
			rule.OnEvent(domain.RuleEvent{
				Kind: domain.EventKindBook, Venue: "binance", Symbol: sym,
				TsServer: int64(i) * 1000, Seq: int64(i),
				BestBid: 50000, BestAsk: 50002,
			})
		}
	}
	if rule.StreamCount() != 2 {
		t.Errorf("StreamCount = %d, want 2", rule.StreamCount())
	}
}

func TestSpreadExplosionIgnoresNonBook(t *testing.T) {
	rule := NewSpreadExplosionRule(DefaultRuleConfig())
	events := rule.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindTrade, Venue: "binance", Symbol: "BTC-USDT",
		TsServer: 1000, Seq: 1, TradePrice: 50000, TradeSize: 1.0,
	})
	if len(events) != 0 {
		t.Fatal("expected no emission for trade event")
	}
}
