package jetstream

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.ArtifactPublisher = (*ArtifactPublisher)(nil)

const instrumentMarketTypeMetaKey = "instrument_market_type"

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
	nowMs := a.clock()
	wireDTO := domainSnapshotToWireDTO(snap, nowMs)
	contentType := chooseArtifactContentType("aggregation.snapshot")
	payload, p := codec.EncodePayload("aggregation.snapshot", 1, contentType, wireDTO)
	if p != nil {
		return p
	}
	metrics.ObserveMROrderBookPublishDepth(snap.BookID.Venue, "bid", len(wireDTO.Bids))
	metrics.ObserveMROrderBookPublishDepth(snap.BookID.Venue, "ask", len(wireDTO.Asks))
	metrics.ObserveMROrderBookWireBytes(snap.BookID.Venue, len(payload))
	env := envelope.Envelope{
		Type:       "aggregation.snapshot",
		Version:    1,
		Venue:      snap.BookID.Venue,
		Instrument: naming.StripMarketType(snap.BookID.Instrument),
		TsIngest:   nowMs,
		Seq:        snap.Seq,
		IdempotencyKey: sharedhash.HashFieldsFast(
			"aggregation.snapshot",
			snap.BookID.Venue,
			snap.BookID.Instrument,
			strconv.FormatInt(snap.Seq, 10),
		),
		ContentType: contentType,
		Payload:     payload,
		Meta:        artifactMetaForInstrument(snap.BookID.Instrument, nil),
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
		Instrument: naming.StripMarketType(evt.BookID.Instrument),
		TsIngest:   a.clock(),
		Seq:        evt.Seq,
		IdempotencyKey: sharedhash.HashFieldsFast(
			"aggregation.orderbook_inconsistency",
			evt.BookID.Venue,
			evt.BookID.Instrument,
			strconv.FormatInt(evt.Seq, 10),
			evt.Reason,
		),
		ContentType: contentType,
		Payload:     payload,
		Meta:        artifactMetaForInstrument(evt.BookID.Instrument, nil),
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
		Instrument: naming.StripMarketType(evt.Candle.Instrument),
		TsIngest:   a.clock(),
		Seq:        evt.Candle.SeqLast,
		IdempotencyKey: sharedhash.HashFieldsFast(
			"aggregation.candle",
			evt.Candle.Venue,
			evt.Candle.Instrument,
			evt.Candle.Timeframe,
			strconv.FormatInt(evt.Candle.WindowStartTs, 10),
		),
		ContentType: contentType,
		Payload:     payload,
		Meta: artifactMetaForInstrument(
			evt.Candle.Instrument,
			map[string]string{"timeframe": evt.Candle.Timeframe},
		),
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
	metrics.ObserveMRStatsWireBytes(evt.Stats.Venue, evt.Stats.Timeframe, len(payload))
	metrics.ObserveMRStatsQualityFlags(
		evt.Stats.Venue,
		evt.Stats.Instrument,
		evt.Stats.Timeframe,
		evt.Stats.QualityFlags,
	)
	env := envelope.Envelope{
		Type:       "aggregation.stats",
		Version:    1,
		Venue:      evt.Stats.Venue,
		Instrument: naming.StripMarketType(evt.Stats.Instrument),
		TsIngest:   a.clock(),
		Seq:        evt.Stats.SeqLast,
		IdempotencyKey: sharedhash.HashFieldsFast(
			"aggregation.stats",
			evt.Stats.Venue,
			evt.Stats.Instrument,
			evt.Stats.Timeframe,
			strconv.FormatInt(evt.Stats.WindowStartTs, 10),
		),
		ContentType: contentType,
		Payload:     payload,
		Meta: artifactMetaForInstrument(
			evt.Stats.Instrument,
			map[string]string{"timeframe": evt.Stats.Timeframe},
		),
	}
	if p := env.Validate(); p != nil {
		return p
	}
	return a.pub.Publish(ctx, env)
}

// PublishTapeClosed publishes a closed tape window event using the codec registry.
func (a *ArtifactPublisher) PublishTapeClosed(ctx context.Context, evt aggdomain.TapeClosed) *problem.Problem {
	wireDTO := domainTapeToWireDTO(evt)
	contentType := chooseArtifactContentType("aggregation.tape")
	payload, p := codec.EncodePayload("aggregation.tape", 1, contentType, wireDTO)
	if p != nil {
		return p
	}
	metrics.ObserveMRTapeWireBytes(evt.Window.Venue, evt.Window.Timeframe, len(payload))
	metrics.ObserveMRTapeQuality(
		evt.Window.Venue,
		evt.Window.Instrument,
		evt.Window.Timeframe,
		evt.Window.TradeCount,
		evt.Window.WindowStartTs,
		evt.Window.WindowEndTs,
		evt.Window.LastSeq,
		evt.Window.LastPrice,
		evt.Window.TotalVolume,
	)
	env := envelope.Envelope{
		Type:       "aggregation.tape",
		Version:    1,
		Venue:      evt.Window.Venue,
		Instrument: naming.StripMarketType(evt.Window.Instrument),
		TsIngest:   evt.Window.WindowEndTs,
		Seq:        evt.Window.LastSeq,
		IdempotencyKey: sharedhash.HashFieldsFast(
			"aggregation.tape",
			evt.Window.Venue,
			evt.Window.Instrument,
			evt.Window.Timeframe,
			strconv.FormatInt(evt.Window.WindowStartTs, 10),
		),
		ContentType: contentType,
		Payload:     payload,
		Meta: artifactMetaForInstrument(
			evt.Window.Instrument,
			map[string]string{"timeframe": evt.Window.Timeframe},
		),
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

func artifactMetaForInstrument(instrument string, base map[string]string) map[string]string {
	marketType := marketTypeFromInstrument(instrument)
	if marketType == "" {
		return base
	}
	if base == nil {
		base = make(map[string]string, 1)
	}
	base[instrumentMarketTypeMetaKey] = marketType
	return base
}

func marketTypeFromInstrument(instrument string) string {
	idx := strings.IndexByte(strings.TrimSpace(instrument), ':')
	if idx < 0 {
		return ""
	}
	mt := strings.ToUpper(strings.TrimSpace(instrument[idx+1:]))
	if mt == "" {
		return ""
	}
	return mt
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
			WindowMs:        s.WindowMs,
			TsIngestMs:      s.TsIngestMs,
			QualityFlags:    s.QualityFlags,
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

func domainTapeToWireDTO(evt aggdomain.TapeClosed) contracts.AggregationTapeV1 {
	t := evt.Window
	return contracts.AggregationTapeV1{
		Venue:         t.Venue,
		Instrument:    t.Instrument,
		Timeframe:     t.Timeframe,
		WindowStartTs: t.WindowStartTs,
		WindowEndTs:   t.WindowEndTs,
		TradeCount:    t.TradeCount,
		BuyCount:      t.BuyCount,
		SellCount:     t.SellCount,
		BuyVolume:     t.BuyVolume,
		SellVolume:    t.SellVolume,
		TotalVolume:   t.TotalVolume,
		BuyNotional:   t.BuyNotional,
		SellNotional:  t.SellNotional,
		VwapPrice:     t.VwapPrice,
		MaxPrice:      t.MaxPrice,
		MinPrice:      t.MinPrice,
		LastPrice:     t.LastPrice,
		MaxTradeSize:  t.MaxTradeSize,
		Rate:          t.Rate(),
		Imbalance:     t.Imbalance(),
		IsBurst:       evt.IsBurst,
		Seq:           t.LastSeq,
		TsIngestMs:    t.WindowEndTs,
	}
}

// domainSnapshotToWireDTO converts a domain SnapshotProduced to the shared wire DTO.
func domainSnapshotToWireDTO(snap aggdomain.SnapshotProduced, nowMs int64) contracts.AggregationSnapshotV2 {
	bids := snapshotLevelsToWire(snap.Bids)
	asks := snapshotLevelsToWire(snap.Asks)
	tsIngestMs := snapshotTsIngestMs(snap.TsIngestMs, nowMs)
	bidCount, askCount := snapshotLevelCounts(snap)
	bestBid, bestAsk := snapshotBestPrices(snap)
	spreadBPS := snapshotSpreadBPS(snap.SpreadBPS, bestBid, bestAsk)
	version := snapshotVersion(snap.Version)
	checksum := snapshotChecksum(snap)
	return contracts.AggregationSnapshotV2{
		Venue:        snap.BookID.Venue,
		Instrument:   snap.BookID.Instrument,
		Seq:          snap.Seq,
		Bids:         bids,
		Asks:         asks,
		BestBidPrice: bestBid,
		BestAskPrice: bestAsk,
		SpreadBPS:    spreadBPS,
		Checksum:     checksum,
		TsIngestMs:   tsIngestMs,
		BidCount:     bidCount,
		AskCount:     askCount,
		DepthCap:     snap.DepthCap,
		Version:      version,
	}
}

func snapshotLevelsToWire(levels []aggdomain.Level) []contracts.AggregationOrderBookLevelV1 {
	if len(levels) == 0 {
		return nil
	}
	out := make([]contracts.AggregationOrderBookLevelV1, len(levels))
	for i, lvl := range levels {
		out[i] = contracts.AggregationOrderBookLevelV1{
			Price:    float64(lvl.Price),
			Quantity: float64(lvl.Quantity),
		}
	}
	return out
}

func snapshotTsIngestMs(tsIngestMs, nowMs int64) int64 {
	if tsIngestMs > 0 {
		return tsIngestMs
	}
	return nowMs
}

func snapshotLevelCounts(snap aggdomain.SnapshotProduced) (bidCount, askCount int) {
	bidCount = snap.BidCount
	if bidCount <= 0 {
		bidCount = len(snap.Bids)
	}
	askCount = snap.AskCount
	if askCount <= 0 {
		askCount = len(snap.Asks)
	}
	return bidCount, askCount
}

func snapshotBestPrices(snap aggdomain.SnapshotProduced) (bestBid, bestAsk float64) {
	bestBid = snap.BestBidPrice
	if bestBid <= 0 && len(snap.Bids) > 0 {
		bestBid = float64(snap.Bids[0].Price)
	}
	bestAsk = snap.BestAskPrice
	if bestAsk <= 0 && len(snap.Asks) > 0 {
		bestAsk = float64(snap.Asks[0].Price)
	}
	return bestBid, bestAsk
}

func snapshotSpreadBPS(spreadBPS, bestBid, bestAsk float64) float64 {
	if spreadBPS != 0 || bestBid <= 0 || bestAsk <= 0 {
		return spreadBPS
	}
	mid := (bestBid + bestAsk) * 0.5
	if mid <= 0 {
		return 0
	}
	return ((bestAsk - bestBid) / mid) * 10_000
}

func snapshotVersion(version int) int {
	if version > 0 {
		return version
	}
	return 2
}

func snapshotChecksum(snap aggdomain.SnapshotProduced) uint32 {
	if snap.Checksum != 0 {
		return snap.Checksum
	}
	return aggdomain.ComputeOrderBookChecksum(snap.Bids, snap.Asks)
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
