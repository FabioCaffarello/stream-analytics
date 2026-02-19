package contracts

import (
	"testing"

	aggregationv1 "github.com/market-raccoon/internal/shared/proto/gen/aggregation/v1"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestConverterCompleteness_CandleClosedV1(t *testing.T) {
	t.Parallel()

	in := &aggregationv1.CandleClosedV1{
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
	}

	wireDTO := ProtoToWireDTOCandleClosedV1(in)
	roundtrip := WireDTOToProtoCandleClosedV1(wireDTO)
	assertAggregationProtoEqual(t, in, roundtrip)
}

func TestConverterCompleteness_StatsWindowClosedV1(t *testing.T) {
	t.Parallel()

	in := &aggregationv1.StatsWindowClosedV1{
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
	}

	wireDTO := ProtoToWireDTOStatsWindowClosedV1(in)
	roundtrip := WireDTOToProtoStatsWindowClosedV1(wireDTO)
	assertAggregationProtoEqual(t, in, roundtrip)
}

func TestConverterCompleteness_CandleClosedV1_NilInput(t *testing.T) {
	t.Parallel()
	wireDTO := ProtoToWireDTOCandleClosedV1(nil)
	if wireDTO.Candle.Venue != "" || wireDTO.Candle.IsClosed {
		t.Fatal("expected zero value from nil proto input")
	}
}

func TestConverterCompleteness_StatsWindowClosedV1_NilInput(t *testing.T) {
	t.Parallel()
	wireDTO := ProtoToWireDTOStatsWindowClosedV1(nil)
	if wireDTO.Stats.Venue != "" || wireDTO.Stats.IsClosed {
		t.Fatal("expected zero value from nil proto input")
	}
}

func assertAggregationProtoEqual(t *testing.T, want, got proto.Message) {
	t.Helper()
	if proto.Equal(want, got) {
		return
	}
	t.Fatalf("proto roundtrip mismatch\nwant:\n%s\ngot:\n%s", prototext.Format(want), prototext.Format(got))
}
