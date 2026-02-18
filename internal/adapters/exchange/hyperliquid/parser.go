package hyperliquid

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
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

// ParseMeta carries parser diagnostics for observability.
type ParseMeta struct {
	EventType  string
	SkipReason string
	Problem    *problem.Problem
	WSStream   string
	Ticker     string
}

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
	if tradeID == "" {
		tradeID = fmt.Sprintf("%d", entry.Tid)
	}
	if strings.TrimSpace(tradeID) == "" || tradeID == "0" {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "hyperliquid trades: trade id is empty")
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
		Payload: domain.TradeTickV1{
			Price:     price,
			Size:      size,
			Side:      side,
			TradeID:   tradeID,
			Timestamp: tsExchange,
		},
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
			Bids:      bids,
			Asks:      asks,
			FirstID:   seq,
			FinalID:   seq,
			Timestamp: tsExchange,
		},
	}, false, nil
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
	return fmt.Sprintf("venue=%s|instrument=%s|trade_id=%s", venue, instrument, tradeID)
}

func buildDepthIdempotencyKey(venue, instrument string, finalUpdateID int64) string {
	return fmt.Sprintf("venue=%s|instrument=%s|final_update_id=%d", venue, instrument, finalUpdateID)
}

func buildInstrumentMetadata(venueSymbol, canonical, marketType string) map[string]string {
	meta := map[string]string{
		"instrument_venue_symbol": strings.ToUpper(strings.TrimSpace(venueSymbol)),
		"instrument_canonical":    canonical,
		"instrument_market_type":  marketType,
	}
	if venueSymbol != "" {
		meta["instrument_pair"] = strings.ToUpper(strings.TrimSpace(venueSymbol)) + "-USD"
	}
	return meta
}

func skipReasonFromProblem(p *problem.Problem) string {
	if p != nil {
		return "parse_error"
	}
	return ""
}

func normalizeMarketType(raw string) string {
	mt, p := domain.NewMarketType(raw)
	if p != nil {
		return domain.MarketTypeUSDMFutures.String()
	}
	return mt.String()
}
