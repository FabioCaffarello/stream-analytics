package app

import (
	"sync"
	"testing"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
)

func validDirective(cmd executiondomain.ControlCommand) executiondomain.ControlDirective {
	return executiondomain.ControlDirective{
		Command:    cmd,
		Reason:     "test",
		IssuedAtMs: 1000,
		Issuer:     "operator-test",
	}
}

func validDirectiveWithTarget(cmd executiondomain.ControlCommand, target string) executiondomain.ControlDirective {
	d := validDirective(cmd)
	d.TargetID = target
	return d
}

func TestDefaultStateIsActive(t *testing.T) {
	cp := NewInMemoryControlPlane()
	snap := cp.Snapshot()
	if snap.State != executiondomain.ControlStateActive {
		t.Fatalf("expected active, got %s", snap.State)
	}
}

func TestPauseFromActive(t *testing.T) {
	cp := NewInMemoryControlPlane()
	if p := cp.Apply(validDirective(executiondomain.CommandPause)); p != nil {
		t.Fatalf("unexpected problem: %s", p.Message)
	}
	snap := cp.Snapshot()
	if snap.State != executiondomain.ControlStatePaused {
		t.Fatalf("expected paused, got %s", snap.State)
	}
}

func TestResumeFromPaused(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirective(executiondomain.CommandPause))
	if p := cp.Apply(validDirective(executiondomain.CommandResume)); p != nil {
		t.Fatalf("unexpected problem: %s", p.Message)
	}
	snap := cp.Snapshot()
	if snap.State != executiondomain.ControlStateActive {
		t.Fatalf("expected active, got %s", snap.State)
	}
}

func TestHaltFromAnyState(t *testing.T) {
	states := []executiondomain.ControlCommand{
		executiondomain.CommandPause,
		executiondomain.CommandDrain,
	}
	for _, setup := range states {
		cp := NewInMemoryControlPlane()
		if setup == executiondomain.CommandDrain || setup == executiondomain.CommandPause {
			cp.Apply(validDirective(setup))
		}
		if p := cp.Apply(validDirective(executiondomain.CommandHalt)); p != nil {
			t.Fatalf("halt from %s: unexpected problem: %s", setup, p.Message)
		}
		snap := cp.Snapshot()
		if snap.State != executiondomain.ControlStateHalted {
			t.Fatalf("expected halted after %s, got %s", setup, snap.State)
		}
	}
	// Also test halt from active
	cp := NewInMemoryControlPlane()
	if p := cp.Apply(validDirective(executiondomain.CommandHalt)); p != nil {
		t.Fatalf("halt from active: unexpected problem: %s", p.Message)
	}
	if cp.Snapshot().State != executiondomain.ControlStateHalted {
		t.Fatal("expected halted from active")
	}
}

func TestResumeFromHaltedFails(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirective(executiondomain.CommandHalt))
	p := cp.Apply(validDirective(executiondomain.CommandResume))
	if p == nil {
		t.Fatal("expected problem when resuming from halted")
	}
}

func TestDrainFromActive(t *testing.T) {
	cp := NewInMemoryControlPlane()
	if p := cp.Apply(validDirective(executiondomain.CommandDrain)); p != nil {
		t.Fatalf("unexpected problem: %s", p.Message)
	}
	snap := cp.Snapshot()
	if snap.State != executiondomain.ControlStateDrained {
		t.Fatalf("expected drained, got %s", snap.State)
	}
}

func TestDrainFromPaused(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirective(executiondomain.CommandPause))
	if p := cp.Apply(validDirective(executiondomain.CommandDrain)); p != nil {
		t.Fatalf("unexpected problem: %s", p.Message)
	}
	if cp.Snapshot().State != executiondomain.ControlStateDrained {
		t.Fatal("expected drained from paused")
	}
}

func TestResumeFromDrained(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirective(executiondomain.CommandDrain))
	if p := cp.Apply(validDirective(executiondomain.CommandResume)); p != nil {
		t.Fatalf("unexpected problem: %s", p.Message)
	}
	if cp.Snapshot().State != executiondomain.ControlStateActive {
		t.Fatal("expected active after resume from drained")
	}
}

func TestDisableStrategyBlocksExecution(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirectiveWithTarget(executiondomain.CommandDisableStrategy, "strat-1"))
	snap := cp.Snapshot()
	allowed, reason := snap.IsExecutionAllowed("strat-1", "adapter-1", "binance", "BTCUSDT")
	if allowed {
		t.Fatal("expected disabled strategy to block execution")
	}
	if reason != executiondomain.ReasonControlPlaneStrategyDisabled {
		t.Fatalf("expected strategy_disabled reason, got %s", reason)
	}
}

func TestEnableStrategyReallows(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirectiveWithTarget(executiondomain.CommandDisableStrategy, "strat-1"))
	cp.Apply(validDirectiveWithTarget(executiondomain.CommandEnableStrategy, "strat-1"))
	snap := cp.Snapshot()
	allowed, _ := snap.IsExecutionAllowed("strat-1", "adapter-1", "binance", "BTCUSDT")
	if !allowed {
		t.Fatal("expected enabled strategy to allow execution")
	}
}

func TestDisableAdapterBlocksExecution(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirectiveWithTarget(executiondomain.CommandDisableAdapter, "sim-adapter"))
	snap := cp.Snapshot()
	allowed, reason := snap.IsExecutionAllowed("strat-1", "sim-adapter", "binance", "BTCUSDT")
	if allowed {
		t.Fatal("expected disabled adapter to block execution")
	}
	if reason != executiondomain.ReasonControlPlaneAdapterDisabled {
		t.Fatalf("expected adapter_disabled reason, got %s", reason)
	}
}

func TestEnableAdapterReallows(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirectiveWithTarget(executiondomain.CommandDisableAdapter, "sim-adapter"))
	cp.Apply(validDirectiveWithTarget(executiondomain.CommandEnableAdapter, "sim-adapter"))
	snap := cp.Snapshot()
	allowed, _ := snap.IsExecutionAllowed("strat-1", "sim-adapter", "binance", "BTCUSDT")
	if !allowed {
		t.Fatal("expected enabled adapter to allow execution")
	}
}

func TestAllowlistOverrideRestrictsVenues(t *testing.T) {
	cp := NewInMemoryControlPlane()
	d := validDirective(executiondomain.CommandUpdateAllowlist)
	d.Parameters = map[string]string{
		"venues": "binance,bybit",
	}
	cp.Apply(d)

	snap := cp.Snapshot()

	// Allowed venue
	allowed, _ := snap.IsExecutionAllowed("s1", "a1", "binance", "BTCUSDT")
	if !allowed {
		t.Fatal("expected binance to be allowed")
	}

	// Blocked venue
	allowed, reason := snap.IsExecutionAllowed("s1", "a1", "kraken", "BTCUSDT")
	if allowed {
		t.Fatal("expected kraken to be restricted")
	}
	if reason != executiondomain.ReasonControlPlaneVenueRestricted {
		t.Fatalf("expected venue_restricted reason, got %s", reason)
	}
}

func TestAllowlistOverrideRestrictsSymbols(t *testing.T) {
	cp := NewInMemoryControlPlane()
	d := validDirective(executiondomain.CommandUpdateAllowlist)
	d.Parameters = map[string]string{
		"symbols": "BTCUSDT,ETHUSDT",
	}
	cp.Apply(d)

	snap := cp.Snapshot()

	allowed, _ := snap.IsExecutionAllowed("s1", "a1", "binance", "BTCUSDT")
	if !allowed {
		t.Fatal("expected BTCUSDT to be allowed")
	}

	allowed, reason := snap.IsExecutionAllowed("s1", "a1", "binance", "SOLUSDT")
	if allowed {
		t.Fatal("expected SOLUSDT to be restricted")
	}
	if reason != executiondomain.ReasonControlPlaneSymbolRestricted {
		t.Fatalf("expected symbol_restricted reason, got %s", reason)
	}
}

func TestInvalidDirectiveEmptyCommand(t *testing.T) {
	d := executiondomain.ControlDirective{
		Command:    "",
		IssuedAtMs: 1000,
		Issuer:     "op",
	}
	cp := NewInMemoryControlPlane()
	p := cp.Apply(d)
	if p == nil {
		t.Fatal("expected problem for empty command")
	}
}

func TestInvalidDirectiveEmptyIssuer(t *testing.T) {
	d := executiondomain.ControlDirective{
		Command:    executiondomain.CommandPause,
		IssuedAtMs: 1000,
		Issuer:     "",
	}
	if p := d.Validate(); p == nil {
		t.Fatal("expected problem for empty issuer")
	}
}

func TestInvalidDirectiveZeroTimestamp(t *testing.T) {
	d := executiondomain.ControlDirective{
		Command:    executiondomain.CommandPause,
		IssuedAtMs: 0,
		Issuer:     "op",
	}
	if p := d.Validate(); p == nil {
		t.Fatal("expected problem for zero timestamp")
	}
}

func TestSnapshotIsImmutableCopy(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirectiveWithTarget(executiondomain.CommandDisableStrategy, "strat-1"))

	snap := cp.Snapshot()
	// Mutate the returned snapshot
	snap.DisabledStrategies["strat-injected"] = struct{}{}
	snap.State = executiondomain.ControlStateHalted

	// Original must be unaffected
	snap2 := cp.Snapshot()
	if snap2.State != executiondomain.ControlStateActive {
		t.Fatal("snapshot mutation leaked to control plane state")
	}
	if _, injected := snap2.DisabledStrategies["strat-injected"]; injected {
		t.Fatal("snapshot mutation leaked to control plane disabled strategies")
	}
}

func TestSnapshotAllowlistImmutableCopy(t *testing.T) {
	cp := NewInMemoryControlPlane()
	d := validDirective(executiondomain.CommandUpdateAllowlist)
	d.Parameters = map[string]string{"venues": "binance"}
	cp.Apply(d)

	snap := cp.Snapshot()
	if snap.AllowlistOverrides == nil {
		t.Fatal("expected allowlist overrides")
	}
	snap.AllowlistOverrides.RestrictVenues["injected"] = struct{}{}

	snap2 := cp.Snapshot()
	if _, injected := snap2.AllowlistOverrides.RestrictVenues["injected"]; injected {
		t.Fatal("allowlist mutation leaked to control plane")
	}
}

func TestConcurrentApplyAndSnapshot(t *testing.T) {
	cp := NewInMemoryControlPlane()
	var wg sync.WaitGroup
	const n = 100

	// Writers: disable strategies concurrently
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d := validDirectiveWithTarget(executiondomain.CommandDisableStrategy, "strat-concurrent")
			d.IssuedAtMs = int64(1000 + idx)
			cp.Apply(d)
		}(i)
	}

	// Readers: snapshot concurrently
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap := cp.Snapshot()
			// Just exercise IsExecutionAllowed to detect races
			snap.IsExecutionAllowed("strat-concurrent", "adapter", "binance", "BTCUSDT")
		}()
	}

	wg.Wait()

	// Final state should have the strategy disabled
	snap := cp.Snapshot()
	if _, ok := snap.DisabledStrategies["strat-concurrent"]; !ok {
		t.Fatal("expected strat-concurrent to be disabled after concurrent writes")
	}
}

func TestSetSimulationProfile(t *testing.T) {
	cp := NewInMemoryControlPlane()

	// Set profile
	d := validDirective(executiondomain.CommandSetSimProfile)
	d.TargetID = "aggressive-fill"
	cp.Apply(d)

	snap := cp.Snapshot()
	if snap.SimulationProfile != "aggressive-fill" {
		t.Fatalf("expected aggressive-fill, got %s", snap.SimulationProfile)
	}

	// Reset to default (empty)
	d2 := validDirective(executiondomain.CommandSetSimProfile)
	d2.TargetID = ""
	d2.IssuedAtMs = 2000
	cp.Apply(d2)

	snap2 := cp.Snapshot()
	if snap2.SimulationProfile != "" {
		t.Fatalf("expected empty profile after reset, got %s", snap2.SimulationProfile)
	}
}

func TestPauseFromPausedFails(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirective(executiondomain.CommandPause))
	p := cp.Apply(validDirective(executiondomain.CommandPause))
	if p == nil {
		t.Fatal("expected problem when pausing from paused")
	}
}

func TestDrainFromDrainedFails(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirective(executiondomain.CommandDrain))
	p := cp.Apply(validDirective(executiondomain.CommandDrain))
	if p == nil {
		t.Fatal("expected problem when draining from drained")
	}
}

func TestPausedStateBlocksExecution(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirective(executiondomain.CommandPause))
	snap := cp.Snapshot()
	allowed, reason := snap.IsExecutionAllowed("s1", "a1", "binance", "BTCUSDT")
	if allowed {
		t.Fatal("expected paused to block execution")
	}
	if reason != executiondomain.ReasonControlPlanePaused {
		t.Fatalf("expected paused reason, got %s", reason)
	}
}

func TestDrainedStateBlocksExecution(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirective(executiondomain.CommandDrain))
	snap := cp.Snapshot()
	allowed, reason := snap.IsExecutionAllowed("s1", "a1", "binance", "BTCUSDT")
	if allowed {
		t.Fatal("expected drained to block execution")
	}
	if reason != executiondomain.ReasonControlPlaneDrained {
		t.Fatalf("expected drained reason, got %s", reason)
	}
}

func TestHaltedStateBlocksExecution(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirective(executiondomain.CommandHalt))
	snap := cp.Snapshot()
	allowed, reason := snap.IsExecutionAllowed("s1", "a1", "binance", "BTCUSDT")
	if allowed {
		t.Fatal("expected halted to block execution")
	}
	if reason != executiondomain.ReasonControlPlaneHalted {
		t.Fatalf("expected halted reason, got %s", reason)
	}
}

func TestLastDirectiveRecorded(t *testing.T) {
	cp := NewInMemoryControlPlane()
	d := validDirective(executiondomain.CommandPause)
	d.Reason = "maintenance window"
	cp.Apply(d)

	snap := cp.Snapshot()
	if snap.LastDirective.Reason != "maintenance window" {
		t.Fatalf("expected last directive reason to be recorded, got %s", snap.LastDirective.Reason)
	}
	if snap.UpdatedAtMs != d.IssuedAtMs {
		t.Fatalf("expected updated_at_ms=%d, got %d", d.IssuedAtMs, snap.UpdatedAtMs)
	}
}

func TestUnknownCommandRejected(t *testing.T) {
	d := executiondomain.ControlDirective{
		Command:    "unknown_cmd",
		IssuedAtMs: 1000,
		Issuer:     "op",
	}
	cp := NewInMemoryControlPlane()
	p := cp.Apply(d)
	if p == nil {
		t.Fatal("expected problem for unknown command")
	}
}

func TestDisableStrategyRequiresTargetID(t *testing.T) {
	d := executiondomain.ControlDirective{
		Command:    executiondomain.CommandDisableStrategy,
		TargetID:   "",
		IssuedAtMs: 1000,
		Issuer:     "op",
	}
	if p := d.Validate(); p == nil {
		t.Fatal("expected problem for disable_strategy with empty target_id")
	}
}

func TestReasonCategoryControlPlane(t *testing.T) {
	reasons := []string{
		executiondomain.ReasonControlPlanePaused,
		executiondomain.ReasonControlPlaneDrained,
		executiondomain.ReasonControlPlaneHalted,
		executiondomain.ReasonControlPlaneStrategyDisabled,
		executiondomain.ReasonControlPlaneAdapterDisabled,
		executiondomain.ReasonControlPlaneVenueRestricted,
		executiondomain.ReasonControlPlaneSymbolRestricted,
	}
	for _, r := range reasons {
		cat := executiondomain.ReasonCategory(r)
		if cat != executiondomain.ReasonCategoryControlPlane {
			t.Fatalf("expected category control_plane for %s, got %s", r, cat)
		}
	}
}

func TestActiveStateAllowsExecution(t *testing.T) {
	cp := NewInMemoryControlPlane()
	snap := cp.Snapshot()
	allowed, reason := snap.IsExecutionAllowed("s1", "a1", "binance", "BTCUSDT")
	if !allowed {
		t.Fatalf("expected active to allow execution, reason: %s", reason)
	}
	if reason != "" {
		t.Fatalf("expected empty reason for allowed, got %s", reason)
	}
}

func TestClearAllowlistOverrides(t *testing.T) {
	cp := NewInMemoryControlPlane()

	// Set overrides
	d := validDirective(executiondomain.CommandUpdateAllowlist)
	d.Parameters = map[string]string{"venues": "binance"}
	cp.Apply(d)

	// Clear by sending empty params
	d2 := validDirective(executiondomain.CommandUpdateAllowlist)
	d2.Parameters = map[string]string{}
	d2.IssuedAtMs = 2000
	cp.Apply(d2)

	snap := cp.Snapshot()
	if snap.AllowlistOverrides != nil {
		t.Fatal("expected nil allowlist overrides after clearing")
	}

	// All venues should be allowed now
	allowed, _ := snap.IsExecutionAllowed("s1", "a1", "kraken", "BTCUSDT")
	if !allowed {
		t.Fatal("expected all venues allowed after clearing overrides")
	}
}

func TestDirectiveHistoryRecorded(t *testing.T) {
	cp := NewInMemoryControlPlane()

	d1 := validDirective(executiondomain.CommandPause)
	d1.Reason = "first"
	cp.Apply(d1)

	d2 := validDirective(executiondomain.CommandResume)
	d2.Reason = "second"
	d2.IssuedAtMs = 2000
	cp.Apply(d2)

	d3 := validDirectiveWithTarget(executiondomain.CommandDisableStrategy, "strat-1")
	d3.Reason = "third"
	d3.IssuedAtMs = 3000
	cp.Apply(d3)

	snap := cp.Snapshot()
	if len(snap.DirectiveHistory) != 3 {
		t.Fatalf("expected 3 directives in history, got %d", len(snap.DirectiveHistory))
	}
	if snap.DirectiveHistory[0].Reason != "first" {
		t.Fatalf("expected first directive reason 'first', got %s", snap.DirectiveHistory[0].Reason)
	}
	if snap.DirectiveHistory[1].Reason != "second" {
		t.Fatalf("expected second directive reason 'second', got %s", snap.DirectiveHistory[1].Reason)
	}
	if snap.DirectiveHistory[2].Reason != "third" {
		t.Fatalf("expected third directive reason 'third', got %s", snap.DirectiveHistory[2].Reason)
	}
}

func TestDirectiveHistoryCappedAt32(t *testing.T) {
	cp := NewInMemoryControlPlane()

	for i := 0; i < 40; i++ {
		d := validDirectiveWithTarget(executiondomain.CommandDisableStrategy, "strat-1")
		d.IssuedAtMs = int64(1000 + i)
		d.Reason = "directive"
		cp.Apply(d)
	}

	snap := cp.Snapshot()
	if len(snap.DirectiveHistory) != 32 {
		t.Fatalf("expected 32 directives in history, got %d", len(snap.DirectiveHistory))
	}
	// Oldest retained should be directive #8 (0-indexed), issued at 1008
	if snap.DirectiveHistory[0].IssuedAtMs != 1008 {
		t.Fatalf("expected oldest retained directive issued_at_ms=1008, got %d", snap.DirectiveHistory[0].IssuedAtMs)
	}
	// Newest should be directive #39, issued at 1039
	if snap.DirectiveHistory[31].IssuedAtMs != 1039 {
		t.Fatalf("expected newest directive issued_at_ms=1039, got %d", snap.DirectiveHistory[31].IssuedAtMs)
	}
}

func TestDirectiveHistorySnapshotImmutable(t *testing.T) {
	cp := NewInMemoryControlPlane()

	d := validDirective(executiondomain.CommandPause)
	d.Reason = "original"
	cp.Apply(d)

	snap := cp.Snapshot()
	if len(snap.DirectiveHistory) != 1 {
		t.Fatalf("expected 1 directive in history, got %d", len(snap.DirectiveHistory))
	}

	// Mutate the returned history slice
	snap.DirectiveHistory[0].Reason = "mutated"
	snap.DirectiveHistory = append(snap.DirectiveHistory, executiondomain.ControlDirective{
		Reason: "injected",
	})

	// Original must be unaffected
	snap2 := cp.Snapshot()
	if len(snap2.DirectiveHistory) != 1 {
		t.Fatalf("expected 1 directive in history after mutation, got %d", len(snap2.DirectiveHistory))
	}
	if snap2.DirectiveHistory[0].Reason != "original" {
		t.Fatalf("expected original reason preserved, got %s", snap2.DirectiveHistory[0].Reason)
	}
}

func TestAdapterIDNormalizedToLowercase(t *testing.T) {
	cp := NewInMemoryControlPlane()
	cp.Apply(validDirectiveWithTarget(executiondomain.CommandDisableAdapter, "SIM-Adapter"))

	snap := cp.Snapshot()
	// Check with different casing - should still block because IsExecutionAllowed lowercases
	allowed, _ := snap.IsExecutionAllowed("s1", "sim-adapter", "binance", "BTCUSDT")
	if allowed {
		t.Fatal("expected adapter disabled with case-insensitive match")
	}

	// Enable with different casing
	cp.Apply(validDirectiveWithTarget(executiondomain.CommandEnableAdapter, "SIM-ADAPTER"))
	snap2 := cp.Snapshot()
	allowed2, _ := snap2.IsExecutionAllowed("s1", "sim-adapter", "binance", "BTCUSDT")
	if !allowed2 {
		t.Fatal("expected adapter enabled after case-insensitive enable")
	}
}
