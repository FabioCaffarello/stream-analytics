package codec_test

import (
	"reflect"
	"testing"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestEncodeDecodePayload_Trade_JSONAndProtoSemanticEquivalence(t *testing.T) {
	bootstrapPayloadRegistry(t)

	in := marketdomain.TradeTickV1{
		Price:     65321.25,
		Size:      1.5,
		Side:      "sell",
		TradeID:   "trade-789",
		Timestamp: 1700001111222,
	}

	jsonBytes, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, in)
	if p != nil {
		t.Fatalf("EncodePayload(JSON): %v", p)
	}
	protoBytes, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeProto, in)
	if p != nil {
		t.Fatalf("EncodePayload(PROTO): %v", p)
	}

	jsonAny, p := codec.DecodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, jsonBytes)
	if p != nil {
		t.Fatalf("DecodePayload(JSON): %v", p)
	}
	protoAny, p := codec.DecodePayload("marketdata.trade", 1, envelope.ContentTypeProto, protoBytes)
	if p != nil {
		t.Fatalf("DecodePayload(PROTO): %v", p)
	}

	jsonOut, ok := jsonAny.(marketdomain.TradeTickV1)
	if !ok {
		t.Fatalf("json decoded type = %T, want %T", jsonAny, marketdomain.TradeTickV1{})
	}
	protoOut, ok := protoAny.(marketdomain.TradeTickV1)
	if !ok {
		t.Fatalf("proto decoded type = %T, want %T", protoAny, marketdomain.TradeTickV1{})
	}

	if jsonOut != in {
		t.Fatalf("json roundtrip mismatch: got %+v want %+v", jsonOut, in)
	}
	if protoOut != in {
		t.Fatalf("proto semantic mismatch: got %+v want %+v", protoOut, in)
	}
}

func TestEncodeDecodePayload_BookDelta_JSONAndProtoSemanticEquivalence(t *testing.T) {
	bootstrapPayloadRegistry(t)

	in := marketdomain.BookDeltaV1{
		Bids: []marketdomain.PriceLevel{
			{Price: 100.5, Size: 2.0},
			{Price: 100.0, Size: 3.5},
		},
		Asks: []marketdomain.PriceLevel{
			{Price: 101.0, Size: 1.25},
			{Price: 101.5, Size: 0.75},
		},
		FirstID:   1200,
		FinalID:   1210,
		PrevFinal: 1199,
		Timestamp: 1700002222333,
	}

	jsonBytes, p := codec.EncodePayload("marketdata.bookdelta", 1, envelope.ContentTypeJSON, in)
	if p != nil {
		t.Fatalf("EncodePayload(JSON): %v", p)
	}
	protoBytes, p := codec.EncodePayload("marketdata.bookdelta", 1, envelope.ContentTypeProto, in)
	if p != nil {
		t.Fatalf("EncodePayload(PROTO): %v", p)
	}

	jsonAny, p := codec.DecodePayload("marketdata.bookdelta", 1, envelope.ContentTypeJSON, jsonBytes)
	if p != nil {
		t.Fatalf("DecodePayload(JSON): %v", p)
	}
	protoAny, p := codec.DecodePayload("marketdata.bookdelta", 1, envelope.ContentTypeProto, protoBytes)
	if p != nil {
		t.Fatalf("DecodePayload(PROTO): %v", p)
	}

	jsonOut, ok := jsonAny.(marketdomain.BookDeltaV1)
	if !ok {
		t.Fatalf("json decoded type = %T, want %T", jsonAny, marketdomain.BookDeltaV1{})
	}
	protoOut, ok := protoAny.(marketdomain.BookDeltaV1)
	if !ok {
		t.Fatalf("proto decoded type = %T, want %T", protoAny, marketdomain.BookDeltaV1{})
	}

	if !reflect.DeepEqual(jsonOut, in) {
		t.Fatalf("json roundtrip mismatch: got %+v want %+v", jsonOut, in)
	}
	if !reflect.DeepEqual(protoOut, in) {
		t.Fatalf("proto semantic mismatch: got %+v want %+v", protoOut, in)
	}
}

func TestEncodePayload_UnknownContentTypeRejected(t *testing.T) {
	bootstrapPayloadRegistry(t)

	_, p := codec.EncodePayload("marketdata.trade", 1, "application/xml", marketdomain.TradeTickV1{})
	if p == nil {
		t.Fatal("expected validation error for unknown content type")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code = %s, want %s", p.Code, problem.ValidationFailed)
	}
}

func bootstrapPayloadRegistry(t *testing.T) {
	t.Helper()
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}
}
