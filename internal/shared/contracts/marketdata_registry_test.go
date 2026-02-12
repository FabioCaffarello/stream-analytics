package contracts_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
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
