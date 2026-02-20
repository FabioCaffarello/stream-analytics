package runtime

import (
	crand "crypto/rand"
	"encoding/binary"
	"math"
	"time"

	"github.com/market-raccoon/internal/shared/problem"
)

// Clock abstracts wall time for deterministic policy tests.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// RNG abstracts jitter randomness for deterministic policy tests.
type RNG interface {
	Float64() float64
}

type stdRNG struct{}

func (stdRNG) Float64() float64 {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return 0.5
	}
	return float64(binary.LittleEndian.Uint64(b[:])) / (1 << 64)
}

// SupervisorConfig configures restart/backoff/degrade behavior.
type SupervisorConfig struct {
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	Jitter      float64

	RestartWindow time.Duration
	RestartLimit  int
	Cooldown      time.Duration
}

// SupervisorDecision is the computed policy result for a failure event.
type SupervisorDecision struct {
	Restart       bool
	Delay         time.Duration
	EnterDegraded bool
	DegradedUntil time.Time
	Reason        string
}

// PolicyStatus is the policy-only health state for one subsystem.
type PolicyStatus struct {
	Degraded      bool
	RestartCount  int
	CooldownUntil time.Time
}

// SupervisorPolicy tracks per-subsystem failure history and computes restart actions.
type SupervisorPolicy struct {
	cfg   SupervisorConfig
	clock Clock
	rng   RNG

	states map[Subsystem]*restartState
}

type restartState struct {
	failures      []time.Time
	degradedUntil time.Time
	restartCount  int
}

// NewSupervisorPolicy creates a policy with defaults suitable for runtime supervision.
func NewSupervisorPolicy(cfg SupervisorConfig, clock Clock, rng RNG) (*SupervisorPolicy, *problem.Problem) {
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 250 * time.Millisecond
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 5 * time.Second
	}
	if cfg.MaxBackoff < cfg.BaseBackoff {
		return nil, problem.New(problem.InvalidArgument, "max backoff must be >= base backoff")
	}
	if cfg.RestartWindow <= 0 {
		cfg.RestartWindow = 30 * time.Second
	}
	if cfg.RestartLimit <= 0 {
		cfg.RestartLimit = 5
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 30 * time.Second
	}
	if cfg.Jitter < 0 || cfg.Jitter > 1 {
		return nil, problem.New(problem.InvalidArgument, "jitter must be in [0,1]")
	}

	if clock == nil {
		clock = systemClock{}
	}
	if rng == nil {
		rng = stdRNG{}
	}

	return &SupervisorPolicy{
		cfg:    cfg,
		clock:  clock,
		rng:    rng,
		states: make(map[Subsystem]*restartState),
	}, nil
}

// OnFailure computes restart/degrade action for a failed subsystem.
func (p *SupervisorPolicy) OnFailure(subsystem Subsystem, now time.Time) SupervisorDecision {
	st := p.state(subsystem)

	if now.Before(st.degradedUntil) {
		return SupervisorDecision{
			Restart:       false,
			Delay:         0,
			EnterDegraded: false,
			DegradedUntil: st.degradedUntil,
			Reason:        "subsystem in cooldown",
		}
	}

	st.failures = pruneFailures(st.failures, now, p.cfg.RestartWindow)
	st.failures = append(st.failures, now)

	if len(st.failures) > p.cfg.RestartLimit {
		st.degradedUntil = now.Add(p.cfg.Cooldown)
		return SupervisorDecision{
			Restart:       false,
			Delay:         0,
			EnterDegraded: true,
			DegradedUntil: st.degradedUntil,
			Reason:        "restart limit exceeded",
		}
	}

	attempt := len(st.failures) - 1
	delay := cappedExponentialBackoff(p.cfg.BaseBackoff, p.cfg.MaxBackoff, attempt)
	delay = applyJitter(delay, p.cfg.Jitter, p.rng)
	st.restartCount++

	return SupervisorDecision{
		Restart:       true,
		Delay:         delay,
		EnterDegraded: false,
		DegradedUntil: st.degradedUntil,
	}
}

// MarkRecovered clears degraded state and failure window for a subsystem.
func (p *SupervisorPolicy) MarkRecovered(subsystem Subsystem) {
	st := p.state(subsystem)
	st.failures = nil
	st.degradedUntil = time.Time{}
}

// Status returns current policy status for a subsystem.
func (p *SupervisorPolicy) Status(subsystem Subsystem) PolicyStatus {
	now := p.clock.Now()
	st := p.state(subsystem)
	return PolicyStatus{
		Degraded:      now.Before(st.degradedUntil),
		RestartCount:  st.restartCount,
		CooldownUntil: st.degradedUntil,
	}
}

func (p *SupervisorPolicy) state(subsystem Subsystem) *restartState {
	st, ok := p.states[subsystem]
	if ok {
		return st
	}
	st = &restartState{}
	p.states[subsystem] = st
	return st
}

func pruneFailures(events []time.Time, now time.Time, window time.Duration) []time.Time {
	if len(events) == 0 {
		return events
	}
	cutoff := now.Add(-window)
	idx := 0
	for idx < len(events) && events[idx].Before(cutoff) {
		idx++
	}
	if idx == 0 {
		return events
	}
	return append([]time.Time(nil), events[idx:]...)
}

func cappedExponentialBackoff(base, capDelay time.Duration, attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	mult := math.Pow(2, float64(attempt))
	raw := time.Duration(float64(base) * mult)
	if raw > capDelay {
		return capDelay
	}
	return raw
}

func applyJitter(delay time.Duration, jitter float64, rng RNG) time.Duration {
	if delay <= 0 || jitter == 0 {
		return delay
	}
	scale := 1 + ((rng.Float64()*2)-1)*jitter
	if scale < 0 {
		scale = 0
	}
	return time.Duration(float64(delay) * scale)
}
