package contracts_test

import (
	"testing"

	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
)

func TestRegisterInsightsV1_RegistersJSONCodec(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterInsightsV1(reg); p != nil {
		t.Fatalf("RegisterInsightsV1: %v", p)
	}

	snapshotKey := codec.SchemaKey{
		Type:    insightsdomain.CrossVenueTradeSnapshotType,
		Version: 1,
		Format:  codec.FormatJSON,
	}
	if _, ok := reg.Encoder(snapshotKey); !ok {
		t.Fatalf("missing encoder for key %+v", snapshotKey)
	}
	if _, ok := reg.Decoder(snapshotKey); !ok {
		t.Fatalf("missing decoder for key %+v", snapshotKey)
	}

	signalKey := codec.SchemaKey{
		Type:    insightsdomain.CrossVenueSpreadSignalType,
		Version: 1,
		Format:  codec.FormatJSON,
	}
	if _, ok := reg.Encoder(signalKey); !ok {
		t.Fatalf("missing encoder for key %+v", signalKey)
	}
	if _, ok := reg.Decoder(signalKey); !ok {
		t.Fatalf("missing decoder for key %+v", signalKey)
	}

	snapshotProtoKey := codec.SchemaKey{
		Type:    insightsdomain.CrossVenueTradeSnapshotType,
		Version: 1,
		Format:  codec.FormatProto,
	}
	if _, ok := reg.Encoder(snapshotProtoKey); ok {
		t.Fatalf("unexpected proto encoder for key %+v", snapshotProtoKey)
	}
	if _, ok := reg.Decoder(snapshotProtoKey); ok {
		t.Fatalf("unexpected proto decoder for key %+v", snapshotProtoKey)
	}

	signalProtoKey := codec.SchemaKey{
		Type:    insightsdomain.CrossVenueSpreadSignalType,
		Version: 1,
		Format:  codec.FormatProto,
	}
	if _, ok := reg.Encoder(signalProtoKey); ok {
		t.Fatalf("unexpected proto encoder for key %+v", signalProtoKey)
	}
	if _, ok := reg.Decoder(signalProtoKey); ok {
		t.Fatalf("unexpected proto decoder for key %+v", signalProtoKey)
	}
}
