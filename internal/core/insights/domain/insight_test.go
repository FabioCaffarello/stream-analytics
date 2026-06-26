package domain_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func validInsight() *domain.Insight {
	return &domain.Insight{
		Type:       domain.InsightType("liquidity_shift"),
		Confidence: 0.85,
		Evidence: []domain.Evidence{
			{Label: "spread_change_pct", Value: 12.5},
			{Label: "volume_surge", Value: true},
		},
		Window:                 "5m",
		Venue:                  "binance",
		Instrument:             "BTCUSDT",
		InvalidationConditions: []string{"spread returns to baseline"},
	}
}

func TestInsight_valid(t *testing.T) {
	ins := validInsight()
	if p := ins.Validate(); p != nil {
		t.Errorf("unexpected problem: %s", p)
	}
}

func TestInsight_missingType(t *testing.T) {
	ins := validInsight()
	ins.Type = ""
	p := ins.Validate()
	if p == nil {
		t.Fatal("expected problem for empty type")
	}
	if p.Code != problem.ValidationFailed {
		t.Errorf("code = %s; want VALIDATION_FAILED", p.Code)
	}
}

func TestInsight_confidenceOutOfRange(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
	}{
		{"negative", -0.1},
		{"above 1", 1.01},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ins := validInsight()
			ins.Confidence = domain.Confidence(tc.confidence)
			p := ins.Validate()
			if p == nil {
				t.Fatal("expected problem")
			}
			if p.Code != problem.ValidationFailed {
				t.Errorf("code = %s; want VALIDATION_FAILED", p.Code)
			}
		})
	}
}

func TestInsight_noEvidence(t *testing.T) {
	ins := validInsight()
	ins.Evidence = nil
	p := ins.Validate()
	if p == nil {
		t.Fatal("expected problem for no evidence")
	}
}

func TestInsight_boundaryConfidence(t *testing.T) {
	for _, c := range []float64{0.0, 1.0} {
		ins := validInsight()
		ins.Confidence = domain.Confidence(c)
		if p := ins.Validate(); p != nil {
			t.Errorf("confidence=%.1f should be valid: %s", c, p)
		}
	}
}
