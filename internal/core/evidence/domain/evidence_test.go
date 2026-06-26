package domain_test

import (
	"math"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

func validEvent() domain.EvidenceEvent {
	return domain.EvidenceEvent{
		Type:        domain.SpreadExplosion,
		TsServer:    1709500000000,
		Venue:       "binance",
		Symbol:      "BTC-USDT",
		StreamID:    "binance/BTC-USDT/book_delta",
		Seq:         42,
		Severity:    domain.SeverityMedium,
		Confidence:  0.85,
		Features:    []domain.EvidenceFeature{{Key: "spread_bps", Value: 45.2}},
		Explanation: "spread z-score exceeded threshold",
		RuleVersion: domain.RuleVersionV0,
		InputWatermark: domain.InputWatermark{
			SeqStart: 33,
			SeqEnd:   42,
		},
	}
}

func TestEvidenceEventValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*domain.EvidenceEvent)
		wantErr bool
	}{
		{name: "valid", modify: func(_ *domain.EvidenceEvent) {}, wantErr: false},
		{name: "empty type", modify: func(e *domain.EvidenceEvent) { e.Type = "" }, wantErr: true},
		{name: "unknown type", modify: func(e *domain.EvidenceEvent) { e.Type = "unknown" }, wantErr: true},
		{name: "empty severity", modify: func(e *domain.EvidenceEvent) { e.Severity = "" }, wantErr: true},
		{name: "zero ts_server", modify: func(e *domain.EvidenceEvent) { e.TsServer = 0 }, wantErr: true},
		{name: "empty stream id", modify: func(e *domain.EvidenceEvent) { e.StreamID = "" }, wantErr: true},
		{name: "invalid seq", modify: func(e *domain.EvidenceEvent) { e.Seq = 0 }, wantErr: true},
		{name: "confidence NaN", modify: func(e *domain.EvidenceEvent) { e.Confidence = math.NaN() }, wantErr: true},
		{name: "empty features", modify: func(e *domain.EvidenceEvent) { e.Features = nil }, wantErr: true},
		{name: "empty feature key", modify: func(e *domain.EvidenceEvent) { e.Features = []domain.EvidenceFeature{{Key: "", Value: 1}} }, wantErr: true},
		{name: "unsorted features", modify: func(e *domain.EvidenceEvent) {
			e.Features = []domain.EvidenceFeature{{Key: "z", Value: 1}, {Key: "a", Value: 2}}
		}, wantErr: true},
		{name: "empty explanation", modify: func(e *domain.EvidenceEvent) { e.Explanation = "" }, wantErr: true},
		{name: "empty rule version", modify: func(e *domain.EvidenceEvent) { e.RuleVersion = "" }, wantErr: true},
		{name: "invalid watermark", modify: func(e *domain.EvidenceEvent) {
			e.InputWatermark.SeqStart = 100
			e.InputWatermark.SeqEnd = 99
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := validEvent()
			tt.modify(&ev)
			p := ev.Validate()
			if tt.wantErr && p == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && p != nil {
				t.Fatalf("unexpected error: %s", p.Message)
			}
		})
	}
}

func TestEvidenceEventValidateAllTypes(t *testing.T) {
	types := []domain.EvidenceType{
		domain.SpreadExplosion,
		domain.LiquidityThinning,
		domain.PersistentImbalance,
		domain.Absorption,
		domain.Sweep,
	}
	for _, typ := range types {
		t.Run(string(typ), func(t *testing.T) {
			ev := validEvent()
			ev.Type = typ
			if p := ev.Validate(); p != nil {
				t.Fatalf("type %s should be valid: %s", typ, p.Message)
			}
		})
	}
}

func TestRuleEventStreamKey(t *testing.T) {
	re := domain.RuleEvent{Venue: "binance", Symbol: "BTC-USDT"}
	if got, want := re.StreamKey(), "binance|BTC-USDT"; got != want {
		t.Fatalf("StreamKey()=%q want=%q", got, want)
	}
}
