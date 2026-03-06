package executionruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	executionapp "github.com/market-raccoon/internal/core/execution/app"
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	executionports "github.com/market-raccoon/internal/core/execution/ports"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/ownership"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	executionStateMaxStreams = 4096
	executionStaleGapWindow  = int64(2048)
)

type streamProgress struct {
	lastSeq       int64
	lastWatermark int64
	owner         string
}

type executionEnvelopeMsg struct {
	Envelope envelope.Envelope
}

type EventPublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

type SubsystemConfig struct {
	Logger       *slog.Logger
	EnvelopeCh   <-chan envelope.Envelope
	Executor     executionports.IntentExecutor
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

	executor     executionports.IntentExecutor
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
	case executionEnvelopeMsg:
		s.processEnvelope(msg.Envelope)
	case actorruntime.ChildFailed:
		if c.Parent() != nil {
			c.Send(c.Parent(), msg)
		}
	default:
		s.logger.Warn("execution subsystem: unknown message", "type", fmt.Sprintf("%T", msg))
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
	if s.executor == nil {
		s.executor = s.cfg.Executor
		if s.executor == nil {
			s.executor = executionapp.NewDefaultGovernedBootstrapExecutor()
		}
	}
	if s.replicaCount <= 0 {
		s.replicaCount = s.cfg.ReplicaCount
		if s.replicaCount <= 0 {
			s.replicaCount = 1
		}
	}
	s.replicaID = s.cfg.ReplicaID
	if s.replicaID < 0 || s.replicaID >= s.replicaCount {
		s.replicaID = 0
	}
	if s.streamState == nil {
		s.streamState = make(map[string]streamProgress, executionStateMaxStreams)
	}
}

func (s *SubsystemActor) onStarted() {
	s.logger.Info("execution subsystem started", "replica_id", s.replicaID, "replica_count", s.replicaCount)
	if s.cfg.EnvelopeCh != nil && s.engine != nil && s.selfPID != nil {
		s.consumeCtx, s.cancel = context.WithCancel(context.Background())
		go s.consumeLoop()
	}
}

func (s *SubsystemActor) onStopped() {
	if s.cancel != nil {
		s.cancel()
	}
	s.logger.Info("execution subsystem stopped")
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
			s.engine.Send(s.selfPID, executionEnvelopeMsg{Envelope: env})
		}
	}
}

func (s *SubsystemActor) processEnvelope(env envelope.Envelope) {
	if env.Type != strategydomain.IntentEventType {
		return
	}
	decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		s.logger.Warn("execution subsystem: decode strategy.intent failed", "err", p.Message)
		return
	}
	intent, ok := decoded.(strategydomain.StrategyIntentV1)
	if !ok {
		s.logger.Warn("execution subsystem: decoded strategy payload type mismatch", "type", fmt.Sprintf("%T", decoded))
		return
	}
	streamKey := ownership.StreamKey{Venue: intent.Scope.Venue, Instrument: intent.Scope.Symbol, Channel: "intent", Timeframe: "raw"}
	if !s.acceptOwner(streamKey) {
		return
	}
	if !s.acceptMonotonic(streamKey, normalizeSeq(env.Seq), env.TsIngest) {
		return
	}

	events := s.executor.ExecuteAt(intent, env.TsIngest)
	for _, ev := range events {
		s.publishExecutionEvent(ev)
	}
}

func (s *SubsystemActor) publishExecutionEvent(ev executiondomain.ExecutionEventV1) {
	payload, p := codec.EncodePayload(executiondomain.EventType, executiondomain.EventVersion, envelope.ContentTypeProto, ev)
	if p != nil {
		s.logger.Warn("execution subsystem: encode execution.event failed", "err", p.Message)
		return
	}
	seq := normalizeSeq(ev.ExecutionSeq)
	env := envelope.Envelope{
		Type:           executiondomain.EventType,
		Version:        executiondomain.EventVersion,
		Venue:          ev.Correlation.Venue,
		Instrument:     ev.Correlation.Symbol,
		TsIngest:       ev.TsEventMs,
		TsExchange:     ev.TsExchangeMs,
		Seq:            seq,
		IdempotencyKey: sharedhash.IdempotencyKeyFast(ev.Correlation.Venue, ev.Correlation.Symbol, executiondomain.EventType, seq),
		ContentType:    envelope.ContentTypeProto,
		Payload:        payload,
		Meta: map[string]string{
			"execution_status":          string(ev.Status),
			"intent_id":                 ev.Correlation.IntentID,
			"event_id":                  ev.EventID,
			"execution_reason":          ev.Reason,
			"execution_reason_category": executiondomain.ReasonCategory(ev.Reason),
			"correlation_id":            ev.Provenance.CorrelationID,
		},
	}
	boundary := s.executor.BoundaryInfo()
	if boundary.Boundary != "" {
		env.Meta["execution_boundary"] = boundary.Boundary
	}
	if boundary.Adapter != "" {
		env.Meta["execution_adapter"] = boundary.Adapter
	}
	if boundary.Mode != "" {
		env.Meta["execution_mode"] = boundary.Mode
	}
	if s.cfg.Publisher == nil {
		return
	}
	if p := s.cfg.Publisher.Publish(context.Background(), env); p != nil {
		s.logger.Warn("execution subsystem: publish execution.event failed", "code", p.Code, "message", p.Message)
	}
}

func (s *SubsystemActor) acceptOwner(streamKey ownership.StreamKey) bool {
	owner := ownership.OwnerReplica(ownership.SubsystemExecution, streamKey, s.replicaCount)
	return owner == s.replicaID
}

func (s *SubsystemActor) acceptMonotonic(streamKey ownership.StreamKey, seq, watermark int64) bool {
	label := ownership.CanonicalLabel(streamKey)
	last := s.streamState[label]
	decision := ownership.DecideMonotonic(ownership.MonotonicInput{
		StreamKey:          label,
		CandidateSeq:       seq,
		CandidateWatermark: watermark,
		LastSeq:            last.lastSeq,
		LastWatermark:      last.lastWatermark,
		LastOwner:          last.owner,
		CandidateOwner:     strconv.Itoa(s.replicaID),
		StaleGapWindow:     executionStaleGapWindow,
	})
	if decision.Action != ownership.ActionAccept {
		return false
	}
	if len(s.streamState) >= executionStateMaxStreams && last.lastSeq == 0 {
		for k := range s.streamState {
			delete(s.streamState, k)
			break
		}
	}
	s.streamState[label] = streamProgress{lastSeq: seq, lastWatermark: watermark, owner: strconv.Itoa(s.replicaID)}
	return true
}

func normalizeSeq(seq int64) int64 {
	if seq <= 0 {
		return 1
	}
	return seq
}
