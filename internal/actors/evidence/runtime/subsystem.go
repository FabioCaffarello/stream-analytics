package evidenceruntime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/anthdm/hollywood/actor"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	evidenceapp "github.com/market-raccoon/internal/core/evidence/app"
	"github.com/market-raccoon/internal/core/evidence/domain"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	envelopeTypeTrade     = "marketdata.trade"
	envelopeTypeBookDelta = "marketdata.bookdelta"
	envelopeTypeCandle    = "aggregation.candle"
	envelopeTypeTape      = "aggregation.tape"
	envelopeTypeSnapshot  = "aggregation.snapshot"

	defaultRegimeMaxStreams = 1024
	defaultRegimeHistoryCap = 20
)

type evidenceEnvelopeMsg struct {
	Envelope envelope.Envelope
}

// EventPublisher publishes evidence envelopes to the runtime bus.
type EventPublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

// SubsystemConfig configures the Evidence subsystem actor.
type SubsystemConfig struct {
	Logger          *slog.Logger
	EnvelopeCh      <-chan envelope.Envelope
	Engine          *evidenceapp.EvidenceEngine
	LELEngine       *evidenceapp.LELEngine
	RegimeStore     *domain.RegimeStore
	RegimeDetectors []evidenceapp.RegimeDetector
	RouterPID       *actor.PID // delivery router to publish evidence envelopes
	Publisher       EventPublisher
	ReplicaID       int
	ReplicaCount    int
}

// SubsystemActor owns the evidence engine lifecycle.
type SubsystemActor struct {
	cfg             SubsystemConfig
	logger          *slog.Logger
	engine          *actor.Engine
	selfPID         *actor.PID
	regimeStore     *domain.RegimeStore
	regimeDetectors []evidenceapp.RegimeDetector
	lelEngine       *evidenceapp.LELEngine
	replicaID       int
	replicaCount    int
	consumeCtx      context.Context
	cancel          context.CancelFunc
}

// NewSubsystemActor creates the evidence subsystem actor producer.
func NewSubsystemActor(cfg SubsystemConfig) actor.Producer {
	return func() actor.Receiver {
		return &SubsystemActor{cfg: cfg}
	}
}

func (s *SubsystemActor) Receive(c *actor.Context) {
	s.ensureDefaults(c)

	switch msg := c.Message().(type) {
	case actor.Initialized:
	case actor.Started:
		s.onStarted(c)
	case actor.Stopped:
		s.onStopped()
	case evidenceEnvelopeMsg:
		s.processEnvelope(msg.Envelope)
	case actorruntime.ChildFailed:
		if c.Parent() != nil {
			c.Send(c.Parent(), msg)
		}
	default:
		s.logger.Warn("evidence subsystem: unknown message", "type", fmt.Sprintf("%T", msg))
	}
}

func (s *SubsystemActor) ensureDefaults(c *actor.Context) {
	if s.logger == nil {
		if s.cfg.Logger != nil {
			s.logger = s.cfg.Logger
		} else {
			s.logger = slog.Default()
		}
	}
	if s.engine == nil && c != nil {
		s.engine = c.Engine()
		s.selfPID = c.PID()
	}
	if s.regimeStore == nil {
		s.regimeStore = s.cfg.RegimeStore
		if s.regimeStore == nil {
			policy, _ := domain.NewRegimeStorePolicy(defaultRegimeMaxStreams, defaultRegimeHistoryCap)
			s.regimeStore = domain.NewRegimeStore(policy)
		}
	}
	if len(s.regimeDetectors) == 0 {
		if len(s.cfg.RegimeDetectors) > 0 {
			s.regimeDetectors = append([]evidenceapp.RegimeDetector(nil), s.cfg.RegimeDetectors...)
		} else {
			s.regimeDetectors = []evidenceapp.RegimeDetector{
				evidenceapp.NewBreakoutRegimeDetector(evidenceapp.DefaultBreakoutPolicy()),
				evidenceapp.NewTrendRegimeDetector(evidenceapp.DefaultTrendPolicy()),
				evidenceapp.NewVolatilityRegimeDetector(evidenceapp.DefaultVolatilityPolicy()),
			}
		}
	}
	if s.lelEngine == nil {
		s.lelEngine = s.cfg.LELEngine
	}
	if s.replicaCount <= 0 {
		s.replicaCount = s.cfg.ReplicaCount
		if envCount := strings.TrimSpace(os.Getenv("PROCESSOR_REPLICAS")); envCount != "" {
			if parsed, err := strconv.Atoi(envCount); err == nil && parsed > 0 {
				s.replicaCount = parsed
			}
		}
		if s.replicaCount <= 0 {
			s.replicaCount = 1
		}
	}
	if s.replicaID < 0 {
		s.replicaID = 0
	}
	if s.replicaID == 0 {
		s.replicaID = s.cfg.ReplicaID
		if envID := strings.TrimSpace(os.Getenv("PROCESSOR_REPLICA_ID")); envID != "" {
			if parsed, err := strconv.Atoi(envID); err == nil && parsed >= 0 {
				s.replicaID = parsed
			}
		}
	}
	if s.replicaID < 0 || s.replicaID >= s.replicaCount {
		s.replicaID = 0
	}
}

func (s *SubsystemActor) onStarted(_ *actor.Context) {
	s.logger.Info("evidence subsystem started", "replica_id", s.replicaID, "replica_count", s.replicaCount)
	if s.cfg.EnvelopeCh != nil && s.engine != nil && s.selfPID != nil {
		s.consumeCtx, s.cancel = context.WithCancel(context.Background())
		go s.consumeLoop()
	}
}

func (s *SubsystemActor) onStopped() {
	if s.cancel != nil {
		s.cancel()
	}
	s.logger.Info("evidence subsystem stopped")
}

func (s *SubsystemActor) consumeLoop() {
	for {
		select {
		case <-s.consumeCtx.Done():
			return
		case env, ok := <-s.cfg.EnvelopeCh:
			if !ok {
				return
			}
			s.engine.Send(s.selfPID, evidenceEnvelopeMsg{Envelope: env})
		}
	}
}

func (s *SubsystemActor) processEnvelope(env envelope.Envelope) {
	if s.processLELEnvelope(env) {
		return
	}

	ruleEvent, ok := s.toRuleEvent(env)
	if !ok {
		return
	}

	if ruleEvent.Kind == domain.EventKindCandle {
		s.processRegimeEvent(env, ruleEvent)
		return
	}
	if s.cfg.Engine == nil {
		return
	}

	evidenceEvents := s.cfg.Engine.OnEvent(ruleEvent)

	for i := range evidenceEvents {
		s.emitEvidence(env, evidenceEvents[i])
	}
}

func (s *SubsystemActor) processLELEnvelope(env envelope.Envelope) bool {
	if s.lelEngine == nil {
		return false
	}
	var (
		in domain.LELEvent
		ok bool
	)
	switch env.Type {
	case envelopeTypeSnapshot:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			return true
		}
		snapshot, castOK := decoded.(contracts.AggregationSnapshotV2)
		if !castOK {
			return true
		}
		in = buildLELSnapshotEvent(env, snapshot)
		ok = true
	case envelopeTypeTape:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			return true
		}
		tape, castOK := decoded.(contracts.AggregationTapeV1)
		if !castOK {
			return true
		}
		in = buildLELTapeEvent(env, tape)
		ok = true
	default:
		return false
	}
	if !ok {
		return true
	}
	if !s.isLELOwner(in.Venue, in.Symbol) {
		return true
	}
	out := s.lelEngine.OnEvent(in)
	for i := range out {
		s.emitLiquidityEvidence(env, out[i])
	}
	return true
}

func (s *SubsystemActor) toRuleEvent(env envelope.Envelope) (domain.RuleEvent, bool) {
	streamID := env.Venue + "/" + env.Instrument + "/" + strings.ToLower(strings.TrimSpace(env.Type))
	if key, p := marketmodel.NewStreamKey(env.Venue, env.Instrument, channelFromEnvelopeType(env.Type)); p == nil {
		streamID = key.String()
	}
	base := domain.RuleEvent{
		Venue:    env.Venue,
		Symbol:   env.Instrument,
		StreamID: streamID,
		TsServer: env.TsIngest,
		Seq:      env.Seq,
	}

	switch env.Type {
	case envelopeTypeTrade:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			return domain.RuleEvent{}, false
		}
		trade, ok := decoded.(marketmodel.Trade)
		if !ok {
			return domain.RuleEvent{}, false
		}
		base.Kind = domain.EventKindTrade
		base.TradePrice = trade.Price
		base.TradeSize = trade.Size
		base.TradeSide = trade.Side
		return base, true

	case envelopeTypeBookDelta:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			return domain.RuleEvent{}, false
		}
		book, ok := decoded.(marketmodel.BookDelta)
		if !ok {
			return domain.RuleEvent{}, false
		}
		base.Kind = domain.EventKindBook
		if len(book.Bids) > 0 {
			base.BestBid = book.Bids[0].Price
		}
		if len(book.Asks) > 0 {
			base.BestAsk = book.Asks[0].Price
		}
		base.BidDepth = sumDepth(book.Bids)
		base.AskDepth = sumDepth(book.Asks)
		base.BidLevels = len(book.Bids)
		base.AskLevels = len(book.Asks)
		return base, true

	case envelopeTypeCandle:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			return domain.RuleEvent{}, false
		}
		candle, ok := decoded.(contracts.AggregationCandleClosedV1)
		if !ok || !candle.Candle.IsClosed {
			return domain.RuleEvent{}, false
		}
		base.Kind = domain.EventKindCandle
		base.CandleOpen = candle.Candle.Open
		base.CandleHigh = candle.Candle.High
		base.CandleLow = candle.Candle.Low
		base.CandleClose = candle.Candle.ClosePrice
		base.CandleVolume = candle.Candle.Volume
		base.CandleBuyVol = candle.Candle.BuyVolume
		base.CandleSellVol = candle.Candle.SellVolume
		base.CandleWindowStart = candle.Candle.WindowStartTs
		base.CandleWindowEnd = candle.Candle.WindowEndTs
		base.CandleTimeframe = candleTimeframe(env, candle)
		return base, true

	default:
		return domain.RuleEvent{}, false
	}
}

func sumDepth(levels []marketmodel.Level) float64 {
	total := 0.0
	for _, l := range levels {
		total += l.Size
	}
	return total
}

func candleTimeframe(env envelope.Envelope, candle contracts.AggregationCandleClosedV1) string {
	if tf := strings.TrimSpace(candle.Candle.Timeframe); tf != "" {
		return tf
	}
	if tf := strings.TrimSpace(env.Meta["timeframe"]); tf != "" {
		return tf
	}
	return "raw"
}

func (s *SubsystemActor) processRegimeEvent(triggerEnv envelope.Envelope, event domain.RuleEvent) {
	if s.regimeStore == nil || len(s.regimeDetectors) == 0 {
		return
	}
	key := domain.RegimeStoreKey{
		Venue:      event.Venue,
		Instrument: event.Symbol,
		Timeframe:  event.CandleTimeframe,
	}
	sample := domain.RegimeCandleSample{
		TsServer:    event.TsServer,
		WindowStart: event.CandleWindowStart,
		WindowEnd:   event.CandleWindowEnd,
		Open:        event.CandleOpen,
		High:        event.CandleHigh,
		Low:         event.CandleLow,
		Close:       event.CandleClose,
		Volume:      event.CandleVolume,
	}
	if p := s.regimeStore.PutCandle(key, sample); p != nil {
		s.logger.Warn("evidence: failed to append candle sample to regime store", "code", p.Code, "message", p.Message)
		return
	}

	candles := s.regimeStore.Candles(key)
	signal, ok := s.detectRegimeSignal(key, candles)
	if !ok {
		return
	}

	prev, hadPrev := s.regimeStore.LastRegime(key)
	if p := s.regimeStore.PutRegime(key, signal); p != nil {
		s.logger.Warn("evidence: failed to append regime signal to store", "code", p.Code, "message", p.Message)
		return
	}

	metrics.SetMRRegimeCurrent(signal.Venue, signal.Instrument, signal.Timeframe, string(signal.Kind))
	metrics.SetMRRegimeStrength(signal.Venue, signal.Instrument, signal.Timeframe, signal.Strength)
	if hadPrev && prev.Kind != signal.Kind {
		metrics.IncMRRegimeTransition(signal.Venue, signal.Instrument, signal.Timeframe, string(prev.Kind), string(signal.Kind))
	}
	if signal.WindowEnd > signal.WindowStart {
		metrics.ObserveMRRegimeDetectionDuration(
			signal.Venue,
			signal.Instrument,
			signal.Timeframe,
			time.Duration(signal.WindowEnd-signal.WindowStart)*time.Millisecond,
		)
	}

	s.emitRegime(triggerEnv, signal)
}

func (s *SubsystemActor) detectRegimeSignal(key domain.RegimeStoreKey, candles []domain.RegimeCandleSample) (domain.RegimeSignal, bool) {
	best := domain.RegimeSignal{}
	bestScore := -1.0
	for i := range s.regimeDetectors {
		detector := s.regimeDetectors[i]
		signal, ok := detector.Detect(key, candles)
		if !ok {
			continue
		}
		score := signal.Strength * signal.Confidence
		if score > bestScore {
			best = signal
			bestScore = score
		}
	}
	return best, bestScore >= 0
}

func (s *SubsystemActor) emitEvidence(triggerEnv envelope.Envelope, ev domain.EvidenceEvent) {
	payload, p := codec.EncodePayload(domain.MicrostructureEvidenceType, domain.MicrostructureEvidenceVersion, envelope.ContentTypeJSON, ev)
	if p != nil {
		s.logger.Warn("evidence: failed to encode evidence payload", "err", p.Message)
		return
	}

	evidenceEnv := envelope.Envelope{
		Type:        domain.MicrostructureEvidenceType,
		Version:     domain.MicrostructureEvidenceVersion,
		Venue:       ev.Venue,
		Instrument:  ev.Symbol,
		TsIngest:    ev.TsServer,
		Seq:         ev.Seq,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
	}

	if s.cfg.Publisher != nil {
		if p := s.cfg.Publisher.Publish(context.Background(), evidenceEnv); p != nil {
			s.logger.Warn("evidence: failed to publish evidence envelope", "code", p.Code, "message", p.Message)
		}
		return
	}

	if s.cfg.RouterPID != nil && s.engine != nil {
		s.engine.Send(s.cfg.RouterPID, deliveryruntime.DeliverEnvelope{Envelope: evidenceEnv})
	}
}

func (s *SubsystemActor) emitLiquidityEvidence(_ envelope.Envelope, ev domain.LiquidityEvidence) {
	payloadDTO := contracts.LiquidityEvidenceV1{
		EvidenceType: string(ev.EvidenceType),
		TsIngestMs:   ev.TsIngestMs,
		Venue:        ev.Venue,
		Symbol:       ev.Symbol,
		WindowMs:     ev.WindowMs,
		Severity:     string(ev.Severity),
		Confidence:   ev.Confidence,
		Explain:      append([]string(nil), ev.Explain...),
		Version:      ev.Version,
		StreamID:     ev.StreamID,
		Seq:          ev.Seq,
		Watermark: contracts.LiquidityInputWatermark{
			SeqStart: ev.Watermark.SeqStart,
			SeqEnd:   ev.Watermark.SeqEnd,
		},
	}
	if len(ev.Metrics) > 0 {
		payloadDTO.Metrics = make([]contracts.LiquidityEvidenceMetric, len(ev.Metrics))
		for i := range ev.Metrics {
			payloadDTO.Metrics[i] = contracts.LiquidityEvidenceMetric{Key: ev.Metrics[i].Key, Value: ev.Metrics[i].Value}
		}
	}

	payload, p := codec.EncodePayload(domain.LiquidityEvidenceEventType, int(domain.LiquidityEvidenceVersion), envelope.ContentTypeJSON, payloadDTO)
	if p != nil {
		s.logger.Warn("evidence: failed to encode liquidity evidence payload", "err", p.Message)
		metrics.IncLELEvidenceDropped("invalid_evidence")
		return
	}
	metrics.ObserveLELWireBudget(string(ev.EvidenceType), len(payload))

	out := envelope.Envelope{
		Type:        domain.LiquidityEvidenceEventType,
		Version:     int(domain.LiquidityEvidenceVersion),
		Venue:       ev.Venue,
		Instrument:  ev.Symbol,
		TsIngest:    ev.TsIngestMs,
		Seq:         ev.Seq,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
	}
	if s.cfg.Publisher != nil {
		if p := s.cfg.Publisher.Publish(context.Background(), out); p != nil {
			s.logger.Warn("evidence: failed to publish liquidity evidence envelope", "code", p.Code, "message", p.Message)
		}
		return
	}
	if s.cfg.RouterPID != nil && s.engine != nil {
		s.engine.Send(s.cfg.RouterPID, deliveryruntime.DeliverEnvelope{Envelope: out})
	}
}

func channelFromEnvelopeType(eventType string) marketmodel.Channel {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case envelopeTypeTrade:
		return marketmodel.ChannelTrade
	case envelopeTypeBookDelta:
		return marketmodel.ChannelBookDelta
	case envelopeTypeCandle:
		return marketmodel.ChannelCandle
	default:
		return marketmodel.ChannelTrade
	}
}

func buildLELSnapshotEvent(env envelope.Envelope, snapshot contracts.AggregationSnapshotV2) domain.LELEvent {
	ts := env.TsIngest
	if snapshot.TsIngestMs > 0 {
		ts = snapshot.TsIngestMs
	}
	seq := env.Seq
	if snapshot.Seq > 0 {
		seq = snapshot.Seq
	}
	return domain.LELEvent{
		Kind:      domain.LELEventKindSnapshot,
		Venue:     pickFirst(snapshot.Venue, env.Venue),
		Symbol:    pickFirst(snapshot.Instrument, env.Instrument),
		StreamID:  env.Venue + "|" + env.Instrument,
		TsServer:  ts,
		Seq:       seq,
		BestBid:   snapshot.BestBidPrice,
		BestAsk:   snapshot.BestAskPrice,
		SpreadBPS: snapshot.SpreadBPS,
		BidDepth:  sumSnapshotDepth(snapshot.Bids),
		AskDepth:  sumSnapshotDepth(snapshot.Asks),
		BidLevels: len(snapshot.Bids),
		AskLevels: len(snapshot.Asks),
	}
}

func buildLELTapeEvent(env envelope.Envelope, tape contracts.AggregationTapeV1) domain.LELEvent {
	ts := env.TsIngest
	if tape.TsIngestMs > 0 {
		ts = tape.TsIngestMs
	}
	seq := env.Seq
	if tape.Seq > 0 {
		seq = tape.Seq
	}
	return domain.LELEvent{
		Kind:          domain.LELEventKindTape,
		Venue:         pickFirst(tape.Venue, env.Venue),
		Symbol:        pickFirst(tape.Instrument, env.Instrument),
		StreamID:      env.Venue + "|" + env.Instrument,
		TsServer:      ts,
		Seq:           seq,
		TradeCount:    tape.TradeCount,
		BuyVolume:     tape.BuyVolume,
		SellVolume:    tape.SellVolume,
		TotalVolume:   tape.TotalVolume,
		VwapPrice:     tape.VwapPrice,
		MaxPrice:      tape.MaxPrice,
		MinPrice:      tape.MinPrice,
		Rate:          tape.Rate,
		Imbalance:     tape.Imbalance,
		IsBurst:       tape.IsBurst,
		WindowStartTs: tape.WindowStartTs,
		WindowEndTs:   tape.WindowEndTs,
	}
}

func sumSnapshotDepth(levels []contracts.AggregationOrderBookLevelV1) float64 {
	total := 0.0
	for i := range levels {
		total += levels[i].Quantity
	}
	return total
}

func pickFirst(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func (s *SubsystemActor) isLELOwner(venue, symbol string) bool {
	if s.replicaCount <= 1 {
		return true
	}
	owner := sharedhash.SumFieldsFast64(strings.ToLower(strings.TrimSpace(venue)), strings.ToLower(strings.TrimSpace(symbol))) % uint64(s.replicaCount)
	return owner == uint64(s.replicaID) //nolint:gosec // replicaID is bounded by replicaCount at startup.
}

func (s *SubsystemActor) emitRegime(triggerEnv envelope.Envelope, signal domain.RegimeSignal) {
	payload, p := codec.EncodePayload(domain.RegimeEvidenceType, domain.RegimeEvidenceVersion, envelope.ContentTypeJSON, signal)
	if p != nil {
		s.logger.Warn("evidence: failed to encode regime payload", "err", p.Message)
		return
	}

	regimeEnv := envelope.Envelope{
		Type:        domain.RegimeEvidenceType,
		Version:     domain.RegimeEvidenceVersion,
		Venue:       signal.Venue,
		Instrument:  signal.Instrument,
		TsIngest:    triggerEnv.TsIngest,
		Seq:         triggerEnv.Seq,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
		Meta: map[string]string{
			"timeframe": signal.Timeframe,
		},
	}

	if s.cfg.Publisher != nil {
		if p := s.cfg.Publisher.Publish(context.Background(), regimeEnv); p != nil {
			s.logger.Warn("evidence: failed to publish regime envelope", "code", p.Code, "message", p.Message)
		}
		return
	}

	if s.cfg.RouterPID != nil && s.engine != nil {
		s.engine.Send(s.cfg.RouterPID, deliveryruntime.DeliverEnvelope{Envelope: regimeEnv})
	}
}
