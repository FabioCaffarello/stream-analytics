package portfolioruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	portfolioapp "github.com/market-raccoon/internal/core/portfolio/app"
	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/ownership"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	portfolioStateMaxStreams = 4096
	portfolioStaleGapWindow  = int64(2048)
)

type streamProgress struct {
	lastSeq       int64
	lastWatermark int64
	owner         string
}

type portfolioEnvelopeMsg struct {
	Envelope envelope.Envelope
}

type EventPublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

type SubsystemConfig struct {
	Logger       *slog.Logger
	EnvelopeCh   <-chan envelope.Envelope
	Projector    *portfolioapp.BootstrapProjector
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

	projector    *portfolioapp.BootstrapProjector
	replicaID    int
	replicaCount int
	streamState  map[string]streamProgress
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
	case portfolioEnvelopeMsg:
		s.processEnvelope(msg.Envelope)
	case actorruntime.ChildFailed:
		if c.Parent() != nil {
			c.Send(c.Parent(), msg)
		}
	default:
		s.logger.Warn("portfolio subsystem: unknown message", "type", fmt.Sprintf("%T", msg))
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
	if s.projector == nil {
		s.projector = s.cfg.Projector
		if s.projector == nil {
			s.projector = portfolioapp.NewBootstrapProjector(portfolioapp.DefaultProjectorConfig())
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
		s.streamState = make(map[string]streamProgress, portfolioStateMaxStreams)
	}
}

func (s *SubsystemActor) onStarted() {
	s.logger.Info("portfolio subsystem started", "replica_id", s.replicaID, "replica_count", s.replicaCount)
	if s.cfg.EnvelopeCh != nil && s.engine != nil && s.selfPID != nil {
		s.consumeCtx, s.cancel = context.WithCancel(context.Background())
		go s.consumeLoop()
	}
}

func (s *SubsystemActor) onStopped() {
	if s.cancel != nil {
		s.cancel()
	}
	s.logger.Info("portfolio subsystem stopped")
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
			s.engine.Send(s.selfPID, portfolioEnvelopeMsg{Envelope: env})
		}
	}
}

func (s *SubsystemActor) processEnvelope(env envelope.Envelope) {
	if env.Type != executiondomain.EventType {
		return
	}
	decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		s.logger.Warn("portfolio subsystem: decode execution.event failed", "err", p.Message)
		return
	}
	ev, ok := decoded.(executiondomain.ExecutionEventV1)
	if !ok {
		s.logger.Warn("portfolio subsystem: decoded execution payload type mismatch", "type", fmt.Sprintf("%T", decoded))
		return
	}

	streamKey := ownership.StreamKey{Venue: ev.Correlation.Venue, Instrument: ev.Correlation.Symbol, Channel: "execution", Timeframe: "raw"}
	if !s.acceptOwner(streamKey) {
		return
	}
	if !s.acceptMonotonic(streamKey, normalizeSeq(ev.ExecutionSeq), ev.TsEventMs) {
		return
	}

	state, ok := s.projector.Apply(ev)
	if !ok {
		s.logger.Warn("portfolio subsystem: projector rejected execution event", "event_id", ev.EventID)
		return
	}
	s.publishPortfolioState(state, ev)
}

func (s *SubsystemActor) publishPortfolioState(state portfoliodomain.PortfolioStateV1, source executiondomain.ExecutionEventV1) {
	payload, p := codec.EncodePayload(portfoliodomain.StateEventType, portfoliodomain.StateEventVersion, envelope.ContentTypeProto, state)
	if p != nil {
		s.logger.Warn("portfolio subsystem: encode portfolio.state failed", "err", p.Message)
		return
	}
	seq := normalizeSeq(state.Provenance.SourceExecutionSeq)
	env := envelope.Envelope{
		Type:           portfoliodomain.StateEventType,
		Version:        portfoliodomain.StateEventVersion,
		Venue:          state.Venue,
		Instrument:     state.Positions[0].Symbol,
		TsIngest:       state.ProjectedAtMs,
		Seq:            seq,
		IdempotencyKey: sharedhash.IdempotencyKeyFast(state.Venue, state.Positions[0].Symbol, portfoliodomain.StateEventType, seq),
		ContentType:    envelope.ContentTypeProto,
		Payload:        payload,
		Meta: map[string]string{
			"state_id":                  state.StateID,
			"source_execution_event_id": state.Provenance.SourceExecutionEventID,
			"source_execution_status":   string(source.Status),
			"source_execution_reason":   source.Reason,
			"correlation_id":            state.Provenance.CorrelationID,
		},
	}
	if s.cfg.Publisher == nil {
		return
	}
	if p := s.cfg.Publisher.Publish(context.Background(), env); p != nil {
		s.logger.Warn("portfolio subsystem: publish portfolio.state failed", "code", p.Code, "message", p.Message)
	}
}

func (s *SubsystemActor) acceptOwner(streamKey ownership.StreamKey) bool {
	owner := ownership.OwnerReplica(ownership.SubsystemPortfolio, streamKey, s.replicaCount)
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
		StaleGapWindow:     portfolioStaleGapWindow,
	})
	if decision.Action != ownership.ActionAccept {
		return false
	}
	if len(s.streamState) >= portfolioStateMaxStreams && last.lastSeq == 0 {
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
