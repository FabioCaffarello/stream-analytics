package contracts

import (
	aggregationv2 "github.com/market-raccoon/internal/shared/proto/gen/aggregation/v2"
)

// WireDTOToProtoCrossVenueBookSnapshotV1 converts the wire DTO to the protobuf message.
func WireDTOToProtoCrossVenueBookSnapshotV1(in AggregationCrossVenueBookSnapshotV1) *aggregationv2.CrossVenueBookSnapshotV1 {
	bids := make([]*aggregationv2.VenueLevel, len(in.BestBids))
	for i, b := range in.BestBids {
		bids[i] = &aggregationv2.VenueLevel{Venue: b.Venue, PriceFp: b.PriceFP, SizeFp: b.SizeFP}
	}
	asks := make([]*aggregationv2.VenueLevel, len(in.BestAsks))
	for i, a := range in.BestAsks {
		asks[i] = &aggregationv2.VenueLevel{Venue: a.Venue, PriceFp: a.PriceFP, SizeFp: a.SizeFP}
	}
	return &aggregationv2.CrossVenueBookSnapshotV1{
		Instrument:         in.Instrument,
		TsServerMs:         in.TsServerMs,
		BestBids:           bids,
		BestAsks:           asks,
		GlobalSpreadBps:    in.GlobalSpreadBPS,
		VenueDivergenceBps: in.VenueDivergenceBPS,
	}
}

// ProtoToWireDTOCrossVenueBookSnapshotV1 converts the protobuf message to the wire DTO.
func ProtoToWireDTOCrossVenueBookSnapshotV1(in *aggregationv2.CrossVenueBookSnapshotV1) AggregationCrossVenueBookSnapshotV1 {
	if in == nil {
		return AggregationCrossVenueBookSnapshotV1{}
	}
	bids := make([]AggregationCrossVenueBookVenueLevelV1, len(in.GetBestBids()))
	for i, b := range in.GetBestBids() {
		bids[i] = AggregationCrossVenueBookVenueLevelV1{Venue: b.GetVenue(), PriceFP: b.GetPriceFp(), SizeFP: b.GetSizeFp()}
	}
	asks := make([]AggregationCrossVenueBookVenueLevelV1, len(in.GetBestAsks()))
	for i, a := range in.GetBestAsks() {
		asks[i] = AggregationCrossVenueBookVenueLevelV1{Venue: a.GetVenue(), PriceFP: a.GetPriceFp(), SizeFP: a.GetSizeFp()}
	}
	return AggregationCrossVenueBookSnapshotV1{
		Instrument:         in.GetInstrument(),
		TsServerMs:         in.GetTsServerMs(),
		BestBids:           bids,
		BestAsks:           asks,
		GlobalSpreadBPS:    in.GetGlobalSpreadBps(),
		VenueDivergenceBPS: in.GetVenueDivergenceBps(),
	}
}
