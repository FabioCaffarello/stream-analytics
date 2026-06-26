package contracts_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/contracts"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
)

func TestRegisterMarketDataV1_RegistersAll(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterMarketDataV1(reg); p != nil {
		t.Fatalf("RegisterMarketDataV1: %v", p)
	}

	eventTypes := []string{
		"marketdata.trade",
		"marketdata.bookdelta",
		"marketdata.markprice",
		"marketdata.liquidation",
		"marketdata.open_interest",
	}
	formats := []codec.Format{codec.FormatJSON, codec.FormatProto}

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
}

func TestRegisterMarketDataPayloadV1_RegistersProtoForCoreSubjects(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterMarketDataPayloadV1(reg); p != nil {
		t.Fatalf("RegisterMarketDataPayloadV1: %v", p)
	}

	coreTypes := []string{
		"marketdata.trade",
		"marketdata.bookdelta",
		"marketdata.markprice",
		"marketdata.open_interest",
	}
	for _, eventType := range coreTypes {
		for _, format := range []codec.Format{codec.FormatJSON, codec.FormatProto} {
			key := codec.SchemaKey{Type: eventType, Version: 1, Format: format}
			if _, ok := reg.Encoder(key); !ok {
				t.Fatalf("missing encoder for key %+v", key)
			}
			if _, ok := reg.Decoder(key); !ok {
				t.Fatalf("missing decoder for key %+v", key)
			}
		}
	}
}
