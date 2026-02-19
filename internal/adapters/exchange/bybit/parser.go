// Package bybit provides Bybit-specific market-data adapter helpers.
package bybit

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	// VenueBybit is the canonical venue identifier emitted for Bybit events.
	VenueBybit = "BYBIT"
)

type tradeEnvelope struct {
	Topic string      `json:"topic"`
	Type  string      `json:"type"`
	TsMs  int64       `json:"ts"`
	Data  []tradeData `json:"data"`
}

type tradeData struct {
	TradeTimeMs int64  `json:"T"`
	Symbol      string `json:"s"`
	Side        string `json:"S"`
	PriceRaw    string `json:"p"`
	SizeRaw     string `json:"v"`
	TradeID     string `json:"i"`
}

type orderBookEnvelope struct {
	Topic string        `json:"topic"`
	Type  string        `json:"type"`
	TsMs  int64         `json:"ts"`
	Data  orderBookData `json:"data"`
}

type orderBookData struct {
	Symbol    string     `json:"s"`
	BidsRaw   [][]string `json:"b"`
	AsksRaw   [][]string `json:"a"`
	UpdateID  int64      `json:"u"`
	Sequence  int64      `json:"seq"`
	PrevFinal int64      `json:"pu"`
	TsMs      int64      `json:"cts"`
}

type tickerEnvelope struct {
	Topic string     `json:"topic"`
	Type  string     `json:"type"`
	TsMs  int64      `json:"ts"`
	Data  tickerData `json:"data"`
}

type tickerData struct {
	Symbol      string `json:"symbol"`
	MarkPrice   string `json:"markPrice"`
	IndexPrice  string `json:"indexPrice"`
	FundingRate string `json:"fundingRate"`
}

type liquidationEnvelope struct {
	Topic string            `json:"topic"`
	Type  string            `json:"type"`
	TsMs  int64             `json:"ts"`
	Data  []liquidationData `json:"data"`
}

type liquidationData struct {
	Symbol      string `json:"s"`
	Side        string `json:"S"`
	PriceRaw    string `json:"p"`
	SizeRaw     string `json:"v"`
	UpdatedTime int64  `json:"T"`
}

// ParseMeta carries parser diagnostics for observability.
type ParseMeta struct {
	EventType  string
	SkipReason string
	Problem    *problem.Problem
	WSStream   string
	Ticker     string
}

// ParseMessage parses Bybit WS payload and maps supported messages to app.IngestRequest.
func ParseMessage(data []byte, recvAt time.Time) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeSpot.String())
	return req, skip, meta.Problem
}

// ParseMessageWithMeta parses Bybit payload and returns telemetry metadata.
func ParseMessageWithMeta(data []byte, recvAt time.Time) (app.IngestRequest, bool, ParseMeta) {
	return ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeSpot.String())
}

// ParseMessageForMarketType parses Bybit payload for an explicit market type.
func ParseMessageForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, marketType)
	return req, skip, meta.Problem
}

// ParseMessageWithMetaForMarketType parses Bybit payload and returns telemetry metadata.
func ParseMessageWithMetaForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, ParseMeta) {
	marketType = normalizeMarketType(marketType)
	meta := ParseMeta{}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		meta.SkipReason = "parse_error"
		meta.Problem = problem.Wrap(err, problem.ValidationFailed, "bybit parser: invalid JSON payload")
		return app.IngestRequest{}, true, meta
	}

	topic := rawString(root, "topic")
	op := strings.ToLower(strings.TrimSpace(rawString(root, "op")))
	msgType := strings.TrimSpace(rawString(root, "type"))
	meta.WSStream = topic
	meta.EventType = deriveEventType(topic, msgType, op)
	meta.Ticker = tickerFromTopic(topic)

	if isControlMessage(topic, op, root) {
		if p := controlMessageProblem(op, root); p != nil {
			meta.Problem = p
			meta.SkipReason = skipReasonFromProblem(p)
			return app.IngestRequest{}, true, meta
		}
		meta.SkipReason = "control_event"
		return app.IngestRequest{}, true, meta
	}

	req, skip, p := parseByTopic(topic, data, recvAt, marketType, msgType, op)
	meta.Problem = p
	meta.SkipReason = skipReasonFromProblem(p)
	if skip && meta.SkipReason == "" {
		meta.SkipReason = defaultSkipReason(topic)
	}
	if strings.TrimSpace(topic) != "" && req.Metadata != nil {
		req.Metadata["ws_stream"] = topic
	}
	if req.Instrument != "" {
		meta.Ticker = req.Instrument
	}
	if p != nil &&
		!strings.HasPrefix(topic, "publicTrade.") &&
		!strings.HasPrefix(topic, "orderbook.") &&
		!strings.HasPrefix(topic, "tickers.") &&
		!strings.HasPrefix(topic, "liquidation.") &&
		!strings.HasPrefix(topic, "allLiquidation.") {
		meta.SkipReason = "unsupported_event"
	}
	return req, skip, meta
}

func parseByTopic(
	topic string,
	data []byte,
	recvAt time.Time,
	marketType string,
	msgType string,
	op string,
) (app.IngestRequest, bool, *problem.Problem) {
	switch {
	case strings.HasPrefix(topic, "publicTrade."):
		return parseTrade(data, recvAt, marketType)
	case strings.HasPrefix(topic, "orderbook."):
		return parseOrderBookDelta(data, recvAt, marketType)
	case strings.HasPrefix(topic, "tickers."):
		return parseMarkPrice(data, recvAt, marketType)
	case strings.HasPrefix(topic, "liquidation."), strings.HasPrefix(topic, "allLiquidation."):
		return parseLiquidation(data, recvAt, marketType)
	default:
		return app.IngestRequest{}, true, unsupportedEventProblem(topic, msgType, op)
	}
}

func isControlMessage(topic, op string, root map[string]json.RawMessage) bool {
	if topic != "" {
		return false
	}
	if op == "ping" || op == "pong" || op == "subscribe" {
		return true
	}
	return hasKey(root, "success") && op == ""
}

func controlMessageProblem(op string, root map[string]json.RawMessage) *problem.Problem {
	if strings.ToLower(strings.TrimSpace(op)) != "subscribe" {
		return nil
	}
	success, ok := rawBool(root, "success")
	if !ok || success {
		return nil
	}
	p := problem.New(problem.ValidationFailed, "bybit subscribe rejected")
	p = problem.WithDetail(p, "reason", "subscribe_rejected")
	if msg := rawString(root, "ret_msg"); msg != "" {
		p = problem.WithDetail(p, "ret_msg", msg)
	}
	if code, ok := rawInt64(root, "ret_code"); ok {
		p = problem.WithDetail(p, "ret_code", code)
	}
	return p
}

func parseTrade(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg tradeEnvelope
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "bybit trade: invalid payload")
	}
	if len(msg.Data) == 0 {
		return app.IngestRequest{}, true, problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit trade: data must not be empty"),
			"reason", "empty_trade_data",
		)
	}
	td := msg.Data[0]

	symbol := strings.TrimSpace(td.Symbol)
	if symbol == "" {
		symbol = symbolFromTopic(msg.Topic)
	}
	instrument := naming.CanonicalInstrument(symbol)
	if instrument == "" {
		return app.IngestRequest{}, true, problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit trade: symbol is empty"),
			"reason", "missing_symbol",
		)
	}

	price, err := strconv.ParseFloat(td.PriceRaw, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "bybit trade: invalid price")
	}
	size, err := strconv.ParseFloat(td.SizeRaw, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "bybit trade: invalid size")
	}

	side, p := normalizeSide(td.Side)
	if p != nil {
		return app.IngestRequest{}, true, p
	}

	tradeID := strings.TrimSpace(td.TradeID)
	if tradeID == "" {
		return app.IngestRequest{}, true, problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit trade: trade id is empty"),
			"reason", "missing_trade_id",
		)
	}

	tsExchange := td.TradeTimeMs
	if tsExchange <= 0 {
		tsExchange = msg.TsMs
	}
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}

	return app.IngestRequest{
		Venue:      VenueBybit,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.trade",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildTradeIdempotencyKey(
			VenueBybit,
			instrument,
			tradeID,
		),
		Metadata: buildInstrumentMetadata(symbol, instrument, marketType),
		Payload: domain.TradeTickV1{
			Price:     price,
			Size:      size,
			Side:      side,
			TradeID:   tradeID,
			Timestamp: tsExchange,
		},
	}, false, nil
}

func parseOrderBookDelta(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg orderBookEnvelope
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "bybit orderbook: invalid payload")
	}

	symbol := strings.TrimSpace(msg.Data.Symbol)
	if symbol == "" {
		symbol = symbolFromTopic(msg.Topic)
	}
	instrument := naming.CanonicalInstrument(symbol)
	if instrument == "" {
		return app.IngestRequest{}, true, problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit orderbook: symbol is empty"),
			"reason", "missing_symbol",
		)
	}

	bids, p := parseLevels(msg.Data.BidsRaw)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	asks, p := parseLevels(msg.Data.AsksRaw)
	if p != nil {
		return app.IngestRequest{}, true, p
	}

	firstID := msg.Data.Sequence
	finalID := msg.Data.UpdateID
	if firstID <= 0 {
		firstID = finalID
	}
	if finalID <= 0 {
		finalID = firstID
	}
	if firstID <= 0 || finalID <= 0 {
		return app.IngestRequest{}, true, problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit orderbook: update ids must be > 0"),
			"reason", "missing_update_id",
		)
	}

	prevFinal := msg.Data.PrevFinal
	if prevFinal <= 0 && finalID > 1 {
		prevFinal = finalID - 1
	}

	tsExchange := msg.Data.TsMs
	if tsExchange <= 0 {
		tsExchange = msg.TsMs
	}
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}

	return app.IngestRequest{
		Venue:      VenueBybit,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.bookdelta",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildDepthIdempotencyKey(
			VenueBybit,
			instrument,
			finalID,
		),
		Metadata: buildInstrumentMetadata(symbol, instrument, marketType),
		Payload: domain.BookDeltaV1{
			Bids:      bids,
			Asks:      asks,
			FirstID:   firstID,
			FinalID:   finalID,
			PrevFinal: prevFinal,
			Timestamp: tsExchange,
		},
	}, false, nil
}

func parseMarkPrice(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg tickerEnvelope
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "bybit ticker: invalid payload")
	}

	symbol := strings.TrimSpace(msg.Data.Symbol)
	if symbol == "" {
		symbol = symbolFromTopic(msg.Topic)
	}
	instrument := naming.CanonicalInstrument(symbol)
	if instrument == "" {
		return app.IngestRequest{}, true, problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit ticker: symbol is empty"),
			"reason", "missing_symbol",
		)
	}

	markPriceRaw := strings.TrimSpace(msg.Data.MarkPrice)
	if markPriceRaw == "" {
		// Bybit can emit ticker updates without markPrice; skip silently.
		return app.IngestRequest{}, true, nil
	}
	markPrice, err := strconv.ParseFloat(markPriceRaw, 64)
	if err != nil || math.IsNaN(markPrice) || math.IsInf(markPrice, 0) || markPrice <= 0 {
		// Non-finite/zero markPrice is non-actionable for our markprice event.
		return app.IngestRequest{}, true, nil
	}

	indexPrice := 0.0
	if strings.TrimSpace(msg.Data.IndexPrice) != "" {
		indexPrice, err = strconv.ParseFloat(msg.Data.IndexPrice, 64)
		if err != nil {
			indexPrice = 0
		}
	}
	fundingRate := 0.0
	if strings.TrimSpace(msg.Data.FundingRate) != "" {
		fundingRate, err = strconv.ParseFloat(msg.Data.FundingRate, 64)
		if err != nil {
			fundingRate = 0
		}
	}

	tsExchange := msg.TsMs
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}

	return app.IngestRequest{
		Venue:      VenueBybit,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.markprice",
		Version:    1,
		TsExchange: tsExchange,
		Metadata:   buildInstrumentMetadata(symbol, instrument, marketType),
		Payload: domain.MarkPriceTickV1{
			MarkPrice:   markPrice,
			IndexPrice:  indexPrice,
			FundingRate: fundingRate,
			Timestamp:   tsExchange,
		},
	}, false, nil
}

func parseLiquidation(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg liquidationEnvelope
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "bybit liquidation: invalid payload")
	}
	if len(msg.Data) == 0 {
		return app.IngestRequest{}, true, problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit liquidation: data must not be empty"),
			"reason", "empty_liquidation_data",
		)
	}
	ld := msg.Data[0]

	symbol := strings.TrimSpace(ld.Symbol)
	if symbol == "" {
		symbol = symbolFromTopic(msg.Topic)
	}
	instrument := naming.CanonicalInstrument(symbol)
	if instrument == "" {
		return app.IngestRequest{}, true, problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit liquidation: symbol is empty"),
			"reason", "missing_symbol",
		)
	}
	side, p := normalizeSide(ld.Side)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	price, err := strconv.ParseFloat(ld.PriceRaw, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "bybit liquidation: invalid price")
	}
	size, err := strconv.ParseFloat(ld.SizeRaw, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "bybit liquidation: invalid size")
	}

	tsExchange := ld.UpdatedTime
	if tsExchange <= 0 {
		tsExchange = msg.TsMs
	}
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}

	return app.IngestRequest{
		Venue:      VenueBybit,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.liquidation",
		Version:    1,
		TsExchange: tsExchange,
		Metadata:   buildInstrumentMetadata(symbol, instrument, marketType),
		Payload: domain.LiquidationTickV1{
			Side:      side,
			Price:     price,
			Size:      size,
			Timestamp: tsExchange,
		},
	}, false, nil
}

func buildTradeIdempotencyKey(venue, instrument, tradeID string) string {
	return fmt.Sprintf("venue=%s|instrument=%s|trade_id=%s", venue, instrument, tradeID)
}

func buildDepthIdempotencyKey(venue, instrument string, finalUpdateID int64) string {
	return fmt.Sprintf("venue=%s|instrument=%s|final_update_id=%d", venue, instrument, finalUpdateID)
}

func parseLevels(raw [][]string) ([]domain.PriceLevel, *problem.Problem) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]domain.PriceLevel, 0, len(raw))
	for _, pair := range raw {
		if len(pair) < 2 {
			return nil, problem.New(problem.ValidationFailed, "bybit orderbook: invalid level pair")
		}
		price, err := strconv.ParseFloat(pair[0], 64)
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "bybit orderbook: invalid level price")
		}
		size, err := strconv.ParseFloat(pair[1], 64)
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, "bybit orderbook: invalid level size")
		}
		out = append(out, domain.PriceLevel{Price: price, Size: size})
	}
	return out, nil
}

func normalizeSide(side string) (string, *problem.Problem) {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "buy":
		return "buy", nil
	case "sell":
		return "sell", nil
	default:
		return "", problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "bybit trade: unsupported side %q", side),
			"reason", "invalid_side",
		)
	}
}

func unsupportedEventProblem(topic, msgType, op string) *problem.Problem {
	p := problem.New(problem.ValidationFailed, "bybit parser: unsupported event type")
	p = problem.WithDetail(p, "reason", "unsupported_event_type")
	if strings.TrimSpace(topic) != "" {
		p = problem.WithDetail(p, "topic", topic)
	}
	if strings.TrimSpace(msgType) != "" {
		p = problem.WithDetail(p, "event_type", msgType)
	}
	if strings.TrimSpace(op) != "" {
		p = problem.WithDetail(p, "op", op)
	}
	return p
}

func skipReasonFromProblem(p *problem.Problem) string {
	if p != nil {
		return "parse_error"
	}
	return ""
}

func defaultSkipReason(topic string) string {
	switch {
	case strings.HasPrefix(topic, "tickers."):
		return "markprice_unavailable"
	default:
		return "skip_unspecified"
	}
}

func deriveEventType(topic, msgType, op string) string {
	switch {
	case strings.HasPrefix(topic, "publicTrade."):
		return "publicTrade"
	case strings.HasPrefix(topic, "orderbook."):
		return "orderbook"
	case strings.HasPrefix(topic, "tickers."):
		return "ticker"
	case strings.HasPrefix(topic, "liquidation."):
		return "liquidation"
	case strings.HasPrefix(topic, "allLiquidation."):
		return "liquidation"
	case strings.TrimSpace(msgType) != "":
		return msgType
	default:
		return op
	}
}

func tickerFromTopic(topic string) string {
	symbol := symbolFromTopic(topic)
	if symbol == "" {
		return ""
	}
	return naming.CanonicalInstrument(symbol)
}

func symbolFromTopic(topic string) string {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return ""
	}
	parts := strings.Split(topic, ".")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func buildInstrumentMetadata(venueSymbol, canonical, marketType string) map[string]string {
	meta := map[string]string{
		"instrument_venue_symbol": strings.ToUpper(strings.TrimSpace(venueSymbol)),
		"instrument_canonical":    canonical,
		"instrument_market_type":  marketType,
	}
	canonicalPair := canonicalPairFromBybitSymbol(venueSymbol)
	if canonicalPair == "" {
		return meta
	}
	id, p := domain.NewInstrumentIdentity(canonicalPair, venueSymbol, marketType)
	if p != nil {
		return meta
	}
	meta["instrument_pair"] = id.Canonical
	meta["instrument_base"] = id.Base
	meta["instrument_quote"] = id.Quote
	meta["instrument_market_type"] = id.MarketType.String()
	return meta
}

func canonicalPairFromBybitSymbol(symbol string) string {
	s := naming.CanonicalInstrument(symbol)
	if s == "" {
		return ""
	}
	for _, quote := range []string{
		"USDT", "USDC", "USD", "BTC", "ETH", "EUR",
	} {
		if strings.HasSuffix(s, quote) && len(s) > len(quote) {
			base := strings.TrimSuffix(s, quote)
			return base + "-" + quote
		}
	}
	return ""
}

func normalizeMarketType(raw string) string {
	mt, p := domain.NewMarketType(raw)
	if p != nil {
		return domain.MarketTypeSpot.String()
	}
	return mt.String()
}

func rawString(obj map[string]json.RawMessage, key string) string {
	raw, ok := obj[key]
	if !ok {
		return ""
	}
	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func hasKey(obj map[string]json.RawMessage, key string) bool {
	_, ok := obj[key]
	return ok
}

func rawBool(obj map[string]json.RawMessage, key string) (bool, bool) {
	raw, ok := obj[key]
	if !ok {
		return false, false
	}
	var out bool
	if err := json.Unmarshal(raw, &out); err != nil {
		return false, false
	}
	return out, true
}

func rawInt64(obj map[string]json.RawMessage, key string) (int64, bool) {
	raw, ok := obj[key]
	if !ok {
		return 0, false
	}
	var out int64
	if err := json.Unmarshal(raw, &out); err == nil {
		return out, true
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int64(f), true
	}
	return 0, false
}
