package deliveryruntime

import (
	"fmt"
	"log/slog"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
)

// SubsystemConfig configures Delivery subsystem actor.
type SubsystemConfig struct {
	Logger        *slog.Logger
	Router        RouterConfig
	OnRouterReady func(pid *actor.PID)
}

// SubsystemActor owns delivery runtime children (router and session actors).
type SubsystemActor struct {
	cfg       SubsystemConfig
	logger    *slog.Logger
	routerPID *actor.PID
}

func NewSubsystemActor(cfg SubsystemConfig) actor.Producer {
	return func() actor.Receiver {
		return &SubsystemActor{cfg: cfg}
	}
}

func (s *SubsystemActor) Receive(c *actor.Context) {
	if s.logger == nil {
		if s.cfg.Logger != nil {
			s.logger = s.cfg.Logger
		} else {
			s.logger = slog.Default()
		}
	}

	switch msg := c.Message().(type) {
	case actor.Initialized:
	case actor.Started:
		s.routerPID = c.SpawnChild(NewRouterActor(s.cfg.Router), "delivery-router", actor.WithID("delivery-router"))
		if s.cfg.OnRouterReady != nil {
			s.cfg.OnRouterReady(s.routerPID)
		}
	case actor.Stopped:
	case actorruntime.ChildFailed:
		if c.Parent() != nil {
			c.Send(c.Parent(), msg)
		}
	default:
		s.logger.Warn("delivery subsystem: unknown message", "type", fmt.Sprintf("%T", msg))
	}
}
