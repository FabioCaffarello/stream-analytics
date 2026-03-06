package strategyruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	signalcore "github.com/market-raccoon/internal/core/signal"
	strategyapp "github.com/market-raccoon/internal/core/strategy/app"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/ownership"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	strategyStateMaxStreams = 4096
	strategyStaleGapWindow  = int64(2048)
)

type streamProgress struct {
	lastSeq       int64
	lastWatermark int64
	owner         string
}

type strategyEnvelopeMsg struct {
	Envelope envelope.Envelope
}

type EventPublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

type SubsystemConfig struct {
	Logger       *slog.Logger
	EnvelopeCh   <-chan envelope.Envelope
	Planner      *strategyapp.IntentPlanner
	Publisher    EventPublisher
	ReplicaID    int
	ReplicaCount int
}

type SubsystemActor struct {
	cfg        SubsystemConfig
	logger     *slog.Logger
	engine     *actor.Engine
	selfPID    *actor.PID
	consumeCtx context.Context
	cancel     context.CancelFunc

	planner      *strategyapp.IntentPlanner
	replicaID    int
	replicaCount int

	streamState map[string]streamProgress
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
	case strategyEnvelopeMsg:
		s.processEnvelope(msg.Envelope)
	case actorruntime.ChildFailed:
		if c.Parent() != nil {
			c.Send(c.Parent(), msg)
		}
	default:
		s.logger.Warn("strategy subsystem: unknown message", "type", fmt.Sprintf("%T", msg))
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
	if s.planner == nil {
		s.planner = s.cfg.Planner
		if s.planner == nil {
			s.planner = strategyapp.NewIntentPlanner(strategyapp.DefaultPlannerConfig())
		}
	}
	if s.replicaCount <= 0 {
		s.replicaCount = s.cfg.ReplicaCount
		if s.replicaCount <= 0 {
			s.replicaCount = 1
		}
	}
	if s.replicaID < 0 || s.replicaID >= s.replicaCount {
		s.replicaID = s.cfg.ReplicaID
		if s.replicaID < 0 || s.replicaID >= s.replicaCount {
			s.replicaID = 0
		}
	}
	if s.streamState == nil {
		s.streamState = make(map[string]streamProgress, strategyStateMaxStreams)
	}
}

func (s *SubsystemActor) onStarted() {
	s.logger.Info("strategy subsystem started", "replica_id", s.replicaID, "replica_count", s.replicaCount)
	if s.cfg.EnvelopeCh != nil && s.engine != nil && s.selfPID != nil {
		s.consumeCtx, s.cancel = context.WithCancel(context.Background())
		go s.consumeLoop()
	}
}

func (s *SubsystemActor) onStopped() {
	if s.cancel != nil {
		s.cancel()
	}
	s.logger.Info("strategy subsystem stopped")
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
			s.engine.Send(s.selfPID, strategyEnvelopeMsg{Envelope: env})
		}
	}
}

func (s *SubsystemActor) processEnvelope(env envelope.Envelope) {
	switch env.Type {
	case signalcore.EventType:
		s.processSignalEvent(env)
	case "signal.composite":
		s.logger.Warn("strategy subsystem: ignored deprecated signal.composite input",
			"reason", "legacy_signal_retired",
			"venue", env.Venue,
			"instrument", env.Instrument,
		)
	}
}

func (s *SubsystemActor) processSignalEvent(env envelope.Envelope) {
	decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		s.logger.Warn("strategy subsystem: decode signal.event failed", "err", p.Message)
		return
	}
	signal, ok := decoded.(marketmodel.SignalEvent)
	if !ok {
		s.logger.Warn("strategy subsystem: decoded signal payload type mismatch", "type", fmt.Sprintf("%T", decoded))
		return
	}
	input := strategyapp.IntentInput{
		Kind:          signal.Type,
		Venue:         envelopeValue(env.Venue, signal.Venue),
		Instrument:    envelopeValue(env.Instrument, signal.Symbol),
		SignalID:      signal.SignalID,
		CorrelationID: signal.CorrelationID,
		Reason:        signal.Explanation,
		Confidence:    signal.Confidence,
		TsServer:      maxInt64(signal.TsServer, env.TsIngest),
		Seq:           normalizeSeq(env.Seq),
	}
	s.planAndPublish(env, input)
}

func (s *SubsystemActor) planAndPublish(source envelope.Envelope, input strategyapp.IntentInput) {
	streamKey := ownership.StreamKey{Venue: input.Venue, Instrument: input.Instrument, Channel: "signal", Timeframe: "raw"}
	if !s.acceptOwner(streamKey) {
		return
	}
	if !s.acceptMonotonic(streamKey, input.Seq, input.TsServer) {
		return
	}

	intent, ok := s.planner.Plan(input)
	if !ok {
		s.logger.Debug("strategy subsystem: planner ignored input", "kind", input.Kind)
		return
	}

	payload, p := codec.EncodePayload(strategydomain.IntentEventType, strategydomain.IntentEventVersion, envelope.ContentTypeProto, intent)
	if p != nil {
		s.logger.Warn("strategy subsystem: encode strategy.intent failed", "err", p.Message)
		return
	}
	seq := normalizeSeq(input.Seq)
	if seq <= 0 {
		seq = 1
	}
	intentEnv := envelope.Envelope{
		Type:           strategydomain.IntentEventType,
		Version:        strategydomain.IntentEventVersion,
		Venue:          intent.Scope.Venue,
		Instrument:     intent.Scope.Symbol,
		TsIngest:       intent.CreatedAtMs,
		Seq:            seq,
		IdempotencyKey: sharedhash.IdempotencyKeyFast(intent.Scope.Venue, intent.Scope.Symbol, strategydomain.IntentEventType, seq),
		ContentType:    envelope.ContentTypeProto,
		Payload:        payload,
		Meta: map[string]string{
			"source_event_type": source.Type,
			"intent_id":         intent.IntentID,
			"input_semantic":    "signal.event",
		},
	}
	if s.cfg.Publisher == nil {
		return
	}
	if p := s.cfg.Publisher.Publish(context.Background(), intentEnv); p != nil {
		s.logger.Warn("strategy subsystem: publish strategy.intent failed", "code", p.Code, "message", p.Message)
	}
}

func (s *SubsystemActor) acceptOwner(streamKey ownership.StreamKey) bool {
	owner := ownership.OwnerReplica(ownership.SubsystemStrategist, streamKey, s.replicaCount)
	return owner == s.replicaID
}

func (s *SubsystemActor) acceptMonotonic(streamKey ownership.StreamKey, seq, watermark int64) bool {
	label := ownership.CanonicalLabel(streamKey)
	last := s.streamState[label]
	decision := ownership.DecideMonotonic(ownership.MonotonicInput{
		StreamKey:          label,
		CandidateSeq:       normalizeSeq(seq),
		CandidateWatermark: watermark,
		LastSeq:            last.lastSeq,
		LastWatermark:      last.lastWatermark,
		LastOwner:          last.owner,
		CandidateOwner:     strconv.Itoa(s.replicaID),
		StaleGapWindow:     strategyStaleGapWindow,
	})
	if decision.Action != ownership.ActionAccept {
		return false
	}
	if len(s.streamState) >= strategyStateMaxStreams && last.lastSeq == 0 {
		// Keep bounded deterministic memory without introducing background eviction workers.
		for k := range s.streamState {
			delete(s.streamState, k)
			break
		}
	}
	s.streamState[label] = streamProgress{
		lastSeq:       normalizeSeq(seq),
		lastWatermark: watermark,
		owner:         strconv.Itoa(s.replicaID),
	}
	return true
}

func normalizeSeq(seq int64) int64 {
	if seq <= 0 {
		return 1
	}
	return seq
}

func envelopeValue(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
