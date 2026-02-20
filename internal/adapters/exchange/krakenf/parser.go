package krakenf

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

const VenueKrakenF = "KRAKENF"

type topEnvelope struct {
	Feed  string `json:"feed"`
	Event string `json:"event"`
}

type tradeMessage struct {
	Feed      string      `json:"feed"`
	ProductID string      `json:"product_id"`
	Trades    []tradeData `json:"trades"`
	PriceRaw  string      `json:"price"`
	QtyRaw    string      `json:"qty"`
	SizeRaw   string      `json:"size"`
	Side      string      `json:"side"`
	TimeRaw   string      `json:"time"`
	Timestamp string      `json:"timestamp"`
	Seq       int64       `json:"seq"`
	UID       string      `json:"uid"`
	TradeID   any         `json:"trade_id"`
}

type tradeData struct {
	PriceRaw  string `json:"price"`
	QtyRaw    string `json:"qty"`
	SizeRaw   string `json:"size"`
	Side      string `json:"side"`
	TimeRaw   string `json:"time"`
	Timestamp string `json:"timestamp"`
	Seq       int64  `json:"seq"`
	UID       string `json:"uid"`
	TradeID   any    `json:"trade_id"`
}

type bookMessage struct {
	Feed      string     `json:"feed"`
	ProductID string     `json:"product_id"`
	Bids      [][]string `json:"bids"`
	Asks      [][]string `json:"asks"`
	Seq       int64      `json:"seq"`
	TimeRaw   string     `json:"time"`
	Timestamp string     `json:"timestamp"`
}

type tickerMessage struct {
	Feed           string `json:"feed"`
	ProductID      string `json:"product_id"`
	MarkPriceRaw   string `json:"mark_price"`
	IndexPriceRaw  string `json:"index_price"`
	FundingRateRaw string `json:"funding_rate"`
	LastRaw        string `json:"last"`
	Seq            int64  `json:"seq"`
	TimeRaw        string `json:"time"`
	Timestamp      string `json:"timestamp"`
}

// ParseMeta carries parser diagnostics for observability.
type ParseMeta struct {
	EventType  string
	SkipReason string
	Problem    *problem.Problem
	WSStream   string
	Ticker     string
}

// ParseMessage parses Kraken Futures payload.
func ParseMessage(data []byte, recvAt time.Time) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeUSDMFutures.String())
	return req, skip, meta.Problem
}

// ParseMessageWithMeta parses Kraken Futures payload and returns parse metadata.
func ParseMessageWithMeta(data []byte, recvAt time.Time) (app.IngestRequest, bool, ParseMeta) {
	return ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeUSDMFutures.String())
}

// ParseMessageForMarketType parses Kraken Futures payload for an explicit market type.
func ParseMessageForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, marketType)
	return req, skip, meta.Problem
}

// ParseMessageWithMetaForMarketType parses Kraken Futures payload and returns parse metadata.
func ParseMessageWithMetaForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, ParseMeta) {
	marketType = normalizeMarketType(marketType)
	meta := ParseMeta{}

	var top topEnvelope
	if err := json.Unmarshal(data, &top); err != nil {
		meta.SkipReason = "parse_error"
		meta.Problem = problem.Wrap(err, problem.ValidationFailed, "krakenf parser: invalid JSON payload")
		return app.IngestRequest{}, true, meta
	}

	feed := strings.TrimSpace(top.Feed)
	event := strings.TrimSpace(top.Event)
	meta.EventType = feed
	if meta.EventType == "" {
		meta.EventType = event
	}
	meta.WSStream = meta.EventType

	if feed == "" {
		if event != "" {
			meta.SkipReason = "control_event"
			return app.IngestRequest{}, true, meta
		}
		meta.SkipReason = "unsupported_event"
		return app.IngestRequest{}, true, meta
	}

	switch feed {
	case "trade", "trade_snapshot":
		req, skip, p := parseTrade(data, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		meta.Ticker = req.Instrument
		return req, skip, meta
	case "book", "book_snapshot":
		req, skip, p := parseBook(data, feed, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		meta.Ticker = req.Instrument
		return req, skip, meta
	case "ticker":
		req, skip, p := parseTicker(data, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		if skip && p == nil {
			meta.SkipReason = "markprice_unavailable"
		}
		meta.Ticker = req.Instrument
		return req, skip, meta
	case "heartbeat":
		meta.SkipReason = "control_event"
		return app.IngestRequest{}, true, meta
	default:
		meta.SkipReason = "unsupported_event"
		return app.IngestRequest{}, true, meta
	}
}

func parseTrade(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg tradeMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "krakenf trade: invalid payload")
	}
	instrument, p := instrumentFromProductID(msg.ProductID)
	if p != nil {
		return app.IngestRequest{}, true, p
	}

	entry := tradeData{
		PriceRaw:  msg.PriceRaw,
		QtyRaw:    msg.QtyRaw,
		SizeRaw:   msg.SizeRaw,
		Side:      msg.Side,
		TimeRaw:   msg.TimeRaw,
		Timestamp: msg.Timestamp,
		Seq:       msg.Seq,
		UID:       msg.UID,
		TradeID:   msg.TradeID,
	}
	if len(msg.Trades) > 0 {
		entry = msg.Trades[0]
	}

	price, p := parseRequiredFloat(entry.PriceRaw, "krakenf trade: invalid price")
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	sizeRaw := entry.QtyRaw
	if strings.TrimSpace(sizeRaw) == "" {
		sizeRaw = entry.SizeRaw
	}
	size, p := parseRequiredFloat(sizeRaw, "krakenf trade: invalid size")
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	side, p := normalizeSide(entry.Side)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tsExchange, p := parseTimestamp(entry.Timestamp, entry.TimeRaw, recvAt)
	if p != nil {
		return app.IngestRequest{}, true, p
	}

	tradeID := strings.TrimSpace(entry.UID)
	if tradeID == "" {
		tradeID = identifierFromAny(entry.TradeID)
	}
	if tradeID == "" && entry.Seq > 0 {
		tradeID = fmt.Sprintf("%d", entry.Seq)
	}
	if tradeID == "" {
		tradeID = fmt.Sprintf("%d", tsExchange)
	}

	return app.IngestRequest{
		Venue:      VenueKrakenF,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.trade",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildTradeIdempotencyKey(
			VenueKrakenF,
			instrument,
			tradeID,
		),
		Metadata: buildInstrumentMetadata(msg.ProductID, instrument, marketType),
		Payload: domain.TradeTickV1{
			Price:     price,
			Size:      size,
			Side:      side,
			TradeID:   tradeID,
			Timestamp: tsExchange,
		},
	}, false, nil
}

func parseBook(data []byte, feed string, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg bookMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "krakenf book: invalid payload")
	}
	instrument, p := instrumentFromProductID(msg.ProductID)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	bids, p := parseTupleLevels(msg.Bids)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	asks, p := parseTupleLevels(msg.Asks)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tsExchange, p := parseTimestamp(msg.Timestamp, msg.TimeRaw, recvAt)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	seq := msg.Seq
	if seq <= 0 {
		seq = tsExchange
	}
	prevFinal := int64(0)
	if seq > 1 {
		prevFinal = seq - 1
	}

	return app.IngestRequest{
		Venue:      VenueKrakenF,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.bookdelta",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildDepthIdempotencyKey(
			VenueKrakenF,
			instrument,
			seq,
		),
		Metadata: buildInstrumentMetadata(msg.ProductID, instrument, marketType),
		Payload: domain.BookDeltaV1{
			Bids:       bids,
			Asks:       asks,
			FirstID:    seq,
			FinalID:    seq,
			PrevFinal:  prevFinal,
			Timestamp:  tsExchange,
			IsSnapshot: strings.HasSuffix(feed, "snapshot"),
		},
	}, false, nil
}

func parseTicker(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg tickerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "krakenf ticker: invalid payload")
	}
	instrument, p := instrumentFromProductID(msg.ProductID)
	if p != nil {
		return app.IngestRequest{}, true, p
	}

	markRaw := msg.MarkPriceRaw
	if strings.TrimSpace(markRaw) == "" {
		markRaw = msg.LastRaw
	}
	if strings.TrimSpace(markRaw) == "" {
		return app.IngestRequest{}, true, nil
	}
	markPrice, p := parseRequiredFloat(markRaw, "krakenf ticker: invalid mark price")
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	indexPrice, p := parseOptionalFloat(msg.IndexPriceRaw, "krakenf ticker: invalid index price")
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	fundingRate, p := parseOptionalFloat(msg.FundingRateRaw, "krakenf ticker: invalid funding rate")
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tsExchange, p := parseTimestamp(msg.Timestamp, msg.TimeRaw, recvAt)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	seq := msg.Seq
	if seq <= 0 {
		seq = tsExchange
	}

	return app.IngestRequest{
		Venue:      VenueKrakenF,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.markprice",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildMarkPriceIdempotencyKey(
			VenueKrakenF,
			instrument,
			seq,
		),
		Metadata: buildInstrumentMetadata(msg.ProductID, instrument, marketType),
		Payload: domain.MarkPriceTickV1{
			MarkPrice:   markPrice,
			IndexPrice:  indexPrice,
			FundingRate: fundingRate,
			Timestamp:   tsExchange,
		},
	}, false, nil
}

func instrumentFromProductID(raw string) (string, *problem.Problem) {
	productID := strings.ToUpper(strings.TrimSpace(raw))
	if productID == "" {
		return "", problem.New(problem.ValidationFailed, "krakenf: product_id is empty")
	}
	for _, prefix := range []string{"PI_", "PF_", "FI_"} {
		productID = strings.TrimPrefix(productID, prefix)
	}
	productID = strings.ReplaceAll(productID, "XBT", "BTC")
	instrument := naming.CanonicalInstrument(productID)
	if instrument == "" {
		return "", problem.New(problem.ValidationFailed, "krakenf: instrument is empty")
	}
	return instrument, nil
}

func parseTupleLevels(raw [][]string) ([]domain.PriceLevel, *problem.Problem) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]domain.PriceLevel, 0, len(raw))
	for _, level := range raw {
		if len(level) < 2 {
			return nil, problem.New(problem.ValidationFailed, "krakenf book: invalid level tuple")
		}
		price, p := parseRequiredFloat(level[0], "krakenf book: invalid level price")
		if p != nil {
			return nil, p
		}
		size, p := parseRequiredFloat(level[1], "krakenf book: invalid level size")
		if p != nil {
			return nil, p
		}
		out = append(out, domain.PriceLevel{Price: price, Size: size})
	}
	return out, nil
}

func parseTimestamp(rfc3339Raw, numericRaw string, recvAt time.Time) (int64, *problem.Problem) {
	seenRaw := ""
	for _, raw := range []string{rfc3339Raw, numericRaw} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		seenRaw = raw
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return ts.UnixMilli(), nil
		}
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return millisFromFloat(f), nil
		}
	}
	if seenRaw != "" {
		return 0, problem.Newf(problem.ValidationFailed, "krakenf: invalid timestamp %q", seenRaw)
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
		return "", problem.Newf(problem.ValidationFailed, "krakenf: unsupported side %q", side)
	}
}

func identifierFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int64:
		return fmt.Sprintf("%d", v)
	case int:
		return fmt.Sprintf("%d", v)
	default:
		return ""
	}
}

func buildTradeIdempotencyKey(venue, instrument, tradeID string) string {
	return fmt.Sprintf("venue=%s|instrument=%s|trade_id=%s", venue, instrument, tradeID)
}

func buildDepthIdempotencyKey(venue, instrument string, finalUpdateID int64) string {
	return fmt.Sprintf("venue=%s|instrument=%s|final_update_id=%d", venue, instrument, finalUpdateID)
}

func buildMarkPriceIdempotencyKey(venue, instrument string, sequence int64) string {
	if sequence <= 0 {
		return ""
	}
	return fmt.Sprintf("venue=%s|instrument=%s|sequence=%d", venue, instrument, sequence)
}

func buildInstrumentMetadata(venueSymbol, canonical, marketType string) map[string]string {
	symbol := strings.ToUpper(strings.TrimSpace(venueSymbol))
	meta := map[string]string{
		"instrument_venue_symbol": symbol,
		"instrument_canonical":    canonical,
		"instrument_market_type":  marketType,
	}
	if symbol != "" {
		meta["instrument_pair"] = symbol
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
