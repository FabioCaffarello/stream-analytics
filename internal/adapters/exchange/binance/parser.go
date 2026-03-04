// Package binance provides Binance-specific market-data adapter helpers.
package binance

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	common "github.com/market-raccoon/internal/adapters/exchange/common"
	"github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	// VenueBinance is the canonical venue identifier emitted for Binance events.
	VenueBinance = "BINANCE"
)

type aggTrade struct {
	Event        string `json:"e"`
	EventTimeMs  int64  `json:"E"`
	TradeTimeMs  int64  `json:"T"`
	Symbol       string `json:"s"`
	AggTradeID   int64  `json:"a"`
	PriceRaw     string `json:"p"`
	QuantityRaw  string `json:"q"`
	BuyerIsMaker bool   `json:"m"`
}

type depthUpdate struct {
	Event       string     `json:"e"`
	EventTimeMs int64      `json:"E"`
	Symbol      string     `json:"s"`
	FirstID     int64      `json:"U"`
	FinalID     int64      `json:"u"`
	PrevFinal   int64      `json:"pu"`
	BidsRaw     [][]string `json:"b"`
	AsksRaw     [][]string `json:"a"`
}

type markPriceUpdate struct {
	Event       string `json:"e"`
	EventTimeMs int64  `json:"E"`
	Symbol      string `json:"s"`
	MarkPrice   string `json:"p"`
	IndexPrice  string `json:"i"`
	FundingRate string `json:"r"`
}

type forceOrderEnvelope struct {
	Event       string     `json:"e"`
	EventTimeMs int64      `json:"E"`
	Order       forceOrder `json:"o"`
}

type forceOrder struct {
	Symbol    string `json:"s"`
	Side      string `json:"S"`
	PriceRaw  string `json:"p"`
	SizeRaw   string `json:"q"`
	TradeTime int64  `json:"T"`
}

// ParseMeta is an alias for the shared parser diagnostics type.
type ParseMeta = common.ParseMeta

// ParseMessage parses Binance WS payload and maps supported messages to app.IngestRequest.
// Returns skip=true for unsupported/heartbeat/control messages.
func ParseMessage(data []byte, recvAt time.Time) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeSpot.String())
	return req, skip, meta.Problem
}

// ParseMessageWithMeta parses Binance payload and returns telemetry metadata.
func ParseMessageWithMeta(data []byte, recvAt time.Time) (app.IngestRequest, bool, ParseMeta) {
	return ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeSpot.String())
}

// ParseMessageForMarketType parses Binance WS payload with explicit market type.
func ParseMessageForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, marketType)
	return req, skip, meta.Problem
}

// ParseMessageWithMetaForMarketType parses Binance payload and returns telemetry metadata.
func ParseMessageWithMetaForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, ParseMeta) {
	marketType = normalizeMarketType(marketType)
	payload := data
	meta := ParseMeta{}

	// Binance combined stream wraps payload as {stream, data}. Use shared helper.
	if p, ws, wrapped, empty := common.UnwrapCombinedStream(data); wrapped {
		meta.WSStream = ws
		meta.EventType = eventTypeFromStream(ws)
		meta.Ticker = tickerFromStream(ws)
		if empty {
			meta.SkipReason = "envelope_empty_data"
			return app.IngestRequest{}, true, meta
		}
		payload = p
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(payload, &obj); err != nil {
		meta.SkipReason = "parse_error"
		meta.Problem = problem.Wrap(err, problem.ValidationFailed, "binance parser: invalid JSON payload")
		return app.IngestRequest{}, true, meta
	}
	var event string
	if rawEvent, ok := obj["e"]; ok {
		if err := json.Unmarshal(rawEvent, &event); err != nil {
			meta.SkipReason = "parse_error"
			meta.Problem = problem.Wrap(err, problem.ValidationFailed, "binance parser: invalid event type")
			return app.IngestRequest{}, true, meta
		}
	}
	if event != "" {
		meta.EventType = event
	}

	req, skip, p, ok := parseByEvent(event, payload, recvAt, marketType)
	if !ok {
		meta.SkipReason = "unsupported_event"
		if meta.EventType == "" {
			meta.EventType = "unknown"
		}
		return app.IngestRequest{}, true, meta
	}
	applyWSStreamMeta(&req, meta.WSStream)
	meta.Ticker = req.Instrument
	meta.SkipReason = skipReasonFromProblem(p)
	meta.Problem = p
	return req, skip, meta
}

func parseByEvent(event string, payload []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem, bool) {
	switch event {
	case "aggTrade":
		req, skip, p := parseAggTrade(payload, recvAt, marketType)
		return req, skip, p, true
	case "depthUpdate":
		req, skip, p := parseDepthUpdate(payload, recvAt, marketType)
		return req, skip, p, true
	case "markPriceUpdate":
		req, skip, p := parseMarkPriceUpdate(payload, recvAt, marketType)
		return req, skip, p, true
	case "forceOrder":
		req, skip, p := parseForceOrder(payload, recvAt, marketType)
		return req, skip, p, true
	default:
		return app.IngestRequest{}, true, nil, false
	}
}

func applyWSStreamMeta(req *app.IngestRequest, wsStream string) {
	if req == nil || wsStream == "" || req.Metadata == nil {
		return
	}
	req.Metadata["ws_stream"] = wsStream
}

func parseMarkPriceUpdate(payload []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var m markPriceUpdate
	if err := json.Unmarshal(payload, &m); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance markPriceUpdate: invalid payload")
	}
	instrument := naming.CanonicalInstrument(m.Symbol)
	if instrument == "" {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "binance markPriceUpdate: symbol is empty")
	}
	markPrice, err := strconv.ParseFloat(m.MarkPrice, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance markPriceUpdate: invalid mark price")
	}
	indexPrice := 0.0
	if strings.TrimSpace(m.IndexPrice) != "" {
		indexPrice, err = strconv.ParseFloat(m.IndexPrice, 64)
		if err != nil {
			return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance markPriceUpdate: invalid index price")
		}
	}
	fundingRate := 0.0
	if strings.TrimSpace(m.FundingRate) != "" {
		fundingRate, err = strconv.ParseFloat(m.FundingRate, 64)
		if err != nil {
			return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance markPriceUpdate: invalid funding rate")
		}
	}
	tsExchange := m.EventTimeMs
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}
	return app.IngestRequest{
		Venue:      VenueBinance,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.markprice",
		Version:    1,
		TsExchange: tsExchange,
		Metadata:   buildInstrumentMetadata(m.Symbol, instrument, marketType),
		Payload: domain.MarkPriceTickV1{
			MarkPrice:   markPrice,
			IndexPrice:  indexPrice,
			FundingRate: fundingRate,
			Timestamp:   tsExchange,
		},
	}, false, nil
}

func parseForceOrder(payload []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var m forceOrderEnvelope
	if err := json.Unmarshal(payload, &m); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance forceOrder: invalid payload")
	}
	instrument := naming.CanonicalInstrument(m.Order.Symbol)
	if instrument == "" {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "binance forceOrder: symbol is empty")
	}
	side, p := normalizeSide(m.Order.Side)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	price, err := strconv.ParseFloat(m.Order.PriceRaw, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance forceOrder: invalid price")
	}
	size, err := strconv.ParseFloat(m.Order.SizeRaw, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance forceOrder: invalid size")
	}
	tsExchange := m.Order.TradeTime
	if tsExchange <= 0 {
		tsExchange = m.EventTimeMs
	}
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}
	return app.IngestRequest{
		Venue:      VenueBinance,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.liquidation",
		Version:    1,
		TsExchange: tsExchange,
		Metadata:   buildInstrumentMetadata(m.Order.Symbol, instrument, marketType),
		Payload: domain.LiquidationTickV1{
			Side:      side,
			Price:     price,
			Size:      size,
			Timestamp: tsExchange,
		},
	}, false, nil
}

func parseAggTrade(payload []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var m aggTrade
	if err := json.Unmarshal(payload, &m); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance aggTrade: invalid payload")
	}

	price, err := strconv.ParseFloat(m.PriceRaw, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance aggTrade: invalid price")
	}
	size, err := strconv.ParseFloat(m.QuantityRaw, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance aggTrade: invalid quantity")
	}
	instrument := naming.CanonicalInstrument(m.Symbol)
	if instrument == "" {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "binance aggTrade: symbol is empty")
	}
	tsExchange := m.TradeTimeMs
	if tsExchange <= 0 {
		tsExchange = m.EventTimeMs
	}
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}

	side := "buy"
	if m.BuyerIsMaker {
		side = "sell"
	}
	tradeID := common.TradeIDStringFromAny(m.AggTradeID)
	trade := domain.TradeTickV1{
		Price:     price,
		Size:      size,
		Side:      side,
		TradeID:   tradeID,
		Timestamp: tsExchange,
	}
	if p := trade.Validate(); p != nil {
		metrics.IncMRTradeBadValue(
			VenueBinance,
			common.ClassifyTradeValidationReason(trade.Price, trade.Size, trade.Side, trade.TradeID, trade.Timestamp),
		)
		return app.IngestRequest{}, true, nil
	}
	metrics.IncMRTradeIngest(VenueBinance)
	if recvTs := recvAt.UnixMilli(); tsExchange > 0 && recvTs > tsExchange {
		metrics.ObserveMRTradeLatency(VenueBinance, float64(recvTs-tsExchange)/1000.0)
	}

	return app.IngestRequest{
		Venue:      VenueBinance,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.trade",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildTradeIdempotencyKey(
			VenueBinance,
			instrument,
			tradeID,
		),
		Metadata: buildInstrumentMetadata(m.Symbol, instrument, marketType),
		Payload:  trade,
	}, false, nil
}

func parseDepthUpdate(payload []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var m depthUpdate
	if err := json.Unmarshal(payload, &m); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "binance depthUpdate: invalid payload")
	}

	instrument := naming.CanonicalInstrument(m.Symbol)
	if instrument == "" {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "binance depthUpdate: symbol is empty")
	}

	bids, p := parseLevels(m.BidsRaw)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	asks, p := parseLevels(m.AsksRaw)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tsExchange := m.EventTimeMs
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}
	if m.FinalID <= 0 {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "binance depthUpdate: final update id must be > 0")
	}

	return app.IngestRequest{
		Venue:      VenueBinance,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.bookdelta",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildDepthIdempotencyKey(
			VenueBinance,
			instrument,
			m.FinalID,
		),
		Metadata: buildInstrumentMetadata(m.Symbol, instrument, marketType),
		Payload: domain.BookDeltaV1{
			Bids:      bids,
			Asks:      asks,
			FirstID:   m.FirstID,
			FinalID:   m.FinalID,
			PrevFinal: m.PrevFinal,
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
	return common.ParseStringLevels(raw, "binance depthUpdate")
}

func normalizeSide(side string) (string, *problem.Problem) {
	return common.NormalizeSide(side, "binance")
}

func skipReasonFromProblem(p *problem.Problem) string {
	return common.SkipReasonFromProblem(p)
}

func eventTypeFromStream(stream string) string {
	if stream == "" {
		return ""
	}
	parts := strings.Split(stream, "@")
	if len(parts) < 2 {
		return ""
	}
	switch parts[1] {
	case "aggTrade":
		return "aggTrade"
	case "depth", "depthUpdate":
		return "depthUpdate"
	default:
		return parts[1]
	}
}

func tickerFromStream(stream string) string {
	parts := strings.Split(strings.TrimSpace(stream), "@")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	return naming.CanonicalInstrument(parts[0])
}

func buildInstrumentMetadata(venueSymbol, canonical, marketType string) map[string]string {
	return common.BuildInstrumentMetadata(venueSymbol, canonical, marketType, canonicalPairFromBinanceSymbol)
}

func canonicalPairFromBinanceSymbol(symbol string) string {
	return common.CanonicalPairFromSuffixList(symbol, []string{
		"USDT", "USDC", "BUSD", "FDUSD", "TUSD", "BTC", "ETH", "BNB", "EUR", "USD",
	})
}

func normalizeMarketType(raw string) string {
	return common.NormalizeMarketTypeSpot(raw)
}
