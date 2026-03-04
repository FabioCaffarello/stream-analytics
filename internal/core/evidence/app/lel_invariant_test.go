package app

import (
	"math"
	"testing"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

type fixedLiquidityRule struct {
	ev domain.LiquidityEvidence
}

func (r fixedLiquidityRule) Name() string { return "fixed_liquidity_rule" }
func (r fixedLiquidityRule) OnEvent(_ domain.LELEvent) []domain.LiquidityEvidence {
	return []domain.LiquidityEvidence{r.ev}
}
func (r fixedLiquidityRule) StreamCount() int     { return 0 }
func (r fixedLiquidityRule) Reset()               {}
func (r fixedLiquidityRule) EvictStream(_ string) {}

func TestLELInvariants_InvalidEvidenceDropped(t *testing.T) {
	base := domain.LiquidityEvidence{
		EvidenceType: domain.LiquidityEvidenceTypeSweep,
		TsIngestMs:   1000,
		Venue:        "BINANCE",
		Symbol:       "BTCUSDT",
		WindowMs:     1,
		Severity:     domain.LiquidityEvidenceSeverityMedium,
		Confidence:   0.8,
		Metrics:      []domain.LiquidityEvidenceMetric{{Key: "a", Value: 1}},
		Explain:      []string{"ok"},
		Version:      domain.LiquidityEvidenceVersion,
		StreamID:     "BINANCE|BTCUSDT",
		Seq:          1,
		Watermark:    domain.LiquidityInputWatermark{SeqStart: 1, SeqEnd: 1},
	}
	tests := []struct {
		name   string
		modify func(*domain.LiquidityEvidence)
	}{
		{name: "nan confidence", modify: func(e *domain.LiquidityEvidence) { e.Confidence = math.NaN() }},
		{name: "negative confidence", modify: func(e *domain.LiquidityEvidence) { e.Confidence = -0.1 }},
		{name: "empty explain", modify: func(e *domain.LiquidityEvidence) { e.Explain = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := base
			tt.modify(&ev)
			engine := NewLELEngine(DefaultLELEngineConfig(), fixedLiquidityRule{ev: ev})
			out := engine.OnEvent(domain.LELEvent{
				Kind:     domain.LELEventKindSnapshot,
				Venue:    "binance",
				Symbol:   "BTC-USDT",
				TsServer: 1000,
				Seq:      1,
				BidDepth: 10,
				AskDepth: 10,
			})
			if len(out) != 0 {
				t.Fatalf("invalid evidence should be dropped, got=%d", len(out))
			}
		})
	}
}

func TestLELInvariants_FeaturesSortedByEngine(t *testing.T) {
	engine := NewLELEngine(DefaultLELEngineConfig(), fixedLiquidityRule{ev: domain.LiquidityEvidence{
		EvidenceType: domain.LiquidityEvidenceTypeSweep,
		TsIngestMs:   1000,
		Venue:        "BINANCE",
		Symbol:       "BTCUSDT",
		WindowMs:     1,
		Severity:     domain.LiquidityEvidenceSeverityMedium,
		Confidence:   0.8,
		Metrics: []domain.LiquidityEvidenceMetric{
			{Key: "z", Value: 3},
			{Key: "a", Value: 1},
			{Key: "a", Value: 2},
		},
		Explain:   []string{"ok"},
		Version:   domain.LiquidityEvidenceVersion,
		StreamID:  "BINANCE|BTCUSDT",
		Seq:       1,
		Watermark: domain.LiquidityInputWatermark{SeqStart: 1, SeqEnd: 1},
	}})
	out := engine.OnEvent(domain.LELEvent{
		Kind:     domain.LELEventKindSnapshot,
		Venue:    "binance",
		Symbol:   "BTC-USDT",
		TsServer: 1000,
		Seq:      1,
		BidDepth: 10,
		AskDepth: 10,
	})
	if len(out) != 1 {
		t.Fatalf("expected one emission, got=%d", len(out))
	}
	if got := len(out[0].Metrics); got != 2 {
		t.Fatalf("dedup metrics len=%d want=2", got)
	}
	if out[0].Metrics[0].Key != "a" || out[0].Metrics[1].Key != "z" {
		t.Fatalf("metrics not sorted: %+v", out[0].Metrics)
	}
}

func TestLELInvariants_AdversarialFixtureFiresAllRules(t *testing.T) {
	cfg := DefaultRuleConfig()
	cfg.CooldownMs = 0
	engine := NewLELEngine(DefaultLELEngineConfig(),
		NewLELBookImbalanceRule(cfg),
		NewLELAbsorptionRule(cfg),
		NewLELSweepRule(cfg),
		NewLELThinningRule(cfg),
		NewLELSpreadRegimeRule(cfg),
	)

	types := map[domain.LiquidityEvidenceType]bool{}
	push := func(events ...domain.LELEvent) {
		for i := range events {
			out := engine.OnEvent(events[i])
			for j := range out {
				types[out[j].EvidenceType] = true
			}
		}
	}

	// BOOK_IMBALANCE + THINNING + SPREAD_REGIME seed and trigger.
	for i := 1; i <= 25; i++ {
		bidDepth, askDepth := 900.0, 100.0
		spread := 4.0
		if i == 25 {
			spread = 120
		}
		push(domain.LELEvent{
			Kind:      domain.LELEventKindSnapshot,
			Venue:     "binance",
			Symbol:    "BTC-USDT",
			TsServer:  int64(i) * 1000,
			Seq:       int64(i),
			BestBid:   100,
			BestAsk:   100.04,
			SpreadBPS: spread,
			BidDepth:  bidDepth,
			AskDepth:  askDepth,
			BidLevels: 20,
			AskLevels: 20,
		})
	}
	// SWEEP trigger.
	push(
		domain.LELEvent{Kind: domain.LELEventKindSnapshot, Venue: "binance", Symbol: "ETH-USDT", TsServer: 20_000, Seq: 1, BidLevels: 20, AskLevels: 20, BidDepth: 1000, AskDepth: 1000},
		domain.LELEvent{Kind: domain.LELEventKindSnapshot, Venue: "binance", Symbol: "ETH-USDT", TsServer: 21_000, Seq: 2, BidLevels: 12, AskLevels: 20, BidDepth: 300, AskDepth: 1000},
	)
	// ABSORPTION trigger.
	push(domain.LELEvent{Kind: domain.LELEventKindSnapshot, Venue: "binance", Symbol: "SOL-USDT", TsServer: 30_000, Seq: 1, SpreadBPS: 5})
	for i := 2; i <= 14; i++ {
		vol := 10.0
		if i >= 12 {
			vol = 120.0
		}
		push(domain.LELEvent{
			Kind:          domain.LELEventKindTape,
			Venue:         "binance",
			Symbol:        "SOL-USDT",
			TsServer:      int64(30+i) * 1000,
			Seq:           int64(i),
			TotalVolume:   vol,
			WindowStartTs: int64(30+i-1) * 1000,
			WindowEndTs:   int64(30+i) * 1000,
		})
	}
	// THINNING explicit trigger on separate stream.
	for i := 1; i <= 10; i++ {
		push(domain.LELEvent{Kind: domain.LELEventKindSnapshot, Venue: "binance", Symbol: "XRP-USDT", TsServer: int64(60+i) * 1000, Seq: int64(i), BidDepth: 500, AskDepth: 500})
	}
	push(domain.LELEvent{Kind: domain.LELEventKindSnapshot, Venue: "binance", Symbol: "XRP-USDT", TsServer: 72_000, Seq: 11, BidDepth: 100, AskDepth: 100})

	for _, typ := range []domain.LiquidityEvidenceType{
		domain.LiquidityEvidenceTypeBookImbalance,
		domain.LiquidityEvidenceTypeAbsorption,
		domain.LiquidityEvidenceTypeSweep,
		domain.LiquidityEvidenceTypeThinning,
		domain.LiquidityEvidenceTypeSpreadRegime,
	} {
		if !types[typ] {
			t.Fatalf("expected evidence type %s at least once", typ)
		}
	}
}

func TestLELInvariants_FlatFixtureHasZeroEvidence(t *testing.T) {
	engine := NewLELEngine(DefaultLELEngineConfig(),
		NewLELBookImbalanceRule(DefaultRuleConfig()),
		NewLELAbsorptionRule(DefaultRuleConfig()),
		NewLELSweepRule(DefaultRuleConfig()),
		NewLELThinningRule(DefaultRuleConfig()),
		NewLELSpreadRegimeRule(DefaultRuleConfig()),
	)
	total := 0
	for i := 1; i <= 120; i++ {
		ev := domain.LELEvent{
			Kind:      domain.LELEventKindSnapshot,
			Venue:     "binance",
			Symbol:    "BTC-USDT",
			TsServer:  int64(i) * 1000,
			Seq:       int64(i),
			BestBid:   100,
			BestAsk:   100.01,
			SpreadBPS: 1,
			BidDepth:  500,
			AskDepth:  500,
			BidLevels: 20,
			AskLevels: 20,
		}
		if i%2 == 1 {
			ev.Kind = domain.LELEventKindTape
			ev.TotalVolume = 5
			ev.WindowStartTs = ev.TsServer - 1000
			ev.WindowEndTs = ev.TsServer
		}
		total += len(engine.OnEvent(ev))
	}
	if total != 0 {
		t.Fatalf("flat fixture emitted %d evidences, want 0", total)
	}
}

func TestLELInvariants_CooldownEnforced(t *testing.T) {
	rule := NewLELSweepRule(DefaultRuleConfig())
	stream := "BINANCE|BTCUSDT"
	_ = rule.OnEvent(domain.LELEvent{
		Kind:      domain.LELEventKindSnapshot,
		Venue:     "binance",
		Symbol:    "BTC-USDT",
		StreamID:  stream,
		TsServer:  1000,
		Seq:       1,
		BidLevels: 20,
		AskLevels: 20,
		BidDepth:  1000,
		AskDepth:  1000,
	})
	emissions := 0
	for i := 2; i <= 6; i++ {
		out := rule.OnEvent(domain.LELEvent{
			Kind:      domain.LELEventKindSnapshot,
			Venue:     "binance",
			Symbol:    "BTC-USDT",
			StreamID:  stream,
			TsServer:  int64(i) * 1000,
			Seq:       int64(i),
			BidLevels: 12,
			AskLevels: 20,
			BidDepth:  300,
			AskDepth:  1000,
		})
		emissions += len(out)
	}
	if emissions > 1 {
		t.Fatalf("cooldown violation: emissions=%d want<=1 in 5s window", emissions)
	}
}
