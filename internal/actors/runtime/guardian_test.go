package runtime

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
)

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

type fixedRNG struct {
	value float64
}

func (f fixedRNG) Float64() float64 { return f.value }

func newTestPolicy(t *testing.T, clock Clock) *SupervisorPolicy {
	t.Helper()
	policy, err := NewSupervisorPolicy(SupervisorConfig{
		BaseBackoff:   time.Second,
		MaxBackoff:    4 * time.Second,
		Jitter:        0,
		RestartWindow: time.Minute,
		RestartLimit:  2,
		Cooldown:      10 * time.Second,
	}, clock, fixedRNG{value: 0.5})
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	return policy
}

func newGuardianForTest(policy *SupervisorPolicy, clock Clock) *Guardian {
	return &Guardian{
		policy:         policy,
		clock:          clock,
		children:       map[Subsystem]*actor.PID{},
		running:        map[Subsystem]bool{},
		degraded:       map[Subsystem]bool{},
		lastError:      map[Subsystem]string{},
		scheduledRetry: map[Subsystem]cancelSchedule{},
		selfPID:        actor.NewPID("local", "guardian"),
	}
}

func TestGuardian_StartStopDeterministicOrder(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	policy := newTestPolicy(t, clock)
	g := newGuardianForTest(policy, clock)

	var started []Subsystem
	var stopped []Subsystem
	pidToSubsystem := map[string]Subsystem{}

	g.spawnFn = func(c *actor.Context, subsystem Subsystem) (*actor.PID, error) {
		started = append(started, subsystem)
		pid := actor.NewPID("local", fmt.Sprintf("%s-child", subsystem))
		pidToSubsystem[pid.ID] = subsystem
		return pid, nil
	}
	g.poisonFn = func(c *actor.Context, pid *actor.PID) {
		stopped = append(stopped, pidToSubsystem[pid.ID])
	}
	g.scheduleFn = func(delay time.Duration, fn func()) cancelSchedule {
		return func() {}
	}

	g.startAll(nil)

	if !reflect.DeepEqual(started, orderedSubsystems) {
		t.Fatalf("start order = %v, want %v", started, orderedSubsystems)
	}

	g.stopAll(nil)

	wantStop := []Subsystem{SubsystemInsights, SubsystemDelivery, SubsystemAggregation, SubsystemMarketData}
	if !reflect.DeepEqual(stopped, wantStop) {
		t.Fatalf("stop order = %v, want %v", stopped, wantStop)
	}

	for _, subsystem := range orderedSubsystems {
		if g.running[subsystem] {
			t.Fatalf("subsystem %s should be stopped", subsystem)
		}
	}
}

func TestGuardian_ChildFailedBackoffAndDegrade(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	policy := newTestPolicy(t, clock)
	g := newGuardianForTest(policy, clock)

	spawnCount := 0
	g.spawnFn = func(c *actor.Context, subsystem Subsystem) (*actor.PID, error) {
		spawnCount++
		return actor.NewPID("local", fmt.Sprintf("%s-%d", subsystem, spawnCount)), nil
	}
	g.sendToSelfFn = func(pid *actor.PID, msg any) {
		if retry, ok := msg.(retrySubsystem); ok {
			g.retrySubsystem(nil, retry.Subsystem)
		}
	}
	g.emitFn = func(c *actor.Context, msg any) {}

	var scheduled []time.Duration
	var callbacks []func()
	g.scheduleFn = func(delay time.Duration, fn func()) cancelSchedule {
		scheduled = append(scheduled, delay)
		callbacks = append(callbacks, fn)
		return func() {}
	}

	g.startSubsystem(nil, SubsystemMarketData)
	if !g.running[SubsystemMarketData] {
		t.Fatal("marketdata should be running after start")
	}

	g.handleChildFailed(nil, ChildFailed{Subsystem: SubsystemMarketData, Kind: "read", Err: errors.New("boom-1")})
	if got, want := scheduled[0], time.Second; got != want {
		t.Fatalf("first backoff delay = %v, want %v", got, want)
	}
	callbacks[0]()
	if !g.running[SubsystemMarketData] {
		t.Fatal("marketdata should be restarted after first failure")
	}

	clock.now = clock.now.Add(2 * time.Second)
	g.handleChildFailed(nil, ChildFailed{Subsystem: SubsystemMarketData, Kind: "read", Err: errors.New("boom-2")})
	if got, want := scheduled[1], 2*time.Second; got != want {
		t.Fatalf("second backoff delay = %v, want %v", got, want)
	}
	callbacks[1]()

	clock.now = clock.now.Add(2 * time.Second)
	g.handleChildFailed(nil, ChildFailed{Subsystem: SubsystemMarketData, Kind: "read", Err: errors.New("boom-3")})
	if !g.degraded[SubsystemMarketData] {
		t.Fatal("marketdata should enter degraded mode after restart limit")
	}
	if got, want := len(scheduled), 3; got != want {
		t.Fatalf("scheduled callbacks count = %d, want %d", got, want)
	}
	if got, want := scheduled[2], 10*time.Second; got != want {
		t.Fatalf("cooldown retry delay = %v, want %v", got, want)
	}
}

func TestGuardian_SnapshotConsistent(t *testing.T) {
	clock := &fakeClock{now: time.Unix(5000, 0)}
	policy := newTestPolicy(t, clock)
	g := newGuardianForTest(policy, clock)

	g.running[SubsystemMarketData] = true
	g.lastError[SubsystemDelivery] = "delivery failure"
	g.degraded[SubsystemDelivery] = true
	_ = policy.OnFailure(SubsystemDelivery, clock.now)

	snap := g.buildSnapshot()
	if !snap.At.Equal(clock.now) {
		t.Fatalf("snapshot at = %v, want %v", snap.At, clock.now)
	}
	if got, want := len(snap.Subsystems), 4; got != want {
		t.Fatalf("snapshot subsystem count = %d, want %d", got, want)
	}

	market := snap.Subsystems[SubsystemMarketData]
	if !market.Running {
		t.Fatal("marketdata should be running in snapshot")
	}

	delivery := snap.Subsystems[SubsystemDelivery]
	if !delivery.Degraded {
		t.Fatal("delivery should be degraded in snapshot")
	}
	if got, want := delivery.LastError, "delivery failure"; got != want {
		t.Fatalf("delivery last error = %q, want %q", got, want)
	}
	if delivery.RestartCount == 0 {
		t.Fatal("delivery restart count should be > 0")
	}
}
