package marketmodel

import (
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

func NormalizeTrade(adapter ExchangeAdapter, symbol Symbol, in Trade, fallbackTS ServerTS) (Trade, *problem.Problem) {
	if adapter == nil {
		adapter = defaultAdapterForVenue("UNKNOWN")
	}
	rule := adapter.Precision(symbol)
	side, p := adapter.NormalizeSide(string(in.Side))
	if p != nil {
		return Trade{}, p
	}
	price := rule.NormalizePrice(in.Price)
	size := rule.NormalizeSize(in.Size)
	out := Trade{
		Price:     price,
		Size:      size,
		Side:      string(side),
		TradeID:   strings.TrimSpace(in.TradeID),
		Timestamp: adapter.NormalizeTimestamp(in.Timestamp, fallbackTS).UnixMilli(),
	}
	if p := out.Validate(); p != nil {
		return Trade{}, p
	}
	return out, nil
}

func NormalizeBookDelta(adapter ExchangeAdapter, symbol Symbol, in BookDelta, fallbackTS ServerTS) (BookDelta, *problem.Problem) {
	if adapter == nil {
		adapter = defaultAdapterForVenue("UNKNOWN")
	}
	rule := adapter.Precision(symbol)
	nb := normalizeLevelsWithRule(in.Bids, rule)
	na := normalizeLevelsWithRule(in.Asks, rule)
	nb, na, p := NormalizeBookOrdering(nb, na, true)
	if p != nil {
		return BookDelta{}, p
	}
	out := BookDelta{
		Bids:       nb,
		Asks:       na,
		FirstID:    in.FirstID,
		FinalID:    in.FinalID,
		PrevFinal:  in.PrevFinal,
		Timestamp:  adapter.NormalizeTimestamp(in.Timestamp, fallbackTS).UnixMilli(),
		IsSnapshot: in.IsSnapshot,
	}
	if out.IsSnapshot {
		if out.FirstID == 0 {
			out.FirstID = out.FinalID
		}
		if out.FinalID == 0 {
			out.FinalID = out.FirstID
		}
	}
	if p := out.Validate(); p != nil {
		return BookDelta{}, p
	}
	return out, nil
}

func NormalizeSnapshot(adapter ExchangeAdapter, symbol Symbol, in BookSnapshot, fallbackTS ServerTS) (BookSnapshot, *problem.Problem) {
	if adapter == nil {
		adapter = defaultAdapterForVenue("UNKNOWN")
	}
	rule := adapter.Precision(symbol)
	nb := normalizeLevelsWithRule(in.Bids, rule)
	na := normalizeLevelsWithRule(in.Asks, rule)
	nb, na, p := NormalizeBookOrdering(nb, na, false)
	if p != nil {
		return BookSnapshot{}, p
	}
	out := BookSnapshot{
		Bids:      nb,
		Asks:      na,
		Timestamp: adapter.NormalizeTimestamp(in.Timestamp, fallbackTS).UnixMilli(),
	}
	if p := out.Validate(); p != nil {
		return BookSnapshot{}, p
	}
	return out, nil
}

func normalizeLevelsWithRule(levels []Level, rule PrecisionRule) []Level {
	if len(levels) == 0 {
		return nil
	}
	out := make([]Level, 0, len(levels))
	for _, lvl := range levels {
		out = append(out, Level{
			Price: rule.NormalizePrice(lvl.Price),
			Size:  rule.NormalizeSize(lvl.Size),
		})
	}
	return out
}

func ChannelFromEventType(eventType string) Channel {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "marketdata.trade":
		return ChannelTrade
	case "marketdata.bookdelta":
		return ChannelBookDelta
	case "marketdata.booksnapshot":
		return ChannelBookSnapshot
	case "marketdata.markprice":
		return ChannelMarkPrice
	case "marketdata.liquidation":
		return ChannelLiquidation
	case "aggregation.candle":
		return ChannelCandle
	case "aggregation.stats":
		return ChannelStats
	case "insights.microstructure_evidence", "insights.regime_evidence", "evidence.microstructure_evidence", "liquidity.evidence":
		return ChannelEvidence
	case "signal.event", "signal.composite":
		return ChannelSignal
	default:
		return ""
	}
}
