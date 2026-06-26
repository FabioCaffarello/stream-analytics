package contracts

import (
	insightsdomain "github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
	insightsv1 "github.com/FabioCaffarello/stream-analytics/internal/shared/proto/gen/insights/v1"
)

func ProtoToDomainVolumeProfileSnapshotV1(in *insightsv1.VolumeProfileSnapshotV1) insightsdomain.VolumeProfileSnapshotV1 {
	if in == nil {
		return insightsdomain.VolumeProfileSnapshotV1{}
	}
	return insightsdomain.VolumeProfileSnapshotV1{
		Venue:         in.GetVenue(),
		Instrument:    in.GetInstrument(),
		Timeframe:     in.GetTimeframe(),
		WindowStartTs: in.GetWindowStartTs(),
		WindowEndTs:   in.GetWindowEndTs(),
		Buckets:       protoToDomainVolumeProfileBuckets(in.GetBuckets()),
		POCPrice:      in.GetPocPrice(),
		ValueAreaLow:  in.GetValueAreaLow(),
		ValueAreaHigh: in.GetValueAreaHigh(),
	}
}

func DomainToProtoVolumeProfileSnapshotV1(in insightsdomain.VolumeProfileSnapshotV1) *insightsv1.VolumeProfileSnapshotV1 {
	return &insightsv1.VolumeProfileSnapshotV1{
		Venue:         in.Venue,
		Instrument:    in.Instrument,
		Timeframe:     in.Timeframe,
		WindowStartTs: in.WindowStartTs,
		WindowEndTs:   in.WindowEndTs,
		Buckets:       domainToProtoVolumeProfileBuckets(in.Buckets),
		PocPrice:      in.POCPrice,
		ValueAreaLow:  in.ValueAreaLow,
		ValueAreaHigh: in.ValueAreaHigh,
	}
}

func protoToDomainVolumeProfileBuckets(in []*insightsv1.VolumeProfileBucketV1) []insightsdomain.VolumeProfileBucketV1 {
	if len(in) == 0 {
		return nil
	}
	out := make([]insightsdomain.VolumeProfileBucketV1, len(in))
	for i := range in {
		out[i] = insightsdomain.VolumeProfileBucketV1{
			PriceLow:    in[i].GetPriceLow(),
			PriceHigh:   in[i].GetPriceHigh(),
			BuyVolume:   in[i].GetBuyVolume(),
			SellVolume:  in[i].GetSellVolume(),
			TotalVolume: in[i].GetTotalVolume(),
			SeqMin:      in[i].GetSeqMin(),
			SeqMax:      in[i].GetSeqMax(),
		}
	}
	return out
}

func domainToProtoVolumeProfileBuckets(in []insightsdomain.VolumeProfileBucketV1) []*insightsv1.VolumeProfileBucketV1 {
	if len(in) == 0 {
		return nil
	}
	out := make([]*insightsv1.VolumeProfileBucketV1, len(in))
	for i := range in {
		out[i] = &insightsv1.VolumeProfileBucketV1{
			PriceLow:    in[i].PriceLow,
			PriceHigh:   in[i].PriceHigh,
			BuyVolume:   in[i].BuyVolume,
			SellVolume:  in[i].SellVolume,
			TotalVolume: in[i].TotalVolume,
			SeqMin:      in[i].SeqMin,
			SeqMax:      in[i].SeqMax,
		}
	}
	return out
}
