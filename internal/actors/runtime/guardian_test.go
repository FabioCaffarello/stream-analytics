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
		readySystems:   map[Subsystem]bool{},
		lastError:      map[Subsystem]string{},
		lastFailureAt:  map[Subsystem]time.Time{},
		lastTransition: map[Subsystem]time.Time{},
		scheduledRetry: map[Subsystem]cancelSchedule{},
		retryGen:       map[Subsystem]uint64{},
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

	wantStop := []Subsystem{SubsystemStorage, SubsystemInsights, SubsystemDelivery, SubsystemAggregation, SubsystemMarketData}
	if !reflect.DeepEqual(stopped, wantStop) {
		t.Fatalf("stop order = %v, want %v", stopped, wantStop)
	}

	for _, subsystem := range orderedSubsystems {
		if g.running[subsystem] {
			t.Fatalf("subsystem %s should be stopped", subsystem)
		}
	}
}

func TestGuardian_StartOrder_DynamicMarketDataKeys(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	policy := newTestPolicy(t, clock)
	g := newGuardianForTest(policy, clock)
	g.cfg = GuardianConfig{
		Factories: map[Subsystem]actor.Producer{
			"marketdata:binance": func() actor.Receiver { return &placeholderReceiver{} },
			"marketdata:bybit":   func() actor.Receiver { return &placeholderReceiver{} },
		},
	}

	var started []Subsystem
	g.spawnFn = func(c *actor.Context, subsystem Subsystem) (*actor.PID, error) {
		started = append(started, subsystem)
		return actor.NewPID("local", fmt.Sprintf("%s-child", subsystem)), nil
	}
	g.poisonFn = func(c *actor.Context, pid *actor.PID) {}
	g.scheduleFn = func(delay time.Duration, fn func()) cancelSchedule { return func() {} }

	g.startAll(nil)

	want := []Subsystem{
		SubsystemAggregation,
		SubsystemDelivery,
		SubsystemInsights,
		SubsystemStorage,
		"marketdata:binance",
		"marketdata:bybit",
	}
	if !reflect.DeepEqual(started, want) {
		t.Fatalf("start order = %v, want %v", started, want)
	}
}

func TestGuardian_StopAll_CancelsAndClearsScheduledRetries(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	policy := newTestPolicy(t, clock)
	g := newGuardianForTest(policy, clock)

	cancelCalls := 0
	g.scheduledRetry[SubsystemMarketData] = func() { cancelCalls++ }
	g.scheduledRetry[SubsystemAggregation] = func() { cancelCalls++ }

	g.stopAll(nil)

	if got, want := cancelCalls, 2; got != want {
		t.Fatalf("scheduled retry cancel calls = %d, want %d", got, want)
	}
	if len(g.scheduledRetry) != 0 {
		t.Fatalf("expected scheduledRetry to be empty, got %d", len(g.scheduledRetry))
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
			g.retrySubsystem(nil, retry.Subsystem, retry.Generation)
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
	if !policy.Status(SubsystemMarketData).Degraded {
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
	_ = policy.OnFailure(SubsystemDelivery, clock.now)
	_ = policy.OnFailure(SubsystemDelivery, clock.now)
	_ = policy.OnFailure(SubsystemDelivery, clock.now)

	snap := g.buildSnapshot()
	if !snap.At.Equal(clock.now) {
		t.Fatalf("snapshot at = %v, want %v", snap.At, clock.now)
	}
	if got, want := len(snap.Subsystems), 5; got != want {
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

func TestGuardian_EnsureDefaultsSafeWithNilContext(t *testing.T) {
	clock := &fakeClock{now: time.Unix(9000, 0)}
	policy := newTestPolicy(t, clock)
	g := &Guardian{
		cfg: GuardianConfig{
			Policy: policy,
			Clock:  clock,
		},
	}

	if ok := g.ensureDefaults(nil); !ok {
		t.Fatal("ensureDefaults should succeed with injected policy and nil context")
	}
	if g.sendToSelfFn == nil {
		t.Fatal("sendToSelfFn must be initialized even when context is nil")
	}

	g.sendToSelfFn(nil, retrySubsystem{})
}

func TestGuardian_ShuttingDown_IgnoresChildFailed(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	policy := newTestPolicy(t, clock)
	g := newGuardianForTest(policy, clock)

	restartScheduled := false
	g.scheduleFn = func(delay time.Duration, fn func()) cancelSchedule {
		restartScheduled = true
		return func() {}
	}
	g.emitFn = func(c *actor.Context, msg any) {}
	g.spawnFn = func(c *actor.Context, subsystem Subsystem) (*actor.PID, error) {
		return actor.NewPID("local", string(subsystem)), nil
	}

	g.startSubsystem(nil, SubsystemMarketData)
	g.shuttingDown = true

	g.handleChildFailed(nil, ChildFailed{Subsystem: SubsystemMarketData, Kind: "test"})

	if restartScheduled {
		t.Fatal("restart should not be scheduled during shutdown")
	}
}

func TestGuardian_ShuttingDown_RetrySubsystemIsNoop(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	policy := newTestPolicy(t, clock)
	g := newGuardianForTest(policy, clock)

	spawnCalled := false
	g.spawnFn = func(c *actor.Context, subsystem Subsystem) (*actor.PID, error) {
		spawnCalled = true
		return actor.NewPID("local", string(subsystem)), nil
	}
	g.emitFn = func(c *actor.Context, msg any) {}

	// Simulate: a retry was scheduled before shutdown, then shutdown happened.
	gen := g.bumpRetryGeneration(SubsystemMarketData)
	g.shuttingDown = true
	spawnCalled = false // reset after bumpRetryGeneration doesn't spawn

	g.retrySubsystem(nil, SubsystemMarketData, gen)

	if spawnCalled {
		t.Fatal("retrySubsystem should be a no-op during shutdown")
	}
}

func TestGuardian_GlobalRestartRateLimit_DefersSixthRestart(t *testing.T) {
	clock := &fakeClock{now: time.Unix(2000, 0)}
	policy, err := NewSupervisorPolicy(SupervisorConfig{
		BaseBackoff:   time.Millisecond,
		MaxBackoff:    time.Second,
		Jitter:        0,
		RestartWindow: time.Minute,
		RestartLimit:  100,
		Cooldown:      time.Second,
	}, clock, fixedRNG{value: 0.5})
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	g := newGuardianForTest(policy, clock)
	g.globalRestartWindow = time.Minute
	g.globalRestartLimit = 5
	g.emitFn = func(c *actor.Context, msg any) {}
	g.spawnFn = func(c *actor.Context, subsystem Subsystem) (*actor.PID, error) {
		return actor.NewPID("local", string(subsystem)), nil
	}
	g.startSubsystem(nil, SubsystemMarketData)

	var scheduled []time.Duration
	g.scheduleFn = func(delay time.Duration, fn func()) cancelSchedule {
		scheduled = append(scheduled, delay)
		return func() {}
	}

	for i := 0; i < 6; i++ {
		g.handleChildFailed(nil, ChildFailed{
			Subsystem: SubsystemMarketData,
			Kind:      "read",
			Err:       errors.New("boom"),
		})
	}
	if len(scheduled) != 6 {
		t.Fatalf("scheduled=%d want=6", len(scheduled))
	}
	if scheduled[5] < 30*time.Second {
		t.Fatalf("expected 6th restart deferred by global limiter, delay=%v", scheduled[5])
	}
}

func TestGuardian_Readiness_NoFactories_AlwaysReady(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	policy := newTestPolicy(t, clock)
	// Guardian with no Factories and nil ExpectedSubsystems → infers empty set → always ready.
	g := newGuardianForTest(policy, clock)
	g.cfg = GuardianConfig{Policy: policy, Clock: clock}

	ready, pending := g.computeReady()
	if !ready {
		t.Fatal("guardian with no factories should be immediately ready")
	}
	if len(pending) != 0 {
		t.Fatalf("pending should be empty, got %v", pending)
	}
}

func TestGuardian_Readiness_WithFactory_PendingUntilStarted(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	policy := newTestPolicy(t, clock)
	g := newGuardianForTest(policy, clock)
	g.cfg = GuardianConfig{
		Policy: policy,
		Clock:  clock,
		Factories: map[Subsystem]actor.Producer{
			SubsystemMarketData: func() actor.Receiver { return &placeholderReceiver{} },
		},
	}

	ready, pending := g.computeReady()
	if ready {
		t.Fatal("guardian should not be ready before subsystem starts")
	}
	if len(pending) != 1 || pending[0] != SubsystemMarketData {
		t.Fatalf("pending = %v, want [%s]", pending, SubsystemMarketData)
	}

	// Simulate successful spawn.
	g.spawnFn = func(c *actor.Context, subsystem Subsystem) (*actor.PID, error) {
		return actor.NewPID("local", string(subsystem)), nil
	}
	g.emitFn = func(c *actor.Context, msg any) {}
	g.startSubsystem(nil, SubsystemMarketData)

	ready, pending = g.computeReady()
	if !ready {
		t.Fatal("guardian should be ready after subsystem starts")
	}
	if len(pending) != 0 {
		t.Fatalf("pending should be empty after start, got %v", pending)
	}
}

func TestGuardian_Readiness_ExplicitExpectedSubsystems(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	policy := newTestPolicy(t, clock)
	g := newGuardianForTest(policy, clock)
	g.cfg = GuardianConfig{
		Policy: policy,
		Clock:  clock,
		// Only marketdata is expected, even though aggregation might also spawn.
		ExpectedSubsystems: []Subsystem{SubsystemMarketData},
	}

	ready, _ := g.computeReady()
	if ready {
		t.Fatal("not ready before marketdata starts")
	}

	g.readySystems[SubsystemMarketData] = true
	ready, pending := g.computeReady()
	if !ready {
		t.Fatal("ready after marketdata marked ready")
	}
	if len(pending) != 0 {
		t.Fatalf("unexpected pending: %v", pending)
	}
}

func TestGuardian_Readiness_DynamicExpectedSubsystems(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	policy := newTestPolicy(t, clock)
	g := newGuardianForTest(policy, clock)
	g.cfg = GuardianConfig{
		Policy: policy,
		Clock:  clock,
		ExpectedSubsystems: []Subsystem{
			"marketdata:binance",
			"marketdata:bybit",
		},
	}

	ready, pending := g.computeReady()
	if ready {
		t.Fatal("expected not ready before dynamic subsystems start")
	}
	if len(pending) != 2 {
		t.Fatalf("pending=%v want 2 subsystems", pending)
	}

	g.readySystems["marketdata:binance"] = true
	ready, pending = g.computeReady()
	if ready {
		t.Fatal("expected not ready with only one dynamic subsystem ready")
	}
	if len(pending) != 1 || pending[0] != "marketdata:bybit" {
		t.Fatalf("pending=%v want [marketdata:bybit]", pending)
	}

	g.readySystems["marketdata:bybit"] = true
	ready, pending = g.computeReady()
	if !ready || len(pending) != 0 {
		t.Fatalf("expected ready after both subsystems, ready=%v pending=%v", ready, pending)
	}
}

func TestGuardian_Readiness_ExplicitEmptySlice_AlwaysReady(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	policy := newTestPolicy(t, clock)
	g := newGuardianForTest(policy, clock)
	g.cfg = GuardianConfig{
		Policy:             policy,
		Clock:              clock,
		ExpectedSubsystems: []Subsystem{}, // explicit empty = no readiness tracking
	}

	ready, pending := g.computeReady()
	if !ready {
		t.Fatal("explicit empty ExpectedSubsystems should always be ready")
	}
	if len(pending) != 0 {
		t.Fatalf("unexpected pending: %v", pending)
	}
}
