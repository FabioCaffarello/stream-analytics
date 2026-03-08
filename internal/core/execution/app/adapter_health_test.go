package app

import "testing"

func TestAdapterHealth_NotTrippedByDefault(t *testing.T) {
	h := newAdapterHealth(3, 10_000)
	if h.isTripped("adapter-a", 1_000) {
		t.Fatal("expected new adapter to not be tripped")
	}
}

func TestAdapterHealth_TripsAfterThreshold(t *testing.T) {
	h := newAdapterHealth(3, 10_000)
	nowMs := int64(1_000)

	// First two failures: circuit stays closed.
	if tripped := h.recordFailure("adapter-a", nowMs); tripped {
		t.Fatal("should not trip after 1 failure")
	}
	if tripped := h.recordFailure("adapter-a", nowMs); tripped {
		t.Fatal("should not trip after 2 failures")
	}
	if h.isTripped("adapter-a", nowMs) {
		t.Fatal("circuit should still be closed after 2 failures")
	}

	// Third failure: circuit trips.
	if tripped := h.recordFailure("adapter-a", nowMs); !tripped {
		t.Fatal("should trip after 3rd failure (threshold)")
	}
	if !h.isTripped("adapter-a", nowMs) {
		t.Fatal("circuit should be tripped")
	}
}

func TestAdapterHealth_CooldownResetsCircuit(t *testing.T) {
	h := newAdapterHealth(3, 10_000)
	tripMs := int64(1_000)

	h.recordFailure("adapter-a", tripMs)
	h.recordFailure("adapter-a", tripMs)
	h.recordFailure("adapter-a", tripMs)

	// Still tripped within cooldown.
	if !h.isTripped("adapter-a", tripMs+5_000) {
		t.Fatal("expected tripped within cooldown window")
	}

	// Cooldown expired -- should allow probe.
	if h.isTripped("adapter-a", tripMs+10_000) {
		t.Fatal("expected circuit to reset after cooldown")
	}

	// Verify internal state was actually reset (half-open).
	snap := h.snapshot()
	if snap["adapter-a"].ConsecutiveFailures != 0 {
		t.Fatalf("consecutive_failures=%d want=0 after cooldown reset", snap["adapter-a"].ConsecutiveFailures)
	}
}

func TestAdapterHealth_SuccessResetsCounter(t *testing.T) {
	h := newAdapterHealth(3, 10_000)
	nowMs := int64(1_000)

	h.recordFailure("adapter-a", nowMs)
	h.recordFailure("adapter-a", nowMs)
	// Two failures, one short of threshold.
	h.recordSuccess("adapter-a")

	// After success, counter resets. Need 3 more to trip.
	h.recordFailure("adapter-a", nowMs)
	h.recordFailure("adapter-a", nowMs)
	if h.isTripped("adapter-a", nowMs) {
		t.Fatal("expected circuit to remain closed after success reset + 2 failures")
	}
}

func TestAdapterHealth_SuccessAfterTrip_ClosesCircuit(t *testing.T) {
	h := newAdapterHealth(2, 10_000)
	nowMs := int64(1_000)

	h.recordFailure("adapter-a", nowMs)
	h.recordFailure("adapter-a", nowMs)
	if !h.isTripped("adapter-a", nowMs) {
		t.Fatal("expected tripped after threshold")
	}

	// Simulate cooldown expiry + successful probe.
	h.isTripped("adapter-a", nowMs+10_000) // resets to half-open
	h.recordSuccess("adapter-a")

	if h.isTripped("adapter-a", nowMs+10_001) {
		t.Fatal("expected closed after successful probe")
	}
	snap := h.snapshot()
	if snap["adapter-a"].TrippedAtMs != 0 {
		t.Fatalf("tripped_at_ms=%d want=0 after success", snap["adapter-a"].TrippedAtMs)
	}
}

func TestAdapterHealth_SnapshotReturnsCorrectState(t *testing.T) {
	h := newAdapterHealth(3, 10_000)
	nowMs := int64(5_000)

	h.recordFailure("adapter-a", nowMs)
	h.recordFailure("adapter-a", nowMs)
	h.recordFailure("adapter-b", nowMs)

	snap := h.snapshot()

	if len(snap) != 2 {
		t.Fatalf("snapshot len=%d want=2", len(snap))
	}
	if snap["adapter-a"].ConsecutiveFailures != 2 {
		t.Fatalf("adapter-a failures=%d want=2", snap["adapter-a"].ConsecutiveFailures)
	}
	if snap["adapter-a"].TrippedAtMs != 0 {
		t.Fatalf("adapter-a tripped_at_ms=%d want=0", snap["adapter-a"].TrippedAtMs)
	}
	if snap["adapter-b"].ConsecutiveFailures != 1 {
		t.Fatalf("adapter-b failures=%d want=1", snap["adapter-b"].ConsecutiveFailures)
	}
}

func TestAdapterHealth_IsolatesAdapters(t *testing.T) {
	h := newAdapterHealth(2, 10_000)
	nowMs := int64(1_000)

	// Trip adapter-a.
	h.recordFailure("adapter-a", nowMs)
	h.recordFailure("adapter-a", nowMs)

	if !h.isTripped("adapter-a", nowMs) {
		t.Fatal("adapter-a should be tripped")
	}
	if h.isTripped("adapter-b", nowMs) {
		t.Fatal("adapter-b should not be affected by adapter-a failures")
	}
}

func TestAdapterHealth_DefaultThresholdAndCooldown(t *testing.T) {
	h := newAdapterHealth(0, 0) // should default to 5 and 30_000
	nowMs := int64(1_000)

	for i := 0; i < 4; i++ {
		h.recordFailure("a", nowMs)
	}
	if h.isTripped("a", nowMs) {
		t.Fatal("should not trip before default threshold of 5")
	}
	h.recordFailure("a", nowMs)
	if !h.isTripped("a", nowMs) {
		t.Fatal("should trip at default threshold of 5")
	}

	// Default cooldown is 30_000ms.
	if h.isTripped("a", nowMs+30_000) {
		t.Fatal("should reset after default cooldown of 30_000ms")
	}
}

func TestAdapterHealth_SuccessOnUnknownAdapter_NoOp(t *testing.T) {
	h := newAdapterHealth(3, 10_000)
	// Should not panic or create entry.
	h.recordSuccess("unknown")
	snap := h.snapshot()
	if len(snap) != 0 {
		t.Fatalf("snapshot len=%d want=0", len(snap))
	}
}
