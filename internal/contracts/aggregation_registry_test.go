package contracts_test

import (
	"testing"

	"github.com/market-raccoon/internal/contracts"
	"github.com/market-raccoon/internal/shared/codec"
)

func TestRegisterAggregationPayloadV1_RegistersDualCodecs(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterAggregationPayloadV1(reg); p != nil {
		t.Fatalf("RegisterAggregationPayloadV1: %v", p)
	}

	eventTypes := []string{
		"aggregation.candle",
		"aggregation.stats",
		"aggregation.tape",
		"aggregation.oi",
		"aggregation.cvd",
		"aggregation.delta_volume",
		"aggregation.bar_stats",
		"aggregation.snapshot",
		"aggregation.orderbook_inconsistency",
	}
	formats := []codec.Format{
		codec.FormatJSON,
		codec.FormatProto,
	}
	for _, eventType := range eventTypes {
		for _, format := range formats {
			key := codec.SchemaKey{Type: eventType, Version: 1, Format: format}
			if _, ok := reg.Encoder(key); !ok {
				t.Fatalf("missing encoder for key %+v", key)
			}
			if _, ok := reg.Decoder(key); !ok {
				t.Fatalf("missing decoder for key %+v", key)
			}
		}
	}

	for _, format := range formats {
		key := codec.SchemaKey{Type: "aggregation.snapshot", Version: 2, Format: format}
		if _, ok := reg.Encoder(key); !ok {
			t.Fatalf("missing encoder for key %+v", key)
		}
		if _, ok := reg.Decoder(key); !ok {
			t.Fatalf("missing decoder for key %+v", key)
		}
	}
}
