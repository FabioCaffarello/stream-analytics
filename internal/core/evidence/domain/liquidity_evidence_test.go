package domain_test

import (
	"math"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

func validLiquidityEvidence() domain.LiquidityEvidence {
	return domain.LiquidityEvidence{
		EvidenceType: domain.LiquidityEvidenceTypeSweep,
		TsIngestMs:   1_700_000_000_000,
		Venue:        "BINANCE",
		Symbol:       "BTCUSDT",
		WindowMs:     1000,
		Severity:     domain.LiquidityEvidenceSeverityHigh,
		Confidence:   0.8,
		Metrics: []domain.LiquidityEvidenceMetric{
			{Key: "a", Value: 1},
			{Key: "b", Value: 2},
		},
		Explain:  []string{"rapid level consumption detected on bid side"},
		Version:  domain.LiquidityEvidenceVersion,
		StreamID: "BINANCE|BTCUSDT",
		Seq:      44,
		Watermark: domain.LiquidityInputWatermark{
			SeqStart: 40,
			SeqEnd:   44,
		},
	}
}

func TestLiquidityEvidenceValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*domain.LiquidityEvidence)
		wantErr bool
	}{
		{name: "valid", modify: func(_ *domain.LiquidityEvidence) {}, wantErr: false},
		{name: "bad type", modify: func(e *domain.LiquidityEvidence) { e.EvidenceType = "UNKNOWN" }, wantErr: true},
		{name: "bad severity", modify: func(e *domain.LiquidityEvidence) { e.Severity = "x" }, wantErr: true},
		{name: "bad confidence nan", modify: func(e *domain.LiquidityEvidence) { e.Confidence = math.NaN() }, wantErr: true},
		{name: "empty explain", modify: func(e *domain.LiquidityEvidence) { e.Explain = nil }, wantErr: true},
		{name: "unsorted metrics", modify: func(e *domain.LiquidityEvidence) {
			e.Metrics = []domain.LiquidityEvidenceMetric{{Key: "z", Value: 1}, {Key: "a", Value: 2}}
		}, wantErr: true},
		{name: "bad watermark", modify: func(e *domain.LiquidityEvidence) {
			e.Watermark.SeqStart = 9
			e.Watermark.SeqEnd = 8
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := validLiquidityEvidence()
			tt.modify(&ev)
			err := ev.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
