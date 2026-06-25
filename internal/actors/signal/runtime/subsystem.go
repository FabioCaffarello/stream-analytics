package signalruntime

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
	"github.com/market-raccoon/internal/contracts"
	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	signalcore "github.com/market-raccoon/internal/core/signal"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	envelopeTypeTrade     = "marketdata.trade"
	envelopeTypeBookDelta = "marketdata.bookdelta"
	envelopeTypeCandle    = "aggregation.candle"
	envelopeTypeStats     = "aggregation.stats"
	envelopeTypeLiquidity = evidencedomain.LiquidityEvidenceEventType
	envelopeTypeRegime    = evidencedomain.RegimeEvidenceType
)

type signalEnvelopeMsg struct {
	Envelope envelope.Envelope
}

type EventPublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

type SubsystemConfig struct {
	Logger        *slog.Logger
	EnvelopeCh    <-chan envelope.Envelope
	Engine        *signalcore.SignalEngine
	Publisher     EventPublisher
	RouterPID     *actor.PID
	ReplicaID     int
	ReplicaCount  int
	TenantMetaKey string
}

type SubsystemActor struct {
	cfg        SubsystemConfig
	logger     *slog.Logger
	engine     *actor.Engine
	selfPID    *actor.PID
	consumeCtx context.Context
	cancel     context.CancelFunc

	signalEngine  *signalcore.SignalEngine
	replicaID     int
	replicaCount  int
	tenantMetaKey string

	streamState    map[string]streamProgress
	streamOrder    []string
	streamOrderIdx int

	dropSamples      map[dropSampleKey]int
	dropSampleDrops  int
	dropSampleWindow int
}

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
		s.onStarted()
	case actor.Stopped:
		s.onStopped()
	case signalEnvelopeMsg:
		s.processEnvelope(msg.Envelope)
	case actorruntime.ChildFailed:
		if c.Parent() != nil {
			c.Send(c.Parent(), msg)
		}
	default:
		s.logger.Warn("signal subsystem: unknown message", "type", fmt.Sprintf("%T", msg))
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
	if s.signalEngine == nil {
		s.signalEngine = s.cfg.Engine
		if s.signalEngine == nil {
			s.signalEngine = signalcore.NewSignalEngine(signalcore.DefaultEngineConfig(), nil)
		}
		s.signalEngine.SetEmitter(s)
	}
	if s.tenantMetaKey == "" {
		s.tenantMetaKey = strings.TrimSpace(s.cfg.TenantMetaKey)
		if s.tenantMetaKey == "" {
			s.tenantMetaKey = "tenant_id"
		}
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
	if s.streamState == nil {
		s.streamState = make(map[string]streamProgress, signalStateMaxStreams)
		metrics.SetBoundedMapSize(signalsBoundedMapName, 0)
		metrics.SetOwnershipContractEntries("signals", 0)
	}
	if s.streamOrder == nil {
		s.streamOrder = make([]string, 0, signalStateMaxStreams)
	}
	if s.dropSamples == nil {
		s.dropSamples = make(map[dropSampleKey]int)
	}
}

func (s *SubsystemActor) onStarted() {
	s.logger.Info("signal subsystem started", "replica_id", s.replicaID, "replica_count", s.replicaCount)
	if s.cfg.EnvelopeCh != nil && s.engine != nil && s.selfPID != nil {
		s.consumeCtx, s.cancel = context.WithCancel(context.Background())
		go s.consumeLoop()
	}
}

func (s *SubsystemActor) onStopped() {
	if s.cancel != nil {
		s.cancel()
	}
	s.flushDropSamples()
	s.logger.Info("signal subsystem stopped")
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
			s.engine.Send(s.selfPID, signalEnvelopeMsg{Envelope: env})
		}
	}
}

func (s *SubsystemActor) processEnvelope(env envelope.Envelope) {
	tenant := s.tenantFromEnv(env)
	eventType := strings.ToLower(strings.TrimSpace(env.Type))
	switch eventType {
	case envelopeTypeBookDelta:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			metrics.IncSignalDrop(dropReasonDecodeFailed)
			return
		}
		delta, ok := decoded.(marketmodel.BookDelta)
		if !ok {
			metrics.IncSignalDrop(dropReasonValidationFail)
			return
		}
		key, ok := signalStreamKey(env.Venue, env.Instrument)
		if !ok {
			metrics.IncSignalDrop(dropReasonValidationFail)
			return
		}
		if !s.acceptOwner(key, env.Seq) {
			return
		}
		streamKey := signalStreamKeyLabel(key)
		if !s.acceptMonotonicProgress(key, streamKey, env.Seq, env.TsIngest) {
			return
		}
		obs := signalcore.MarketObservation{
			Key:      key,
			Tenant:   tenant,
			TsServer: env.TsIngest,
			Seq:      env.Seq,
			BidDepth: sumDepth(delta.Bids),
			AskDepth: sumDepth(delta.Asks),
		}
		if len(delta.Bids) > 0 {
			obs.BestBid = delta.Bids[0].Price
		}
		if len(delta.Asks) > 0 {
			obs.BestAsk = delta.Asks[0].Price
		}
		s.recordMarket(obs)
		return
	case envelopeTypeTrade, envelopeTypeCandle, envelopeTypeStats:
		key, ok := signalStreamKey(env.Venue, env.Instrument)
		if !ok {
			metrics.IncSignalDrop(dropReasonValidationFail)
			return
		}
		if !s.acceptOwner(key, env.Seq) {
			return
		}
		streamKey := signalStreamKeyLabel(key)
		if !s.acceptMonotonicProgress(key, streamKey, env.Seq, env.TsIngest) {
			return
		}
		s.recordMarket(signalcore.MarketObservation{
			Key:      key,
			Tenant:   tenant,
			TsServer: env.TsIngest,
			Seq:      env.Seq,
		})
		return
	case envelopeTypeRegime, "insights.regime_evidence": // legacy compat
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			metrics.IncSignalDrop(dropReasonDecodeFailed)
			return
		}
		regime, ok := decoded.(evidencedomain.RegimeSignal)
		if !ok {
			metrics.IncSignalDrop(dropReasonValidationFail)
			return
		}
		if p := regime.Validate(); p != nil {
			metrics.IncSignalDrop(dropReasonValidationFail)
			return
		}
		key, ok := signalStreamKey(regime.Venue, regime.Instrument)
		if !ok {
			metrics.IncSignalDrop(dropReasonValidationFail)
			return
		}
		if !s.acceptOwner(key, env.Seq) {
			return
		}
		seq := env.Seq
		if seq <= 0 {
			seq = 1
		}
		tsServer := env.TsIngest
		if regime.WindowEnd > 0 {
			tsServer = regime.WindowEnd
		}
		streamKey := signalStreamKeyLabel(key)
		if !s.acceptMonotonicProgress(key, streamKey, seq, tsServer) {
			return
		}
		s.recordMarket(signalcore.MarketObservation{
			Key:      key,
			Tenant:   tenant,
			TsServer: tsServer,
			Seq:      seq,
		})
		return
	case envelopeTypeLiquidity:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			metrics.IncSignalDrop(dropReasonDecodeFailed)
			metrics.IncSignalLELAdaptError("decode_failed")
			return
		}
		wire, ok := decoded.(contracts.LiquidityEvidenceV1)
		if !ok {
			metrics.IncSignalDrop(dropReasonValidationFail)
			metrics.IncSignalLELAdaptError("decode_type_mismatch")
			return
		}
		adapted, p := signalcore.LELToEvidenceEvent(liquidityWireToDomain(wire))
		if p != nil {
			metrics.IncSignalDrop(dropReasonValidationFail)
			metrics.IncSignalLELAdaptError(string(p.Code))
			return
		}
		key, ok := signalStreamKey(adapted.Venue, adapted.Symbol)
		if !ok {
			metrics.IncSignalDrop(dropReasonValidationFail)
			return
		}
		if !s.acceptOwner(key, adapted.Seq) {
			return
		}
		streamKey := signalStreamKeyLabel(key)
		if !s.acceptMonotonicProgress(key, streamKey, adapted.Seq, adapted.TsServer) {
			return
		}
		metrics.IncSignalLELAdapted(string(wire.EvidenceType))
		s.evaluateEvidenceEvent(key, tenant, adapted)
		return
	case evidencedomain.MicrostructureEvidenceType, "insights.microstructure_evidence": // legacy compat
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			metrics.IncSignalDrop(dropReasonDecodeFailed)
			return
		}
		ev, ok := decoded.(evidencedomain.EvidenceEvent)
		if !ok {
			metrics.IncSignalDrop(dropReasonValidationFail)
			return
		}
		if p := ev.Validate(); p != nil {
			metrics.IncSignalDrop(dropReasonValidationFail)
			return
		}
		key, ok := signalStreamKey(ev.Venue, ev.Symbol)
		if !ok {
			metrics.IncSignalDrop(dropReasonValidationFail)
			return
		}
		if !s.acceptOwner(key, ev.Seq) {
			return
		}
		streamKey := signalStreamKeyLabel(key)
		if !s.acceptMonotonicProgress(key, streamKey, ev.Seq, ev.TsServer) {
			return
		}
		s.evaluateEvidenceEvent(key, tenant, ev)
	}
}

func (s *SubsystemActor) evaluateEvidenceEvent(key marketmodel.StreamKey, tenant string, ev evidencedomain.EvidenceEvent) {
	streamKey := signalStreamKeyLabel(key)

	emissions, evictions, dedupTypes, rateLimitedTypes, evalSpanMs, p := s.signalEngine.OnEvidenceEvent(key, tenant, ev)
	if p != nil {
		s.logger.Warn("signal subsystem: evaluate evidence failed", "code", p.Code, "message", p.Message)
		return
	}
	s.noteStreamProgress(streamKey, ev.Seq, ev.TsServer, strconv.Itoa(s.ownerReplicaID(key)))
	s.recordEvictions(evictions)
	for i := range dedupTypes {
		metrics.IncSignalDedup(dedupTypes[i])
		metrics.IncSignalDrop(dropReasonDuplicate)
	}
	if evalSpanMs > 0 {
		metrics.ObserveSignalEvalLatency(time.Duration(evalSpanMs) * time.Millisecond)
	}
	if len(rateLimitedTypes) > 0 {
		for range rateLimitedTypes {
			metrics.IncSignalDrop(dropReasonRateLimited)
		}
		s.logger.Debug("signal subsystem: tenant rate-limited signal emissions", "count", len(rateLimitedTypes), "tenant", tenant)
	}
	for i := range emissions {
		metrics.IncSignalEmitted(emissions[i].Event.Type, emissions[i].Event.Severity)
	}
	metrics.SetSignalStateEntries(s.signalEngine.StoreEntries())
}

func (s *SubsystemActor) recordMarket(obs signalcore.MarketObservation) {
	evictions, p := s.signalEngine.OnMarketEvent(obs)
	if p != nil {
		s.logger.Warn("signal subsystem: observe market failed", "code", p.Code, "message", p.Message)
		return
	}
	s.noteStreamProgress(signalStreamKeyLabel(obs.Key), obs.Seq, obs.TsServer, strconv.Itoa(s.ownerReplicaID(obs.Key)))
	s.recordEvictions(evictions)
	metrics.SetSignalStateEntries(s.signalEngine.StoreEntries())
}

func (s *SubsystemActor) recordEvictions(evictions []signalcore.EvictionReason) {
	for i := range evictions {
		metrics.IncSignalEvicted(string(evictions[i]))
	}
}

func (s *SubsystemActor) tenantFromEnv(env envelope.Envelope) string {
	if len(env.Meta) == 0 {
		return "default"
	}
	if tenant := strings.TrimSpace(env.Meta[s.tenantMetaKey]); tenant != "" {
		return tenant
	}
	return "default"
}

func signalStreamKey(venue, symbol string) (marketmodel.StreamKey, bool) {
	key, p := marketmodel.NewStreamKey(venue, symbol, marketmodel.ChannelEvidence)
	if p != nil {
		return marketmodel.StreamKey{}, false
	}
	return key, true
}

func sumDepth(levels []marketmodel.Level) float64 {
	total := 0.0
	for i := range levels {
		total += levels[i].Size
	}
	return total
}

func liquidityWireToDomain(in contracts.LiquidityEvidenceV1) evidencedomain.LiquidityEvidence {
	out := evidencedomain.LiquidityEvidence{
		EvidenceType: evidencedomain.LiquidityEvidenceType(in.EvidenceType),
		TsIngestMs:   in.TsIngestMs,
		Venue:        in.Venue,
		Symbol:       in.Symbol,
		WindowMs:     in.WindowMs,
		Severity:     evidencedomain.LiquidityEvidenceSeverity(in.Severity),
		Confidence:   in.Confidence,
		Explain:      append([]string(nil), in.Explain...),
		Version:      in.Version,
		StreamID:     in.StreamID,
		Seq:          in.Seq,
		Watermark: evidencedomain.LiquidityInputWatermark{
			SeqStart: in.Watermark.SeqStart,
			SeqEnd:   in.Watermark.SeqEnd,
		},
	}
	if len(in.Metrics) > 0 {
		out.Metrics = make([]evidencedomain.LiquidityEvidenceMetric, len(in.Metrics))
		for i := range in.Metrics {
			out.Metrics[i] = evidencedomain.LiquidityEvidenceMetric{
				Key:   in.Metrics[i].Key,
				Value: in.Metrics[i].Value,
			}
		}
	}
	return out
}

func (s *SubsystemActor) Emit(emission signalcore.Emission) *problem.Problem {
	contentType := envelope.ContentTypeProto
	payload, p := codec.EncodePayload(signalcore.EventType, signalcore.EventVersion, contentType, emission.Event)
	if p != nil {
		return p
	}
	metrics.ObserveSignalWireBytes(emission.Event.Type, len(payload))
	metrics.ObserveSignalQuality(
		emission.Event.Type,
		emission.Event.Explain,
		emission.Event.CorrelationIDs,
		emission.Event.Confidence,
	)
	venue := strings.ToLower(strings.TrimSpace(string(emission.StreamKey.Venue)))
	instrument := strings.TrimSpace(string(emission.StreamKey.Symbol))
	if emission.Event.Scope == marketmodel.SignalScopeMarket {
		venue = "global"
		instrument = "MARKET"
	}
	meta := map[string]string{
		"kind":           strings.ToLower(strings.TrimSpace(emission.Event.Type)),
		"timeframe":      "raw",
		"scope":          string(emission.Event.Scope),
		"signal_id":      emission.Event.SignalID,
		"intent_id":      deterministicIntentID(emission),
		"rule_id":        emission.Event.RuleID,
		"rule_version":   emission.Event.RuleVersion,
		"correlation_id": emission.Event.CorrelationID,
	}
	env := envelope.Envelope{
		Type:           signalcore.EventType,
		Version:        signalcore.EventVersion,
		Venue:          venue,
		Instrument:     instrument,
		TsIngest:       emission.Event.TsServer,
		Seq:            emission.Seq,
		IdempotencyKey: sharedhash.IdempotencyKeyFast(venue, instrument, signalcore.EventType, emission.Seq),
		ContentType:    contentType,
		Payload:        payload,
		Meta:           meta,
	}
	if s.cfg.Publisher != nil {
		return s.cfg.Publisher.Publish(context.Background(), env)
	}
	if s.cfg.RouterPID != nil && s.engine != nil {
		s.engine.Send(s.cfg.RouterPID, deliveryruntime.DeliverEnvelope{Envelope: env})
	}
	return nil
}
