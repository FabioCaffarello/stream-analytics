package app

import (
	"testing"
	"time"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

func testEngineConfig(now func() time.Time) EngineConfig {
	return EngineConfig{
		MaxStreamsPerRule: 256,
		MaxStreamsGlobal:  10,
		StreamTTL:         5 * time.Minute,
		SweepInterval:     1 * time.Minute,
		Now:               now,
	}
}

func TestEngineDispatchesToAllRules(t *testing.T) {
	now := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
	cfg := testEngineConfig(func() time.Time { return now })
	ruleCfg := DefaultRuleConfig()

	engine := NewEvidenceEngine(cfg,
		NewSpreadExplosionRule(ruleCfg),
		NewLiquidityThinningRule(ruleCfg),
		NewPersistentImbalanceRule(ruleCfg),
		NewAbsorptionRule(ruleCfg),
	)

	// Feed a book event — should be dispatched to all book-consuming rules
	ev := domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Instrument: "BTC-USDT",
		TsServer: 1000, Seq: 1,
		BestBid: 50000, BestAsk: 50002,
		BidDepth: 1000, AskDepth: 1000,
	}
	events := engine.OnEvent(ev)
	// No emission expected on first event, but no panic
	_ = events

	stats := engine.Stats()
	if stats.TotalStreams != 1 {
		t.Errorf("TotalStreams = %d, want 1", stats.TotalStreams)
	}
	if len(stats.RuleStreams) != 4 {
		t.Errorf("RuleStreams count = %d, want 4", len(stats.RuleStreams))
	}
}

func TestEngineGlobalCapEviction(t *testing.T) {
	now := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
	cfg := testEngineConfig(func() time.Time { return now })
	cfg.MaxStreamsGlobal = 3
	cfg.SweepInterval = 1 * time.Hour // disable periodic sweep

	engine := NewEvidenceEngine(cfg, NewSpreadExplosionRule(DefaultRuleConfig()))

	// Add 5 streams — should evict down to cap
	for i := range 5 {
		engine.OnEvent(domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance",
			Instrument: "SYM-" + string(rune('A'+i)),
			TsServer:   int64(i) * 1000, Seq: int64(i),
			BestBid: 50000, BestAsk: 50002,
		})
	}

	stats := engine.Stats()
	if stats.TotalStreams > cfg.MaxStreamsGlobal {
		t.Errorf("TotalStreams = %d, want <= %d", stats.TotalStreams, cfg.MaxStreamsGlobal)
	}
}

func TestEngineTTLSweep(t *testing.T) {
	now := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	cfg := testEngineConfig(clock)
	cfg.StreamTTL = 2 * time.Minute
	cfg.SweepInterval = 1 * time.Minute

	engine := NewEvidenceEngine(cfg, NewSpreadExplosionRule(DefaultRuleConfig()))

	// Add a stream
	engine.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Instrument: "BTC-USDT",
		TsServer: 1000, Seq: 1, BestBid: 50000, BestAsk: 50002,
	})

	if engine.Stats().TotalStreams != 1 {
		t.Fatal("expected 1 stream")
	}

	// Advance time past TTL + sweep interval
	now = now.Add(3 * time.Minute)
	engine.cfg.Now = func() time.Time { return now }

	// Trigger sweep via another event on a different stream
	engine.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "coinbase", Instrument: "ETH-USD",
		TsServer: 200000, Seq: 2, BestBid: 3000, BestAsk: 3001,
	})

	stats := engine.Stats()
	if stats.TotalStreams != 1 {
		t.Errorf("TotalStreams after sweep = %d, want 1 (old stream evicted)", stats.TotalStreams)
	}
	if stats.TotalEvicted < 1 {
		t.Errorf("TotalEvicted = %d, want >= 1", stats.TotalEvicted)
	}
}

func TestEngineDeterminism(t *testing.T) {
	makeCfg := func() EngineConfig {
		now := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
		return testEngineConfig(func() time.Time { return now })
	}
	makeRules := func() []domain.EvidenceRule {
		rc := DefaultRuleConfig()
		return []domain.EvidenceRule{
			NewSpreadExplosionRule(rc),
			NewLiquidityThinningRule(rc),
		}
	}

	// Build a deterministic event sequence
	events := []domain.RuleEvent{}
	for i := range 20 {
		events = append(events, domain.RuleEvent{
			Kind: domain.EventKindBook, Venue: "binance", Instrument: "BTC-USDT",
			TsServer: int64(i) * 1000, Seq: int64(i),
			BestBid: 50000, BestAsk: 50002, BidDepth: 1000, AskDepth: 1000,
		})
	}
	// Add a spike
	events = append(events, domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Instrument: "BTC-USDT",
		TsServer: 25000, Seq: 25,
		BestBid: 50000, BestAsk: 50100, BidDepth: 200, AskDepth: 200,
	})

	// Run twice
	run := func() []domain.EvidenceEvent {
		engine := NewEvidenceEngine(makeCfg(), makeRules()...)
		var all []domain.EvidenceEvent
		for _, ev := range events {
			all = append(all, engine.OnEvent(ev)...)
		}
		return all
	}

	result1 := run()
	result2 := run()

	if len(result1) != len(result2) {
		t.Fatalf("determinism: run1 emitted %d, run2 emitted %d", len(result1), len(result2))
	}
	for i := range result1 {
		if result1[i].Kind != result2[i].Kind || result1[i].SeqTrigger != result2[i].SeqTrigger {
			t.Errorf("determinism mismatch at index %d", i)
		}
	}
}

func TestEngineStats(t *testing.T) {
	now := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
	cfg := testEngineConfig(func() time.Time { return now })
	engine := NewEvidenceEngine(cfg,
		NewSpreadExplosionRule(DefaultRuleConfig()),
		NewAbsorptionRule(DefaultRuleConfig()),
	)

	engine.OnEvent(domain.RuleEvent{
		Kind: domain.EventKindBook, Venue: "binance", Instrument: "BTC-USDT",
		TsServer: 1000, Seq: 1, BestBid: 50000, BestAsk: 50002,
		BidDepth: 1000, AskDepth: 1000,
	})

	stats := engine.Stats()
	if stats.TotalStreams != 1 {
		t.Errorf("TotalStreams = %d, want 1", stats.TotalStreams)
	}
	if _, ok := stats.RuleStreams["spread_explosion"]; !ok {
		t.Error("expected spread_explosion in RuleStreams")
	}
	if _, ok := stats.RuleStreams["absorption"]; !ok {
		t.Error("expected absorption in RuleStreams")
	}
}
