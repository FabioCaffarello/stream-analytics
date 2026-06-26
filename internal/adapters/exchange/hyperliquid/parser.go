package hyperliquid

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	common "github.com/FabioCaffarello/stream-analytics/internal/adapters/exchange/common"
	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

const VenueHyperLiquid = "HYPERLIQUID"

type wsResponse struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

type tradeEntry struct {
	Coin string `json:"coin"`
	Side string `json:"side"`
	Px   string `json:"px"`
	Sz   string `json:"sz"`
	Time int64  `json:"time"`
	Hash string `json:"hash"`
	Tid  int64  `json:"tid"`
}

type l2BookData struct {
	Coin   string         `json:"coin"`
	Time   int64          `json:"time"`
	Levels [2][]bookLevel `json:"levels"`
}

type bookLevel struct {
	Px string `json:"px"`
	Sz string `json:"sz"`
	N  int    `json:"n"`
}

// ParseMeta is an alias for the shared parser diagnostics type.
type ParseMeta = common.ParseMeta

// ParseMessage parses HyperLiquid payload.
func ParseMessage(data []byte, recvAt time.Time) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeUSDMFutures.String())
	return req, skip, meta.Problem
}

// ParseMessageWithMeta parses HyperLiquid payload and returns parse metadata.
func ParseMessageWithMeta(data []byte, recvAt time.Time) (app.IngestRequest, bool, ParseMeta) {
	return ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeUSDMFutures.String())
}

// ParseMessageForMarketType parses HyperLiquid payload for an explicit market type.
func ParseMessageForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, marketType)
	return req, skip, meta.Problem
}

// ParseMessageWithMetaForMarketType parses HyperLiquid payload and returns parse metadata.
func ParseMessageWithMetaForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, ParseMeta) {
	marketType = normalizeMarketType(marketType)
	meta := ParseMeta{}

	var msg wsResponse
	if err := json.Unmarshal(data, &msg); err != nil {
		meta.SkipReason = "parse_error"
		meta.Problem = problem.Wrap(err, problem.ValidationFailed, "hyperliquid parser: invalid JSON payload")
		return app.IngestRequest{}, true, meta
	}

	meta.EventType = strings.TrimSpace(msg.Channel)
	meta.WSStream = meta.EventType
	if meta.EventType == "" {
		meta.SkipReason = "control_event"
		return app.IngestRequest{}, true, meta
	}

	switch meta.EventType {
	case "trades":
		req, skip, p := parseTrades(msg.Data, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		meta.Ticker = req.Instrument
		return req, skip, meta
	case "l2Book":
		req, skip, p := parseL2Book(msg.Data, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		meta.Ticker = req.Instrument
		return req, skip, meta
	case "subscriptionResponse", "pong":
		meta.SkipReason = "control_event"
		return app.IngestRequest{}, true, meta
	default:
		meta.SkipReason = "unsupported_event"
		return app.IngestRequest{}, true, meta
	}
}

func parseTrades(data json.RawMessage, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var entries []tradeEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "hyperliquid trades: invalid payload")
	}
	if len(entries) == 0 {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "hyperliquid trades: data must not be empty")
	}
	entry := entries[0]
	instrument, p := instrumentFromCoin(entry.Coin)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	side, p := normalizeSide(entry.Side)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	price, err := strconv.ParseFloat(entry.Px, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "hyperliquid trades: invalid price")
	}
	size, err := strconv.ParseFloat(entry.Sz, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "hyperliquid trades: invalid size")
	}
	tsExchange := entry.Time
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}
	tradeID := strings.TrimSpace(entry.Hash)
	if tradeID == "" || isZeroHash(tradeID) {
		tradeID = common.TradeIDStringFromAny(entry.Tid)
	}
	if strings.TrimSpace(tradeID) == "" || tradeID == "0" {
		metrics.IncMRTradeBadValue(VenueHyperLiquid, "empty_trade_id")
		return app.IngestRequest{}, true, nil
	}
	trade := domain.TradeTickV1{
		Price:     price,
		Size:      size,
		Side:      side,
		TradeID:   tradeID,
		Timestamp: tsExchange,
	}
	if p := trade.Validate(); p != nil {
		metrics.IncMRTradeBadValue(
			VenueHyperLiquid,
			common.ClassifyTradeValidationReason(trade.Price, trade.Size, trade.Side, trade.TradeID, trade.Timestamp),
		)
		return app.IngestRequest{}, true, nil
	}
	metrics.IncMRTradeIngest(VenueHyperLiquid)
	if recvTs := recvAt.UnixMilli(); tsExchange > 0 && recvTs > tsExchange {
		metrics.ObserveMRTradeLatency(VenueHyperLiquid, float64(recvTs-tsExchange)/1000.0)
	}

	return app.IngestRequest{
		Venue:      VenueHyperLiquid,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.trade",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildTradeIdempotencyKey(
			VenueHyperLiquid,
			instrument,
			tradeID,
		),
		Metadata: buildInstrumentMetadata(entry.Coin, instrument, marketType),
		Payload:  trade,
	}, false, nil
}

func parseL2Book(data json.RawMessage, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg l2BookData
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "hyperliquid l2Book: invalid payload")
	}
	instrument, p := instrumentFromCoin(msg.Coin)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	bids, p := parseBookLevels(msg.Levels[0])
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	asks, p := parseBookLevels(msg.Levels[1])
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	bids, asks = normalizeBookSides(bids, asks)
	if len(bids) > 0 && len(asks) > 0 {
		bestBid := maxPrice(bids)
		bestAsk := minPrice(asks)
		if bestBid >= bestAsk {
			return app.IngestRequest{}, true, problem.Newf(
				problem.ValidationFailed,
				"hyperliquid l2Book: crossed snapshot best bid %.8f >= best ask %.8f",
				bestBid,
				bestAsk,
			)
		}
	}
	tsExchange := msg.Time
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}
	seq := msg.Time
	if seq <= 0 {
		seq = tsExchange
	}

	return app.IngestRequest{
		Venue:      VenueHyperLiquid,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.bookdelta",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildDepthIdempotencyKey(
			VenueHyperLiquid,
			instrument,
			seq,
		),
		Metadata: buildInstrumentMetadata(msg.Coin, instrument, marketType),
		Payload: domain.BookDeltaV1{
			Bids:       bids,
			Asks:       asks,
			FirstID:    seq,
			FinalID:    seq,
			Timestamp:  tsExchange,
			IsSnapshot: true, // HyperLiquid always sends full L2 snapshots
		},
	}, false, nil
}

// normalizeBookSides ensures sides are ordered as bids/asks.
// Hyperliquid may emit l2Book levels with reversed side ordering in some feeds.
func normalizeBookSides(bids, asks []domain.PriceLevel) ([]domain.PriceLevel, []domain.PriceLevel) {
	if len(bids) == 0 || len(asks) == 0 {
		return bids, asks
	}

	bestBid := maxPrice(bids)
	bestAsk := minPrice(asks)
	if bestBid < bestAsk {
		return bids, asks
	}

	swappedBestBid := maxPrice(asks)
	swappedBestAsk := minPrice(bids)
	if swappedBestBid < swappedBestAsk {
		return asks, bids
	}
	return bids, asks
}

func maxPrice(levels []domain.PriceLevel) float64 {
	if len(levels) == 0 {
		return 0
	}
	max := levels[0].Price
	for i := 1; i < len(levels); i++ {
		if levels[i].Price > max {
			max = levels[i].Price
		}
	}
	return max
}

func minPrice(levels []domain.PriceLevel) float64 {
	if len(levels) == 0 {
		return 0
	}
	min := levels[0].Price
	for i := 1; i < len(levels); i++ {
		if levels[i].Price < min {
			min = levels[i].Price
		}
	}
	return min
}

func parseBookLevels(raw []bookLevel) ([]domain.PriceLevel, *problem.Problem) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]domain.PriceLevel, 0, len(raw))
	for _, level := range raw {
		price, err := strconv.ParseFloat(level.Px, 64)
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "hyperliquid l2Book: invalid level price")
		}
		size, err := strconv.ParseFloat(level.Sz, 64)
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "hyperliquid l2Book: invalid level size")
		}
		out = append(out, domain.PriceLevel{Price: price, Size: size})
	}
	return out, nil
}

func instrumentFromCoin(rawCoin string) (string, *problem.Problem) {
	coin := naming.CanonicalInstrument(rawCoin)
	if coin == "" {
		return "", problem.New(problem.ValidationFailed, "hyperliquid: coin is empty")
	}
	instrument := naming.CanonicalInstrument(coin + "USD")
	if instrument == "" {
		return "", problem.New(problem.ValidationFailed, "hyperliquid: instrument is empty")
	}
	return instrument, nil
}

func normalizeSide(side string) (string, *problem.Problem) {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "B", "BUY":
		return "buy", nil
	case "A", "S", "SELL", "ASK":
		return "sell", nil
	default:
		return "", problem.Newf(problem.ValidationFailed, "hyperliquid: unsupported side %q", side)
	}
}

func buildTradeIdempotencyKey(venue, instrument, tradeID string) string {
	return common.BuildTradeIdempotencyKey(venue, instrument, tradeID)
}

func buildDepthIdempotencyKey(venue, instrument string, finalUpdateID int64) string {
	return common.BuildDepthIdempotencyKey(venue, instrument, finalUpdateID)
}

func buildInstrumentMetadata(venueSymbol, canonical, marketType string) map[string]string {
	return common.BuildInstrumentMetadata(venueSymbol, canonical, marketType, func(vs string) string {
		vs = strings.ToUpper(strings.TrimSpace(vs))
		if vs != "" {
			return vs + "-USD"
		}
		return ""
	})
}

func skipReasonFromProblem(p *problem.Problem) string {
	return common.SkipReasonFromProblem(p)
}

func isZeroHash(s string) bool {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '0' {
			return false
		}
	}
	return true
}

// allMidsData is the envelope for the allMids broadcast channel.
type allMidsData struct {
	Mids map[string]string `json:"mids"`
}

// ParseAllMids returns a batch parser for the HyperLiquid allMids broadcast.
// The returned function produces one MarkPriceTickV1 IngestRequest per
// subscribed coin. Messages for other channels return nil (not handled).
func ParseAllMids(subscribedCoins map[string]bool, marketType string) func(data []byte, recvAt time.Time) ([]app.IngestRequest, error) {
	marketType = normalizeMarketType(marketType)
	return func(data []byte, recvAt time.Time) ([]app.IngestRequest, error) {
		var msg wsResponse
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "hyperliquid allMids: invalid JSON")
		}
		if msg.Channel != "allMids" {
			return nil, nil // not handled — fall through to single parser
		}

		var payload allMidsData
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "hyperliquid allMids: invalid data")
		}

		tsExchange := recvAt.UnixMilli()
		reqs := make([]app.IngestRequest, 0, len(subscribedCoins))
		for coin, midStr := range payload.Mids {
			upperCoin := strings.ToUpper(strings.TrimSpace(coin))
			if !subscribedCoins[upperCoin] {
				continue
			}
			mid, err := strconv.ParseFloat(midStr, 64)
			if err != nil {
				continue // skip unparseable prices
			}
			instrument, p := instrumentFromCoin(upperCoin)
			if p != nil {
				continue
			}
			reqs = append(reqs, app.IngestRequest{
				Venue:          VenueHyperLiquid,
				Instrument:     instrument,
				MarketType:     marketType,
				EventType:      "marketdata.markprice",
				Version:        1,
				TsExchange:     tsExchange,
				IdempotencyKey: "venue=" + VenueHyperLiquid + "|instrument=" + instrument + "|markprice|ts=" + strconv.FormatInt(tsExchange, 10),
				Metadata:       buildInstrumentMetadata(upperCoin, instrument, marketType),
				Payload: domain.MarkPriceTickV1{
					MarkPrice:   mid,
					IndexPrice:  0,
					FundingRate: 0,
					Timestamp:   tsExchange,
				},
			})
		}
		return reqs, nil
	}
}

func normalizeMarketType(raw string) string {
	return common.NormalizeMarketTypeFutures(raw)
}
