package contracts_test

import (
	"testing"

	"github.com/market-raccoon/internal/contracts"
	"github.com/market-raccoon/internal/shared/codec"
	marketdatav1 "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1"
	"google.golang.org/protobuf/proto"
)

func TestProtoCodec_RegistryRoundtrip_SemanticEquivalence(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterMarketDataV1(reg); p != nil {
		t.Fatalf("RegisterMarketDataV1: %v", p)
	}

	key := codec.SchemaKey{
		Type:    "marketdata.trade",
		Version: 1,
		Format:  codec.FormatProto,
	}
	enc, ok := reg.Encoder(key)
	if !ok {
		t.Fatalf("missing encoder for key %+v", key)
	}
	dec, ok := reg.Decoder(key)
	if !ok {
		t.Fatalf("missing decoder for key %+v", key)
	}

	in := &marketdatav1.TradeTickV1{
		Price:       65111.5,
		Size:        2.25,
		Side:        "sell",
		TradeId:     "trade-9988",
		TimestampMs: 1700005555666,
	}

	data, p := enc.Encode(in)
	if p != nil {
		t.Fatalf("encode: %v", p)
	}
	outAny, p := dec.Decode(data)
	if p != nil {
		t.Fatalf("decode: %v", p)
	}
	out, ok := outAny.(*marketdatav1.TradeTickV1)
	if !ok {
		t.Fatalf("decode type = %T; want *marketdatav1.TradeTickV1", outAny)
	}
	if !proto.Equal(in, out) {
		t.Fatalf("registry proto roundtrip mismatch: got %+v want %+v", out, in)
	}

	// Validate semantic equivalence at the contract boundary helper.
	if got, want := contracts.ProtoToDomainTradeTickV1(out), contracts.ProtoToDomainTradeTickV1(in); got != want {
		t.Fatalf("semantic equivalence mismatch: got %+v want %+v", got, want)
	}
}
