package app

import (
	"testing"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	executiongovernance "github.com/market-raccoon/internal/core/execution/governance"
	executionports "github.com/market-raccoon/internal/core/execution/ports"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
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
