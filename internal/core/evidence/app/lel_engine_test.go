package app

import (
	"encoding/json"
	"testing"

	"github.com/market-raccoon/internal/core/evidence/domain"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type fixedLELRule struct{}

func (fixedLELRule) Name() string { return "fixed_lel" }
func (fixedLELRule) StreamCount() int {
	return 0
}
func (fixedLELRule) Reset()               {}
func (fixedLELRule) EvictStream(_ string) {}
func (fixedLELRule) OnEvent(ev domain.LELEvent) []domain.LiquidityEvidence {
	return []domain.LiquidityEvidence{{
		EvidenceType: domain.LiquidityEvidenceTypeSweep,
		TsIngestMs:   ev.TsServer,
		Venue:        ev.Venue,
		Symbol:       ev.Symbol,
		WindowMs:     1,
		Severity:     domain.LiquidityEvidenceSeverityMedium,
		Confidence:   1,
		Metrics:      []domain.LiquidityEvidenceMetric{{Key: "x", Value: 1}},
		Explain:      []string{"fixed"},
		Version:      domain.LiquidityEvidenceVersion,
		StreamID:     ev.StreamKey(),
		Seq:          ev.Seq,
		Watermark: domain.LiquidityInputWatermark{
			SeqStart: ev.Seq,
			SeqEnd:   ev.Seq,
		},
	}}
}

func TestLELEngineRejectsNonMonotonicSeq(t *testing.T) {
	engine := NewLELEngine(LELEngineConfig{
		MaxStreamsGlobal: 8,
		StreamTTLMillis:  60_000,
	}, fixedLELRule{})

	base := testutil.ToFloat64(metrics.LELEvidenceDroppedTotal.WithLabelValues("non_monotonic_seq"))
	event := domain.LELEvent{
		Kind:     domain.LELEventKindSnapshot,
		Venue:    "binance",
		Symbol:   "btc-usdt",
		TsServer: 1000,
		Seq:      10,
		BidDepth: 1,
		AskDepth: 1,
	}
	if got := len(engine.OnEvent(event)); got != 1 {
		t.Fatalf("first emissions=%d want=1", got)
	}
	if got := len(engine.OnEvent(event)); got != 0 {
		t.Fatalf("second emissions=%d want=0", got)
	}
	after := testutil.ToFloat64(metrics.LELEvidenceDroppedTotal.WithLabelValues("non_monotonic_seq"))
	if after <= base {
		t.Fatalf("lel_evidence_dropped_total did not increment, before=%f after=%f", base, after)
	}
}

func TestLELEngineDeterminismByteIdentical(t *testing.T) {
	run := func() [][]byte {
		engine := NewLELEngine(DefaultLELEngineConfig(), NewLELBookImbalanceRule(DefaultRuleConfig()))
		encoded := make([][]byte, 0, 8)
		for i := 1; i <= 12; i++ {
			out := engine.OnEvent(domain.LELEvent{
				Kind:      domain.LELEventKindSnapshot,
				Venue:     "binance",
				Symbol:    "BTC-USDT",
				TsServer:  int64(i) * 1000,
				Seq:       int64(i),
				BidDepth:  900,
				AskDepth:  100,
				BidLevels: 20,
				AskLevels: 20,
			})
			for j := range out {
				raw, err := json.Marshal(out[j])
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				encoded = append(encoded, raw)
			}
		}
		return encoded
	}
	a := run()
	b := run()
	if len(a) != len(b) {
		t.Fatalf("len mismatch %d vs %d", len(a), len(b))
	}
	for i := range a {
		if string(a[i]) != string(b[i]) {
			t.Fatalf("event[%d] mismatch\n%s\n%s", i, string(a[i]), string(b[i]))
		}
	}
}
