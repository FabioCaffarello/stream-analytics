package domain_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

func TestRegimeSignalValidate(t *testing.T) {
	sig := domain.RegimeSignal{
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Timeframe:   "1m",
		Kind:        domain.RegimeTrending,
		Strength:    0.8,
		Confidence:  0.9,
		WindowStart: 60_000,
		WindowEnd:   120_000,
		Features: []domain.FeaturePair{
			{Name: "slope", Value: 0.5},
		},
	}
	if p := sig.Validate(); p != nil {
		t.Fatalf("valid signal failed validation: %v", p)
	}
}
