package contracts

import (
	"testing"

	marketdatav1 "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestConverterCompleteness_TradeTickV1(t *testing.T) {
	t.Parallel()

	in := &marketdatav1.TradeTickV1{
		Price:       65321.25,
		Size:        1.5,
		Side:        "sell",
		TradeId:     "trade-789",
		TimestampMs: 1700001111222,
	}

	domain := ProtoToDomainTradeTickV1(in)
	roundtrip := DomainToProtoTradeTickV1(domain)
	assertProtoSemanticallyEqual(t, in, roundtrip)
}

func TestConverterCompleteness_BookDeltaV1(t *testing.T) {
	t.Parallel()

	in := &marketdatav1.BookDeltaV1{
		Bids: []*marketdatav1.PriceLevel{
			{Price: 100.5, Size: 2.0},
			{Price: 100.0, Size: 3.5},
		},
		Asks: []*marketdatav1.PriceLevel{
			{Price: 101.0, Size: 1.25},
			{Price: 101.5, Size: 0.75},
		},
		FirstUpdateId:     1200,
		FinalUpdateId:     1210,
		PrevFinalUpdateId: 1199,
		TimestampMs:       1700002222333,
	}

	domain := ProtoToDomainBookDeltaV1(in)
	roundtrip := DomainToProtoBookDeltaV1(domain)
	assertProtoSemanticallyEqual(t, in, roundtrip)
}

func TestConverterCompleteness_MarkPriceTickV1(t *testing.T) {
	t.Parallel()

	in := &marketdatav1.MarkPriceTickV1{
		MarkPrice:   65500.1,
		IndexPrice:  65495.8,
		FundingRate: 0.00025,
		TimestampMs: 1700003333444,
	}

	domain := ProtoToDomainMarkPriceTickV1(in)
	roundtrip := DomainToProtoMarkPriceTickV1(domain)
	assertProtoSemanticallyEqual(t, in, roundtrip)
}

func TestConverterCompleteness_LiquidationTickV1(t *testing.T) {
	t.Parallel()

	in := &marketdatav1.LiquidationTickV1{
		Side:        "buy",
		Price:       64000.75,
		Size:        12.5,
		TimestampMs: 1700004444555,
	}

	domain := ProtoToDomainLiquidationTickV1(in)
	roundtrip := DomainToProtoLiquidationTickV1(domain)
	assertProtoSemanticallyEqual(t, in, roundtrip)
}

func assertProtoSemanticallyEqual(t *testing.T, want, got proto.Message) {
	t.Helper()
	if proto.Equal(want, got) {
		return
	}
	t.Fatalf("proto roundtrip mismatch\nwant:\n%s\ngot:\n%s", prototext.Format(want), prototext.Format(got))
}
