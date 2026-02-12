package contracts

import (
	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	marketdatav1 "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1"
)

func ProtoToDomainTradeTickV1(in *marketdatav1.TradeTickV1) marketdomain.TradeTickV1 {
	if in == nil {
		return marketdomain.TradeTickV1{}
	}
	return marketdomain.TradeTickV1{
		Price:     in.GetPrice(),
		Size:      in.GetSize(),
		Side:      in.GetSide(),
		TradeID:   in.GetTradeId(),
		Timestamp: in.GetTimestampMs(),
	}
}

func ProtoToDomainBookDeltaV1(in *marketdatav1.BookDeltaV1) marketdomain.BookDeltaV1 {
	if in == nil {
		return marketdomain.BookDeltaV1{}
	}
	return marketdomain.BookDeltaV1{
		Bids:      protoToDomainPriceLevels(in.GetBids()),
		Asks:      protoToDomainPriceLevels(in.GetAsks()),
		FirstID:   in.GetFirstUpdateId(),
		FinalID:   in.GetFinalUpdateId(),
		PrevFinal: in.GetPrevFinalUpdateId(),
		Timestamp: in.GetTimestampMs(),
	}
}

func protoToDomainPriceLevels(in []*marketdatav1.PriceLevel) []marketdomain.PriceLevel {
	if len(in) == 0 {
		return nil
	}
	out := make([]marketdomain.PriceLevel, len(in))
	for i := range in {
		out[i] = marketdomain.PriceLevel{
			Price: in[i].GetPrice(),
			Size:  in[i].GetSize(),
		}
	}
	return out
}
