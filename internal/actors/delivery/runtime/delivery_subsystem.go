package deliveryruntime

import (
	"context"
	"fmt"
	"log/slog"

	actorruntime "github.com/FabioCaffarello/stream-analytics/internal/actors/runtime"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/anthdm/hollywood/actor"
)

type subsystemEnvelopeMsg struct {
	Envelope envelope.Envelope
}

// SubsystemConfig configures Delivery subsystem actor.
type SubsystemConfig struct {
	Logger        *slog.Logger
	Router        RouterConfig
	EnvelopeCh    <-chan envelope.Envelope
	MaxSessions   int
	Backpressure  string
	NATSDurable   string
	NATSSubjects  []string
	OnRouterReady func(pid *actor.PID)
	OnReady       func(subsystemPID, routerPID *actor.PID)
	// RouterProducer overrides child producer in tests.
	RouterProducer actor.Producer
}

// SubsystemActor owns delivery runtime children (router and session actors).
type SubsystemActor struct {
	cfg       SubsystemConfig
	logger    *slog.Logger
	routerPID *actor.PID

	engine     *actor.Engine
	selfPID    *actor.PID
	consumeCtx context.Context
	cancel     context.CancelFunc
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
		s.onStarted(c)
	case actor.Stopped:
		s.onStopped()
	case subsystemEnvelopeMsg:
		if s.routerPID != nil {
			s.engine.Send(s.routerPID, DeliverEnvelope(msg))
		}
	case SpawnSession:
		cfg := msg.Config
		if cfg.RouterPID == nil {
			cfg.RouterPID = s.routerPID
		}
		pid := c.SpawnChild(NewSessionActor(cfg), "delivery-session")
		c.Respond(SpawnSessionAck{PID: pid})
	case actorruntime.ChildFailed:
		if c.Parent() != nil {
			c.Send(c.Parent(), msg)
		}
	default:
		s.logger.Warn("delivery subsystem: unknown message", "type", fmt.Sprintf("%T", msg))
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
}

func (s *SubsystemActor) onStarted(c *actor.Context) {
	routerCfg := s.cfg.Router
	if s.cfg.MaxSessions > 0 && routerCfg.MaxActiveSessions <= 0 {
		routerCfg.MaxActiveSessions = s.cfg.MaxSessions
	}
	routerProducer := s.cfg.RouterProducer
	if routerProducer == nil {
		routerProducer = NewRouterActor(routerCfg)
	}
	s.routerPID = c.SpawnChild(routerProducer, "delivery-router", actor.WithID("delivery-router"))
	if s.cfg.OnRouterReady != nil {
		s.cfg.OnRouterReady(s.routerPID)
	}
	if s.cfg.OnReady != nil {
		s.cfg.OnReady(s.selfPID, s.routerPID)
	}
	if s.cfg.EnvelopeCh != nil && s.engine != nil && s.selfPID != nil {
		s.consumeCtx, s.cancel = context.WithCancel(context.Background())
		go s.consumeLoop()
	}
}

func (s *SubsystemActor) onStopped() {
	if s.cancel != nil {
		s.cancel()
	}
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
			s.engine.Send(s.selfPID, subsystemEnvelopeMsg{Envelope: env})
		}
	}
}
