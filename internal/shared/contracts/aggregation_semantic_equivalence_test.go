package contracts_test

import (
	"reflect"
	"testing"

	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
)

func TestCandleClosedV1_JSON_vs_Proto_SemanticEquivalence(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterAggregationPayloadV1(reg); p != nil {
		t.Fatalf("RegisterAggregationPayloadV1: %v", p)
	}

	canonical := contracts.AggregationCandleClosedV1{
		Candle: contracts.AggregationCandleV1{
			Venue:         "binance",
			Instrument:    "BTC-USDT",
			Timeframe:     "1m",
			WindowStartTs: 1700000000000,
			WindowEndTs:   1700000060000,
			Open:          65000.5,
			High:          65100.0,
			Low:           64900.0,
			ClosePrice:    65050.25,
			Volume:        123.456,
			BuyVolume:     70.5,
			SellVolume:    52.956,
			TradeCount:    1500,
			SeqFirst:      100,
			SeqLast:       1599,
			IsClosed:      true,
		},
	}

	// JSON roundtrip
	jsonKey := codec.SchemaKey{Type: "aggregation.candle", Version: 1, Format: codec.FormatJSON}
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
	jsonResult, ok := jsonAny.(contracts.AggregationCandleClosedV1)
	if !ok {
		t.Fatalf("json decoded type = %T; want contracts.AggregationCandleClosedV1", jsonAny)
	}

	// Proto roundtrip
	protoKey := codec.SchemaKey{Type: "aggregation.candle", Version: 1, Format: codec.FormatProto}
	protoEnc, _ := reg.Encoder(protoKey)
	protoDec, _ := reg.Decoder(protoKey)
	protoBytes, p := protoEnc.Encode(canonical)
	if p != nil {
		t.Fatalf("proto encode: %v", p)
	}
	protoAny, p := protoDec.Decode(protoBytes)
	if p != nil {
		t.Fatalf("proto decode: %v", p)
	}
	protoResult, ok := protoAny.(contracts.AggregationCandleClosedV1)
	if !ok {
		t.Fatalf("proto decoded type = %T; want contracts.AggregationCandleClosedV1", protoAny)
	}

	// Both should equal the canonical input
	if !reflect.DeepEqual(jsonResult, canonical) {
		t.Fatalf("json roundtrip mismatch: got %+v want %+v", jsonResult, canonical)
	}
	if !reflect.DeepEqual(protoResult, canonical) {
		t.Fatalf("proto roundtrip mismatch: got %+v want %+v", protoResult, canonical)
	}
}

func TestStatsWindowClosedV1_JSON_vs_Proto_SemanticEquivalence(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterAggregationPayloadV1(reg); p != nil {
		t.Fatalf("RegisterAggregationPayloadV1: %v", p)
	}

	canonical := contracts.AggregationStatsWindowClosedV1{
		Stats: contracts.AggregationStatsWindowV1{
			Venue:           "bybit",
			Instrument:      "ETH-USDT",
			Timeframe:       "5m",
			WindowStartTs:   1700000000000,
			WindowEndTs:     1700000300000,
			LiqBuyVolume:    50.5,
			LiqSellVolume:   30.25,
			LiqTotalVolume:  80.75,
			LiqCount:        42,
			MarkPriceOpen:   3500.10,
			MarkPriceHigh:   3520.50,
			MarkPriceLow:    3490.00,
			MarkPriceClose:  3510.75,
			FundingRateAvg:  0.0001,
			FundingRateLast: 0.00012,
			SeqFirst:        200,
			SeqLast:         450,
			IsClosed:        true,
		},
	}

	// JSON roundtrip
	jsonKey := codec.SchemaKey{Type: "aggregation.stats", Version: 1, Format: codec.FormatJSON}
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
	jsonResult, ok := jsonAny.(contracts.AggregationStatsWindowClosedV1)
	if !ok {
		t.Fatalf("json decoded type = %T; want contracts.AggregationStatsWindowClosedV1", jsonAny)
	}

	// Proto roundtrip
	protoKey := codec.SchemaKey{Type: "aggregation.stats", Version: 1, Format: codec.FormatProto}
	protoEnc, _ := reg.Encoder(protoKey)
	protoDec, _ := reg.Decoder(protoKey)
	protoBytes, p := protoEnc.Encode(canonical)
	if p != nil {
		t.Fatalf("proto encode: %v", p)
	}
	protoAny, p := protoDec.Decode(protoBytes)
	if p != nil {
		t.Fatalf("proto decode: %v", p)
	}
	protoResult, ok := protoAny.(contracts.AggregationStatsWindowClosedV1)
	if !ok {
		t.Fatalf("proto decoded type = %T; want contracts.AggregationStatsWindowClosedV1", protoAny)
	}

	if !reflect.DeepEqual(jsonResult, canonical) {
		t.Fatalf("json roundtrip mismatch: got %+v want %+v", jsonResult, canonical)
	}
	if !reflect.DeepEqual(protoResult, canonical) {
		t.Fatalf("proto roundtrip mismatch: got %+v want %+v", protoResult, canonical)
	}
}

func TestSnapshotV1_JSON_vs_Proto_SemanticEquivalence(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterAggregationPayloadV1(reg); p != nil {
		t.Fatalf("RegisterAggregationPayloadV1: %v", p)
	}

	canonical := contracts.AggregationSnapshotV1{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        42,
		Bids: []contracts.AggregationOrderBookLevelV1{
			{Price: 65000.5, Quantity: 1.25},
			{Price: 64999.0, Quantity: 3.5},
		},
		Asks: []contracts.AggregationOrderBookLevelV1{
			{Price: 65001.0, Quantity: 0.75},
			{Price: 65002.5, Quantity: 2.0},
		},
	}

	jsonKey := codec.SchemaKey{Type: "aggregation.snapshot", Version: 1, Format: codec.FormatJSON}
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
	jsonResult, ok := jsonAny.(contracts.AggregationSnapshotV1)
	if !ok {
		t.Fatalf("json decoded type = %T; want contracts.AggregationSnapshotV1", jsonAny)
	}

	protoKey := codec.SchemaKey{Type: "aggregation.snapshot", Version: 1, Format: codec.FormatProto}
	protoEnc, _ := reg.Encoder(protoKey)
	protoDec, _ := reg.Decoder(protoKey)
	protoBytes, p := protoEnc.Encode(canonical)
	if p != nil {
		t.Fatalf("proto encode: %v", p)
	}
	protoAny, p := protoDec.Decode(protoBytes)
	if p != nil {
		t.Fatalf("proto decode: %v", p)
	}
	protoResult, ok := protoAny.(contracts.AggregationSnapshotV1)
	if !ok {
		t.Fatalf("proto decoded type = %T; want contracts.AggregationSnapshotV1", protoAny)
	}

	if !reflect.DeepEqual(jsonResult, canonical) {
		t.Fatalf("json roundtrip mismatch: got %+v want %+v", jsonResult, canonical)
	}
	if !reflect.DeepEqual(protoResult, canonical) {
		t.Fatalf("proto roundtrip mismatch: got %+v want %+v", protoResult, canonical)
	}
}

func TestOrderBookInconsistencyV1_JSON_vs_Proto_SemanticEquivalence(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterAggregationPayloadV1(reg); p != nil {
		t.Fatalf("RegisterAggregationPayloadV1: %v", p)
	}

	canonical := contracts.AggregationOrderBookInconsistencyV1{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        99,
		Reason:     "crossed_book",
	}

	jsonKey := codec.SchemaKey{Type: "aggregation.orderbook_inconsistency", Version: 1, Format: codec.FormatJSON}
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
	jsonResult, ok := jsonAny.(contracts.AggregationOrderBookInconsistencyV1)
	if !ok {
		t.Fatalf("json decoded type = %T; want contracts.AggregationOrderBookInconsistencyV1", jsonAny)
	}

	protoKey := codec.SchemaKey{Type: "aggregation.orderbook_inconsistency", Version: 1, Format: codec.FormatProto}
	protoEnc, _ := reg.Encoder(protoKey)
	protoDec, _ := reg.Decoder(protoKey)
	protoBytes, p := protoEnc.Encode(canonical)
	if p != nil {
		t.Fatalf("proto encode: %v", p)
	}
	protoAny, p := protoDec.Decode(protoBytes)
	if p != nil {
		t.Fatalf("proto decode: %v", p)
	}
	protoResult, ok := protoAny.(contracts.AggregationOrderBookInconsistencyV1)
	if !ok {
		t.Fatalf("proto decoded type = %T; want contracts.AggregationOrderBookInconsistencyV1", protoAny)
	}

	if !reflect.DeepEqual(jsonResult, canonical) {
		t.Fatalf("json roundtrip mismatch: got %+v want %+v", jsonResult, canonical)
	}
	if !reflect.DeepEqual(protoResult, canonical) {
		t.Fatalf("proto roundtrip mismatch: got %+v want %+v", protoResult, canonical)
	}
}
