package contracts

import (
	aggregationv1 "github.com/market-raccoon/internal/shared/proto/gen/aggregation/v1"
	aggregationv2 "github.com/market-raccoon/internal/shared/proto/gen/aggregation/v2"
)

const (
	minProtoInt32 = int64(-1 << 31)
	maxProtoInt32 = int64(1<<31 - 1)
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
		WindowMs:        s.WindowMs,
		TsIngestMs:      s.TsIngestMs,
		QualityFlags:    s.QualityFlags,
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
			WindowMs:        in.GetWindowMs(),
			TsIngestMs:      in.GetTsIngestMs(),
			QualityFlags:    in.GetQualityFlags(),
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

// WireDTOToProtoTapeWindowV1 converts the wire DTO to the protobuf message.
func WireDTOToProtoTapeWindowV1(in AggregationTapeV1) *aggregationv1.TapeWindowV1 {
	return &aggregationv1.TapeWindowV1{
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
}

// ProtoToWireDTOTapeWindowV1 converts the protobuf message to the wire DTO.
func ProtoToWireDTOTapeWindowV1(in *aggregationv1.TapeWindowV1) AggregationTapeV1 {
	if in == nil {
		return AggregationTapeV1{}
	}
	return AggregationTapeV1{
		Venue:         in.GetVenue(),
		Instrument:    in.GetInstrument(),
		Timeframe:     in.GetTimeframe(),
		WindowStartTs: in.GetWindowStartTs(),
		WindowEndTs:   in.GetWindowEndTs(),
		TradeCount:    in.GetTradeCount(),
		BuyCount:      in.GetBuyCount(),
		SellCount:     in.GetSellCount(),
		BuyVolume:     in.GetBuyVolume(),
		SellVolume:    in.GetSellVolume(),
		TotalVolume:   in.GetTotalVolume(),
		BuyNotional:   in.GetBuyNotional(),
		SellNotional:  in.GetSellNotional(),
		VwapPrice:     in.GetVwapPrice(),
		MaxPrice:      in.GetMaxPrice(),
		MinPrice:      in.GetMinPrice(),
		LastPrice:     in.GetLastPrice(),
		MaxTradeSize:  in.GetMaxTradeSize(),
		Rate:          in.GetRate(),
		Imbalance:     in.GetImbalance(),
		IsBurst:       in.GetIsBurst(),
		Seq:           in.GetSeq(),
		TsIngestMs:    in.GetTsIngestMs(),
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

// WireDTOToProtoSnapshotV2 converts the wire DTO to the protobuf message.
func WireDTOToProtoSnapshotV2(in AggregationSnapshotV2) *aggregationv2.OrderBookSnapshotV2 {
	bids := make([]*aggregationv2.OrderBookLevelV1, len(in.Bids))
	for i, b := range in.Bids {
		bids[i] = &aggregationv2.OrderBookLevelV1{Price: b.Price, Quantity: b.Quantity}
	}
	asks := make([]*aggregationv2.OrderBookLevelV1, len(in.Asks))
	for i, a := range in.Asks {
		asks[i] = &aggregationv2.OrderBookLevelV1{Price: a.Price, Quantity: a.Quantity}
	}
	return &aggregationv2.OrderBookSnapshotV2{
		Venue:        in.Venue,
		Instrument:   in.Instrument,
		Seq:          in.Seq,
		Bids:         bids,
		Asks:         asks,
		BestBidPrice: in.BestBidPrice,
		BestAskPrice: in.BestAskPrice,
		SpreadBps:    in.SpreadBPS,
		Checksum:     in.Checksum,
		TsIngestMs:   in.TsIngestMs,
		BidCount:     boundedInt32(in.BidCount),
		AskCount:     boundedInt32(in.AskCount),
		DepthCap:     boundedInt32(in.DepthCap),
		Version:      boundedInt32(in.Version),
	}
}

// ProtoToWireDTOSnapshotV2 converts the protobuf message to the wire DTO.
func ProtoToWireDTOSnapshotV2(in *aggregationv2.OrderBookSnapshotV2) AggregationSnapshotV2 {
	if in == nil {
		return AggregationSnapshotV2{}
	}
	bids := make([]AggregationOrderBookLevelV1, len(in.GetBids()))
	for i, b := range in.GetBids() {
		bids[i] = AggregationOrderBookLevelV1{Price: b.GetPrice(), Quantity: b.GetQuantity()}
	}
	asks := make([]AggregationOrderBookLevelV1, len(in.GetAsks()))
	for i, a := range in.GetAsks() {
		asks[i] = AggregationOrderBookLevelV1{Price: a.GetPrice(), Quantity: a.GetQuantity()}
	}
	return AggregationSnapshotV2{
		Venue:        in.GetVenue(),
		Instrument:   in.GetInstrument(),
		Seq:          in.GetSeq(),
		Bids:         bids,
		Asks:         asks,
		BestBidPrice: in.GetBestBidPrice(),
		BestAskPrice: in.GetBestAskPrice(),
		SpreadBPS:    in.GetSpreadBps(),
		Checksum:     in.GetChecksum(),
		TsIngestMs:   in.GetTsIngestMs(),
		BidCount:     int(in.GetBidCount()),
		AskCount:     int(in.GetAskCount()),
		DepthCap:     int(in.GetDepthCap()),
		Version:      int(in.GetVersion()),
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

func boundedInt32(v int) int32 {
	iv := int64(v)
	switch {
	case iv > maxProtoInt32:
		return int32(maxProtoInt32)
	case iv < minProtoInt32:
		return int32(minProtoInt32)
	default:
		return int32(iv)
	}
}
