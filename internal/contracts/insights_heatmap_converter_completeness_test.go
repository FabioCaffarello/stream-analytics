package contracts

import (
	"testing"

	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	insightsv1 "github.com/market-raccoon/internal/shared/proto/gen/insights/v1"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestConverterCompleteness_HeatmapArtifactV1(t *testing.T) {
	t.Parallel()

	in := &insightsv1.HeatmapArtifactV1{
		Venue:         "binance",
		Instrument:    "BTC-USDT",
		Timeframe:     "1m",
		WindowStartTs: 1700000000000,
		WindowEndTs:   1700000060000,
		Cells: []*insightsv1.HeatmapCellV1{
			{
				PriceBucketLow:  65000.0,
				PriceBucketHigh: 65100.0,
				SizeBucket:      "small",
				BidLiquidity:    150.5,
				AskLiquidity:    120.3,
				TradeVolume:     45.2,
				SeqMin:          100,
				SeqMax:          200,
				Samples:         50,
			},
			{
				PriceBucketLow:  65100.0,
				PriceBucketHigh: 65200.0,
				SizeBucket:      "medium",
				BidLiquidity:    80.0,
				AskLiquidity:    95.5,
				TradeVolume:     30.1,
				SeqMin:          150,
				SeqMax:          250,
				Samples:         30,
			},
		},
	}

	domain := ProtoToDomainHeatmapArtifactV1(in)
	roundtrip := DomainToProtoHeatmapArtifactV1(domain)
	if !proto.Equal(in, roundtrip) {
		t.Fatalf("proto roundtrip mismatch\nwant:\n%s\ngot:\n%s", prototext.Format(in), prototext.Format(roundtrip))
	}
}

func TestConverterCompleteness_HeatmapArtifactV1_NilInput(t *testing.T) {
	t.Parallel()
	domain := ProtoToDomainHeatmapArtifactV1(nil)
	if domain.Venue != "" || len(domain.Cells) != 0 {
		t.Fatal("expected zero value from nil proto input")
	}
}

func TestConverterCompleteness_HeatmapArtifactV1_EmptyCells(t *testing.T) {
	t.Parallel()
	domain := insightsdomain.HeatmapArtifactV1{
		Venue:         "binance",
		Instrument:    "BTC-USDT",
		Timeframe:     "1m",
		WindowStartTs: 1700000000000,
		WindowEndTs:   1700000060000,
	}
	pb := DomainToProtoHeatmapArtifactV1(domain)
	if len(pb.GetCells()) != 0 {
		t.Fatalf("expected nil cells for empty input, got %d", len(pb.GetCells()))
	}
	back := ProtoToDomainHeatmapArtifactV1(pb)
	if back.Venue != domain.Venue || back.Instrument != domain.Instrument {
		t.Fatalf("metadata mismatch: got %+v want %+v", back, domain)
	}
}
