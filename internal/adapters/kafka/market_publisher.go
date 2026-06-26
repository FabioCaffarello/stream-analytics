package kafka

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	marketdatapb "github.com/FabioCaffarello/stream-analytics/internal/shared/proto/gen/marketdata/v1"
	"google.golang.org/protobuf/proto"
)

// tradeKafkaMessage is the flat JSON schema published to the market.trades topic.
type tradeKafkaMessage struct {
	Venue        string  `json:"venue"`
	Symbol       string  `json:"symbol"`
	TradeID      string  `json:"trade_id"`
	Price        float64 `json:"price"`
	Quantity     float64 `json:"quantity"`
	Side         string  `json:"side"`
	TsExchangeMs int64   `json:"ts_exchange_ms"`
	TsIngestMs   int64   `json:"ts_ingest_ms"`
}

// tradeDomain is a minimal struct for JSON-decoding trade payloads.
type tradeDomain struct {
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	Side      string  `json:"side"`
	TradeID   string  `json:"trade_id"`
	Timestamp int64   `json:"timestamp"`
}

// MarketPublisherConfig configures the Kafka analytics publisher.
type MarketPublisherConfig struct {
	Writer         *Writer
	TradesTopic    string
	OrderbookTopic string
}

// MarketPublisher publishes market data envelopes to Kafka for analytics.
// It satisfies ports.EventPublisher without importing that package.
type MarketPublisher struct {
	writer         *Writer
	tradesTopic    string
	orderbookTopic string
}

// NewMarketPublisher creates a MarketPublisher wrapping the provided Writer.
func NewMarketPublisher(cfg MarketPublisherConfig) (*MarketPublisher, *problem.Problem) {
	if cfg.Writer == nil {
		return nil, problem.New(problem.ValidationFailed, "kafka writer must not be nil")
	}
	if strings.TrimSpace(cfg.TradesTopic) == "" {
		return nil, problem.New(problem.ValidationFailed, "kafka trades topic must not be empty")
	}
	if strings.TrimSpace(cfg.OrderbookTopic) == "" {
		return nil, problem.New(problem.ValidationFailed, "kafka orderbook topic must not be empty")
	}
	return &MarketPublisher{
		writer:         cfg.Writer,
		tradesTopic:    cfg.TradesTopic,
		orderbookTopic: cfg.OrderbookTopic,
	}, nil
}

// Publish routes the envelope to the appropriate Kafka topic.
// Non-market events are silently ignored (returns nil).
func (p *MarketPublisher) Publish(ctx context.Context, env envelope.Envelope) *problem.Problem {
	switch {
	case strings.HasPrefix(env.Type, "marketdata.trade"):
		return p.publishTrade(ctx, env)
	case strings.HasPrefix(env.Type, "marketdata.bookdelta"):
		return p.publishBookdelta(ctx, env)
	default:
		return nil
	}
}

// decodeTrade extracts trade fields from an envelope payload, handling both
// protobuf (application/protobuf) and JSON (application/json) encodings.
func decodeTrade(env envelope.Envelope) (price, size float64, side, tradeID string, timestampMs int64) {
	if env.ContentType == envelope.ContentTypeProto {
		var tick marketdatapb.TradeTickV1
		if err := proto.Unmarshal(env.Payload, &tick); err != nil {
			return
		}
		return tick.Price, tick.Size, tick.Side, tick.TradeId, tick.TimestampMs
	}
	var t tradeDomain
	if err := json.Unmarshal(env.Payload, &t); err != nil {
		return
	}
	return t.Price, t.Size, t.Side, t.TradeID, t.Timestamp
}

func (p *MarketPublisher) publishTrade(ctx context.Context, env envelope.Envelope) *problem.Problem {
	price, size, side, tradeID, timestampMs := decodeTrade(env)
	if tradeID == "" && price == 0 {
		return nil // unparseable payload — skip silently
	}
	tsExchange := env.TsExchange
	if tsExchange <= 0 {
		tsExchange = timestampMs
	}
	tsIngest := env.TsIngest
	if tsIngest <= 0 {
		tsIngest = time.Now().UnixMilli()
	}
	msg := tradeKafkaMessage{
		Venue:        env.Venue,
		Symbol:       env.Instrument,
		TradeID:      tradeID,
		Price:        price,
		Quantity:     size,
		Side:         side,
		TsExchangeMs: tsExchange,
		TsIngestMs:   tsIngest,
	}
	value, err := json.Marshal(msg)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "kafka trade marshal failed")
	}
	key := []byte(env.Venue + ":" + env.Instrument)
	return p.writer.Write(ctx, p.tradesTopic, key, value, nil)
}

func (p *MarketPublisher) publishBookdelta(_ context.Context, _ envelope.Envelope) *problem.Problem {
	// Orderbook deltas exceed Kafka's default max.message.bytes (1 MB).
	// The analytics pipeline only consumes trades; skip bookdeltas silently.
	return nil
}

// Close shuts down the underlying writer.
func (p *MarketPublisher) Close() error {
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
}
