package contracts_test

import (
	"testing"

	signalsdomain "github.com/market-raccoon/internal/core/signals/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
)

func TestRegisterSignalsPayloadV1(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterSignalsPayloadV1(reg); p != nil {
		t.Fatalf("RegisterSignalsPayloadV1 failed: %s", p.Message)
	}

	jsonKey := codec.SchemaKey{
		Type:    signalsdomain.CompositeSignalType,
		Version: 1,
		Format:  codec.FormatJSON,
	}
	if _, ok := reg.Encoder(jsonKey); !ok {
		t.Fatal("missing signal JSON encoder")
	}
	if _, ok := reg.Decoder(jsonKey); !ok {
		t.Fatal("missing signal JSON decoder")
	}

	protoKey := codec.SchemaKey{
		Type:    signalsdomain.CompositeSignalType,
		Version: 1,
		Format:  codec.FormatProto,
	}
	if _, ok := reg.Encoder(protoKey); !ok {
		t.Fatal("missing signal proto encoder")
	}
	if _, ok := reg.Decoder(protoKey); !ok {
		t.Fatal("missing signal proto decoder")
	}
}
