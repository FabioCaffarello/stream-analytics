package runtime

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/shared/metrics"
)

// GuardianConfig configures runtime orchestration behavior.
type GuardianConfig struct {
	Policy *SupervisorPolicy
	Logger *slog.Logger
	Clock  Clock

	// Factories maps each Subsystem to an actor.Producer used when spawning
	// that subsystem's child actor.  Any subsystem not present in the map is
	// spawned as a no-op placeholder (safe for subsystems not yet wired).
	//
	// This allows cmd-level wiring to inject concrete implementations without
	// modifying the Guardian itself.
	Factories map[Subsystem]actor.Producer

	// ExpectedSubsystems lists the subsystems that must be Running for the
	// Guardian to report Ready==true via ReadyQuery.  If nil, the set is
	// inferred from Factories (subsystems with a non-nil factory are expected).
	// Pass an explicit empty slice to disable readiness tracking entirely
	// (Guardian will always report Ready==true).
	ExpectedSubsystems []Subsystem
}

// Guardian orchestrates subsystem actors and enforces supervisor policy.
type Guardian struct {
	cfg GuardianConfig

	policy *SupervisorPolicy
	clock  Clock
	logger *slog.Logger

	children       map[Subsystem]*actor.PID
	running        map[Subsystem]bool
	lastError      map[Subsystem]string
	lastFailureAt  map[Subsystem]time.Time
	lastTransition map[Subsystem]time.Time
	connected      map[Subsystem]bool
	lastMessageAt  map[Subsystem]time.Time
	lastPublishAt  map[Subsystem]time.Time
	scheduledRetry map[Subsystem]cancelSchedule
	retryGen       map[Subsystem]uint64

	// readySystems tracks subsystems that have started at least once.
	// Used to answer ReadyQuery.
	readySystems map[Subsystem]bool
	// expectedSubsystems is derived from cfg at first use.
	expectedSubsystems []Subsystem
	// shuttingDown is true after a Stop message is received; prevents restarts.
	shuttingDown bool

	started bool

	spawnFn  func(c *actor.Context, subsystem Subsystem) (*actor.PID, error)
	poisonFn func(c *actor.Context, pid *actor.PID)
	sendFn   func(c *actor.Context, pid *actor.PID, msg any)
	emitFn   func(c *actor.Context, msg any)

	selfPID      *actor.PID
	sendToSelfFn func(pid *actor.PID, msg any)

	scheduleFn func(delay time.Duration, fn func()) cancelSchedule

	globalRestartWindow  time.Duration
	globalRestartLimit   int
	globalRestartHistory []time.Time
}

type retrySubsystem struct {
	Subsystem  Subsystem
	Generation uint64
}

type cancelSchedule func()

// NewGuardian returns the runtime guardian actor producer.
func NewGuardian(cfg GuardianConfig) actor.Producer {
	return func() actor.Receiver {
		return &Guardian{cfg: cfg}
	}
}

func (g *Guardian) Receive(c *actor.Context) {
	if !g.ensureDefaults(c) {
		return
	}

	switch msg := c.Message().(type) {
	case actor.Initialized:
		// no-op; engine lifecycle preamble.
	case actor.Started:
		g.startAll(c)
	case actor.Stopped:
		g.stopAll(c)
	case Start:
		g.startAll(c)
	case ReloadConfig:
		g.stopAll(c)
		g.startAll(c)
	case Ping:
		if msg.ReplyTo != nil {
			g.sendFn(c, msg.ReplyTo, Pong{At: g.clock.Now()})
		}
	case Snapshot:
		// Honour explicit ReplyTo first; fall back to sender so that
		// engine.Request() works without a ReplyTo (HTTP handler pattern).
		replyTo := msg.ReplyTo
		if replyTo == nil {
			replyTo = c.Sender()
		}
		if replyTo != nil {
			g.sendFn(c, replyTo, g.buildSnapshot())
		}
	case ReadyQuery:
		replyTo := msg.ReplyTo
		if replyTo == nil {
			replyTo = c.Sender()
		}
		if replyTo != nil {
			ready, pending := g.computeReady()
			g.sendFn(c, replyTo, ReadyResponse{Ready: ready, Pending: pending})
		}
	case Stop:
		g.shuttingDown = true
		g.stopAll(c)
	case ChildFailed:
		g.handleChildFailed(c, msg)
	case SubsystemHeartbeat:
		g.handleHeartbeat(msg)
	case retrySubsystem:
		g.retrySubsystem(c, msg.Subsystem, msg.Generation)
	default:
		g.logger.Warn("runtime guardian unknown message", "msg", fmt.Sprintf("%T", msg))
	}
}

func (g *Guardian) ensureDefaults(c *actor.Context) bool {
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
				if c != nil {
					c.Engine().Poison(c.PID())
				}
				return false
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
	if g.readySystems == nil {
		g.readySystems = make(map[Subsystem]bool)
	}
	if g.lastError == nil {
		g.lastError = make(map[Subsystem]string)
	}
	if g.lastFailureAt == nil {
		g.lastFailureAt = make(map[Subsystem]time.Time)
	}
	if g.lastTransition == nil {
		g.lastTransition = make(map[Subsystem]time.Time)
	}
	if g.connected == nil {
		g.connected = make(map[Subsystem]bool)
	}
	if g.lastMessageAt == nil {
		g.lastMessageAt = make(map[Subsystem]time.Time)
	}
	if g.lastPublishAt == nil {
		g.lastPublishAt = make(map[Subsystem]time.Time)
	}
	if g.scheduledRetry == nil {
		g.scheduledRetry = make(map[Subsystem]cancelSchedule)
	}
	if g.retryGen == nil {
		g.retryGen = make(map[Subsystem]uint64)
	}

	if g.spawnFn == nil {
		g.spawnFn = g.spawnSubsystem
	}
	if g.poisonFn == nil {
		g.poisonFn = func(ac *actor.Context, pid *actor.PID) {
			if ac == nil || pid == nil {
				return
			}
			ac.Engine().Poison(pid)
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
	if g.globalRestartWindow <= 0 {
		g.globalRestartWindow = time.Minute
	}
	if g.globalRestartLimit <= 0 {
		g.globalRestartLimit = 5
	}
	if g.selfPID == nil && c != nil && c.PID() != nil {
		cloned := *c.PID().CloneVT()
		g.selfPID = &cloned
	}
	if g.sendToSelfFn == nil {
		var engine *actor.Engine
		if c != nil {
			engine = c.Engine()
		}
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
	return true
}

func (g *Guardian) startAll(c *actor.Context) {
	if g.started {
		return
	}
	g.started = true

	for _, subsystem := range g.managedSubsystems() {
		g.startSubsystem(c, subsystem)
	}
}

func (g *Guardian) stopAll(c *actor.Context) {
	if g.connected == nil {
		g.connected = make(map[Subsystem]bool)
	}
	for subsystem, cancel := range g.scheduledRetry {
		cancel()
		delete(g.scheduledRetry, subsystem)
		g.retryGen[subsystem]++
	}

	managed := g.managedSubsystems()
	for i := len(managed) - 1; i >= 0; i-- {
		subsystem := managed[i]
		pid := g.children[subsystem]
		if pid == nil {
			g.running[subsystem] = false
			g.connected[subsystem] = false
			g.lastTransition[subsystem] = g.clock.Now()
			metrics.SetGuardianSubsystemState(string(subsystem), 0)
			continue
		}
		g.poisonFn(c, pid)
		g.children[subsystem] = nil
		g.running[subsystem] = false
		g.connected[subsystem] = false
		g.lastTransition[subsystem] = g.clock.Now()
		metrics.SetGuardianSubsystemState(string(subsystem), 0)
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
	g.lastError[subsystem] = ""
	g.lastTransition[subsystem] = g.clock.Now()
	g.readySystems[subsystem] = true // v1 optimistic: ready on first successful spawn
	metrics.SetGuardianSubsystemState(string(subsystem), 1)

	if status := g.policy.Status(subsystem); status.Degraded {
		g.policy.MarkRecovered(subsystem)
		g.emitFn(c, Recovered{Subsystem: subsystem})
		g.lastTransition[subsystem] = g.clock.Now()
	}
}

// computeReady returns whether all expected subsystems have started at least once.
func (g *Guardian) computeReady() (ready bool, pending []Subsystem) {
	expected := g.ensureExpected()
	if len(expected) == 0 {
		return true, nil
	}
	for _, sub := range expected {
		if !g.readySystems[sub] {
			pending = append(pending, sub)
		}
	}
	return len(pending) == 0, pending
}

// ensureExpected returns the list of expected subsystems, computing it from
// cfg.Factories the first time it is called (if cfg.ExpectedSubsystems is nil).
func (g *Guardian) ensureExpected() []Subsystem {
	if g.expectedSubsystems != nil {
		return g.expectedSubsystems
	}
	if g.cfg.ExpectedSubsystems != nil {
		g.expectedSubsystems = g.cfg.ExpectedSubsystems
		return g.expectedSubsystems
	}
	// Infer from Factories: subsystems with a non-nil factory are expected.
	inferred := make([]Subsystem, 0, len(g.cfg.Factories))
	for sub, factory := range g.cfg.Factories {
		if factory == nil {
			continue
		}
		inferred = append(inferred, sub)
	}
	sort.Slice(inferred, func(i, j int) bool { return inferred[i] < inferred[j] })
	g.expectedSubsystems = inferred
	return g.expectedSubsystems
}

func (g *Guardian) retrySubsystem(c *actor.Context, subsystem Subsystem, generation uint64) {
	if g.shuttingDown {
		return // stale retry during shutdown; discard
	}
	if g.retryGen[subsystem] != generation {
		return
	}
	delete(g.scheduledRetry, subsystem)
	status := g.policy.Status(subsystem)
	now := g.clock.Now()
	if status.Degraded && now.Before(status.CooldownUntil) {
		return
	}
	g.startSubsystem(c, subsystem)
}

func (g *Guardian) handleChildFailed(c *actor.Context, msg ChildFailed) {
	if msg.Subsystem == "" {
		return
	}
	if g.connected == nil {
		g.connected = make(map[Subsystem]bool)
	}
	if g.shuttingDown {
		return // no restarts during controlled shutdown
	}
	if msg.Err != nil {
		g.lastError[msg.Subsystem] = msg.Err.Error()
	} else {
		g.lastError[msg.Subsystem] = msg.Kind
	}
	g.lastFailureAt[msg.Subsystem] = g.clock.Now()
	g.running[msg.Subsystem] = false
	g.connected[msg.Subsystem] = false
	if pid := g.children[msg.Subsystem]; pid != nil {
		if g.poisonFn != nil {
			g.poisonFn(c, pid)
		}
	}
	g.children[msg.Subsystem] = nil
	g.lastTransition[msg.Subsystem] = g.clock.Now()

	now := g.clock.Now()
	decision := g.policy.OnFailure(msg.Subsystem, now)
	gen := g.bumpRetryGeneration(msg.Subsystem)
	if decision.EnterDegraded {
		metrics.IncGuardianDegraded(string(msg.Subsystem))
		metrics.SetGuardianSubsystemState(string(msg.Subsystem), 2)
		g.emitFn(c, Degraded{Subsystem: msg.Subsystem, Reason: decision.Reason})
		g.lastTransition[msg.Subsystem] = g.clock.Now()
		target := g.selfPID
		send := g.sendToSelfFn
		delay := decision.DegradedUntil.Sub(now)
		if delay < 0 {
			delay = 0
		}
		cancel := g.scheduleFn(delay, func() {
			send(target, retrySubsystem{Subsystem: msg.Subsystem, Generation: gen})
		})
		g.replaceScheduledRetry(msg.Subsystem, cancel)
		return
	}

	if !decision.Restart {
		return
	}
	if !g.allowGlobalRestart(now) {
		metrics.GuardianRateLimitedTotal.Inc()
		metrics.IncGuardianDegraded(string(msg.Subsystem))
		metrics.SetGuardianSubsystemState(string(msg.Subsystem), 2)
		target := g.selfPID
		send := g.sendToSelfFn
		delay := g.nextGlobalRetryDelay(now)
		if delay <= 0 {
			delay = time.Second
		}
		cancel := g.scheduleFn(delay, func() {
			send(target, retrySubsystem{Subsystem: msg.Subsystem, Generation: gen})
		})
		g.replaceScheduledRetry(msg.Subsystem, cancel)
		return
	}
	metrics.IncGuardianRestart(string(msg.Subsystem), msg.Kind)
	target := g.selfPID
	send := g.sendToSelfFn
	cancel := g.scheduleFn(decision.Delay, func() {
		send(target, retrySubsystem{Subsystem: msg.Subsystem, Generation: gen})
	})
	g.replaceScheduledRetry(msg.Subsystem, cancel)
}

func (g *Guardian) bumpRetryGeneration(subsystem Subsystem) uint64 {
	g.retryGen[subsystem]++
	return g.retryGen[subsystem]
}

func (g *Guardian) replaceScheduledRetry(subsystem Subsystem, cancel cancelSchedule) {
	if prev, ok := g.scheduledRetry[subsystem]; ok {
		prev()
	}
	g.scheduledRetry[subsystem] = cancel
}

func (g *Guardian) allowGlobalRestart(now time.Time) bool {
	if g.globalRestartLimit <= 0 || g.globalRestartWindow <= 0 {
		return true
	}
	g.globalRestartHistory = g.pruneGlobalRestarts(now)
	if len(g.globalRestartHistory) >= g.globalRestartLimit {
		return false
	}
	g.globalRestartHistory = append(g.globalRestartHistory, now)
	return true
}

func (g *Guardian) nextGlobalRetryDelay(now time.Time) time.Duration {
	if g.globalRestartLimit <= 0 || g.globalRestartWindow <= 0 {
		return 0
	}
	g.globalRestartHistory = g.pruneGlobalRestarts(now)
	if len(g.globalRestartHistory) < g.globalRestartLimit {
		return 0
	}
	if len(g.globalRestartHistory) == 0 {
		return g.globalRestartWindow
	}
	oldest := g.globalRestartHistory[0]
	delay := oldest.Add(g.globalRestartWindow).Sub(now)
	if delay < 0 {
		return 0
	}
	return delay
}

func (g *Guardian) pruneGlobalRestarts(now time.Time) []time.Time {
	if len(g.globalRestartHistory) == 0 {
		return g.globalRestartHistory
	}
	cutoff := now.Add(-g.globalRestartWindow)
	idx := 0
	for idx < len(g.globalRestartHistory) && g.globalRestartHistory[idx].Before(cutoff) {
		idx++
	}
	if idx == 0 {
		return g.globalRestartHistory
	}
	return append([]time.Time(nil), g.globalRestartHistory[idx:]...)
}

func (g *Guardian) handleHeartbeat(msg SubsystemHeartbeat) {
	if msg.Subsystem == "" {
		return
	}
	g.connected[msg.Subsystem] = msg.Connected
	if !msg.LastMessageAt.IsZero() {
		g.lastMessageAt[msg.Subsystem] = msg.LastMessageAt
	}
	if !msg.LastPublishAt.IsZero() {
		g.lastPublishAt[msg.Subsystem] = msg.LastPublishAt
	}
}

func (g *Guardian) buildSnapshot() SnapshotState {
	managed := g.managedSubsystems()
	state := SnapshotState{
		At:         g.clock.Now(),
		Subsystems: make(map[Subsystem]SubsystemState, len(managed)),
	}
	for _, subsystem := range managed {
		policyState := g.policy.Status(subsystem)
		s := SubsystemState{
			Running:          g.running[subsystem],
			Degraded:         policyState.Degraded,
			Connected:        g.connected[subsystem],
			HasChild:         g.children[subsystem] != nil,
			LastError:        g.lastError[subsystem],
			LastFailureAt:    g.lastFailureAt[subsystem],
			LastTransitionAt: g.lastTransition[subsystem],
			LastMessageAt:    g.lastMessageAt[subsystem],
			LastPublishAt:    g.lastPublishAt[subsystem],
			RestartCount:     policyState.RestartCount,
			CooldownUntil:    policyState.CooldownUntil,
		}
		if pid := g.children[subsystem]; pid != nil {
			s.ChildPID = pid.String()
		}
		state.Subsystems[subsystem] = s
	}
	return state
}

func (g *Guardian) spawnSubsystem(c *actor.Context, subsystem Subsystem) (*actor.PID, error) {
	if c == nil {
		return nil, fmt.Errorf("actor context is nil")
	}
	producer := subsystemPlaceholder(subsystem)
	if factory, ok := g.cfg.Factories[subsystem]; ok && factory != nil {
		producer = factory
	}
	pid := c.SpawnChild(
		producer,
		"runtime-subsystem",
		actor.WithID(fmt.Sprintf("runtime-%s", sanitizeSubsystemID(subsystem))),
	)
	return pid, nil
}

func (g *Guardian) managedSubsystems() []Subsystem {
	managed := make([]Subsystem, 0, len(orderedSubsystems)+len(g.cfg.Factories)+len(g.cfg.ExpectedSubsystems))
	seen := make(map[Subsystem]struct{}, len(orderedSubsystems)+len(g.cfg.Factories)+len(g.cfg.ExpectedSubsystems))
	hasDynamicMarketData := false

	for sub := range g.cfg.Factories {
		if strings.HasPrefix(string(sub), string(SubsystemMarketData)+":") {
			hasDynamicMarketData = true
			break
		}
	}
	for _, sub := range g.cfg.ExpectedSubsystems {
		if strings.HasPrefix(string(sub), string(SubsystemMarketData)+":") {
			hasDynamicMarketData = true
			break
		}
	}

	for _, sub := range orderedSubsystems {
		if hasDynamicMarketData && sub == SubsystemMarketData {
			continue
		}
		managed = append(managed, sub)
		seen[sub] = struct{}{}
	}

	var extras []Subsystem
	for sub := range g.cfg.Factories {
		if _, ok := seen[sub]; ok {
			continue
		}
		extras = append(extras, sub)
		seen[sub] = struct{}{}
	}
	for _, sub := range g.cfg.ExpectedSubsystems {
		if _, ok := seen[sub]; ok {
			continue
		}
		extras = append(extras, sub)
		seen[sub] = struct{}{}
	}
	sort.Slice(extras, func(i, j int) bool { return extras[i] < extras[j] })
	managed = append(managed, extras...)
	return managed
}

func sanitizeSubsystemID(subsystem Subsystem) string {
	id := strings.TrimSpace(string(subsystem))
	if id == "" {
		return "unknown"
	}
	repl := strings.NewReplacer(":", "-", "/", "-", " ", "-", "\t", "-", "\n", "-")
	return repl.Replace(id)
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
