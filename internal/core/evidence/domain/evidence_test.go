package domain_test

import (
	"math"
	"testing"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

func validEvent() domain.EvidenceEvent {
	return domain.EvidenceEvent{
		Kind:        domain.SpreadExplosion,
		TsServer:    1709500000000,
		Venue:       "binance",
		Symbol:      "BTC-USDT",
		Severity:    domain.SeverityMedium,
		Confidence:  0.85,
		Features:    []string{"spread_bps"},
		FeatureVals: []float64{45.2},
		Reason:      "spread z-score exceeded threshold",
		SeqTrigger:  42,
	}
}

func TestEvidenceEventValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*domain.EvidenceEvent)
		wantErr bool
	}{
		{
			name:    "valid event",
			modify:  func(_ *domain.EvidenceEvent) {},
			wantErr: false,
		},
		{
			name:    "empty kind",
			modify:  func(e *domain.EvidenceEvent) { e.Kind = "" },
			wantErr: true,
		},
		{
			name:    "unknown kind",
			modify:  func(e *domain.EvidenceEvent) { e.Kind = "unknown" },
			wantErr: true,
		},
		{
			name:    "empty severity",
			modify:  func(e *domain.EvidenceEvent) { e.Severity = "" },
			wantErr: true,
		},
		{
			name:    "unknown severity",
			modify:  func(e *domain.EvidenceEvent) { e.Severity = "extreme" },
			wantErr: true,
		},
		{
			name:    "zero ts_server",
			modify:  func(e *domain.EvidenceEvent) { e.TsServer = 0 },
			wantErr: true,
		},
		{
			name:    "negative ts_server",
			modify:  func(e *domain.EvidenceEvent) { e.TsServer = -1 },
			wantErr: true,
		},
		{
			name:    "empty venue",
			modify:  func(e *domain.EvidenceEvent) { e.Venue = "" },
			wantErr: true,
		},
		{
			name:    "whitespace venue",
			modify:  func(e *domain.EvidenceEvent) { e.Venue = "  " },
			wantErr: true,
		},
		{
			name:    "empty symbol",
			modify:  func(e *domain.EvidenceEvent) { e.Symbol = "" },
			wantErr: true,
		},
		{
			name:    "confidence below zero",
			modify:  func(e *domain.EvidenceEvent) { e.Confidence = -0.1 },
			wantErr: true,
		},
		{
			name:    "confidence above one",
			modify:  func(e *domain.EvidenceEvent) { e.Confidence = 1.1 },
			wantErr: true,
		},
		{
			name:    "confidence NaN",
			modify:  func(e *domain.EvidenceEvent) { e.Confidence = math.NaN() },
			wantErr: true,
		},
		{
			name:    "empty features",
			modify:  func(e *domain.EvidenceEvent) { e.Features = nil; e.FeatureVals = nil },
			wantErr: true,
		},
		{
			name: "mismatched parallel arrays",
			modify: func(e *domain.EvidenceEvent) {
				e.Features = []string{"a", "b"}
				e.FeatureVals = []float64{1.0}
			},
			wantErr: true,
		},
		{
			name: "NaN feature value",
			modify: func(e *domain.EvidenceEvent) {
				e.FeatureVals = []float64{math.NaN()}
			},
			wantErr: true,
		},
		{
			name: "Inf feature value",
			modify: func(e *domain.EvidenceEvent) {
				e.FeatureVals = []float64{math.Inf(1)}
			},
			wantErr: true,
		},
		{
			name:    "empty reason",
			modify:  func(e *domain.EvidenceEvent) { e.Reason = "" },
			wantErr: true,
		},
		{
			name:    "whitespace reason",
			modify:  func(e *domain.EvidenceEvent) { e.Reason = "  " },
			wantErr: true,
		},
		{
			name:    "confidence exactly zero",
			modify:  func(e *domain.EvidenceEvent) { e.Confidence = 0 },
			wantErr: false,
		},
		{
			name:    "confidence exactly one",
			modify:  func(e *domain.EvidenceEvent) { e.Confidence = 1.0 },
			wantErr: false,
		},
		{
			name: "all valid kinds",
			modify: func(e *domain.EvidenceEvent) {
				// test each kind individually
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := validEvent()
			tt.modify(&ev)
			p := ev.Validate()
			if tt.wantErr && p == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.wantErr && p != nil {
				t.Errorf("expected no error, got: %s", p.Message)
			}
		})
	}
}

func TestEvidenceEventValidateAllKinds(t *testing.T) {
	kinds := []domain.EvidenceKind{
		domain.SpreadExplosion,
		domain.LiquidityThinning,
		domain.PersistentImbalance,
		domain.Absorption,
		domain.Sweep,
	}
	for _, k := range kinds {
		t.Run(string(k), func(t *testing.T) {
			ev := validEvent()
			ev.Kind = k
			if p := ev.Validate(); p != nil {
				t.Errorf("kind %s should be valid, got: %s", k, p.Message)
			}
		})
	}
}

func TestRuleEventStreamKey(t *testing.T) {
	re := domain.RuleEvent{Venue: "binance", Instrument: "BTC-USDT"}
	got := re.StreamKey()
	want := "binance|BTC-USDT"
	if got != want {
		t.Errorf("StreamKey() = %q, want %q", got, want)
	}
}
