package contracts_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
)

func TestRegisterAggregationPayloadV1_RegistersJSONCodecs(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterAggregationPayloadV1(reg); p != nil {
		t.Fatalf("RegisterAggregationPayloadV1: %v", p)
	}

	eventTypes := []string{
		"aggregation.candle",
		"aggregation.stats",
	}
	for _, eventType := range eventTypes {
		key := codec.SchemaKey{Type: eventType, Version: 1, Format: codec.FormatJSON}
		if _, ok := reg.Encoder(key); !ok {
			t.Fatalf("missing encoder for key %+v", key)
		}
		if _, ok := reg.Decoder(key); !ok {
			t.Fatalf("missing decoder for key %+v", key)
		}
	}
}
