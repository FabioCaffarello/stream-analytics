package contracts

import (
	"testing"

	aggregationv1 "github.com/market-raccoon/internal/shared/proto/gen/aggregation/v1"
	aggregationv2 "github.com/market-raccoon/internal/shared/proto/gen/aggregation/v2"
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
		WindowMs:        300000,
		TsIngestMs:      1700000300123,
		QualityFlags:    5,
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

func TestConverterCompleteness_TapeWindowV1(t *testing.T) {
	t.Parallel()

	in := &aggregationv1.TapeWindowV1{
		Venue:         "binance",
		Instrument:    "BTC-USDT",
		Timeframe:     "1s",
		WindowStartTs: 1700000000000,
		WindowEndTs:   1700000001000,
		TradeCount:    125,
		BuyCount:      70,
		SellCount:     55,
		BuyVolume:     12.5,
		SellVolume:    10.4,
		TotalVolume:   22.9,
		BuyNotional:   812345.6,
		SellNotional:  676543.2,
		VwapPrice:     65012.3,
		MaxPrice:      65100.0,
		MinPrice:      64950.0,
		LastPrice:     65055.5,
		MaxTradeSize:  2.25,
		Rate:          125.0,
		Imbalance:     0.0917,
		IsBurst:       true,
		Seq:           4242,
		TsIngestMs:    1700000001001,
	}

	wireDTO := ProtoToWireDTOTapeWindowV1(in)
	roundtrip := WireDTOToProtoTapeWindowV1(wireDTO)
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

func TestConverterCompleteness_TapeWindowV1_NilInput(t *testing.T) {
	t.Parallel()
	wireDTO := ProtoToWireDTOTapeWindowV1(nil)
	if wireDTO.Venue != "" || wireDTO.TradeCount != 0 || wireDTO.IsBurst {
		t.Fatal("expected zero value from nil proto input")
	}
}

func TestConverterCompleteness_OpenInterestWindowV1(t *testing.T) {
	t.Parallel()

	in := &aggregationv1.OpenInterestWindowV1{
		Venue:         "binance",
		Instrument:    "BTC-USDT",
		Timeframe:     "raw",
		WindowStartTs: 1700000000000,
		WindowEndTs:   1700000000000,
		OpenInterest:  1_250_000.0,
		Delta:         2_500.5,
		DeltaPct:      0.002,
		Seq:           4201,
		TsIngestMs:    1700000000001,
	}

	wireDTO := ProtoToWireDTOOpenInterestWindowV1(in)
	roundtrip := WireDTOToProtoOpenInterestWindowV1(wireDTO)
	assertAggregationProtoEqual(t, in, roundtrip)
}

func TestConverterCompleteness_OpenInterestWindowV1_NilInput(t *testing.T) {
	t.Parallel()
	wireDTO := ProtoToWireDTOOpenInterestWindowV1(nil)
	if wireDTO.Venue != "" || wireDTO.OpenInterest != 0 || wireDTO.Seq != 0 {
		t.Fatal("expected zero value from nil proto input")
	}
}

func TestConverterCompleteness_DeltaVolumeWindowV1(t *testing.T) {
	t.Parallel()

	in := &aggregationv1.DeltaVolumeWindowV1{
		Venue:         "binance",
		Instrument:    "BTC-USDT",
		Timeframe:     "1s",
		WindowStartTs: 1700000000000,
		WindowEndTs:   1700000001000,
		BuyVolume:     15.5,
		SellVolume:    12.25,
		DeltaVolume:   3.25,
		Seq:           777,
		TsIngestMs:    1700000001000,
	}

	wireDTO := ProtoToWireDTODeltaVolumeWindowV1(in)
	roundtrip := WireDTOToProtoDeltaVolumeWindowV1(wireDTO)
	assertAggregationProtoEqual(t, in, roundtrip)
}

func TestConverterCompleteness_DeltaVolumeWindowV1_NilInput(t *testing.T) {
	t.Parallel()
	wireDTO := ProtoToWireDTODeltaVolumeWindowV1(nil)
	if wireDTO.Venue != "" || wireDTO.DeltaVolume != 0 || wireDTO.Seq != 0 {
		t.Fatal("expected zero value from nil proto input")
	}
}

func TestConverterCompleteness_CVDWindowV1(t *testing.T) {
	t.Parallel()

	in := &aggregationv1.CVDWindowV1{
		Venue:         "binance",
		Instrument:    "BTC-USDT",
		Timeframe:     "1s",
		WindowStartTs: 1700000000000,
		WindowEndTs:   1700000001000,
		DeltaVolume:   3.25,
		Cvd:           42.75,
		Seq:           777,
		TsIngestMs:    1700000001000,
	}

	wireDTO := ProtoToWireDTOCVDWindowV1(in)
	roundtrip := WireDTOToProtoCVDWindowV1(wireDTO)
	assertAggregationProtoEqual(t, in, roundtrip)
}

func TestConverterCompleteness_CVDWindowV1_NilInput(t *testing.T) {
	t.Parallel()
	wireDTO := ProtoToWireDTOCVDWindowV1(nil)
	if wireDTO.Venue != "" || wireDTO.CVD != 0 || wireDTO.Seq != 0 {
		t.Fatal("expected zero value from nil proto input")
	}
}

func TestConverterCompleteness_BarStatsWindowV1(t *testing.T) {
	t.Parallel()

	in := &aggregationv1.BarStatsWindowV1{
		Venue:         "binance",
		Instrument:    "BTC-USDT",
		Timeframe:     "1s",
		WindowStartTs: 1700000000000,
		WindowEndTs:   1700000001000,
		TradeCount:    100,
		BuyCount:      55,
		SellCount:     45,
		TotalVolume:   30.5,
		BuyVolume:     16.0,
		SellVolume:    14.5,
		VwapPrice:     65000.5,
		LastPrice:     65001.0,
		MaxPrice:      65010.0,
		MinPrice:      64990.0,
		Imbalance:     0.04918,
		IsBurst:       true,
		Seq:           777,
		TsIngestMs:    1700000001000,
	}

	wireDTO := ProtoToWireDTOBarStatsWindowV1(in)
	roundtrip := WireDTOToProtoBarStatsWindowV1(wireDTO)
	assertAggregationProtoEqual(t, in, roundtrip)
}

func TestConverterCompleteness_BarStatsWindowV1_NilInput(t *testing.T) {
	t.Parallel()
	wireDTO := ProtoToWireDTOBarStatsWindowV1(nil)
	if wireDTO.Venue != "" || wireDTO.TradeCount != 0 || wireDTO.Seq != 0 {
		t.Fatal("expected zero value from nil proto input")
	}
}

func TestConverterCompleteness_SnapshotV1(t *testing.T) {
	t.Parallel()

	in := &aggregationv1.OrderBookSnapshotV1{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        42,
		Bids: []*aggregationv1.OrderBookLevelV1{
			{Price: 65000.5, Quantity: 1.25},
			{Price: 64999.0, Quantity: 3.5},
		},
		Asks: []*aggregationv1.OrderBookLevelV1{
			{Price: 65001.0, Quantity: 0.75},
			{Price: 65002.5, Quantity: 2.0},
		},
	}

	wireDTO := ProtoToWireDTOSnapshotV1(in)
	roundtrip := WireDTOToProtoSnapshotV1(wireDTO)
	assertAggregationProtoEqual(t, in, roundtrip)
}

func TestConverterCompleteness_SnapshotV1_NilInput(t *testing.T) {
	t.Parallel()
	wireDTO := ProtoToWireDTOSnapshotV1(nil)
	if wireDTO.Venue != "" || wireDTO.Seq != 0 || len(wireDTO.Bids) != 0 {
		t.Fatal("expected zero value from nil proto input")
	}
}

func TestConverterCompleteness_SnapshotV1_EmptyLevels(t *testing.T) {
	t.Parallel()

	in := &aggregationv1.OrderBookSnapshotV1{
		Venue:      "bybit",
		Instrument: "ETH-USDT",
		Seq:        1,
	}

	wireDTO := ProtoToWireDTOSnapshotV1(in)
	roundtrip := WireDTOToProtoSnapshotV1(wireDTO)
	assertAggregationProtoEqual(t, in, roundtrip)
}

func TestConverterCompleteness_SnapshotV2(t *testing.T) {
	t.Parallel()

	in := &aggregationv2.OrderBookSnapshotV2{
		Venue:        "binance",
		Instrument:   "BTC-USDT",
		Seq:          42,
		BestBidPrice: 65000.5,
		BestAskPrice: 65001.0,
		SpreadBps:    0.0769,
		Checksum:     12345,
		TsIngestMs:   1700000000001,
		BidCount:     2,
		AskCount:     2,
		DepthCap:     50,
		Version:      2,
		Bids: []*aggregationv2.OrderBookLevelV1{
			{Price: 65000.5, Quantity: 1.25},
			{Price: 64999.0, Quantity: 3.5},
		},
		Asks: []*aggregationv2.OrderBookLevelV1{
			{Price: 65001.0, Quantity: 0.75},
			{Price: 65002.5, Quantity: 2.0},
		},
	}

	wireDTO := ProtoToWireDTOSnapshotV2(in)
	roundtrip := WireDTOToProtoSnapshotV2(wireDTO)
	assertAggregationProtoEqual(t, in, roundtrip)
}

func TestConverterCompleteness_SnapshotV2_NilInput(t *testing.T) {
	t.Parallel()
	wireDTO := ProtoToWireDTOSnapshotV2(nil)
	if wireDTO.Venue != "" || wireDTO.Seq != 0 || len(wireDTO.Bids) != 0 || wireDTO.Version != 0 {
		t.Fatal("expected zero value from nil proto input")
	}
}

func TestConverterCompleteness_InconsistencyV1(t *testing.T) {
	t.Parallel()

	in := &aggregationv1.OrderBookInconsistencyV1{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        99,
		Reason:     "crossed_book",
	}

	wireDTO := ProtoToWireDTOInconsistencyV1(in)
	roundtrip := WireDTOToProtoInconsistencyV1(wireDTO)
	assertAggregationProtoEqual(t, in, roundtrip)
}

func TestConverterCompleteness_InconsistencyV1_NilInput(t *testing.T) {
	t.Parallel()
	wireDTO := ProtoToWireDTOInconsistencyV1(nil)
	if wireDTO.Venue != "" || wireDTO.Reason != "" {
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
