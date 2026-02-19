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

func DomainToProtoTradeTickV1(in marketdomain.TradeTickV1) *marketdatav1.TradeTickV1 {
	return &marketdatav1.TradeTickV1{
		Price:       in.Price,
		Size:        in.Size,
		Side:        in.Side,
		TradeId:     in.TradeID,
		TimestampMs: in.Timestamp,
	}
}

func ProtoToDomainBookDeltaV1(in *marketdatav1.BookDeltaV1) marketdomain.BookDeltaV1 {
	if in == nil {
		return marketdomain.BookDeltaV1{}
	}
	return marketdomain.BookDeltaV1{
		Bids:       protoToDomainPriceLevels(in.GetBids()),
		Asks:       protoToDomainPriceLevels(in.GetAsks()),
		FirstID:    in.GetFirstUpdateId(),
		FinalID:    in.GetFinalUpdateId(),
		PrevFinal:  in.GetPrevFinalUpdateId(),
		Timestamp:  in.GetTimestampMs(),
		IsSnapshot: in.GetIsSnapshot(),
	}
}

func DomainToProtoBookDeltaV1(in marketdomain.BookDeltaV1) *marketdatav1.BookDeltaV1 {
	return &marketdatav1.BookDeltaV1{
		Bids:              domainToProtoPriceLevels(in.Bids),
		Asks:              domainToProtoPriceLevels(in.Asks),
		FirstUpdateId:     in.FirstID,
		FinalUpdateId:     in.FinalID,
		PrevFinalUpdateId: in.PrevFinal,
		TimestampMs:       in.Timestamp,
		IsSnapshot:        in.IsSnapshot,
	}
}

func ProtoToDomainMarkPriceTickV1(in *marketdatav1.MarkPriceTickV1) marketdomain.MarkPriceTickV1 {
	if in == nil {
		return marketdomain.MarkPriceTickV1{}
	}
	return marketdomain.MarkPriceTickV1{
		MarkPrice:   in.GetMarkPrice(),
		IndexPrice:  in.GetIndexPrice(),
		FundingRate: in.GetFundingRate(),
		Timestamp:   in.GetTimestampMs(),
	}
}

func DomainToProtoMarkPriceTickV1(in marketdomain.MarkPriceTickV1) *marketdatav1.MarkPriceTickV1 {
	return &marketdatav1.MarkPriceTickV1{
		MarkPrice:   in.MarkPrice,
		IndexPrice:  in.IndexPrice,
		FundingRate: in.FundingRate,
		TimestampMs: in.Timestamp,
	}
}

func ProtoToDomainLiquidationTickV1(in *marketdatav1.LiquidationTickV1) marketdomain.LiquidationTickV1 {
	if in == nil {
		return marketdomain.LiquidationTickV1{}
	}
	return marketdomain.LiquidationTickV1{
		Side:      in.GetSide(),
		Price:     in.GetPrice(),
		Size:      in.GetSize(),
		Timestamp: in.GetTimestampMs(),
	}
}

func DomainToProtoLiquidationTickV1(in marketdomain.LiquidationTickV1) *marketdatav1.LiquidationTickV1 {
	return &marketdatav1.LiquidationTickV1{
		Side:        in.Side,
		Price:       in.Price,
		Size:        in.Size,
		TimestampMs: in.Timestamp,
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

func domainToProtoPriceLevels(in []marketdomain.PriceLevel) []*marketdatav1.PriceLevel {
	if len(in) == 0 {
		return nil
	}
	out := make([]*marketdatav1.PriceLevel, len(in))
	for i := range in {
		out[i] = &marketdatav1.PriceLevel{
			Price: in[i].Price,
			Size:  in[i].Size,
		}
	}
	return out
}
