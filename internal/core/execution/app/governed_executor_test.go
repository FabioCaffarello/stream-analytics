package app

import (
	"strconv"
	"testing"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	executiongovernance "github.com/market-raccoon/internal/core/execution/governance"
	executionports "github.com/market-raccoon/internal/core/execution/ports"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

type fakeGovernedAdapter struct {
	events []executiondomain.ExecutionEventV1
	calls  int
}

func (f *fakeGovernedAdapter) ExecuteAt(_ strategydomain.StrategyIntentV1, _ int64) []executiondomain.ExecutionEventV1 {
	f.calls++
	return f.events
}

func (f *fakeGovernedAdapter) BoundaryInfo() executionports.BoundaryInfo {
	return executionports.BoundaryInfo{Boundary: "execution.adapter", Adapter: "fake.adapter", Mode: "real_adapter_safe"}
}

type fakeCredentialResolver struct {
	result executiongovernance.CredentialResolution
}

func (f fakeCredentialResolver) ResolveCredential(requirement executiongovernance.CredentialRequirement, observedAtMs int64) executiongovernance.CredentialResolution {
	result := f.result
	result.Requirement = requirement
	if result.EvaluatedAtMs == 0 {
		result.EvaluatedAtMs = observedAtMs
	}
	if result.Status == executiongovernance.CredentialResolutionResolved {
		result.Availability = executiongovernance.CredentialAvailabilityAvailable
		result.Credential = resolvedCredentialFixture(requirement, observedAtMs)
	}
	return result
}

func TestStaticCapabilityAuthorizer_DenyByDefaultWithoutGrant(t *testing.T) {
	decision := StaticCapabilityAuthorizer{}.Authorize(validIntentFixture(), 1_700_000_002_000)
	if decision.Authorized {
		t.Fatal("expected deny-by-default without grant")
	}
	if decision.Reason != executiondomain.ReasonGovernanceNoGrant {
		t.Fatalf("reason=%q want=%q", decision.Reason, executiondomain.ReasonGovernanceNoGrant)
	}
}

func TestStaticCapabilityAuthorizer_EnforcesScopeAndLimit(t *testing.T) {
	intent := validIntentFixture()
	grant := executiongovernance.ExecutionGrant{
		GrantID:   "grant-1",
		Boundary:  "execution.adapter",
		AdapterID: "binance.spot",
		Mode:      "real_adapter_safe",
		SafeMode:  true,
		TradeOnly: true,
		Scope: executiongovernance.ExecutionScope{
			AllowedVenues:   map[string]struct{}{"binance": {}},
			AllowedSymbols:  map[string]struct{}{"BTCUSDT": {}},
			AllowedAccounts: map[string]struct{}{"paper": {}},
		},
		Limits: executiongovernance.ExecutionLimits{
			MaxIntentTTLms: 120_000,
			MaxAbsQuantity: 1,
		},
	}

	decision := StaticCapabilityAuthorizer{Grant: &grant}.Authorize(intent, 1_700_000_002_000)
	if decision.Authorized {
		t.Fatal("expected size above grant limit to be denied")
	}
	if decision.Reason != executiondomain.ReasonGovernanceSizeTooLarge {
		t.Fatalf("reason=%q want=%q", decision.Reason, executiondomain.ReasonGovernanceSizeTooLarge)
	}

	intent.Scope.Symbol = "ETHUSDT"
	intent.Sizing.Value = 0.5
	decision = StaticCapabilityAuthorizer{Grant: &grant}.Authorize(intent, 1_700_000_002_000)
	if decision.Reason != executiondomain.ReasonGovernanceSymbolNotAllowed {
		t.Fatalf("reason=%q want=%q", decision.Reason, executiondomain.ReasonGovernanceSymbolNotAllowed)
	}
}

func TestGovernedExecutor_UsesAuthorizedAdapter(t *testing.T) {
	intent := validIntentFixture()
	grant := governedGrantFixture()
	adapter := &fakeGovernedAdapter{events: []executiondomain.ExecutionEventV1{{
		EventID:      "accepted-1",
		Status:       executiondomain.ExecutionStatusAccepted,
		Correlation:  executiondomain.ExecutionCorrelation{IntentID: intent.IntentID, OrderID: "order-1", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:    1_700_000_002_000,
		ExecutionSeq: 1,
		Attempt:      1,
		RequestedQty: 1.5,
		LeavesQty:    1.5,
		Reason:       "accepted_real_adapter_test_order",
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: intent.Provenance.CorrelationID,
			Source:        "fake",
		},
	}}}

	governance := NewStaticExecutionGovernance(StaticExecutionGovernanceConfig{
		Authorizer: StaticCapabilityAuthorizer{Grant: &grant},
		Selector: NewStaticAdapterSelector(AdapterRoute{
			Boundary:              "execution.adapter",
			AdapterID:             "binance.spot",
			Mode:                  "real_adapter_safe",
			CredentialRequirement: credentialRequirementFixture(),
		}),
		CredentialResolver: fakeCredentialResolver{result: executiongovernance.CredentialResolution{
			Status: executiongovernance.CredentialResolutionResolved,
		}},
		BoundaryInfo: executionports.BoundaryInfo{
			Boundary: "execution.adapter",
			Adapter:  "binance.spot",
			Mode:     "real_adapter_safe",
		},
	})

	executor := NewGovernedExecutor(GovernedExecutorConfig{
		Governance: governance,
		Adapters: map[string]executionports.IntentExecutor{
			"binance.spot": adapter,
		},
	})
	events := executor.ExecuteAt(intent, 1_700_000_002_000)
	if len(events) != 1 {
		t.Fatalf("events=%d want=1", len(events))
	}
	if adapter.calls != 1 {
		t.Fatalf("adapter calls=%d want=1", adapter.calls)
	}
}

func TestGovernedExecutor_SeparatesCredentialAndAdapterFailures(t *testing.T) {
	intent := validIntentFixture()
	grant := governedGrantFixture()

	selector := NewStaticAdapterSelector(AdapterRoute{
		Boundary:              "execution.adapter",
		AdapterID:             "binance.spot",
		Mode:                  "real_adapter_safe",
		CredentialRequirement: credentialRequirementFixture(),
	})
	governance := NewStaticExecutionGovernance(StaticExecutionGovernanceConfig{
		Authorizer: StaticCapabilityAuthorizer{Grant: &grant},
		Selector:   selector,
		CredentialResolver: fakeCredentialResolver{result: executiongovernance.CredentialResolution{
			Availability: executiongovernance.CredentialAvailabilityUnavailable,
			Status:       executiongovernance.CredentialResolutionDenied,
			Reason:       executiondomain.ReasonCredentialsUnavailableMaterialMissing,
		}},
		BoundaryInfo: executionports.BoundaryInfo{
			Boundary: "execution.adapter",
			Adapter:  "binance.spot",
			Mode:     "real_adapter_safe",
		},
	})

	executor := NewGovernedExecutor(GovernedExecutorConfig{
		Governance: governance,
		Adapters:   map[string]executionports.IntentExecutor{},
	})
	events := executor.ExecuteAt(intent, 1_700_000_002_000)
	if got := events[0].Reason; got != executiondomain.ReasonCredentialsUnavailableMaterialMissing {
		t.Fatalf("credential reason=%q want=%q", got, executiondomain.ReasonCredentialsUnavailableMaterialMissing)
	}
	if got := executiondomain.ReasonCategory(events[0].Reason); got != executiondomain.ReasonCategoryCredentialsUnavailable {
		t.Fatalf("credential category=%q want=%q", got, executiondomain.ReasonCategoryCredentialsUnavailable)
	}

	governance = NewStaticExecutionGovernance(StaticExecutionGovernanceConfig{
		Authorizer: StaticCapabilityAuthorizer{Grant: &grant},
		Selector:   selector,
		CredentialResolver: fakeCredentialResolver{result: executiongovernance.CredentialResolution{
			Status: executiongovernance.CredentialResolutionResolved,
		}},
		BoundaryInfo: executionports.BoundaryInfo{
			Boundary: "execution.adapter",
			Adapter:  "binance.spot",
			Mode:     "real_adapter_safe",
		},
	})
	executor = NewGovernedExecutor(GovernedExecutorConfig{
		Governance: governance,
		Adapters:   map[string]executionports.IntentExecutor{},
	})
	events = executor.ExecuteAt(intent, 1_700_000_002_000)
	if got := events[0].Reason; got != executiondomain.ReasonAdapterSelectionUnavailable {
		t.Fatalf("adapter reason=%q want=%q", got, executiondomain.ReasonAdapterSelectionUnavailable)
	}
	if got := executiondomain.ReasonCategory(events[0].Reason); got != executiondomain.ReasonCategoryAdapterSelectionDenied {
		t.Fatalf("adapter category=%q want=%q", got, executiondomain.ReasonCategoryAdapterSelectionDenied)
	}
}

func TestGovernedExecutor_RejectsExpiredCredentialLease(t *testing.T) {
	intent := validIntentFixture()
	grant := governedGrantFixture()

	governance := NewStaticExecutionGovernance(StaticExecutionGovernanceConfig{
		Authorizer: StaticCapabilityAuthorizer{Grant: &grant},
		Selector: NewStaticAdapterSelector(AdapterRoute{
			Boundary:              "execution.adapter",
			AdapterID:             "binance.spot",
			Mode:                  "real_adapter_safe",
			CredentialRequirement: credentialRequirementFixture(),
		}),
		CredentialResolver: fakeCredentialResolver{result: executiongovernance.CredentialResolution{
			Availability: executiongovernance.CredentialAvailabilityAvailable,
			Status:       executiongovernance.CredentialResolutionDenied,
			Reason:       executiondomain.ReasonCredentialsLeaseExpired,
			Credential: executiongovernance.ResolvedCredential{
				Lease: executiongovernance.CredentialLease{State: executiongovernance.CredentialLeaseStateExpired},
			},
		}},
		BoundaryInfo: executionports.BoundaryInfo{
			Boundary: "execution.adapter",
			Adapter:  "binance.spot",
			Mode:     "real_adapter_safe",
		},
	})

	executor := NewGovernedExecutor(GovernedExecutorConfig{Governance: governance})
	events := executor.ExecuteAt(intent, 1_700_000_002_000)
	if got := events[0].Reason; got != executiondomain.ReasonCredentialsLeaseExpired {
		t.Fatalf("reason=%q want=%q", got, executiondomain.ReasonCredentialsLeaseExpired)
	}
	if got := executiondomain.ReasonCategory(events[0].Reason); got != executiondomain.ReasonCategoryCredentialsLeaseDenied {
		t.Fatalf("category=%q want=%q", got, executiondomain.ReasonCategoryCredentialsLeaseDenied)
	}
}

func governedGrantFixture() executiongovernance.ExecutionGrant {
	return executiongovernance.ExecutionGrant{
		GrantID:   "grant-1",
		Boundary:  "execution.adapter",
		AdapterID: "binance.spot",
		Mode:      "real_adapter_safe",
		SafeMode:  true,
		TradeOnly: true,
		Scope: executiongovernance.ExecutionScope{
			AllowedVenues:   map[string]struct{}{"binance": {}},
			AllowedSymbols:  map[string]struct{}{"BTCUSDT": {}},
			AllowedAccounts: map[string]struct{}{"paper": {}},
		},
		Limits: executiongovernance.ExecutionLimits{
			MaxIntentTTLms: 120_000,
			MaxAbsQuantity: 5,
			MaxNotionalUSD: 500,
			MaxSlippageBps: 50,
		},
	}
}

func credentialRequirementFixture() executiongovernance.CredentialRequirement {
	return executiongovernance.CredentialRequirement{
		Required:            true,
		Boundary:            "execution.adapter",
		AdapterID:           "binance.spot",
		Mode:                "real_adapter_safe",
		Scope:               "trade_only",
		TradeOnly:           true,
		AcceptedResolverIDs: []string{"credentials.trade_broker.v1"},
		AcceptedProviderIDs: []string{"credentials.provider.env.trade_static"},
	}
}

func resolvedCredentialFixture(requirement executiongovernance.CredentialRequirement, observedAtMs int64) executiongovernance.ResolvedCredential {
	return executiongovernance.ResolvedCredential{
		Boundary:  requirement.Boundary,
		AdapterID: requirement.AdapterID,
		Mode:      requirement.Mode,
		Scope:     requirement.Scope,
		TradeOnly: true,
		Venue:     requirement.Venue,
		AccountID: requirement.AccountID,
		Symbol:    requirement.Symbol,
		Lease: executiongovernance.CredentialLease{
			LeaseID:      "lease-1",
			State:        executiongovernance.CredentialLeaseStateActive,
			IssuedAtMs:   observedAtMs,
			ValidUntilMs: observedAtMs + 30_000,
		},
		Provenance: executiongovernance.CredentialProvenance{
			ResolverID: "credentials.trade_broker.v1",
			ProviderID: "credentials.provider.env.trade_static",
		},
	}
}

// --- Control Plane test helpers ---

type fakeControlPlane struct {
	snapshot executiondomain.ControlSnapshot
}

func (f *fakeControlPlane) Snapshot() executiondomain.ControlSnapshot {
	return f.snapshot
}

func (f *fakeControlPlane) Apply(_ executiondomain.ControlDirective) *problem.Problem {
	return nil
}

func governedExecutorWithControlPlane(cp executionports.ControlPlane) (*GovernedExecutor, *fakeGovernedAdapter) {
	intent := validIntentFixture()
	grant := governedGrantFixture()
	adapter := &fakeGovernedAdapter{events: []executiondomain.ExecutionEventV1{{
		EventID:      "accepted-cp",
		Status:       executiondomain.ExecutionStatusAccepted,
		Correlation:  executiondomain.ExecutionCorrelation{IntentID: intent.IntentID, OrderID: "order-cp", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:    1_700_000_002_000,
		ExecutionSeq: 1,
		Attempt:      1,
		RequestedQty: 1.5,
		LeavesQty:    1.5,
		Reason:       "accepted_control_plane_test",
		Provenance:   executiondomain.ExecutionProvenance{CorrelationID: intent.Provenance.CorrelationID, Source: "fake"},
	}}}

	governance := NewStaticExecutionGovernance(StaticExecutionGovernanceConfig{
		Authorizer: StaticCapabilityAuthorizer{Grant: &grant},
		Selector: NewStaticAdapterSelector(AdapterRoute{
			Boundary:              "execution.adapter",
			AdapterID:             "binance.spot",
			Mode:                  "real_adapter_safe",
			CredentialRequirement: credentialRequirementFixture(),
		}),
		CredentialResolver: fakeCredentialResolver{result: executiongovernance.CredentialResolution{
			Status: executiongovernance.CredentialResolutionResolved,
		}},
		BoundaryInfo: executionports.BoundaryInfo{
			Boundary: "execution.adapter",
			Adapter:  "binance.spot",
			Mode:     "real_adapter_safe",
		},
	})

	executor := NewGovernedExecutor(GovernedExecutorConfig{
		Governance: governance,
		Adapters: map[string]executionports.IntentExecutor{
			"binance.spot": adapter,
		},
		ControlPlane: cp,
	})
	return executor, adapter
}

func TestGovernedExecutor_NilControlPlane_BehaviorUnchanged(t *testing.T) {
	executor, adapter := governedExecutorWithControlPlane(nil)
	events := executor.ExecuteAt(validIntentFixture(), 1_700_000_002_000)
	if len(events) != 1 {
		t.Fatalf("events=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusAccepted {
		t.Fatalf("status=%q want=%q", events[0].Status, executiondomain.ExecutionStatusAccepted)
	}
	if adapter.calls != 1 {
		t.Fatalf("adapter calls=%d want=1", adapter.calls)
	}
}

func TestGovernedExecutor_ControlPlaneHalted_RejectsIntent(t *testing.T) {
	cp := &fakeControlPlane{snapshot: executiondomain.ControlSnapshot{
		State:       executiondomain.ControlStateHalted,
		UpdatedAtMs: 1_700_000_001_000,
	}}
	executor, adapter := governedExecutorWithControlPlane(cp)
	events := executor.ExecuteAt(validIntentFixture(), 1_700_000_002_000)
	if len(events) != 1 {
		t.Fatalf("events=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusRejected {
		t.Fatalf("status=%q want=%q", events[0].Status, executiondomain.ExecutionStatusRejected)
	}
	if events[0].Reason != executiondomain.ReasonControlPlaneHalted {
		t.Fatalf("reason=%q want=%q", events[0].Reason, executiondomain.ReasonControlPlaneHalted)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter should not be called when control plane halts, calls=%d", adapter.calls)
	}
}

func TestGovernedExecutor_ControlPlanePaused_RejectsIntent(t *testing.T) {
	cp := &fakeControlPlane{snapshot: executiondomain.ControlSnapshot{
		State:       executiondomain.ControlStatePaused,
		UpdatedAtMs: 1_700_000_001_000,
	}}
	executor, adapter := governedExecutorWithControlPlane(cp)
	events := executor.ExecuteAt(validIntentFixture(), 1_700_000_002_000)
	if len(events) != 1 {
		t.Fatalf("events=%d want=1", len(events))
	}
	if events[0].Reason != executiondomain.ReasonControlPlanePaused {
		t.Fatalf("reason=%q want=%q", events[0].Reason, executiondomain.ReasonControlPlanePaused)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter should not be called when control plane paused, calls=%d", adapter.calls)
	}
}

func TestGovernedExecutor_ControlPlaneDisablesStrategy_RejectsIntent(t *testing.T) {
	intent := validIntentFixture()
	cp := &fakeControlPlane{snapshot: executiondomain.ControlSnapshot{
		State:              executiondomain.ControlStateActive,
		DisabledStrategies: map[string]struct{}{intent.Strategy.StrategyID: {}},
		UpdatedAtMs:        1_700_000_001_000,
	}}
	executor, adapter := governedExecutorWithControlPlane(cp)
	events := executor.ExecuteAt(intent, 1_700_000_002_000)
	if len(events) != 1 {
		t.Fatalf("events=%d want=1", len(events))
	}
	if events[0].Reason != executiondomain.ReasonControlPlaneStrategyDisabled {
		t.Fatalf("reason=%q want=%q", events[0].Reason, executiondomain.ReasonControlPlaneStrategyDisabled)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter should not be called when strategy disabled, calls=%d", adapter.calls)
	}
}

func TestGovernedExecutor_IdempotencyRejectsDuplicateIntent(t *testing.T) {
	executor, adapter := governedExecutorWithControlPlane(nil)
	intent := validIntentFixture()

	// First call: should succeed and dispatch to adapter.
	events := executor.ExecuteAt(intent, 1_700_000_002_000)
	if len(events) != 1 {
		t.Fatalf("first call: events=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusAccepted {
		t.Fatalf("first call: status=%q want=%q", events[0].Status, executiondomain.ExecutionStatusAccepted)
	}
	if adapter.calls != 1 {
		t.Fatalf("first call: adapter calls=%d want=1", adapter.calls)
	}

	// Second call with same intentID: should be rejected as duplicate.
	events = executor.ExecuteAt(intent, 1_700_000_003_000)
	if len(events) != 1 {
		t.Fatalf("duplicate call: events=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusRejected {
		t.Fatalf("duplicate call: status=%q want=%q", events[0].Status, executiondomain.ExecutionStatusRejected)
	}
	if events[0].Reason != executiondomain.ReasonDuplicateIntent {
		t.Fatalf("duplicate call: reason=%q want=%q", events[0].Reason, executiondomain.ReasonDuplicateIntent)
	}
	if adapter.calls != 1 {
		t.Fatalf("duplicate call: adapter should not be called again, calls=%d", adapter.calls)
	}

	// Verify decision record reflects the duplicate rejection.
	rec := executor.LastDecisionRecord()
	if rec.FinalDecision != "rejected" {
		t.Fatalf("decision record: final_decision=%q want=%q", rec.FinalDecision, "rejected")
	}
	if rec.FinalReason != executiondomain.ReasonDuplicateIntent {
		t.Fatalf("decision record: final_reason=%q want=%q", rec.FinalReason, executiondomain.ReasonDuplicateIntent)
	}

	// Third call with different intentID: should succeed.
	intent2 := validIntentFixture()
	intent2.IntentID = "intent-fixture-2"
	events = executor.ExecuteAt(intent2, 1_700_000_004_000)
	if len(events) != 1 {
		t.Fatalf("different intent: events=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusAccepted {
		t.Fatalf("different intent: status=%q want=%q", events[0].Status, executiondomain.ExecutionStatusAccepted)
	}
	if adapter.calls != 2 {
		t.Fatalf("different intent: adapter calls=%d want=2", adapter.calls)
	}
}

func TestGovernedExecutor_ControlPlaneActive_FlowsThrough(t *testing.T) {
	cp := &fakeControlPlane{snapshot: executiondomain.ControlSnapshot{
		State:       executiondomain.ControlStateActive,
		UpdatedAtMs: 1_700_000_001_000,
	}}
	executor, adapter := governedExecutorWithControlPlane(cp)
	events := executor.ExecuteAt(validIntentFixture(), 1_700_000_002_000)
	if len(events) != 1 {
		t.Fatalf("events=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusAccepted {
		t.Fatalf("status=%q want=%q", events[0].Status, executiondomain.ExecutionStatusAccepted)
	}
	if adapter.calls != 1 {
		t.Fatalf("adapter calls=%d want=1", adapter.calls)
	}
}

// --- Circuit Breaker (Adapter Health) tests ---

func governedExecutorWithFailingAdapter() (*GovernedExecutor, *fakeGovernedAdapter) {
	intent := validIntentFixture()
	grant := governedGrantFixture()
	adapter := &fakeGovernedAdapter{events: []executiondomain.ExecutionEventV1{{
		EventID:      "failed-1",
		Status:       executiondomain.ExecutionStatusFailed,
		Correlation:  executiondomain.ExecutionCorrelation{IntentID: intent.IntentID, OrderID: "order-fail", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:    1_700_000_002_000,
		ExecutionSeq: 1,
		Attempt:      1,
		RequestedQty: 1.5,
		LeavesQty:    1.5,
		Reason:       "venue_runtime_failed_adapter_call",
		Provenance:   executiondomain.ExecutionProvenance{CorrelationID: intent.Provenance.CorrelationID, Source: "fake"},
	}}}

	governance := NewStaticExecutionGovernance(StaticExecutionGovernanceConfig{
		Authorizer: StaticCapabilityAuthorizer{Grant: &grant},
		Selector: NewStaticAdapterSelector(AdapterRoute{
			Boundary:              "execution.adapter",
			AdapterID:             "binance.spot",
			Mode:                  "real_adapter_safe",
			CredentialRequirement: credentialRequirementFixture(),
		}),
		CredentialResolver: fakeCredentialResolver{result: executiongovernance.CredentialResolution{
			Status: executiongovernance.CredentialResolutionResolved,
		}},
		BoundaryInfo: executionports.BoundaryInfo{
			Boundary: "execution.adapter",
			Adapter:  "binance.spot",
			Mode:     "real_adapter_safe",
		},
	})

	executor := NewGovernedExecutor(GovernedExecutorConfig{
		Governance: governance,
		Adapters: map[string]executionports.IntentExecutor{
			"binance.spot": adapter,
		},
	})
	return executor, adapter
}

func TestGovernedExecutor_CircuitBreaker_TripsAfterConsecutiveFailures(t *testing.T) {
	executor, adapter := governedExecutorWithFailingAdapter()
	baseMs := int64(1_700_000_002_000)

	// Default threshold is 5. Send 5 intents that all produce "failed" status.
	for i := 0; i < 5; i++ {
		intent := validIntentFixture()
		intent.IntentID = "intent-fail-" + strconv.Itoa(i)
		events := executor.ExecuteAt(intent, baseMs+int64(i))
		if len(events) != 1 {
			t.Fatalf("iteration %d: events=%d want=1", i, len(events))
		}
		if events[0].Status != executiondomain.ExecutionStatusFailed {
			t.Fatalf("iteration %d: status=%q want=%q", i, events[0].Status, executiondomain.ExecutionStatusFailed)
		}
	}
	if adapter.calls != 5 {
		t.Fatalf("adapter calls=%d want=5 (all dispatched before trip)", adapter.calls)
	}

	// 6th intent: circuit should be tripped, rejected without calling adapter.
	intent6 := validIntentFixture()
	intent6.IntentID = "intent-fail-5"
	events := executor.ExecuteAt(intent6, baseMs+5)
	if len(events) != 1 {
		t.Fatalf("tripped call: events=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusRejected {
		t.Fatalf("tripped call: status=%q want=%q", events[0].Status, executiondomain.ExecutionStatusRejected)
	}
	if events[0].Reason != executiondomain.ReasonAdapterSelectionCircuitOpen {
		t.Fatalf("tripped call: reason=%q want=%q", events[0].Reason, executiondomain.ReasonAdapterSelectionCircuitOpen)
	}
	if adapter.calls != 5 {
		t.Fatalf("tripped call: adapter should not be called, calls=%d want=5", adapter.calls)
	}

	// Verify decision record.
	rec := executor.LastDecisionRecord()
	if rec.FinalDecision != "rejected" {
		t.Fatalf("decision record: final_decision=%q want=%q", rec.FinalDecision, "rejected")
	}
	if rec.FinalReason != executiondomain.ReasonAdapterSelectionCircuitOpen {
		t.Fatalf("decision record: final_reason=%q want=%q", rec.FinalReason, executiondomain.ReasonAdapterSelectionCircuitOpen)
	}

	// Verify reason is classified as retryable.
	if !executiondomain.IsRetryable(events[0].Reason) {
		t.Fatal("circuit open reason should be retryable")
	}

	// Verify health snapshot.
	snap := executor.AdapterHealthSnapshots()
	if snap["binance.spot"].ConsecutiveFailures < 5 {
		t.Fatalf("snapshot failures=%d want>=5", snap["binance.spot"].ConsecutiveFailures)
	}
	if snap["binance.spot"].TrippedAtMs == 0 {
		t.Fatal("snapshot tripped_at_ms should be non-zero")
	}
}

func TestGovernedExecutor_CircuitBreaker_CooldownAllowsProbe(t *testing.T) {
	executor, adapter := governedExecutorWithFailingAdapter()
	tripMs := int64(1_700_000_002_000)

	// Trip the circuit: 5 consecutive failures.
	for i := 0; i < 5; i++ {
		intent := validIntentFixture()
		intent.IntentID = "intent-trip-" + strconv.Itoa(i)
		executor.ExecuteAt(intent, tripMs)
	}

	// Confirm tripped.
	trippedIntent := validIntentFixture()
	trippedIntent.IntentID = "intent-tripped"
	events := executor.ExecuteAt(trippedIntent, tripMs+1)
	if events[0].Reason != executiondomain.ReasonAdapterSelectionCircuitOpen {
		t.Fatalf("expected circuit open, got reason=%q", events[0].Reason)
	}

	// After cooldown (30s), circuit should allow a probe call.
	// Switch adapter to return success for the probe.
	adapter.events = []executiondomain.ExecutionEventV1{{
		EventID:      "accepted-probe",
		Status:       executiondomain.ExecutionStatusAccepted,
		Correlation:  executiondomain.ExecutionCorrelation{IntentID: "intent-probe", OrderID: "order-probe", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:    tripMs + 30_001,
		ExecutionSeq: 1,
		Attempt:      1,
		RequestedQty: 1.5,
		LeavesQty:    1.5,
		Reason:       "accepted_probe",
		Provenance:   executiondomain.ExecutionProvenance{CorrelationID: "corr-fixture", Source: "fake"},
	}}

	probeIntent := validIntentFixture()
	probeIntent.IntentID = "intent-probe"
	probeIntent.CreatedAtMs = tripMs + 30_000
	probeIntent.ExpiresAtMs = tripMs + 60_000
	events = executor.ExecuteAt(probeIntent, tripMs+30_001)
	if len(events) != 1 {
		t.Fatalf("probe call: events=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusAccepted {
		t.Fatalf("probe call: status=%q want=%q", events[0].Status, executiondomain.ExecutionStatusAccepted)
	}
	// Adapter should have been called for the probe (5 original + 1 probe = calls incremented).
	if adapter.calls < 6 {
		t.Fatalf("probe call: adapter calls=%d want>=6", adapter.calls)
	}

	// After successful probe, circuit should be fully closed.
	snap := executor.AdapterHealthSnapshots()
	if snap["binance.spot"].TrippedAtMs != 0 {
		t.Fatalf("expected circuit closed after successful probe, tripped_at_ms=%d", snap["binance.spot"].TrippedAtMs)
	}
}

func TestGovernedExecutor_CircuitBreaker_SuccessResetsBeforeTrip(t *testing.T) {
	executor, adapter := governedExecutorWithFailingAdapter()
	baseMs := int64(1_700_000_002_000)

	// 4 failures (one short of threshold).
	for i := 0; i < 4; i++ {
		intent := validIntentFixture()
		intent.IntentID = "intent-prefail-" + strconv.Itoa(i)
		executor.ExecuteAt(intent, baseMs+int64(i))
	}

	// Now switch adapter to return success.
	adapter.events = []executiondomain.ExecutionEventV1{{
		EventID:      "accepted-reset",
		Status:       executiondomain.ExecutionStatusAccepted,
		Correlation:  executiondomain.ExecutionCorrelation{IntentID: "intent-success", OrderID: "order-reset", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:    baseMs + 4,
		ExecutionSeq: 1,
		Attempt:      1,
		RequestedQty: 1.5,
		LeavesQty:    1.5,
		Reason:       "accepted_reset",
		Provenance:   executiondomain.ExecutionProvenance{CorrelationID: "corr-fixture", Source: "fake"},
	}}

	successIntent := validIntentFixture()
	successIntent.IntentID = "intent-success"
	events := executor.ExecuteAt(successIntent, baseMs+4)
	if events[0].Status != executiondomain.ExecutionStatusAccepted {
		t.Fatalf("success call: status=%q want=%q", events[0].Status, executiondomain.ExecutionStatusAccepted)
	}

	// Counter should be reset. Switch back to failing and confirm 5 more needed to trip.
	adapter.events = []executiondomain.ExecutionEventV1{{
		EventID:      "failed-post",
		Status:       executiondomain.ExecutionStatusFailed,
		Correlation:  executiondomain.ExecutionCorrelation{IntentID: "intent-post", OrderID: "order-post", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:    baseMs + 10,
		ExecutionSeq: 1,
		Attempt:      1,
		RequestedQty: 1.5,
		LeavesQty:    1.5,
		Reason:       "venue_runtime_failed_adapter_call",
		Provenance:   executiondomain.ExecutionProvenance{CorrelationID: "corr-fixture", Source: "fake"},
	}}

	for i := 0; i < 4; i++ {
		intent := validIntentFixture()
		intent.IntentID = "intent-postfail-" + strconv.Itoa(i)
		executor.ExecuteAt(intent, baseMs+10+int64(i))
	}

	// Should not be tripped yet (only 4 failures since reset).
	checkIntent := validIntentFixture()
	checkIntent.IntentID = "intent-postcheck"
	events = executor.ExecuteAt(checkIntent, baseMs+20)
	if events[0].Status == executiondomain.ExecutionStatusRejected && events[0].Reason == executiondomain.ReasonAdapterSelectionCircuitOpen {
		t.Fatal("circuit should not be tripped after only 4 failures post-reset")
	}
}
