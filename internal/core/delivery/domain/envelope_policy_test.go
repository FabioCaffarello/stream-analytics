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
