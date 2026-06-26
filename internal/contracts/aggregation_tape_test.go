package contracts

import (
	"encoding/json"
	"testing"

	aggregationv1 "github.com/FabioCaffarello/stream-analytics/internal/shared/proto/gen/aggregation/v1"
	"google.golang.org/protobuf/proto"
)

func TestAggregationTapeV1_RoundTrip_JSON(t *testing.T) {
	in := AggregationTapeV1{
		Venue:         "binance",
		Instrument:    "BTC-USDT",
		Timeframe:     "1s",
		WindowStartTs: 1_700_000_000_000,
		WindowEndTs:   1_700_000_001_000,
		TradeCount:    120,
		BuyCount:      73,
		SellCount:     47,
		BuyVolume:     14.5,
		SellVolume:    9.25,
		TotalVolume:   23.75,
		BuyNotional:   913250.12,
		SellNotional:  583420.88,
		VwapPrice:     62989.345,
		MaxPrice:      63010,
		MinPrice:      62910,
		LastPrice:     62995,
		MaxTradeSize:  2.5,
		Rate:          120,
		Imbalance:     0.221,
		IsBurst:       true,
		Seq:           99887,
		TsIngestMs:    1_700_000_001_005,
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out AggregationTapeV1
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("roundtrip mismatch got=%+v want=%+v", out, in)
	}
}

func TestAggregationTapeV1_Proto_RoundTrip(t *testing.T) {
	in := AggregationTapeV1{
		Venue:         "bybit",
		Instrument:    "ETH-USDT",
		Timeframe:     "250ms",
		WindowStartTs: 1_700_000_000_250,
		WindowEndTs:   1_700_000_000_500,
		TradeCount:    37,
		BuyCount:      20,
		SellCount:     17,
		BuyVolume:     8.2,
		SellVolume:    7.9,
		TotalVolume:   16.1,
		BuyNotional:   28400.1,
		SellNotional:  27355.7,
		VwapPrice:     3468.2,
		MaxPrice:      3472,
		MinPrice:      3461,
		LastPrice:     3469.3,
		MaxTradeSize:  1.2,
		Rate:          148,
		Imbalance:     0.019,
		IsBurst:       false,
		Seq:           123456,
		TsIngestMs:    1_700_000_000_501,
	}

	msg := WireDTOToProtoTapeWindowV1(in)
	out := ProtoToWireDTOTapeWindowV1(msg)
	if out != in {
		t.Fatalf("wire/proto mismatch got=%+v want=%+v", out, in)
	}

	expected := &aggregationv1.TapeWindowV1{
		Venue:         in.Venue,
		Instrument:    in.Instrument,
		Timeframe:     in.Timeframe,
		WindowStartTs: in.WindowStartTs,
		WindowEndTs:   in.WindowEndTs,
		TradeCount:    in.TradeCount,
		BuyCount:      in.BuyCount,
		SellCount:     in.SellCount,
		BuyVolume:     in.BuyVolume,
		SellVolume:    in.SellVolume,
		TotalVolume:   in.TotalVolume,
		BuyNotional:   in.BuyNotional,
		SellNotional:  in.SellNotional,
		VwapPrice:     in.VwapPrice,
		MaxPrice:      in.MaxPrice,
		MinPrice:      in.MinPrice,
		LastPrice:     in.LastPrice,
		MaxTradeSize:  in.MaxTradeSize,
		Rate:          in.Rate,
		Imbalance:     in.Imbalance,
		IsBurst:       in.IsBurst,
		Seq:           in.Seq,
		TsIngestMs:    in.TsIngestMs,
	}
	if !proto.Equal(msg, expected) {
		t.Fatalf("unexpected proto message got=%v want=%v", msg, expected)
	}
}
