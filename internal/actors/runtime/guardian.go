package runtime

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/anthdm/hollywood/actor"
)

// GuardianConfig configures runtime orchestration behavior.
type GuardianConfig struct {
	Policy *SupervisorPolicy
	Logger *slog.Logger
	Clock  Clock
}

// Guardian orchestrates subsystem actors and enforces supervisor policy.
type Guardian struct {
	cfg GuardianConfig

	policy *SupervisorPolicy
	clock  Clock
	logger *slog.Logger

	children       map[Subsystem]*actor.PID
	running        map[Subsystem]bool
	degraded       map[Subsystem]bool
	lastError      map[Subsystem]string
	scheduledRetry map[Subsystem]cancelSchedule

	repeaterStopFn func()
	started        bool

	spawnFn  func(c *actor.Context, subsystem Subsystem) (*actor.PID, error)
	poisonFn func(c *actor.Context, pid *actor.PID)
	sendFn   func(c *actor.Context, pid *actor.PID, msg any)
	emitFn   func(c *actor.Context, msg any)

	selfPID      *actor.PID
	sendToSelfFn func(pid *actor.PID, msg any)

	scheduleFn func(delay time.Duration, fn func()) cancelSchedule
}

type retrySubsystem struct {
	Subsystem Subsystem
}

type cancelSchedule func()

// NewGuardian returns the runtime guardian actor producer.
func NewGuardian(cfg GuardianConfig) actor.Producer {
	return func() actor.Receiver {
		return &Guardian{cfg: cfg}
	}
}

func (g *Guardian) Receive(c *actor.Context) {
	g.ensureDefaults(c)

	switch msg := c.Message().(type) {
	case actor.Started:
		g.startAll(c)
	case actor.Stopped:
		g.stopAll(c)
	case Start:
		g.startAll(c)
	case Stop:
		g.stopAll(c)
	case ReloadConfig:
		g.stopAll(c)
		g.startAll(c)
	case Ping:
		if msg.ReplyTo != nil {
			g.sendFn(c, msg.ReplyTo, Pong{At: g.clock.Now()})
		}
	case Snapshot:
		if msg.ReplyTo != nil {
			g.sendFn(c, msg.ReplyTo, g.buildSnapshot())
		}
	case ChildFailed:
		g.handleChildFailed(c, msg)
	case retrySubsystem:
		g.retrySubsystem(c, msg.Subsystem)
	default:
		g.logger.Warn("runtime guardian unknown message", "msg", fmt.Sprintf("%T", msg))
	}
}

func (g *Guardian) ensureDefaults(c *actor.Context) {
	if g.clock == nil {
		if g.cfg.Clock != nil {
			g.clock = g.cfg.Clock
		} else {
			g.clock = systemClock{}
		}
	}
	if g.logger == nil {
		if g.cfg.Logger != nil {
			g.logger = g.cfg.Logger
		} else {
			g.logger = slog.Default()
		}
	}
	if g.policy == nil {
		if g.cfg.Policy != nil {
			g.policy = g.cfg.Policy
		} else {
			policy, err := NewSupervisorPolicy(SupervisorConfig{}, g.clock, nil)
			if err != nil {
				g.logger.Error("failed to create default supervisor policy", "err", err)
				return
			}
			g.policy = policy
		}
	}

	if g.children == nil {
		g.children = make(map[Subsystem]*actor.PID)
	}
	if g.running == nil {
		g.running = make(map[Subsystem]bool)
	}
	if g.degraded == nil {
		g.degraded = make(map[Subsystem]bool)
	}
	if g.lastError == nil {
		g.lastError = make(map[Subsystem]string)
	}
	if g.scheduledRetry == nil {
		g.scheduledRetry = make(map[Subsystem]cancelSchedule)
	}

	if g.spawnFn == nil {
		g.spawnFn = g.spawnSubsystem
	}
	if g.poisonFn == nil {
		g.poisonFn = func(ac *actor.Context, pid *actor.PID) {
			if ac == nil || pid == nil {
				return
			}
			<-ac.Engine().Poison(pid).Done()
		}
	}
	if g.sendFn == nil {
		g.sendFn = func(ac *actor.Context, pid *actor.PID, msg any) {
			if ac == nil || pid == nil {
				return
			}
			ac.Send(pid, msg)
		}
	}
	if g.scheduleFn == nil {
		g.scheduleFn = func(delay time.Duration, fn func()) cancelSchedule {
			t := time.AfterFunc(delay, fn)
			return func() {
				t.Stop()
			}
		}
	}
	if g.selfPID == nil && c.PID() != nil {
		cloned := *c.PID().CloneVT()
		g.selfPID = &cloned
	}
	if g.sendToSelfFn == nil {
		engine := c.Engine()
		g.sendToSelfFn = func(pid *actor.PID, msg any) {
			if engine == nil || pid == nil {
				return
			}
			engine.Send(pid, msg)
		}
	}
	if g.emitFn == nil {
		g.emitFn = func(ac *actor.Context, msg any) {
			if ac == nil || ac.Parent() == nil {
				return
			}
			ac.Send(ac.Parent(), msg)
		}
	}

}

func (g *Guardian) startAll(c *actor.Context) {
	if g.started {
		return
	}
	g.started = true
	if c != nil && g.repeaterStopFn == nil {
		repeater := c.Engine().SendRepeat(c.PID(), Ping{ReplyTo: c.PID()}, 30*time.Second)
		g.repeaterStopFn = func() { repeater.Stop() }
	}

	for _, subsystem := range orderedSubsystems {
		g.startSubsystem(c, subsystem)
	}
}

func (g *Guardian) stopAll(c *actor.Context) {
	g.stopRepeater()

	for subsystem, cancel := range g.scheduledRetry {
		cancel()
		delete(g.scheduledRetry, subsystem)
	}

	for i := len(orderedSubsystems) - 1; i >= 0; i-- {
		subsystem := orderedSubsystems[i]
		pid := g.children[subsystem]
		if pid == nil {
			g.running[subsystem] = false
			continue
		}
		g.poisonFn(c, pid)
		g.children[subsystem] = nil
		g.running[subsystem] = false
	}
	g.started = false
}

func (g *Guardian) startSubsystem(c *actor.Context, subsystem Subsystem) {
	pid, err := g.spawnFn(c, subsystem)
	if err != nil {
		g.lastError[subsystem] = err.Error()
		g.running[subsystem] = false
		g.logger.Error("failed to spawn subsystem", "subsystem", subsystem, "err", err)
		return
	}

	g.children[subsystem] = pid
	g.running[subsystem] = true

	if g.degraded[subsystem] {
		g.degraded[subsystem] = false
		g.policy.MarkRecovered(subsystem)
		g.emitFn(c, Recovered{Subsystem: subsystem})
	}
}

func (g *Guardian) retrySubsystem(c *actor.Context, subsystem Subsystem) {
	delete(g.scheduledRetry, subsystem)
	if g.degraded[subsystem] {
		now := g.clock.Now()
		status := g.policy.Status(subsystem)
		if status.Degraded && now.Before(status.CooldownUntil) {
			return
		}
	}
	g.startSubsystem(c, subsystem)
}

func (g *Guardian) handleChildFailed(c *actor.Context, msg ChildFailed) {
	if msg.Subsystem == "" {
		return
	}
	if msg.Err != nil {
		g.lastError[msg.Subsystem] = msg.Err.Error()
	} else {
		g.lastError[msg.Subsystem] = msg.Kind
	}
	g.running[msg.Subsystem] = false
	g.children[msg.Subsystem] = nil

	now := g.clock.Now()
	decision := g.policy.OnFailure(msg.Subsystem, now)
	if decision.EnterDegraded {
		g.degraded[msg.Subsystem] = true
		g.emitFn(c, Degraded{Subsystem: msg.Subsystem, Reason: decision.Reason})
		target := g.selfPID
		send := g.sendToSelfFn
		delay := decision.DegradedUntil.Sub(now)
		if delay < 0 {
			delay = 0
		}
		cancel := g.scheduleFn(delay, func() {
			send(target, retrySubsystem{Subsystem: msg.Subsystem})
		})
		g.replaceScheduledRetry(msg.Subsystem, cancel)
		return
	}

	if !decision.Restart {
		return
	}
	target := g.selfPID
	send := g.sendToSelfFn
	cancel := g.scheduleFn(decision.Delay, func() {
		send(target, retrySubsystem{Subsystem: msg.Subsystem})
	})
	g.replaceScheduledRetry(msg.Subsystem, cancel)
}

func (g *Guardian) replaceScheduledRetry(subsystem Subsystem, cancel cancelSchedule) {
	if prev, ok := g.scheduledRetry[subsystem]; ok {
		prev()
	}
	g.scheduledRetry[subsystem] = cancel
}

func (g *Guardian) buildSnapshot() SnapshotState {
	state := SnapshotState{
		At:         g.clock.Now(),
		Subsystems: make(map[Subsystem]SubsystemState, len(orderedSubsystems)),
	}
	for _, subsystem := range orderedSubsystems {
		policyState := g.policy.Status(subsystem)
		state.Subsystems[subsystem] = SubsystemState{
			Running:       g.running[subsystem],
			Degraded:      g.degraded[subsystem] || policyState.Degraded,
			LastError:     g.lastError[subsystem],
			RestartCount:  policyState.RestartCount,
			CooldownUntil: policyState.CooldownUntil,
		}
	}
	return state
}

func (g *Guardian) stopRepeater() {
	if g.repeaterStopFn == nil {
		return
	}
	g.repeaterStopFn()
	g.repeaterStopFn = nil
}

func (g *Guardian) spawnSubsystem(c *actor.Context, subsystem Subsystem) (*actor.PID, error) {
	if c == nil {
		return nil, fmt.Errorf("actor context is nil")
	}
	pid := c.SpawnChild(
		subsystemPlaceholder(subsystem),
		"runtime-subsystem",
		actor.WithID(fmt.Sprintf("runtime-%s", subsystem)),
	)
	return pid, nil
}

func subsystemPlaceholder(subsystem Subsystem) actor.Producer {
	return func() actor.Receiver {
		return &placeholderReceiver{subsystem: subsystem}
	}
}

type placeholderReceiver struct {
	subsystem Subsystem
}

func (p *placeholderReceiver) Receive(c *actor.Context) {
	switch c.Message().(type) {
	case actor.Started, actor.Stopped:
		return
	default:
		return
	}
}
