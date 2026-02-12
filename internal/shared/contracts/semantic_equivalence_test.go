package contracts_test

import (
	"reflect"
	"testing"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	marketdatav1 "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1"
)

func TestTradeTickV1_JSON_vs_Proto_SemanticEquivalence(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterMarketDataV1(reg); p != nil {
		t.Fatalf("RegisterMarketDataV1: %v", p)
	}

	canonical := marketdomain.TradeTickV1{
		Price:     65321.25,
		Size:      1.5,
		Side:      "sell",
		TradeID:   "trade-789",
		Timestamp: 1700001111222,
	}

	jsonKey := codec.SchemaKey{Type: "marketdata.trade", Version: 1, Format: codec.FormatJSON}
	jsonEnc, _ := reg.Encoder(jsonKey)
	jsonDec, _ := reg.Decoder(jsonKey)
	jsonBytes, p := jsonEnc.Encode(canonical)
	if p != nil {
		t.Fatalf("json encode: %v", p)
	}
	jsonAny, p := jsonDec.Decode(jsonBytes)
	if p != nil {
		t.Fatalf("json decode: %v", p)
	}
	jsonTrade, ok := jsonAny.(marketdomain.TradeTickV1)
	if !ok {
		t.Fatalf("json decoded type = %T; want marketdomain.TradeTickV1", jsonAny)
	}

	protoKey := codec.SchemaKey{Type: "marketdata.trade", Version: 1, Format: codec.FormatProto}
	protoEnc, _ := reg.Encoder(protoKey)
	protoDec, _ := reg.Decoder(protoKey)
	protoInput := &marketdatav1.TradeTickV1{
		Price:       canonical.Price,
		Size:        canonical.Size,
		Side:        canonical.Side,
		TradeId:     canonical.TradeID,
		TimestampMs: canonical.Timestamp,
	}
	protoBytes, p := protoEnc.Encode(protoInput)
	if p != nil {
		t.Fatalf("proto encode: %v", p)
	}
	protoAny, p := protoDec.Decode(protoBytes)
	if p != nil {
		t.Fatalf("proto decode: %v", p)
	}
	protoMsg, ok := protoAny.(*marketdatav1.TradeTickV1)
	if !ok {
		t.Fatalf("proto decoded type = %T; want *marketdatav1.TradeTickV1", protoAny)
	}
	fromProto := contracts.ProtoToDomainTradeTickV1(protoMsg)

	if jsonTrade != canonical {
		t.Fatalf("json roundtrip mismatch: got %+v want %+v", jsonTrade, canonical)
	}
	if fromProto != canonical {
		t.Fatalf("proto semantic mismatch: got %+v want %+v", fromProto, canonical)
	}
}

func TestBookDeltaV1_JSON_vs_Proto_SemanticEquivalence(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterMarketDataV1(reg); p != nil {
		t.Fatalf("RegisterMarketDataV1: %v", p)
	}

	canonical := marketdomain.BookDeltaV1{
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

	jsonKey := codec.SchemaKey{Type: "marketdata.bookdelta", Version: 1, Format: codec.FormatJSON}
	jsonEnc, _ := reg.Encoder(jsonKey)
	jsonDec, _ := reg.Decoder(jsonKey)
	jsonBytes, p := jsonEnc.Encode(canonical)
	if p != nil {
		t.Fatalf("json encode: %v", p)
	}
	jsonAny, p := jsonDec.Decode(jsonBytes)
	if p != nil {
		t.Fatalf("json decode: %v", p)
	}
	jsonBook, ok := jsonAny.(marketdomain.BookDeltaV1)
	if !ok {
		t.Fatalf("json decoded type = %T; want marketdomain.BookDeltaV1", jsonAny)
	}

	protoKey := codec.SchemaKey{Type: "marketdata.bookdelta", Version: 1, Format: codec.FormatProto}
	protoEnc, _ := reg.Encoder(protoKey)
	protoDec, _ := reg.Decoder(protoKey)
	protoInput := &marketdatav1.BookDeltaV1{
		Bids: []*marketdatav1.PriceLevel{
			{Price: canonical.Bids[0].Price, Size: canonical.Bids[0].Size},
			{Price: canonical.Bids[1].Price, Size: canonical.Bids[1].Size},
		},
		Asks: []*marketdatav1.PriceLevel{
			{Price: canonical.Asks[0].Price, Size: canonical.Asks[0].Size},
			{Price: canonical.Asks[1].Price, Size: canonical.Asks[1].Size},
		},
		FirstUpdateId:     canonical.FirstID,
		FinalUpdateId:     canonical.FinalID,
		PrevFinalUpdateId: canonical.PrevFinal,
		TimestampMs:       canonical.Timestamp,
	}
	protoBytes, p := protoEnc.Encode(protoInput)
	if p != nil {
		t.Fatalf("proto encode: %v", p)
	}
	protoAny, p := protoDec.Decode(protoBytes)
	if p != nil {
		t.Fatalf("proto decode: %v", p)
	}
	protoMsg, ok := protoAny.(*marketdatav1.BookDeltaV1)
	if !ok {
		t.Fatalf("proto decoded type = %T; want *marketdatav1.BookDeltaV1", protoAny)
	}
	fromProto := contracts.ProtoToDomainBookDeltaV1(protoMsg)

	if !reflect.DeepEqual(jsonBook, canonical) {
		t.Fatalf("json roundtrip mismatch: got %+v want %+v", jsonBook, canonical)
	}
	if !reflect.DeepEqual(fromProto, canonical) {
		t.Fatalf("proto semantic mismatch: got %+v want %+v", fromProto, canonical)
	}
}
