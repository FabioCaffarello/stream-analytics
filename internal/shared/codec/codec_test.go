package codec_test

import (
	"testing"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	marketdatav1 "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	reg := codec.NewRegistry()
	key := codec.SchemaKey{
		Type:    "marketdata.trade",
		Version: 1,
		Format:  codec.FormatJSON,
	}

	jsonCodec := codec.JSONCodec[marketdomain.TradeTickV1]{}
	if p := reg.Register(key, jsonCodec, jsonCodec); p != nil {
		t.Fatalf("register codec: %v", p)
	}

	if _, ok := reg.Encoder(key); !ok {
		t.Fatalf("expected encoder for key %+v", key)
	}
	if _, ok := reg.Decoder(key); !ok {
		t.Fatalf("expected decoder for key %+v", key)
	}

	if _, ok := reg.Encoder(codec.SchemaKey{Type: key.Type, Version: key.Version, Format: codec.FormatProto}); ok {
		t.Fatalf("expected missing encoder for unregistered format")
	}
	if _, ok := reg.Decoder(codec.SchemaKey{Type: "marketdata.unknown", Version: 1, Format: codec.FormatJSON}); ok {
		t.Fatalf("expected missing decoder for unknown type")
	}
}

func TestJSONCodec_Roundtrip_TradeTickV1(t *testing.T) {
	c := codec.JSONCodec[marketdomain.TradeTickV1]{}
	in := marketdomain.TradeTickV1{
		Price:     65000.25,
		Size:      0.42,
		Side:      "buy",
		TradeID:   "t-123",
		Timestamp: 1700000000123,
	}

	data, p := c.Encode(in)
	if p != nil {
		t.Fatalf("encode: %v", p)
	}
	outAny, p := c.Decode(data)
	if p != nil {
		t.Fatalf("decode: %v", p)
	}

	out, ok := outAny.(marketdomain.TradeTickV1)
	if !ok {
		t.Fatalf("decode type = %T; want %T", outAny, marketdomain.TradeTickV1{})
	}
	if out != in {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", out, in)
	}
}

func TestProtoCodec_Roundtrip_TradeTickV1_ProtoGenerated(t *testing.T) {
	c := codec.ProtoCodec[*marketdatav1.TradeTickV1]{
		New: func() *marketdatav1.TradeTickV1 { return &marketdatav1.TradeTickV1{} },
	}
	in := &marketdatav1.TradeTickV1{
		Price:       65000.25,
		Size:        0.42,
		Side:        "buy",
		TradeId:     "t-123",
		TimestampMs: 1700000000123,
	}

	data, p := c.Encode(in)
	if p != nil {
		t.Fatalf("encode: %v", p)
	}
	outAny, p := c.Decode(data)
	if p != nil {
		t.Fatalf("decode: %v", p)
	}

	out, ok := outAny.(*marketdatav1.TradeTickV1)
	if !ok {
		t.Fatalf("decode type = %T; want *marketdatav1.TradeTickV1", outAny)
	}
	if out.GetPrice() != in.GetPrice() ||
		out.GetSize() != in.GetSize() ||
		out.GetSide() != in.GetSide() ||
		out.GetTradeId() != in.GetTradeId() ||
		out.GetTimestampMs() != in.GetTimestampMs() {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", out, in)
	}
}
