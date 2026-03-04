package app

import (
	"math"
	"testing"

	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
)

func TestSignalComposer_SingleEvidence(t *testing.T) {
	composer := NewSignalComposer(DefaultComposePolicy())
	micro := baseMicroEvidence("binance", 1700000000000, 0.8)

	out, ok := composer.Compose(ComposeInput{Micro: micro, Timeframe: "1m"})
	if !ok {
		t.Fatal("expected composed signal")
	}
	if math.Abs(out.Signal.Confidence-0.8) > 1e-12 {
		t.Fatalf("confidence=%0.12f want=0.800000000000", out.Signal.Confidence)
	}
}

func TestSignalComposer_RegimeBoost(t *testing.T) {
	composer := NewSignalComposer(DefaultComposePolicy())
	micro := baseMicroEvidence("binance", 1700000000000, 0.6)
	regime := evidencedomain.RegimeSignal{
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Timeframe:   "1m",
		Kind:        evidencedomain.RegimeTrending,
		Strength:    0.8,
		Confidence:  0.9,
		WindowStart: 1699999940000,
		WindowEnd:   1700000000000,
		Features: []evidencedomain.FeaturePair{
			{Name: "slope_ratio", Value: 0.003},
		},
	}

	out, ok := composer.Compose(ComposeInput{Micro: micro, Regime: &regime})
	if !ok {
		t.Fatal("expected composed signal")
	}
	if !out.RegimeBoosted {
		t.Fatal("expected regime boost flag")
	}
	if math.Abs(out.Signal.Confidence-0.696) > 1e-12 {
		t.Fatalf("confidence=%0.12f want=0.696000000000", out.Signal.Confidence)
	}
}

func TestSignalComposer_CrossVenue(t *testing.T) {
	composer := NewSignalComposer(DefaultComposePolicy())
	first := baseMicroEvidence("binance", 1700000000000, 0.61)
	second := baseMicroEvidence("bybit", 1700000003000, 0.62)

	if _, ok := composer.Compose(ComposeInput{Micro: first, Timeframe: "1m"}); ok {
		t.Fatal("did not expect first signal without cross-venue confirmation")
	}
	out, ok := composer.Compose(ComposeInput{Micro: second, Timeframe: "1m"})
	if !ok {
		t.Fatal("expected second composed signal")
	}
	if !out.CorrelationHit {
		t.Fatal("expected cross-venue correlation hit")
	}
	want := 0.62 * 1.15
	if math.Abs(out.Signal.Confidence-want) > 1e-12 {
		t.Fatalf("confidence=%0.12f want=%0.12f", out.Signal.Confidence, want)
	}
}

func baseMicroEvidence(venue string, ts int64, confidence float64) evidencedomain.EvidenceEvent {
	return evidencedomain.EvidenceEvent{
		Type:       evidencedomain.Absorption,
		TsServer:   ts,
		Venue:      venue,
		Symbol:     "BTCUSDT",
		StreamID:   venue + "/BTCUSDT/trade",
		Seq:        ts / 1000,
		Severity:   evidencedomain.SeverityMedium,
		Confidence: confidence,
		Features: []evidencedomain.EvidenceFeature{
			{Key: "volume_ratio", Value: 2.1},
		},
		Explanation: "absorption detected",
		RuleVersion: evidencedomain.RuleVersionV0,
		InputWatermark: evidencedomain.InputWatermark{
			SeqStart: ts / 1000,
			SeqEnd:   ts / 1000,
		},
	}
}
