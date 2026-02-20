package runtime

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newSupervisorPolicyForTest creates a policy with explicit config values
// suitable for most supervisor tests: base=1s, max=8s, jitter=0, window=1m,
// limit=3, cooldown=30s. rng defaults to fixedRNG{0.5}.
func newSupervisorPolicyForTest(t *testing.T, clock Clock, rng RNG) *SupervisorPolicy {
	t.Helper()
	if rng == nil {
		rng = fixedRNG{value: 0.5}
	}
	p, prob := NewSupervisorPolicy(SupervisorConfig{
		BaseBackoff:   time.Second,
		MaxBackoff:    8 * time.Second,
		Jitter:        0,
		RestartWindow: time.Minute,
		RestartLimit:  3,
		Cooldown:      30 * time.Second,
	}, clock, rng)
	if prob != nil {
		t.Fatalf("newSupervisorPolicyForTest: %v", prob)
	}
	return p
}

// ---------------------------------------------------------------------------
// 1. NewSupervisorPolicy defaults
// ---------------------------------------------------------------------------

func TestNewSupervisorPolicy_ZeroConfig_FillsDefaults(t *testing.T) {
	p, prob := NewSupervisorPolicy(SupervisorConfig{}, nil, nil)
	if prob != nil {
		t.Fatalf("unexpected error: %v", prob)
	}

	// Verify via observable behavior: first failure should restart with
	// the default BaseBackoff (250ms).
	now := time.Unix(1000, 0)
	d := p.OnFailure(SubsystemMarketData, now)
	if !d.Restart {
		t.Fatal("expected restart on first failure")
	}
	if d.Delay != 250*time.Millisecond {
		t.Fatalf("default base backoff delay = %v, want 250ms", d.Delay)
	}
}

// ---------------------------------------------------------------------------
// 2. NewSupervisorPolicy validation
// ---------------------------------------------------------------------------

func TestNewSupervisorPolicy_Validation(t *testing.T) {
	tests := []struct {
		name string
		cfg  SupervisorConfig
	}{
		{
			name: "MaxBackoff < BaseBackoff",
			cfg: SupervisorConfig{
				BaseBackoff: 10 * time.Second,
				MaxBackoff:  1 * time.Second,
			},
		},
		{
			name: "Jitter negative",
			cfg: SupervisorConfig{
				BaseBackoff: time.Second,
				MaxBackoff:  5 * time.Second,
				Jitter:      -0.1,
			},
		},
		{
			name: "Jitter > 1",
			cfg: SupervisorConfig{
				BaseBackoff: time.Second,
				MaxBackoff:  5 * time.Second,
				Jitter:      1.5,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, prob := NewSupervisorPolicy(tc.cfg, nil, nil)
			if prob == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 3. Exponential backoff
// ---------------------------------------------------------------------------

func TestOnFailure_ExponentialBackoff(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	p := newSupervisorPolicyForTest(t, clock, nil)

	// Attempt 0 (first failure in window) -> base = 1s
	d0 := p.OnFailure(SubsystemMarketData, clock.now)
	if !d0.Restart {
		t.Fatal("attempt 0: expected restart")
	}
	if d0.Delay != time.Second {
		t.Fatalf("attempt 0: delay = %v, want 1s", d0.Delay)
	}

	// Attempt 1 -> 2 * base = 2s
	clock.now = clock.now.Add(time.Second)
	d1 := p.OnFailure(SubsystemMarketData, clock.now)
	if !d1.Restart {
		t.Fatal("attempt 1: expected restart")
	}
	if d1.Delay != 2*time.Second {
		t.Fatalf("attempt 1: delay = %v, want 2s", d1.Delay)
	}

	// Attempt 2 -> 4 * base = 4s
	clock.now = clock.now.Add(time.Second)
	d2 := p.OnFailure(SubsystemMarketData, clock.now)
	if !d2.Restart {
		t.Fatal("attempt 2: expected restart")
	}
	if d2.Delay != 4*time.Second {
		t.Fatalf("attempt 2: delay = %v, want 4s", d2.Delay)
	}
}

// ---------------------------------------------------------------------------
// 4. Backoff capped at MaxBackoff
// ---------------------------------------------------------------------------

func TestOnFailure_BackoffCappedAtMax(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	// Use config where cap is hit quickly: base=1s, max=2s, limit=10.
	p, prob := NewSupervisorPolicy(SupervisorConfig{
		BaseBackoff:   time.Second,
		MaxBackoff:    2 * time.Second,
		Jitter:        0,
		RestartWindow: time.Minute,
		RestartLimit:  10,
		Cooldown:      30 * time.Second,
	}, clock, fixedRNG{value: 0.5})
	if prob != nil {
		t.Fatalf("new policy: %v", prob)
	}

	// Attempt 0 -> 1s (base)
	d := p.OnFailure(SubsystemAggregation, clock.now)
	if d.Delay != time.Second {
		t.Fatalf("attempt 0: delay = %v, want 1s", d.Delay)
	}

	// Attempt 1 -> min(2s, 2s) = 2s (cap reached)
	clock.now = clock.now.Add(time.Second)
	d = p.OnFailure(SubsystemAggregation, clock.now)
	if d.Delay != 2*time.Second {
		t.Fatalf("attempt 1: delay = %v, want 2s", d.Delay)
	}

	// Attempt 2 -> min(4s, 2s) = 2s (capped)
	clock.now = clock.now.Add(time.Second)
	d = p.OnFailure(SubsystemAggregation, clock.now)
	if d.Delay != 2*time.Second {
		t.Fatalf("attempt 2: delay = %v, want 2s (capped)", d.Delay)
	}

	// Attempt 3 -> min(8s, 2s) = 2s (capped)
	clock.now = clock.now.Add(time.Second)
	d = p.OnFailure(SubsystemAggregation, clock.now)
	if d.Delay != 2*time.Second {
		t.Fatalf("attempt 3: delay = %v, want 2s (capped)", d.Delay)
	}
}

// ---------------------------------------------------------------------------
// 5. Jitter bounds
// ---------------------------------------------------------------------------

func TestOnFailure_JitterBounds(t *testing.T) {
	tests := []struct {
		name      string
		rngValue  float64
		wantDelay time.Duration
	}{
		{
			// rng=0.0 -> scale = 1 + ((0.0*2)-1)*0.5 = 1 + (-1)*0.5 = 0.5
			// delay = 0.5 * 1s = 500ms
			name:      "rng=0.0 yields minimum scale 0.5",
			rngValue:  0.0,
			wantDelay: 500 * time.Millisecond,
		},
		{
			// rng=1.0 -> scale = 1 + ((1.0*2)-1)*0.5 = 1 + (1)*0.5 = 1.5
			// delay = 1.5 * 1s = 1500ms
			name:      "rng=1.0 yields maximum scale 1.5",
			rngValue:  1.0,
			wantDelay: 1500 * time.Millisecond,
		},
		{
			// rng=0.5 -> scale = 1 + ((0.5*2)-1)*0.5 = 1 + 0*0.5 = 1.0
			// delay = 1.0 * 1s = 1000ms (unchanged)
			name:      "rng=0.5 yields scale 1.0 (no change)",
			rngValue:  0.5,
			wantDelay: time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeClock{now: time.Unix(1000, 0)}
			p, prob := NewSupervisorPolicy(SupervisorConfig{
				BaseBackoff:   time.Second,
				MaxBackoff:    8 * time.Second,
				Jitter:        0.5,
				RestartWindow: time.Minute,
				RestartLimit:  5,
				Cooldown:      30 * time.Second,
			}, clock, fixedRNG{value: tc.rngValue})
			if prob != nil {
				t.Fatalf("new policy: %v", prob)
			}

			d := p.OnFailure(SubsystemMarketData, clock.now)
			if !d.Restart {
				t.Fatal("expected restart")
			}
			if d.Delay != tc.wantDelay {
				t.Fatalf("delay = %v, want %v", d.Delay, tc.wantDelay)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 6. Jitter zero leaves delay unchanged
// ---------------------------------------------------------------------------

func TestOnFailure_ZeroJitter_DelayUnchanged(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	p, prob := NewSupervisorPolicy(SupervisorConfig{
		BaseBackoff:   time.Second,
		MaxBackoff:    8 * time.Second,
		Jitter:        0,
		RestartWindow: time.Minute,
		RestartLimit:  5,
		Cooldown:      30 * time.Second,
	}, clock, fixedRNG{value: 0.0}) // extreme rng value should not matter
	if prob != nil {
		t.Fatalf("new policy: %v", prob)
	}

	d := p.OnFailure(SubsystemMarketData, clock.now)
	if d.Delay != time.Second {
		t.Fatalf("delay = %v, want 1s (jitter=0 should not modify)", d.Delay)
	}
}

// ---------------------------------------------------------------------------
// 7. Restart limit exceeded -> degraded
// ---------------------------------------------------------------------------

func TestOnFailure_RestartLimitExceeded_EntersDegraded(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	p := newSupervisorPolicyForTest(t, clock, nil)
	// RestartLimit is 3. Failures 0..2 restart; failure 3 (4 total) degrades.

	for i := 0; i < 3; i++ {
		clock.now = clock.now.Add(time.Second)
		d := p.OnFailure(SubsystemDelivery, clock.now)
		if !d.Restart {
			t.Fatalf("failure %d: expected restart", i)
		}
		if d.EnterDegraded {
			t.Fatalf("failure %d: should not enter degraded yet", i)
		}
	}

	// Failure #4 (RestartLimit+1) should trigger degradation.
	clock.now = clock.now.Add(time.Second)
	d := p.OnFailure(SubsystemDelivery, clock.now)
	if d.Restart {
		t.Fatal("expected no restart after limit exceeded")
	}
	if !d.EnterDegraded {
		t.Fatal("expected EnterDegraded=true")
	}
	if d.Reason != "restart limit exceeded" {
		t.Fatalf("reason = %q, want %q", d.Reason, "restart limit exceeded")
	}
	expectedDegradedUntil := clock.now.Add(30 * time.Second)
	if !d.DegradedUntil.Equal(expectedDegradedUntil) {
		t.Fatalf("DegradedUntil = %v, want %v", d.DegradedUntil, expectedDegradedUntil)
	}
}

// ---------------------------------------------------------------------------
// 8. Cooldown blocks restart
// ---------------------------------------------------------------------------

func TestOnFailure_CooldownBlocksRestart(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	p := newSupervisorPolicyForTest(t, clock, nil)

	// Exhaust restart limit to enter degraded mode.
	for i := 0; i <= 3; i++ {
		clock.now = clock.now.Add(time.Second)
		p.OnFailure(SubsystemStorage, clock.now)
	}

	// Verify degraded.
	status := p.Status(SubsystemStorage)
	if !status.Degraded {
		t.Fatal("expected degraded after limit exceeded")
	}

	// Failure during cooldown should be rejected.
	clock.now = clock.now.Add(5 * time.Second) // still within 30s cooldown
	d := p.OnFailure(SubsystemStorage, clock.now)
	if d.Restart {
		t.Fatal("expected no restart during cooldown")
	}
	if d.Reason != "subsystem in cooldown" {
		t.Fatalf("reason = %q, want %q", d.Reason, "subsystem in cooldown")
	}
	if d.EnterDegraded {
		t.Fatal("should not re-enter degraded; already degraded")
	}
}

// ---------------------------------------------------------------------------
// 9. Window pruning
// ---------------------------------------------------------------------------

func TestOnFailure_WindowPruning_AllowsRestartsAfterWindowExpires(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	p := newSupervisorPolicyForTest(t, clock, nil)
	// RestartWindow = 1 minute, RestartLimit = 3.

	// Record 3 failures (fills the limit).
	for i := 0; i < 3; i++ {
		clock.now = clock.now.Add(time.Second)
		d := p.OnFailure(SubsystemInsights, clock.now)
		if !d.Restart {
			t.Fatalf("failure %d: expected restart", i)
		}
	}

	// Advance time past the restart window so old failures are pruned.
	clock.now = clock.now.Add(2 * time.Minute)

	// This failure should succeed because old failures were pruned.
	d := p.OnFailure(SubsystemInsights, clock.now)
	if !d.Restart {
		t.Fatal("expected restart after window expired (old failures pruned)")
	}
	if d.EnterDegraded {
		t.Fatal("should not degrade after window pruning")
	}
	// After pruning, this is the only failure in the window -> attempt 0 -> base delay.
	if d.Delay != time.Second {
		t.Fatalf("delay = %v, want 1s (attempt 0 after pruning)", d.Delay)
	}
}

// ---------------------------------------------------------------------------
// 10. MarkRecovered
// ---------------------------------------------------------------------------

func TestMarkRecovered_ClearsFailuresAndDegradedState(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	p := newSupervisorPolicyForTest(t, clock, nil)

	// Exhaust limit to enter degraded.
	for i := 0; i <= 3; i++ {
		clock.now = clock.now.Add(time.Second)
		p.OnFailure(SubsystemMarketData, clock.now)
	}
	if !p.Status(SubsystemMarketData).Degraded {
		t.Fatal("precondition: expected degraded")
	}

	p.MarkRecovered(SubsystemMarketData)

	status := p.Status(SubsystemMarketData)
	if status.Degraded {
		t.Fatal("expected not degraded after MarkRecovered")
	}
	if !status.CooldownUntil.IsZero() {
		t.Fatalf("CooldownUntil = %v, want zero", status.CooldownUntil)
	}

	// Next failure should restart normally at base delay (attempt 0).
	clock.now = clock.now.Add(time.Second)
	d := p.OnFailure(SubsystemMarketData, clock.now)
	if !d.Restart {
		t.Fatal("expected restart after MarkRecovered")
	}
	if d.Delay != time.Second {
		t.Fatalf("delay = %v, want 1s (fresh attempt after recovery)", d.Delay)
	}
}

// ---------------------------------------------------------------------------
// 11. Status returns correct state
// ---------------------------------------------------------------------------

func TestStatus_ReturnsCorrectState(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	p := newSupervisorPolicyForTest(t, clock, nil)

	// Initial status: no failures recorded.
	s := p.Status(SubsystemDelivery)
	if s.Degraded {
		t.Fatal("expected not degraded initially")
	}
	if s.RestartCount != 0 {
		t.Fatalf("RestartCount = %d, want 0", s.RestartCount)
	}
	if !s.CooldownUntil.IsZero() {
		t.Fatalf("CooldownUntil = %v, want zero", s.CooldownUntil)
	}

	// After two successful restarts.
	clock.now = clock.now.Add(time.Second)
	p.OnFailure(SubsystemDelivery, clock.now)
	clock.now = clock.now.Add(time.Second)
	p.OnFailure(SubsystemDelivery, clock.now)

	s = p.Status(SubsystemDelivery)
	if s.Degraded {
		t.Fatal("should not be degraded after 2 failures (limit=3)")
	}
	if s.RestartCount != 2 {
		t.Fatalf("RestartCount = %d, want 2", s.RestartCount)
	}

	// After exceeding limit (4th failure triggers degradation).
	clock.now = clock.now.Add(time.Second)
	p.OnFailure(SubsystemDelivery, clock.now) // 3rd restart
	clock.now = clock.now.Add(time.Second)
	p.OnFailure(SubsystemDelivery, clock.now) // exceeds limit

	s = p.Status(SubsystemDelivery)
	if !s.Degraded {
		t.Fatal("expected degraded after limit exceeded")
	}
	if s.RestartCount != 3 {
		t.Fatalf("RestartCount = %d, want 3 (4th failure degraded, no restart)", s.RestartCount)
	}
	expectedCooldown := clock.now.Add(30 * time.Second)
	if !s.CooldownUntil.Equal(expectedCooldown) {
		t.Fatalf("CooldownUntil = %v, want %v", s.CooldownUntil, expectedCooldown)
	}
}

// ---------------------------------------------------------------------------
// 12. Multiple subsystems isolated
// ---------------------------------------------------------------------------

func TestOnFailure_SubsystemIsolation(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	p := newSupervisorPolicyForTest(t, clock, nil)

	// Exhaust limit for subsystem A (marketdata).
	for i := 0; i <= 3; i++ {
		clock.now = clock.now.Add(time.Second)
		p.OnFailure(SubsystemMarketData, clock.now)
	}
	if !p.Status(SubsystemMarketData).Degraded {
		t.Fatal("marketdata should be degraded")
	}

	// Subsystem B (delivery) should be completely unaffected.
	clock.now = clock.now.Add(time.Second)
	d := p.OnFailure(SubsystemDelivery, clock.now)
	if !d.Restart {
		t.Fatal("delivery should restart independently")
	}
	if d.EnterDegraded {
		t.Fatal("delivery should not be degraded")
	}

	sDelivery := p.Status(SubsystemDelivery)
	if sDelivery.Degraded {
		t.Fatal("delivery status should not be degraded")
	}
	if sDelivery.RestartCount != 1 {
		t.Fatalf("delivery RestartCount = %d, want 1", sDelivery.RestartCount)
	}

	sMarket := p.Status(SubsystemMarketData)
	if !sMarket.Degraded {
		t.Fatal("marketdata should still be degraded")
	}
}

// ---------------------------------------------------------------------------
// 13. applyJitter edge case: zero/negative delay
// ---------------------------------------------------------------------------

func TestApplyJitter_ZeroAndNegativeDelay(t *testing.T) {
	rng := fixedRNG{value: 0.5}

	// Zero delay -> returned unchanged.
	got := applyJitter(0, 0.5, rng)
	if got != 0 {
		t.Fatalf("applyJitter(0, 0.5) = %v, want 0", got)
	}

	// Negative delay -> returned unchanged (guard: delay <= 0).
	got = applyJitter(-time.Second, 0.5, rng)
	if got != -time.Second {
		t.Fatalf("applyJitter(-1s, 0.5) = %v, want -1s", got)
	}
}

// ---------------------------------------------------------------------------
// Additional edge-case coverage
// ---------------------------------------------------------------------------

func TestCappedExponentialBackoff_NegativeAttempt(t *testing.T) {
	// Negative attempt is treated as attempt 0.
	got := cappedExponentialBackoff(time.Second, 10*time.Second, -1)
	if got != time.Second {
		t.Fatalf("cappedExponentialBackoff(1s, 10s, -1) = %v, want 1s", got)
	}
}

func TestCappedExponentialBackoff_LargeAttempt_Capped(t *testing.T) {
	// Very large attempt number should be capped at MaxBackoff.
	got := cappedExponentialBackoff(time.Second, 10*time.Second, 100)
	if got != 10*time.Second {
		t.Fatalf("cappedExponentialBackoff(1s, 10s, 100) = %v, want 10s", got)
	}
}

func TestPruneFailures_EmptySlice(t *testing.T) {
	now := time.Unix(1000, 0)
	got := pruneFailures(nil, now, time.Minute)
	if len(got) != 0 {
		t.Fatalf("pruneFailures(nil) = %v, want empty", got)
	}
}

func TestPruneFailures_AllWithinWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	events := []time.Time{
		now.Add(-30 * time.Second),
		now.Add(-10 * time.Second),
		now.Add(-1 * time.Second),
	}
	got := pruneFailures(events, now, time.Minute)
	if len(got) != 3 {
		t.Fatalf("pruneFailures kept %d, want 3 (all within window)", len(got))
	}
}

func TestPruneFailures_SomeExpired(t *testing.T) {
	now := time.Unix(1000, 0)
	events := []time.Time{
		now.Add(-2 * time.Minute),  // expired
		now.Add(-90 * time.Second), // expired
		now.Add(-30 * time.Second), // kept
		now.Add(-10 * time.Second), // kept
	}
	got := pruneFailures(events, now, time.Minute)
	if len(got) != 2 {
		t.Fatalf("pruneFailures kept %d, want 2", len(got))
	}
}

func TestOnFailure_CooldownExpires_AllowsNewCycle(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	p := newSupervisorPolicyForTest(t, clock, nil)

	// Exhaust limit to enter degraded.
	for i := 0; i <= 3; i++ {
		clock.now = clock.now.Add(time.Second)
		p.OnFailure(SubsystemMarketData, clock.now)
	}
	if !p.Status(SubsystemMarketData).Degraded {
		t.Fatal("precondition: expected degraded")
	}

	// Advance past cooldown (30s) AND restart window (1m) so everything resets.
	clock.now = clock.now.Add(2 * time.Minute)

	// Now OnFailure should restart (cooldown expired, old failures pruned).
	d := p.OnFailure(SubsystemMarketData, clock.now)
	if !d.Restart {
		t.Fatal("expected restart after cooldown + window expired")
	}
	if d.EnterDegraded {
		t.Fatal("should not re-degrade immediately")
	}
}

func TestOnFailure_DegradedUntil_FieldPopulatedAfterDegrade(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	p := newSupervisorPolicyForTest(t, clock, nil)

	// First 3 restarts should have DegradedUntil as zero time.
	for i := 0; i < 3; i++ {
		clock.now = clock.now.Add(time.Second)
		d := p.OnFailure(SubsystemAggregation, clock.now)
		if !d.DegradedUntil.IsZero() {
			t.Fatalf("failure %d: DegradedUntil should be zero before degradation, got %v", i, d.DegradedUntil)
		}
	}

	// 4th failure triggers degradation.
	clock.now = clock.now.Add(time.Second)
	d := p.OnFailure(SubsystemAggregation, clock.now)
	if d.DegradedUntil.IsZero() {
		t.Fatal("DegradedUntil should be set after entering degraded")
	}

	// Subsequent failure during cooldown should echo the DegradedUntil.
	clock.now = clock.now.Add(time.Second)
	d2 := p.OnFailure(SubsystemAggregation, clock.now)
	if !d2.DegradedUntil.Equal(d.DegradedUntil) {
		t.Fatalf("cooldown DegradedUntil = %v, want %v", d2.DegradedUntil, d.DegradedUntil)
	}
}

func TestMarkRecovered_UnknownSubsystem_NoOp(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	p := newSupervisorPolicyForTest(t, clock, nil)

	// MarkRecovered on unknown subsystem should not panic.
	p.MarkRecovered("nonexistent")

	// Verify the state was created but is clean.
	s := p.Status("nonexistent")
	if s.Degraded {
		t.Fatal("unknown subsystem should not be degraded")
	}
	if s.RestartCount != 0 {
		t.Fatalf("RestartCount = %d, want 0", s.RestartCount)
	}
}
