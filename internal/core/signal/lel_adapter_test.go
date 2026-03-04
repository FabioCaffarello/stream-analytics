package signal

import (
	"strings"
	"testing"

	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
)

func TestLELToEvidenceEvent_TypeMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lelType  evidencedomain.LiquidityEvidenceType
		expected evidencedomain.EvidenceType
	}{
		{name: "BOOK_IMBALANCE", lelType: evidencedomain.LiquidityEvidenceTypeBookImbalance, expected: evidencedomain.PersistentImbalance},
		{name: "ABSORPTION", lelType: evidencedomain.LiquidityEvidenceTypeAbsorption, expected: evidencedomain.Absorption},
		{name: "SWEEP", lelType: evidencedomain.LiquidityEvidenceTypeSweep, expected: evidencedomain.Sweep},
		{name: "THINNING", lelType: evidencedomain.LiquidityEvidenceTypeThinning, expected: evidencedomain.LiquidityThinning},
		{name: "SPREAD_REGIME", lelType: evidencedomain.LiquidityEvidenceTypeSpreadRegime, expected: evidencedomain.SpreadExplosion},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := baseLELFixture()
			in.EvidenceType = tc.lelType

			out, p := LELToEvidenceEvent(in)
			if p != nil {
				t.Fatalf("LELToEvidenceEvent failed: %v", p)
			}
			if got := out.Type; got != tc.expected {
				t.Fatalf("mapped type=%q want=%q", got, tc.expected)
			}
			if out.RuleVersion != "v1" {
				t.Fatalf("rule_version=%q want=v1", out.RuleVersion)
			}
		})
	}
}

func TestLELToEvidenceEvent_EmptyMetricsFallbackFeature(t *testing.T) {
	t.Parallel()

	in := baseLELFixture()
	in.Metrics = nil
	out, p := LELToEvidenceEvent(in)
	if p != nil {
		t.Fatalf("LELToEvidenceEvent failed: %v", p)
	}
	if got := len(out.Features); got != 1 {
		t.Fatalf("features len=%d want=1", got)
	}
	if out.Features[0].Key != "evidence_type" {
		t.Fatalf("fallback feature key=%q want=evidence_type", out.Features[0].Key)
	}
}

func TestLELToEvidenceEvent_MaxMetricsSorted(t *testing.T) {
	t.Parallel()

	in := baseLELFixture()
	in.Metrics = []evidencedomain.LiquidityEvidenceMetric{
		{Key: "z", Value: 1},
		{Key: "m", Value: 1},
		{Key: "x", Value: 1},
		{Key: "c", Value: 1},
		{Key: "b", Value: 1},
		{Key: "n", Value: 1},
		{Key: "a", Value: 1},
		{Key: "y", Value: 1},
	}

	out, p := LELToEvidenceEvent(in)
	if p != nil {
		t.Fatalf("LELToEvidenceEvent failed: %v", p)
	}
	if got := len(out.Features); got != 8 {
		t.Fatalf("features len=%d want=8", got)
	}
	for i := 1; i < len(out.Features); i++ {
		if out.Features[i-1].Key >= out.Features[i].Key {
			t.Fatalf("features are not sorted: %+v", out.Features)
		}
	}
}

func TestLELToEvidenceEvent_ExplainJoinWithSemicolon(t *testing.T) {
	t.Parallel()

	in := baseLELFixture()
	in.Explain = []string{"pressure rising", "thin asks", "sweep detected"}

	out, p := LELToEvidenceEvent(in)
	if p != nil {
		t.Fatalf("LELToEvidenceEvent failed: %v", p)
	}
	if got, want := out.Explanation, "pressure rising; thin asks; sweep detected"; got != want {
		t.Fatalf("explanation=%q want=%q", got, want)
	}
}

func TestLELToEvidenceEvent_ConfidencePassthrough(t *testing.T) {
	t.Parallel()

	in := baseLELFixture()
	in.Confidence = 0.42

	out, p := LELToEvidenceEvent(in)
	if p != nil {
		t.Fatalf("LELToEvidenceEvent failed: %v", p)
	}
	if got, want := out.Confidence, in.Confidence; got != want {
		t.Fatalf("confidence=%f want=%f", got, want)
	}
}

func TestLELToEvidenceEvent_UnmappedTypeReturnsProblem(t *testing.T) {
	t.Parallel()

	in := baseLELFixture()
	in.EvidenceType = evidencedomain.LiquidityEvidenceType("UNKNOWN")

	_, p := LELToEvidenceEvent(in)
	if p == nil {
		t.Fatal("expected problem for unmapped type")
	}
	if !strings.Contains(strings.ToLower(p.Message), "not mapped") {
		t.Fatalf("unexpected problem message: %q", p.Message)
	}
}

func baseLELFixture() evidencedomain.LiquidityEvidence {
	return evidencedomain.LiquidityEvidence{
		EvidenceType: evidencedomain.LiquidityEvidenceTypeSweep,
		TsIngestMs:   1710000000123,
		Venue:        "binance",
		Symbol:       "BTC-USDT",
		WindowMs:     3000,
		Severity:     evidencedomain.LiquidityEvidenceSeverityHigh,
		Confidence:   0.91,
		Metrics: []evidencedomain.LiquidityEvidenceMetric{
			{Key: "pressure", Value: 2.5},
		},
		Explain:  []string{"sweep detected"},
		Version:  evidencedomain.LiquidityEvidenceVersion,
		StreamID: "BINANCE|BTCUSDT",
		Seq:      42,
		Watermark: evidencedomain.LiquidityInputWatermark{
			SeqStart: 40,
			SeqEnd:   42,
		},
	}
}
