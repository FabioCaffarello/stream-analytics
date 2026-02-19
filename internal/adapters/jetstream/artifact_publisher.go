package jetstream

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.ArtifactPublisher = (*ArtifactPublisher)(nil)

// envelopePublisher is the minimal interface for publishing envelopes.
type envelopePublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

// ArtifactPublisher is a JetStream-backed implementation of aggports.ArtifactPublisher.
// It converts aggregation domain events into canonical envelopes and publishes them.
type ArtifactPublisher struct {
	pub    envelopePublisher
	logger *slog.Logger
	clock  func() int64
}

// NewArtifactPublisher creates a JetStream artifact publisher.
func NewArtifactPublisher(pub *Publisher, logger *slog.Logger) *ArtifactPublisher {
	return &ArtifactPublisher{
		pub:    pub,
		logger: logger,
		clock:  func() int64 { return time.Now().UnixMilli() },
	}
}

// PublishSnapshot publishes an aggregation snapshot event.
func (a *ArtifactPublisher) PublishSnapshot(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	wireDTO := domainSnapshotToWireDTO(snap)
	contentType := chooseArtifactContentType("aggregation.snapshot")
	payload, p := codec.EncodePayload("aggregation.snapshot", 1, contentType, wireDTO)
	if p != nil {
		return p
	}
	env := envelope.Envelope{
		Type:       "aggregation.snapshot",
		Version:    1,
		Venue:      snap.BookID.Venue,
		Instrument: snap.BookID.Instrument,
		TsIngest:   a.clock(),
		Seq:        snap.Seq,
		IdempotencyKey: sharedhash.HashFields(
			"aggregation.snapshot",
			snap.BookID.Venue,
			snap.BookID.Instrument,
			strconv.FormatInt(snap.Seq, 10),
		),
		ContentType: contentType,
		Payload:     payload,
	}
	if p := env.Validate(); p != nil {
		return p
	}
	return a.pub.Publish(ctx, env)
}

// PublishInconsistent publishes an orderbook inconsistency event.
func (a *ArtifactPublisher) PublishInconsistent(ctx context.Context, evt aggdomain.OrderBookInconsistentDetected) *problem.Problem {
	wireDTO := domainInconsistentToWireDTO(evt)
	contentType := chooseArtifactContentType("aggregation.orderbook_inconsistency")
	payload, p := codec.EncodePayload("aggregation.orderbook_inconsistency", 1, contentType, wireDTO)
	if p != nil {
		return p
	}
	env := envelope.Envelope{
		Type:       "aggregation.orderbook_inconsistency",
		Version:    1,
		Venue:      evt.BookID.Venue,
		Instrument: evt.BookID.Instrument,
		TsIngest:   a.clock(),
		Seq:        evt.Seq,
		IdempotencyKey: sharedhash.HashFields(
			"aggregation.orderbook_inconsistency",
			evt.BookID.Venue,
			evt.BookID.Instrument,
			strconv.FormatInt(evt.Seq, 10),
			evt.Reason,
		),
		ContentType: contentType,
		Payload:     payload,
	}
	if p := env.Validate(); p != nil {
		return p
	}
	return a.pub.Publish(ctx, env)
}

// PublishCandleClosed publishes a closed candle event using the codec registry.
func (a *ArtifactPublisher) PublishCandleClosed(ctx context.Context, evt aggdomain.CandleClosed) *problem.Problem {
	wireDTO := domainCandleToWireDTO(evt)
	contentType := chooseArtifactContentType("aggregation.candle")
	payload, p := codec.EncodePayload("aggregation.candle", 1, contentType, wireDTO)
	if p != nil {
		return p
	}
	env := envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      evt.Candle.Venue,
		Instrument: evt.Candle.Instrument,
		TsIngest:   a.clock(),
		Seq:        evt.Candle.SeqLast,
		IdempotencyKey: sharedhash.HashFields(
			"aggregation.candle",
			evt.Candle.Venue,
			evt.Candle.Instrument,
			evt.Candle.Timeframe,
			strconv.FormatInt(evt.Candle.WindowStartTs, 10),
		),
		ContentType: contentType,
		Payload:     payload,
	}
	if p := env.Validate(); p != nil {
		return p
	}
	return a.pub.Publish(ctx, env)
}

// PublishStatsClosed publishes a closed stats window event using the codec registry.
func (a *ArtifactPublisher) PublishStatsClosed(ctx context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
	wireDTO := domainStatsToWireDTO(evt)
	contentType := chooseArtifactContentType("aggregation.stats")
	payload, p := codec.EncodePayload("aggregation.stats", 1, contentType, wireDTO)
	if p != nil {
		return p
	}
	env := envelope.Envelope{
		Type:       "aggregation.stats",
		Version:    1,
		Venue:      evt.Stats.Venue,
		Instrument: evt.Stats.Instrument,
		TsIngest:   a.clock(),
		Seq:        evt.Stats.SeqLast,
		IdempotencyKey: sharedhash.HashFields(
			"aggregation.stats",
			evt.Stats.Venue,
			evt.Stats.Instrument,
			evt.Stats.Timeframe,
			strconv.FormatInt(evt.Stats.WindowStartTs, 10),
		),
		ContentType: contentType,
		Payload:     payload,
	}
	if p := env.Validate(); p != nil {
		return p
	}
	return a.pub.Publish(ctx, env)
}

// chooseArtifactContentType selects proto or JSON based on rollout flags.
func chooseArtifactContentType(eventType string) string {
	if contracts.ProtoRolloutEnabledForEventType(eventType) {
		return envelope.ContentTypeProto
	}
	return envelope.ContentTypeJSON
}

// domainCandleToWireDTO converts a domain CandleClosed to the shared wire DTO.
func domainCandleToWireDTO(evt aggdomain.CandleClosed) contracts.AggregationCandleClosedV1 {
	c := evt.Candle
	return contracts.AggregationCandleClosedV1{
		Candle: contracts.AggregationCandleV1{
			Venue:         c.Venue,
			Instrument:    c.Instrument,
			Timeframe:     c.Timeframe,
			WindowStartTs: c.WindowStartTs,
			WindowEndTs:   c.WindowEndTs,
			Open:          c.Open,
			High:          c.High,
			Low:           c.Low,
			ClosePrice:    c.ClosePrice,
			Volume:        c.Volume,
			BuyVolume:     c.BuyVolume,
			SellVolume:    c.SellVolume,
			TradeCount:    c.TradeCount,
			SeqFirst:      c.SeqFirst,
			SeqLast:       c.SeqLast,
			IsClosed:      c.IsClosed,
		},
	}
}

// domainStatsToWireDTO converts a domain StatsWindowClosed to the shared wire DTO.
func domainStatsToWireDTO(evt aggdomain.StatsWindowClosed) contracts.AggregationStatsWindowClosedV1 {
	s := evt.Stats
	return contracts.AggregationStatsWindowClosedV1{
		Stats: contracts.AggregationStatsWindowV1{
			Venue:           s.Venue,
			Instrument:      s.Instrument,
			Timeframe:       s.Timeframe,
			WindowStartTs:   s.WindowStartTs,
			WindowEndTs:     s.WindowEndTs,
			LiqBuyVolume:    s.LiqBuyVolume,
			LiqSellVolume:   s.LiqSellVolume,
			LiqTotalVolume:  s.LiqTotalVolume,
			LiqCount:        s.LiqCount,
			MarkPriceOpen:   s.MarkPriceOpen,
			MarkPriceHigh:   s.MarkPriceHigh,
			MarkPriceLow:    s.MarkPriceLow,
			MarkPriceClose:  s.MarkPriceClose,
			FundingRateAvg:  s.FundingRateAvg,
			FundingRateLast: s.FundingRateLast,
			SeqFirst:        s.SeqFirst,
			SeqLast:         s.SeqLast,
			IsClosed:        s.IsClosed,
		},
	}
}

// domainSnapshotToWireDTO converts a domain SnapshotProduced to the shared wire DTO.
func domainSnapshotToWireDTO(snap aggdomain.SnapshotProduced) contracts.AggregationSnapshotV1 {
	bids := make([]contracts.AggregationOrderBookLevelV1, len(snap.Bids))
	for i, b := range snap.Bids {
		bids[i] = contracts.AggregationOrderBookLevelV1{Price: float64(b.Price), Quantity: float64(b.Quantity)}
	}
	asks := make([]contracts.AggregationOrderBookLevelV1, len(snap.Asks))
	for i, a := range snap.Asks {
		asks[i] = contracts.AggregationOrderBookLevelV1{Price: float64(a.Price), Quantity: float64(a.Quantity)}
	}
	return contracts.AggregationSnapshotV1{
		Venue:      snap.BookID.Venue,
		Instrument: snap.BookID.Instrument,
		Seq:        snap.Seq,
		Bids:       bids,
		Asks:       asks,
	}
}

// domainInconsistentToWireDTO converts a domain OrderBookInconsistentDetected to the shared wire DTO.
func domainInconsistentToWireDTO(evt aggdomain.OrderBookInconsistentDetected) contracts.AggregationOrderBookInconsistencyV1 {
	return contracts.AggregationOrderBookInconsistencyV1{
		Venue:      evt.BookID.Venue,
		Instrument: evt.BookID.Instrument,
		Seq:        evt.Seq,
		Reason:     evt.Reason,
	}
}
