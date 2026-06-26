// Package bybit provides Bybit-specific market-data adapter helpers.
package bybit

import (
	"encoding/json"
	"math"
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

// ParseMeta is an alias for the shared parser diagnostics type.
type ParseMeta = common.ParseMeta

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
		metrics.IncMRTradeBadValue(VenueBybit, "empty_trade_id")
		return app.IngestRequest{}, true, nil
	}

	tsExchange := td.TradeTimeMs
	if tsExchange <= 0 {
		tsExchange = msg.TsMs
	}
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
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
			VenueBybit,
			common.ClassifyTradeValidationReason(trade.Price, trade.Size, trade.Side, trade.TradeID, trade.Timestamp),
		)
		return app.IngestRequest{}, true, nil
	}
	metrics.IncMRTradeIngest(VenueBybit)
	if recvTs := recvAt.UnixMilli(); tsExchange > 0 && recvTs > tsExchange {
		metrics.ObserveMRTradeLatency(VenueBybit, float64(recvTs-tsExchange)/1000.0)
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
		Payload:  trade,
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

	firstID, finalID, prevFinal, isSnapshot, p := resolveOrderBookWindow(msg)
	if p != nil {
		return app.IngestRequest{}, true, p
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
			Bids:       bids,
			Asks:       asks,
			FirstID:    firstID,
			FinalID:    finalID,
			PrevFinal:  prevFinal,
			Timestamp:  tsExchange,
			IsSnapshot: isSnapshot,
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

	markPrice, markPriceOK := parsePositiveFiniteFloat(msg.Data.MarkPrice)
	indexPrice, indexPriceOK := parsePositiveFiniteFloat(msg.Data.IndexPrice)
	if !markPriceOK {
		// Bybit delta ticker updates can omit markPrice while still carrying indexPrice.
		// Use indexPrice as deterministic fallback to avoid dropping actionable updates.
		if indexPriceOK {
			markPrice = indexPrice
		} else {
			return app.IngestRequest{}, true, nil
		}
	}
	if !indexPriceOK {
		indexPrice = 0
	}

	var err error
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
	return common.BuildTradeIdempotencyKey(venue, instrument, tradeID)
}

func buildDepthIdempotencyKey(venue, instrument string, finalUpdateID int64) string {
	return common.BuildDepthIdempotencyKey(venue, instrument, finalUpdateID)
}

func parseLevels(raw [][]string) ([]domain.PriceLevel, *problem.Problem) {
	return common.ParseStringLevels(raw, "bybit orderbook")
}

func normalizeSide(side string) (string, *problem.Problem) {
	return common.NormalizeSide(side, "bybit")
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
	return common.SkipReasonFromProblem(p)
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
	return common.BuildInstrumentMetadata(venueSymbol, canonical, marketType, canonicalPairFromBybitSymbol)
}

func canonicalPairFromBybitSymbol(symbol string) string {
	return common.CanonicalPairFromSuffixList(symbol, []string{
		"USDT", "USDC", "USD", "BTC", "ETH", "EUR",
	})
}

func normalizeMarketType(raw string) string {
	return common.NormalizeMarketTypeSpot(raw)
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

func parsePositiveFiniteFloat(raw string) (float64, bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return 0, false
	}
	out, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(out) || math.IsInf(out, 0) || out <= 0 {
		return 0, false
	}
	return out, true
}

func resolveOrderBookWindow(msg orderBookEnvelope) (firstID, finalID, prevFinal int64, isSnapshot bool, p *problem.Problem) {
	isSnapshot = strings.EqualFold(strings.TrimSpace(msg.Type), "snapshot")

	finalID = msg.Data.UpdateID
	if finalID <= 0 {
		finalID = msg.Data.Sequence
	}
	prevFinal = msg.Data.PrevFinal
	if prevFinal <= 0 && finalID > 1 {
		prevFinal = finalID - 1
	}

	if isSnapshot {
		firstID = finalID
		if firstID <= 0 {
			firstID = msg.Data.Sequence
		}
	} else {
		if prevFinal > 0 {
			firstID = prevFinal + 1
		}
		if firstID <= 0 {
			firstID = finalID
		}
	}

	if firstID <= 0 || finalID <= 0 {
		return 0, 0, 0, false, problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit orderbook: update ids must be > 0"),
			"reason", "missing_update_id",
		)
	}
	if !isSnapshot && finalID < firstID {
		return 0, 0, 0, false, problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit orderbook: final update id must be >= first update id"),
			"reason", "invalid_update_window",
		)
	}

	return firstID, finalID, prevFinal, isSnapshot, nil
}
