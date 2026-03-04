package signalsruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anthdm/hollywood/actor"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	signalsapp "github.com/market-raccoon/internal/core/signals/app"
	signalsdomain "github.com/market-raccoon/internal/core/signals/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

const defaultRegimeCacheMaxStreams = 1024

type signalEnvelopeMsg struct {
	Envelope envelope.Envelope
}

// EventPublisher publishes composed signal envelopes to the runtime bus.
type EventPublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

// SubsystemConfig configures the Signals subsystem actor.
type SubsystemConfig struct {
	Logger                *slog.Logger
	EnvelopeCh            <-chan envelope.Envelope
	Composer              *signalsapp.SignalComposer
	Limiter               *signalsapp.SignalRateLimiter
	RegimeCacheMaxStreams int
	RouterPID             *actor.PID // delivery router to publish signal envelopes
	Publisher             EventPublisher
}

// SubsystemActor owns the signal composer lifecycle.
type SubsystemActor struct {
	cfg        SubsystemConfig
	logger     *slog.Logger
	engine     *actor.Engine
	selfPID    *actor.PID
	consumeCtx context.Context
	cancel     context.CancelFunc

	composer *signalsapp.SignalComposer
	limiter  *signalsapp.SignalRateLimiter

	regimeCacheMaxStreams int
	regimeByKey           map[string]evidencedomain.RegimeSignal
	regimeOrder           []string
}

// NewSubsystemActor creates the signals subsystem actor producer.
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
		s.logger.Warn("signals subsystem: unknown message", "type", fmt.Sprintf("%T", msg))
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
	if s.composer == nil {
		s.composer = s.cfg.Composer
		if s.composer == nil {
			s.composer = signalsapp.NewSignalComposer(signalsapp.DefaultComposePolicy())
		}
	}
	if s.limiter == nil {
		s.limiter = s.cfg.Limiter
		if s.limiter == nil {
			s.limiter = signalsapp.NewSignalRateLimiter(signalsapp.DefaultRateLimitPolicy())
		}
	}
	if s.regimeCacheMaxStreams == 0 {
		s.regimeCacheMaxStreams = s.cfg.RegimeCacheMaxStreams
		if s.regimeCacheMaxStreams <= 0 {
			s.regimeCacheMaxStreams = defaultRegimeCacheMaxStreams
		}
	}
	if s.regimeByKey == nil {
		s.regimeByKey = make(map[string]evidencedomain.RegimeSignal, s.regimeCacheMaxStreams)
		s.regimeOrder = make([]string, 0, s.regimeCacheMaxStreams)
	}
}

func (s *SubsystemActor) onStarted() {
	s.logger.Info("signals subsystem started")
	if s.cfg.EnvelopeCh != nil && s.engine != nil && s.selfPID != nil {
		s.consumeCtx, s.cancel = context.WithCancel(context.Background())
		go s.consumeLoop()
	}
}

func (s *SubsystemActor) onStopped() {
	if s.cancel != nil {
		s.cancel()
	}
	s.logger.Info("signals subsystem stopped")
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
	switch env.Type {
	case evidencedomain.RegimeEvidenceType:
		s.processRegimeEnvelope(env)
	case evidencedomain.MicrostructureEvidenceType:
		s.processMicroEnvelope(env)
	}
}

func (s *SubsystemActor) processRegimeEnvelope(env envelope.Envelope) {
	decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		return
	}
	regime, ok := decoded.(evidencedomain.RegimeSignal)
	if !ok {
		return
	}
	if p := regime.Validate(); p != nil {
		return
	}
	key := regimeCacheKey(regime.Venue, regime.Instrument)
	s.putRegime(key, regime)
}

func (s *SubsystemActor) processMicroEnvelope(env envelope.Envelope) {
	decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		return
	}
	micro, ok := decoded.(evidencedomain.EvidenceEvent)
	if !ok {
		return
	}
	if p := micro.Validate(); p != nil {
		return
	}

	regime, ok := s.regimeByKey[regimeCacheKey(micro.Venue, micro.Symbol)]
	var regimePtr *evidencedomain.RegimeSignal
	if ok {
		regimeCopy := regime
		regimePtr = &regimeCopy
	}

	result, composed := s.composer.Compose(signalsapp.ComposeInput{
		Micro:     micro,
		Regime:    regimePtr,
		Timeframe: strings.TrimSpace(env.Meta["timeframe"]),
	})
	if !composed {
		return
	}

	decision := s.limiter.Allow(result.Signal)
	if decision.Deduplicated {
		metrics.IncMRSignalDeduplicated(result.Signal.Kind, result.Signal.Venue, result.Signal.Instrument)
		return
	}
	if decision.RateLimited {
		metrics.IncMRSignalRateLimited(result.Signal.Venue, result.Signal.Instrument)
		return
	}
	if !decision.Allowed {
		return
	}

	metrics.IncMRSignalEmitted(result.Signal.Kind, result.Signal.Venue, result.Signal.Instrument, result.Signal.Severity)
	metrics.ObserveMRSignalCompositionDuration(result.Signal.Kind, time.Duration(maxInt64(result.CorrelationSpanMs, 0))*time.Millisecond)
	metrics.ObserveMRSignalConfidence(result.Signal.Kind, result.Signal.Confidence)
	if result.CorrelationHit {
		metrics.IncMRSignalCorrelationHit(result.Signal.Kind)
	}
	if result.RegimeBoosted {
		metrics.IncMRSignalRegimeBoost(result.Signal.Kind, result.Signal.RegimeKind)
	}

	s.emitSignal(env, result.Signal)
}

func (s *SubsystemActor) emitSignal(triggerEnv envelope.Envelope, signal signalsdomain.CompositeSignalV1) {
	payload, p := codec.EncodePayload(signalsdomain.CompositeSignalType, signalsdomain.CompositeSignalVersion, envelope.ContentTypeJSON, signal)
	if p != nil {
		s.logger.Warn("signals: failed to encode composite signal payload", "err", p.Message)
		return
	}

	signalEnv := envelope.Envelope{
		Type:        signalsdomain.CompositeSignalType,
		Version:     signalsdomain.CompositeSignalVersion,
		Venue:       signal.Venue,
		Instrument:  signal.Instrument,
		TsIngest:    signal.TsServer,
		Seq:         signal.Seq,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
		Meta: map[string]string{
			"timeframe": signal.Timeframe,
			"kind":      signal.Kind,
		},
	}

	if s.cfg.Publisher != nil {
		if p := s.cfg.Publisher.Publish(context.Background(), signalEnv); p != nil {
			s.logger.Warn("signals: failed to publish composite signal envelope", "code", p.Code, "message", p.Message)
		}
		return
	}

	if s.cfg.RouterPID != nil && s.engine != nil {
		s.engine.Send(s.cfg.RouterPID, deliveryruntime.DeliverEnvelope{Envelope: signalEnv})
	}

	_ = triggerEnv
}

func (s *SubsystemActor) putRegime(key string, regime evidencedomain.RegimeSignal) {
	if _, exists := s.regimeByKey[key]; exists {
		// LRU: move accessed key to end of order list.
		s.removeRegimeOrderKey(key)
	} else if len(s.regimeByKey) >= s.regimeCacheMaxStreams {
		s.evictOldestRegime()
	}
	s.regimeOrder = append(s.regimeOrder, key)
	s.regimeByKey[key] = regime
}

func (s *SubsystemActor) removeRegimeOrderKey(key string) {
	dst := s.regimeOrder[:0]
	for _, k := range s.regimeOrder {
		if k != key {
			dst = append(dst, k)
		}
	}
	s.regimeOrder = dst
}

func (s *SubsystemActor) evictOldestRegime() {
	if len(s.regimeOrder) == 0 {
		return
	}
	victim := s.regimeOrder[0]
	s.regimeOrder = s.regimeOrder[1:]
	delete(s.regimeByKey, victim)
}

func regimeCacheKey(venue, instrument string) string {
	return strings.ToLower(strings.TrimSpace(venue)) + "|" + strings.ToUpper(strings.TrimSpace(instrument))
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
