package coinbase

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	common "github.com/market-raccoon/internal/adapters/exchange/common"
	"github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const VenueCoinbase = "COINBASE"

type wsMessage struct {
	Type string `json:"type"`
}

type matchMessage struct {
	Type      string `json:"type"`
	TradeID   int64  `json:"trade_id"`
	ProductID string `json:"product_id"`
	Price     string `json:"price"`
	Size      string `json:"size"`
	Side      string `json:"side"`
	Time      string `json:"time"`
}

type snapshotMessage struct {
	Type      string     `json:"type"`
	ProductID string     `json:"product_id"`
	Bids      [][]string `json:"bids"`
	Asks      [][]string `json:"asks"`
	Sequence  int64      `json:"sequence"`
}

type l2UpdateMessage struct {
	Type      string     `json:"type"`
	ProductID string     `json:"product_id"`
	Time      string     `json:"time"`
	Changes   [][]string `json:"changes"`
	Sequence  int64      `json:"sequence"`
}

type tickerMessage struct {
	Type      string `json:"type"`
	ProductID string `json:"product_id"`
	Price     string `json:"price"`
	Time      string `json:"time"`
	Sequence  int64  `json:"sequence"`
}

// ParseMeta is an alias for the shared parser diagnostics type.
type ParseMeta = common.ParseMeta

// ParseMessage parses Coinbase payload.
func ParseMessage(data []byte, recvAt time.Time) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeSpot.String())
	return req, skip, meta.Problem
}

// ParseMessageWithMeta parses Coinbase payload and returns parse metadata.
func ParseMessageWithMeta(data []byte, recvAt time.Time) (app.IngestRequest, bool, ParseMeta) {
	return ParseMessageWithMetaForMarketType(data, recvAt, domain.MarketTypeSpot.String())
}

// ParseMessageForMarketType parses Coinbase payload for an explicit market type.
func ParseMessageForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	req, skip, meta := ParseMessageWithMetaForMarketType(data, recvAt, marketType)
	return req, skip, meta.Problem
}

// ParseMessageWithMetaForMarketType parses Coinbase payload and returns parse metadata.
func ParseMessageWithMetaForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, ParseMeta) {
	marketType = normalizeMarketType(marketType)
	meta := ParseMeta{}

	var top wsMessage
	if err := json.Unmarshal(data, &top); err != nil {
		meta.SkipReason = "parse_error"
		meta.Problem = problem.Wrap(err, problem.ValidationFailed, "coinbase parser: invalid JSON payload")
		return app.IngestRequest{}, true, meta
	}

	meta.EventType = strings.TrimSpace(top.Type)
	meta.WSStream = meta.EventType

	switch meta.EventType {
	case "match", "last_match":
		req, skip, p := parseTrade(data, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		meta.Ticker = req.Instrument
		return req, skip, meta
	case "snapshot":
		req, skip, p := parseSnapshot(data, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		meta.Ticker = req.Instrument
		return req, skip, meta
	case "l2update":
		req, skip, p := parseL2Update(data, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		meta.Ticker = req.Instrument
		return req, skip, meta
	case "ticker":
		req, skip, p := parseTicker(data, recvAt, marketType)
		meta.Problem = p
		meta.SkipReason = skipReasonFromProblem(p)
		meta.Ticker = req.Instrument
		return req, skip, meta
	case "error", "subscriptions", "heartbeat":
		meta.SkipReason = "control_event"
		return app.IngestRequest{}, true, meta
	default:
		meta.SkipReason = "unsupported_event"
		return app.IngestRequest{}, true, meta
	}
}

func parseTrade(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg matchMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "coinbase match: invalid payload")
	}
	instrument, p := instrumentFromProductID(msg.ProductID)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	price, err := strconv.ParseFloat(msg.Price, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "coinbase match: invalid price")
	}
	size, err := strconv.ParseFloat(msg.Size, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "coinbase match: invalid size")
	}
	side, p := normalizeSide(msg.Side)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tsExchange, p := parseExchangeTimeMillis(msg.Time, recvAt)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tradeID := common.TradeIDStringFromAny(msg.TradeID)
	if msg.TradeID <= 0 {
		return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "coinbase match: trade_id must be > 0")
	}

	return app.IngestRequest{
		Venue:      VenueCoinbase,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.trade",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildTradeIdempotencyKey(
			VenueCoinbase,
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

func parseSnapshot(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg snapshotMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "coinbase snapshot: invalid payload")
	}
	instrument, p := instrumentFromProductID(msg.ProductID)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	bids, p := parseLevels(msg.Bids)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	asks, p := parseLevels(msg.Asks)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	tsExchange := recvAt.UnixMilli()

	return app.IngestRequest{
		Venue:      VenueCoinbase,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.bookdelta",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildDepthIdempotencyKeyFromSequenceOrPayload(
			VenueCoinbase,
			instrument,
			msg.Sequence,
			data,
		),
		Metadata: buildInstrumentMetadata(msg.ProductID, instrument, marketType),
		Payload: domain.BookDeltaV1{
			Bids:       bids,
			Asks:       asks,
			FirstID:    0,
			FinalID:    1,
			Timestamp:  tsExchange,
			IsSnapshot: true, // Coinbase "snapshot" message is a full L2 book
		},
	}, false, nil
}

func parseL2Update(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg l2UpdateMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "coinbase l2update: invalid payload")
	}
	instrument, p := instrumentFromProductID(msg.ProductID)
	if p != nil {
		return app.IngestRequest{}, true, p
	}

	bids := make([]domain.PriceLevel, 0, len(msg.Changes))
	asks := make([]domain.PriceLevel, 0, len(msg.Changes))
	for _, change := range msg.Changes {
		if len(change) < 3 {
			return app.IngestRequest{}, true, problem.New(problem.ValidationFailed, "coinbase l2update: invalid change entry")
		}
		side := strings.ToLower(strings.TrimSpace(change[0]))
		price, err := strconv.ParseFloat(change[1], 64)
		if err != nil {
			return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "coinbase l2update: invalid level price")
		}
		size, err := strconv.ParseFloat(change[2], 64)
		if err != nil {
			return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "coinbase l2update: invalid level size")
		}
		level := domain.PriceLevel{Price: price, Size: size}
		switch side {
		case "buy":
			bids = append(bids, level)
		case "sell":
			asks = append(asks, level)
		default:
			return app.IngestRequest{}, true, problem.Newf(problem.ValidationFailed, "coinbase l2update: unsupported side %q", change[0])
		}
	}

	tsExchange, p := parseExchangeTimeMillis(msg.Time, recvAt)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	if tsExchange <= 0 {
		tsExchange = recvAt.UnixMilli()
	}

	return app.IngestRequest{
		Venue:      VenueCoinbase,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.bookdelta",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildDepthIdempotencyKeyFromSequenceOrPayload(
			VenueCoinbase,
			instrument,
			msg.Sequence,
			data,
		),
		Metadata: buildInstrumentMetadata(msg.ProductID, instrument, marketType),
		Payload: domain.BookDeltaV1{
			Bids:      bids,
			Asks:      asks,
			FirstID:   0,
			FinalID:   tsExchange,
			Timestamp: tsExchange,
		},
	}, false, nil
}

func parseTicker(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem) {
	var msg tickerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "coinbase ticker: invalid payload")
	}
	instrument, p := instrumentFromProductID(msg.ProductID)
	if p != nil {
		return app.IngestRequest{}, true, p
	}
	markPrice, err := strconv.ParseFloat(msg.Price, 64)
	if err != nil {
		return app.IngestRequest{}, true, problem.Wrap(err, problem.ValidationFailed, "coinbase ticker: invalid price")
	}
	tsExchange, p := parseExchangeTimeMillis(msg.Time, recvAt)
	if p != nil {
		return app.IngestRequest{}, true, p
	}

	return app.IngestRequest{
		Venue:      VenueCoinbase,
		Instrument: instrument,
		MarketType: marketType,
		EventType:  "marketdata.markprice",
		Version:    1,
		TsExchange: tsExchange,
		IdempotencyKey: buildMarkPriceIdempotencyKey(
			VenueCoinbase,
			instrument,
			msg.Sequence,
		),
		Metadata: buildInstrumentMetadata(msg.ProductID, instrument, marketType),
		Payload: domain.MarkPriceTickV1{
			MarkPrice:   markPrice,
			IndexPrice:  0,
			FundingRate: 0,
			Timestamp:   tsExchange,
		},
	}, false, nil
}

func instrumentFromProductID(productID string) (string, *problem.Problem) {
	instrument := naming.CanonicalInstrument(productID)
	if instrument == "" {
		return "", problem.New(problem.ValidationFailed, "coinbase: product_id is empty")
	}
	return instrument, nil
}

func parseExchangeTimeMillis(raw string, recvAt time.Time) (int64, *problem.Problem) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return recvAt.UnixMilli(), nil
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return 0, problem.Wrap(err, problem.ValidationFailed, "coinbase: invalid timestamp")
	}
	return ts.UnixMilli(), nil
}

func parseLevels(raw [][]string) ([]domain.PriceLevel, *problem.Problem) {
	return common.ParseStringLevels(raw, "coinbase orderbook")
}

func normalizeSide(side string) (string, *problem.Problem) {
	return common.NormalizeSide(side, "coinbase")
}

func buildTradeIdempotencyKey(venue, instrument, tradeID string) string {
	return common.BuildTradeIdempotencyKey(venue, instrument, tradeID)
}

func buildDepthIdempotencyKey(venue, instrument string, finalUpdateID int64) string {
	return common.BuildDepthIdempotencyKey(venue, instrument, finalUpdateID)
}

func buildDepthIdempotencyKeyFromSequenceOrPayload(venue, instrument string, sequence int64, payload []byte) string {
	if sequence > 0 {
		return buildDepthIdempotencyKey(venue, instrument, sequence)
	}
	return "venue=" + venue + "|instrument=" + instrument + "|payload_sha=" + sharedhash.HashBytes(payload)
}

func buildMarkPriceIdempotencyKey(venue, instrument string, sequence int64) string {
	return common.BuildMarkPriceIdempotencyKey(venue, instrument, sequence)
}

func buildInstrumentMetadata(venueSymbol, canonical, marketType string) map[string]string {
	return common.BuildInstrumentMetadata(venueSymbol, canonical, marketType, func(vs string) string {
		return normalizeProductID(vs)
	})
}

func skipReasonFromProblem(p *problem.Problem) string {
	return common.SkipReasonFromProblem(p)
}

func normalizeMarketType(raw string) string {
	return common.NormalizeMarketTypeSpot(raw)
}
