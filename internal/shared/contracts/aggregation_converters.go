package contracts

import (
	aggregationv1 "github.com/market-raccoon/internal/shared/proto/gen/aggregation/v1"
)

// WireDTOToProtoCandleClosedV1 converts the wire DTO to the protobuf message.
func WireDTOToProtoCandleClosedV1(in AggregationCandleClosedV1) *aggregationv1.CandleClosedV1 {
	c := in.Candle
	return &aggregationv1.CandleClosedV1{
		Venue:         c.Venue,
		Instrument:    c.Instrument,
		Timeframe:     c.Timeframe,
		WindowStartTs: c.WindowStartTs,
		WindowEndTs:   c.WindowEndTs,
		Open:          c.Open,
		High:          c.High,
		Low:           c.Low,
		ClosePrice:    c.ClosePrice,
		Volume:        c.Volume,
		BuyVolume:     c.BuyVolume,
		SellVolume:    c.SellVolume,
		TradeCount:    c.TradeCount,
		SeqFirst:      c.SeqFirst,
		SeqLast:       c.SeqLast,
		IsClosed:      c.IsClosed,
	}
}

// ProtoToWireDTOCandleClosedV1 converts the protobuf message to the wire DTO.
func ProtoToWireDTOCandleClosedV1(in *aggregationv1.CandleClosedV1) AggregationCandleClosedV1 {
	if in == nil {
		return AggregationCandleClosedV1{}
	}
	return AggregationCandleClosedV1{
		Candle: AggregationCandleV1{
			Venue:         in.GetVenue(),
			Instrument:    in.GetInstrument(),
			Timeframe:     in.GetTimeframe(),
			WindowStartTs: in.GetWindowStartTs(),
			WindowEndTs:   in.GetWindowEndTs(),
			Open:          in.GetOpen(),
			High:          in.GetHigh(),
			Low:           in.GetLow(),
			ClosePrice:    in.GetClosePrice(),
			Volume:        in.GetVolume(),
			BuyVolume:     in.GetBuyVolume(),
			SellVolume:    in.GetSellVolume(),
			TradeCount:    in.GetTradeCount(),
			SeqFirst:      in.GetSeqFirst(),
			SeqLast:       in.GetSeqLast(),
			IsClosed:      in.GetIsClosed(),
		},
	}
}

// WireDTOToProtoStatsWindowClosedV1 converts the wire DTO to the protobuf message.
func WireDTOToProtoStatsWindowClosedV1(in AggregationStatsWindowClosedV1) *aggregationv1.StatsWindowClosedV1 {
	s := in.Stats
	return &aggregationv1.StatsWindowClosedV1{
		Venue:           s.Venue,
		Instrument:      s.Instrument,
		Timeframe:       s.Timeframe,
		WindowStartTs:   s.WindowStartTs,
		WindowEndTs:     s.WindowEndTs,
		LiqBuyVolume:    s.LiqBuyVolume,
		LiqSellVolume:   s.LiqSellVolume,
		LiqTotalVolume:  s.LiqTotalVolume,
		LiqCount:        s.LiqCount,
		MarkPriceOpen:   s.MarkPriceOpen,
		MarkPriceHigh:   s.MarkPriceHigh,
		MarkPriceLow:    s.MarkPriceLow,
		MarkPriceClose:  s.MarkPriceClose,
		FundingRateAvg:  s.FundingRateAvg,
		FundingRateLast: s.FundingRateLast,
		SeqFirst:        s.SeqFirst,
		SeqLast:         s.SeqLast,
		IsClosed:        s.IsClosed,
	}
}

// ProtoToWireDTOStatsWindowClosedV1 converts the protobuf message to the wire DTO.
func ProtoToWireDTOStatsWindowClosedV1(in *aggregationv1.StatsWindowClosedV1) AggregationStatsWindowClosedV1 {
	if in == nil {
		return AggregationStatsWindowClosedV1{}
	}
	return AggregationStatsWindowClosedV1{
		Stats: AggregationStatsWindowV1{
			Venue:           in.GetVenue(),
			Instrument:      in.GetInstrument(),
			Timeframe:       in.GetTimeframe(),
			WindowStartTs:   in.GetWindowStartTs(),
			WindowEndTs:     in.GetWindowEndTs(),
			LiqBuyVolume:    in.GetLiqBuyVolume(),
			LiqSellVolume:   in.GetLiqSellVolume(),
			LiqTotalVolume:  in.GetLiqTotalVolume(),
			LiqCount:        in.GetLiqCount(),
			MarkPriceOpen:   in.GetMarkPriceOpen(),
			MarkPriceHigh:   in.GetMarkPriceHigh(),
			MarkPriceLow:    in.GetMarkPriceLow(),
			MarkPriceClose:  in.GetMarkPriceClose(),
			FundingRateAvg:  in.GetFundingRateAvg(),
			FundingRateLast: in.GetFundingRateLast(),
			SeqFirst:        in.GetSeqFirst(),
			SeqLast:         in.GetSeqLast(),
			IsClosed:        in.GetIsClosed(),
		},
	}
}

// WireDTOToProtoSnapshotV1 converts the wire DTO to the protobuf message.
func WireDTOToProtoSnapshotV1(in AggregationSnapshotV1) *aggregationv1.OrderBookSnapshotV1 {
	bids := make([]*aggregationv1.OrderBookLevelV1, len(in.Bids))
	for i, b := range in.Bids {
		bids[i] = &aggregationv1.OrderBookLevelV1{Price: b.Price, Quantity: b.Quantity}
	}
	asks := make([]*aggregationv1.OrderBookLevelV1, len(in.Asks))
	for i, a := range in.Asks {
		asks[i] = &aggregationv1.OrderBookLevelV1{Price: a.Price, Quantity: a.Quantity}
	}
	return &aggregationv1.OrderBookSnapshotV1{
		Venue:      in.Venue,
		Instrument: in.Instrument,
		Seq:        in.Seq,
		Bids:       bids,
		Asks:       asks,
	}
}

// ProtoToWireDTOSnapshotV1 converts the protobuf message to the wire DTO.
func ProtoToWireDTOSnapshotV1(in *aggregationv1.OrderBookSnapshotV1) AggregationSnapshotV1 {
	if in == nil {
		return AggregationSnapshotV1{}
	}
	bids := make([]AggregationOrderBookLevelV1, len(in.GetBids()))
	for i, b := range in.GetBids() {
		bids[i] = AggregationOrderBookLevelV1{Price: b.GetPrice(), Quantity: b.GetQuantity()}
	}
	asks := make([]AggregationOrderBookLevelV1, len(in.GetAsks()))
	for i, a := range in.GetAsks() {
		asks[i] = AggregationOrderBookLevelV1{Price: a.GetPrice(), Quantity: a.GetQuantity()}
	}
	return AggregationSnapshotV1{
		Venue:      in.GetVenue(),
		Instrument: in.GetInstrument(),
		Seq:        in.GetSeq(),
		Bids:       bids,
		Asks:       asks,
	}
}

// WireDTOToProtoInconsistencyV1 converts the wire DTO to the protobuf message.
func WireDTOToProtoInconsistencyV1(in AggregationOrderBookInconsistencyV1) *aggregationv1.OrderBookInconsistencyV1 {
	return &aggregationv1.OrderBookInconsistencyV1{
		Venue:      in.Venue,
		Instrument: in.Instrument,
		Seq:        in.Seq,
		Reason:     in.Reason,
	}
}

// ProtoToWireDTOInconsistencyV1 converts the protobuf message to the wire DTO.
func ProtoToWireDTOInconsistencyV1(in *aggregationv1.OrderBookInconsistencyV1) AggregationOrderBookInconsistencyV1 {
	if in == nil {
		return AggregationOrderBookInconsistencyV1{}
	}
	return AggregationOrderBookInconsistencyV1{
		Venue:      in.GetVenue(),
		Instrument: in.GetInstrument(),
		Seq:        in.GetSeq(),
		Reason:     in.GetReason(),
	}
}
