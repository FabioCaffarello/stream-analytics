package app

import (
	"encoding/json"
	"testing"

	"github.com/market-raccoon/internal/core/evidence/domain"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestEngineSeqMonotonicPerStream(t *testing.T) {
	engine := NewEvidenceEngine(EngineConfig{
		MaxStreamsGlobal: 8,
		StreamTTLMillis:  60_000,
	}, fixedEmitRule{})

	baseDrop := testutil.ToFloat64(metrics.EvidenceDroppedTotal.WithLabelValues("non_monotonic_seq"))

	first := engine.OnEvent(domain.RuleEvent{
		Kind:     domain.EventKindBook,
		Venue:    "binance",
		Symbol:   "BTC-USDT",
		StreamID: "binance/BTC-USDT/book_delta",
		TsServer: 1000,
		Seq:      10,
		BestBid:  100,
		BestAsk:  101,
	})
	if len(first) != 1 {
		t.Fatalf("first emissions=%d want=1", len(first))
	}

	second := engine.OnEvent(domain.RuleEvent{
		Kind:     domain.EventKindBook,
		Venue:    "binance",
		Symbol:   "BTC-USDT",
		StreamID: "binance/BTC-USDT/book_delta",
		TsServer: 1100,
		Seq:      10,
		BestBid:  100,
		BestAsk:  101,
	})
	if len(second) != 0 {
		t.Fatalf("second emissions=%d want=0", len(second))
	}
	afterDrop := testutil.ToFloat64(metrics.EvidenceDroppedTotal.WithLabelValues("non_monotonic_seq"))
	if afterDrop <= baseDrop {
		t.Fatalf("evidence_dropped_total not incremented: before=%f after=%f", baseDrop, afterDrop)
	}
}

func TestEngineDeterminismByteIdentical(t *testing.T) {
	run := func() [][]byte {
		engine := NewEvidenceEngine(EngineConfig{
			MaxStreamsGlobal: 16,
			StreamTTLMillis:  600_000,
		},
			NewSpreadExplosionRule(DefaultRuleConfig()),
			NewLiquidityThinningRule(DefaultRuleConfig()),
			NewPersistentImbalanceRule(DefaultRuleConfig()),
		)

		fixtures := make([]domain.RuleEvent, 0, 64)
		for i := 1; i <= 20; i++ {
			fixtures = append(fixtures, domain.RuleEvent{
				Kind:      domain.EventKindBook,
				Venue:     "binance",
				Symbol:    "BTC-USDT",
				StreamID:  "binance/BTC-USDT/book_delta",
				TsServer:  int64(i) * 1000,
				Seq:       int64(i),
				BestBid:   50_000,
				BestAsk:   50_002,
				BidDepth:  1000,
				AskDepth:  1000,
				BidLevels: 20,
				AskLevels: 20,
			})
		}
		fixtures = append(fixtures,
			domain.RuleEvent{
				Kind:      domain.EventKindBook,
				Venue:     "binance",
				Symbol:    "BTC-USDT",
				StreamID:  "binance/BTC-USDT/book_delta",
				TsServer:  21_000,
				Seq:       21,
				BestBid:   50_000,
				BestAsk:   50_200,
				BidDepth:  350,
				AskDepth:  350,
				BidLevels: 8,
				AskLevels: 8,
			},
			domain.RuleEvent{
				Kind:      domain.EventKindBook,
				Venue:     "binance",
				Symbol:    "BTC-USDT",
				StreamID:  "binance/BTC-USDT/book_delta",
				TsServer:  22_000,
				Seq:       22,
				BestBid:   50_000,
				BestAsk:   50_200,
				BidDepth:  900,
				AskDepth:  100,
				BidLevels: 10,
				AskLevels: 3,
			},
		)

		encoded := make([][]byte, 0, 8)
		for i := range fixtures {
			emitted := engine.OnEvent(fixtures[i])
			for j := range emitted {
				raw, err := json.Marshal(emitted[j])
				if err != nil {
					t.Fatalf("marshal evidence: %v", err)
				}
				encoded = append(encoded, raw)
			}
		}
		return encoded
	}

	first := run()
	second := run()
	if len(first) != len(second) {
		t.Fatalf("len(first)=%d len(second)=%d", len(first), len(second))
	}
	for i := range first {
		if string(first[i]) != string(second[i]) {
			t.Fatalf("event bytes mismatch at %d\nfirst=%s\nsecond=%s", i, string(first[i]), string(second[i]))
		}
	}
}

func TestEngineBoundedEviction(t *testing.T) {
	engine := NewEvidenceEngine(EngineConfig{
		MaxStreamsGlobal: 2,
		StreamTTLMillis:  60_000,
	}, fixedEmitRule{})

	before := testutil.ToFloat64(metrics.EvidenceStateEvictedTotal.WithLabelValues("capacity"))

	inputs := []domain.RuleEvent{
		{Kind: domain.EventKindBook, Venue: "binance", Symbol: "BTC-USDT", StreamID: "binance/BTC-USDT/book_delta", TsServer: 1000, Seq: 1, BestBid: 1, BestAsk: 2},
		{Kind: domain.EventKindBook, Venue: "binance", Symbol: "ETH-USDT", StreamID: "binance/ETH-USDT/book_delta", TsServer: 2000, Seq: 1, BestBid: 1, BestAsk: 2},
		{Kind: domain.EventKindBook, Venue: "binance", Symbol: "SOL-USDT", StreamID: "binance/SOL-USDT/book_delta", TsServer: 3000, Seq: 1, BestBid: 1, BestAsk: 2},
	}
	for i := range inputs {
		_ = engine.OnEvent(inputs[i])
	}

	if stats := engine.Stats(); stats.TotalStreams > 2 {
		t.Fatalf("total streams=%d want<=2", stats.TotalStreams)
	}
	after := testutil.ToFloat64(metrics.EvidenceStateEvictedTotal.WithLabelValues("capacity"))
	if after <= before {
		t.Fatalf("capacity evictions not incremented: before=%f after=%f", before, after)
	}
}

type fixedEmitRule struct{}

func (fixedEmitRule) Name() string         { return "fixed_emit" }
func (fixedEmitRule) StreamCount() int     { return 0 }
func (fixedEmitRule) Reset()               {}
func (fixedEmitRule) EvictStream(_ string) {}
func (fixedEmitRule) OnEvent(ev domain.RuleEvent) []domain.EvidenceEvent {
	return []domain.EvidenceEvent{{
		Type:        domain.Sweep,
		TsServer:    ev.TsServer,
		Venue:       ev.Venue,
		Symbol:      ev.Symbol,
		StreamID:    resolveStreamID(ev),
		Seq:         ev.Seq,
		Severity:    domain.SeverityMedium,
		Confidence:  1.0,
		Features:    []domain.EvidenceFeature{{Key: "x", Value: 1}},
		Explanation: "fixed emit",
		RuleVersion: domain.RuleVersionV0,
		InputWatermark: domain.InputWatermark{
			SeqStart: ev.Seq,
			SeqEnd:   ev.Seq,
		},
	}}
}
