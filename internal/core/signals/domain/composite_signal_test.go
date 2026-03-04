package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/signals/domain"
)

func TestCompositeSignalValidate(t *testing.T) {
	signal := domain.CompositeSignalV1{
		Kind:       "absorption",
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Timeframe:  "1m",
		TsServer:   1700000000000,
		Severity:   "high",
		Confidence: 0.87,
		Evidence: []domain.SignalFeature{
			{Label: "volume_ratio", Value: "2.100000"},
		},
		RegimeKind:     "trending",
		RegimeStrength: 0.8,
		Reason:         "microstructure evidence with regime context",
		Seq:            42,
		SourceKinds:    []string{"absorption", "trending"},
	}
	if p := signal.Validate(); p != nil {
		t.Fatalf("expected valid signal, got %v", p)
	}
}
