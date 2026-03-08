package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestValidateEnvelopeForDelivery_AllowsAggregationSnapshot(t *testing.T) {
	p := domain.ValidateEnvelopeForDelivery(envelope.Envelope{
		Type:    "aggregation.snapshot",
		Version: 1,
	})
	if p != nil {
		t.Fatalf("ValidateEnvelopeForDelivery failed: %v", p)
	}
}

func TestValidateEnvelopeForDelivery_AllowsAggregationCandleAndStats(t *testing.T) {
	for _, eventType := range []string{
		"marketdata.open_interest",
		"aggregation.candle",
		"aggregation.stats",
		"aggregation.tape",
		"aggregation.oi",
		"aggregation.delta_volume",
		"aggregation.cvd",
		"aggregation.bar_stats",
		"aggregation.orderbook_inconsistency",
		"insights.heatmap_snapshot",
		"insights.heatmap_delta",
		"insights.volume_profile_snapshot",
		"insights.volume_profile_delta",
		"insights.session_volume_profile",
		"insights.tpo_profile",
		"insights.fused_volume_profile_snapshot",
		"insights.fused_heatmap_snapshot",
		"liquidity.evidence",
		"signal.event",
		"signal.composite",
		"strategy.intent",
		"execution.event",
		"portfolio.state",
	} {
		p := domain.ValidateEnvelopeForDelivery(envelope.Envelope{
			Type:    eventType,
			Version: 1,
		})
		if p != nil {
			t.Fatalf("ValidateEnvelopeForDelivery(%q) failed: %v", eventType, p)
		}
	}
}

func TestValidateEnvelopeForDelivery_RejectsUnknownType(t *testing.T) {
	p := domain.ValidateEnvelopeForDelivery(envelope.Envelope{
		Type:    "insights.unknown",
		Version: 1,
	})
	if p == nil {
		t.Fatal("expected validation failure for unknown type")
	}
}

func TestValidateEnvelopeForDelivery_RejectsWrongVersion(t *testing.T) {
	p := domain.ValidateEnvelopeForDelivery(envelope.Envelope{
		Type:    "aggregation.snapshot",
		Version: 2,
	})
	if p == nil {
		t.Fatal("expected validation failure for wrong version")
	}
}
