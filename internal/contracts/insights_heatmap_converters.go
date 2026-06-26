package contracts

import (
	insightsdomain "github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
	insightsv1 "github.com/FabioCaffarello/stream-analytics/internal/shared/proto/gen/insights/v1"
)

// DomainToProtoHeatmapArtifactV1 converts the domain heatmap artifact to the protobuf message.
func DomainToProtoHeatmapArtifactV1(in insightsdomain.HeatmapArtifactV1) *insightsv1.HeatmapArtifactV1 {
	return &insightsv1.HeatmapArtifactV1{
		Venue:         in.Venue,
		Instrument:    in.Instrument,
		Timeframe:     in.Timeframe,
		WindowStartTs: in.WindowStartTs,
		WindowEndTs:   in.WindowEndTs,
		Cells:         domainToProtoHeatmapCells(in.Cells),
	}
}

// ProtoToDomainHeatmapArtifactV1 converts the protobuf message to the domain heatmap artifact.
func ProtoToDomainHeatmapArtifactV1(in *insightsv1.HeatmapArtifactV1) insightsdomain.HeatmapArtifactV1 {
	if in == nil {
		return insightsdomain.HeatmapArtifactV1{}
	}
	return insightsdomain.HeatmapArtifactV1{
		Venue:         in.GetVenue(),
		Instrument:    in.GetInstrument(),
		Timeframe:     in.GetTimeframe(),
		WindowStartTs: in.GetWindowStartTs(),
		WindowEndTs:   in.GetWindowEndTs(),
		Cells:         protoToDomainHeatmapCells(in.GetCells()),
	}
}

func domainToProtoHeatmapCells(in []insightsdomain.HeatmapCellV1) []*insightsv1.HeatmapCellV1 {
	if len(in) == 0 {
		return nil
	}
	out := make([]*insightsv1.HeatmapCellV1, len(in))
	for i := range in {
		out[i] = &insightsv1.HeatmapCellV1{
			PriceBucketLow:  in[i].PriceBucketLow,
			PriceBucketHigh: in[i].PriceBucketHigh,
			SizeBucket:      in[i].SizeBucket,
			BidLiquidity:    in[i].BidLiquidity,
			AskLiquidity:    in[i].AskLiquidity,
			TradeVolume:     in[i].TradeVolume,
			SeqMin:          in[i].SeqMin,
			SeqMax:          in[i].SeqMax,
			Samples:         in[i].Samples,
		}
	}
	return out
}

func protoToDomainHeatmapCells(in []*insightsv1.HeatmapCellV1) []insightsdomain.HeatmapCellV1 {
	if len(in) == 0 {
		return nil
	}
	out := make([]insightsdomain.HeatmapCellV1, len(in))
	for i := range in {
		out[i] = insightsdomain.HeatmapCellV1{
			PriceBucketLow:  in[i].GetPriceBucketLow(),
			PriceBucketHigh: in[i].GetPriceBucketHigh(),
			SizeBucket:      in[i].GetSizeBucket(),
			BidLiquidity:    in[i].GetBidLiquidity(),
			AskLiquidity:    in[i].GetAskLiquidity(),
			TradeVolume:     in[i].GetTradeVolume(),
			SeqMin:          in[i].GetSeqMin(),
			SeqMax:          in[i].GetSeqMax(),
			Samples:         in[i].GetSamples(),
		}
	}
	return out
}
