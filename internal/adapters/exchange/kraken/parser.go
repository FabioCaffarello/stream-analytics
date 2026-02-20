package kraken

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	common "github.com/market-raccoon/internal/adapters/exchange/common"
	"github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const VenueKraken = "KRAKEN"

type wsEnvelope struct {
	Channel string          `json:"channel"`
	Type    string          `json:"type"`
	Method  string          `json:"method"`
	Success *bool           `json:"success"`
	Data    json.RawMessage `json:"data"`
}

type tradeChannelData struct {
	Symbol string       `json:"symbol"`
	Trades []tradeEntry `json:"trades"`
}

type tradeEntry struct {
	PriceRaw  string  `json:"price"`
	QtyRaw    string  `json:"qty"`
	SizeRaw   string  `json:"size"`
	Side      string  `json:"side"`
	Timestamp string  `json:"timestamp"`
	TimeIn    float64 `json:"time_in"`
	TradeID   any     `json:"trade_id"`
	ID        any     `json:"id"`
}

type bookChannelData struct {
	Symbol    string      `json:"symbol"`
	Bids      []bookLevel `json:"bids"`
	Asks      []bookLevel `json:"asks"`
	Sequence  int64       `json:"sequence"`
	Checksum  int64       `json:"checksum"`
	Timestamp string      `json:"timestamp"`
}

type bookLevel struct {
	PriceRaw string `json:"price"`
	QtyRaw   string `json:"qty"`
	SizeRaw  string `json:"size"`
}

type tickerChannelData struct {
	Symbol         string `json:"symbol"`
	MarkPriceRaw   string `json:"mark_price"`
	IndexPriceRaw  string `json:"index_price"`
	FundingRateRaw string `json:"funding_rate"`
	LastRaw        string `json:"last"`
	Sequence       int64  `json:"sequence"`
	Timestamp      string `json:"timestamp"`
}

// ParseMeta is an alias for the shared parser diagnostics type.
type ParseMeta = common.ParseMeta

// ParseMessage parses Kraken payload.
func ParseMessage(data []byte, recvAt time.Time) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeSpot.String())
	return req, skip, meta.Problem
}

// ParseMessageWithMeta parses Kraken payload and returns parse metadata.
func ParseMessageWithMeta(data []byte, recvAt time.Time) (app.IngestRequest, bool, ParseMeta) {
	return ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeSpot.String())
}

// ParseMessageForMarketType parses Kraken payload for an explicit market type.
func ParseMessageForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, marketType)
	return req, skip, meta.Problem
}

// ParseMessageWithMetaForMarketType parses Kraken payload and returns parse metadata.
func ParseMessageWithMetaForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, ParseMeta) {
	marketType = normalizeMarketType(marketType)
	meta := ParseMeta{}

	var msg wsEnvelope
	if err := json.Unmarshal(data, &msg); err != nil {
		meta.SkipReason = "parse_error"
		meta.Problem = problem.Wrap(err, problem.ValidationFailed, "kraken parser: invalid JSON payload")
		return app.IngestRequest{}, true, meta
	}

	meta.EventType = strings.TrimSpace(msg.Channel)
	meta.WSStream = meta.EventType

	if meta.EventType == "" {
		if strings.TrimSpace(msg.Method) != "" || msg.Success != nil {
			meta.SkipReason = "control_event"
			return app.IngestRequest{}, true, meta
		}
		meta.SkipReason = "unsupported_event"
		return app.IngestRequest{}, true, meta
	}

	switch meta.EventType {
	case "trade":
		req, skip, p := parseTrade(msg.Data, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		meta.Ticker = req.Instrument
		return req, skip, meta
	case "book":
		req, skip, p := parseBook(msg.Data, msg.Type, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		meta.Ticker = req.Instrument
		return req, skip, meta
	case "ticker":
		req, skip, p := parseTicker(msg.Data, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		if skip && p == nil {
			meta.SkipReason = "markprice_unavailable"
		}
		meta.Ticker = req.Instrument
		return req, skip, meta
	default:
		meta.SkipReason = "unsupported_event"
		return app.IngestRequest{}, true, meta
	}
}

func parseTrade(data json.RawMessage, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var rows []tradeChannelData
	if err := json.Unmarshal(data, &rows); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "kraken trade: invalid payload")
	}
	if len(rows) == 0 || len(rows[0].Trades) == 0 {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "kraken trade: data must not be empty")
	}

	row := rows[0]
	entry := row.Trades[0]
	instrument, p := instrumentFromSymbol(row.Symbol)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	price, p := parseRequiredFloat(entry.PriceRaw, "kraken trade: invalid price")
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	sizeRaw := entry.QtyRaw
	if strings.TrimSpace(sizeRaw) == "" {
		sizeRaw = entry.SizeRaw
	}
	size, p := parseRequiredFloat(sizeRaw, "kraken trade: invalid size")
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	side, p := normalizeSide(entry.Side)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tsExchange, p := parseTimestamp(entry.Timestamp, entry.TimeIn, recvAt)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tradeID := identifierFromAny(entry.TradeID)
	if tradeID == "" {
		tradeID = identifierFromAny(entry.ID)
	}
	if tradeID == "" {
		tradeID = common.TradeIDStringFromAny(tsExchange)
	}

	return app.IngestRequest{
		Venue:      VenueKraken,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.trade",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildTradeIdempotencyKey(
			VenueKraken,
			instrument,
			tradeID,
		),
		Metadata: buildInstrumentMetadata(row.Symbol, instrument, marketType),
		Payload: domain.TradeTickV1{
			Price:     price,
			Size:      size,
			Side:      side,
			TradeID:   tradeID,
			Timestamp: tsExchange,
		},
	}, false, nil
}

func parseBook(data json.RawMessage, msgType string, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var rows []bookChannelData
	if err := json.Unmarshal(data, &rows); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "kraken book: invalid payload")
	}
	if len(rows) == 0 {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "kraken book: data must not be empty")
	}

	row := rows[0]
	instrument, p := instrumentFromSymbol(row.Symbol)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	bids, p := parseBookLevels(row.Bids)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	asks, p := parseBookLevels(row.Asks)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tsExchange, p := parseTimestamp(row.Timestamp, 0, recvAt)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	seq := row.Sequence
	if seq <= 0 {
		seq = row.Checksum
	}
	if seq <= 0 {
		seq = tsExchange
	}
	prevFinal := int64(0)
	if seq > 1 {
		prevFinal = seq - 1
	}

	return app.IngestRequest{
		Venue:      VenueKraken,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.bookdelta",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildDepthIdempotencyKey(
			VenueKraken,
			instrument,
			seq,
		),
		Metadata: buildInstrumentMetadata(row.Symbol, instrument, marketType),
		Payload: domain.BookDeltaV1{
			Bids:       bids,
			Asks:       asks,
			FirstID:    seq,
			FinalID:    seq,
			PrevFinal:  prevFinal,
			Timestamp:  tsExchange,
			IsSnapshot: strings.EqualFold(strings.TrimSpace(msgType), "snapshot"),
		},
	}, false, nil
}

func parseTicker(data json.RawMessage, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var rows []tickerChannelData
	if err := json.Unmarshal(data, &rows); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "kraken ticker: invalid payload")
	}
	if len(rows) == 0 {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "kraken ticker: data must not be empty")
	}

	row := rows[0]
	instrument, p := instrumentFromSymbol(row.Symbol)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	markRaw := row.MarkPriceRaw
	if strings.TrimSpace(markRaw) == "" {
		markRaw = row.LastRaw
	}
	if strings.TrimSpace(markRaw) == "" {
		return app.IngestRequest{}, true, nil
	}
	markPrice, p := parseRequiredFloat(markRaw, "kraken ticker: invalid mark price")
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	indexPrice, p := parseOptionalFloat(row.IndexPriceRaw, "kraken ticker: invalid index price")
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	fundingRate, p := parseOptionalFloat(row.FundingRateRaw, "kraken ticker: invalid funding rate")
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tsExchange, p := parseTimestamp(row.Timestamp, 0, recvAt)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	sequence := row.Sequence
	if sequence <= 0 {
		sequence = tsExchange
	}

	return app.IngestRequest{
		Venue:      VenueKraken,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.markprice",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildMarkPriceIdempotencyKey(
			VenueKraken,
			instrument,
			sequence,
		),
		Metadata: buildInstrumentMetadata(row.Symbol, instrument, marketType),
		Payload: domain.MarkPriceTickV1{
			MarkPrice:   markPrice,
			IndexPrice:  indexPrice,
			FundingRate: fundingRate,
			Timestamp:   tsExchange,
		},
	}, false, nil
}

func instrumentFromSymbol(raw string) (string, *problem.Problem) {
	symbol := strings.ToUpper(strings.TrimSpace(raw))
	if symbol == "" {
		return "", problem.New(problem.ValidationFailed, "kraken: symbol is empty")
	}
	symbol = strings.ReplaceAll(symbol, "XBT", "BTC")
	instrument := naming.CanonicalInstrument(symbol)
	if instrument == "" {
		return "", problem.New(problem.ValidationFailed, "kraken: instrument is empty")
	}
	return instrument, nil
}

func parseTimestamp(raw string, fallback float64, recvAt time.Time) (int64, *problem.Problem) {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return ts.UnixMilli(), nil
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, problem.Wrap(err, problem.ValidationFailed, "kraken: invalid timestamp")
		}
		return millisFromFloat(f), nil
	}
	if fallback > 0 {
		return millisFromFloat(fallback), nil
	}
	return recvAt.UnixMilli(), nil
}

func millisFromFloat(v float64) int64 {
	if v >= 1e12 {
		return int64(v)
	}
	if v >= 1e9 {
		return int64(v * 1000)
	}
	return int64(v)
}

func parseBookLevels(raw []bookLevel) ([]domain.PriceLevel, *problem.Problem) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]domain.PriceLevel, 0, len(raw))
	for _, level := range raw {
		sizeRaw := level.QtyRaw
		if strings.TrimSpace(sizeRaw) == "" {
			sizeRaw = level.SizeRaw
		}
		price, p := parseRequiredFloat(level.PriceRaw, "kraken book: invalid level price")
		if p != nil {
			return nil, p
		}
		size, p := parseRequiredFloat(sizeRaw, "kraken book: invalid level size")
		if p != nil {
			return nil, p
		}
		out = append(out, domain.PriceLevel{Price: price, Size: size})
	}
	return out, nil
}

func parseRequiredFloat(raw, msg string) (float64, *problem.Problem) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, problem.Wrap(err, problem.ValidationFailed, msg)
	}
	return value, nil
}

func parseOptionalFloat(raw, msg string) (float64, *problem.Problem) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, problem.Wrap(err, problem.ValidationFailed, msg)
	}
	return value, nil
}

func normalizeSide(side string) (string, *problem.Problem) {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "B", "BUY", "BID":
		return "buy", nil
	case "S", "SELL", "ASK", "A":
		return "sell", nil
	default:
		return "", problem.Newf(problem.ValidationFailed, "kraken: unsupported side %q", side)
	}
}

func identifierFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.FormatInt(int64(v), 10)
	default:
		return ""
	}
}

func buildTradeIdempotencyKey(venue, instrument, tradeID string) string {
	return common.BuildTradeIdempotencyKey(venue, instrument, tradeID)
}

func buildDepthIdempotencyKey(venue, instrument string, finalUpdateID int64) string {
	return common.BuildDepthIdempotencyKey(venue, instrument, finalUpdateID)
}

func buildMarkPriceIdempotencyKey(venue, instrument string, sequence int64) string {
	return common.BuildMarkPriceIdempotencyKey(venue, instrument, sequence)
}

func buildInstrumentMetadata(venueSymbol, canonical, marketType string) map[string]string {
	return common.BuildInstrumentMetadata(venueSymbol, canonical, marketType, func(vs string) string {
		pair := strings.ToUpper(strings.TrimSpace(vs))
		if pair != "" {
			return strings.ReplaceAll(pair, "/", "-")
		}
		return ""
	})
}

func skipReasonFromProblem(p *problem.Problem) string {
	return common.SkipReasonFromProblem(p)
}

func normalizeMarketType(raw string) string {
	return common.NormalizeMarketTypeSpot(raw)
}
