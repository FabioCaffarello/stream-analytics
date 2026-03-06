package ports

import (
	executiongovernance "github.com/market-raccoon/internal/core/execution/governance"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
)

type CredentialResolver interface {
	ResolveCredential(requirement executiongovernance.CredentialRequirement, observedAtMs int64) executiongovernance.CredentialResolution
}

type CapabilityAuthorizer interface {
	Authorize(intent strategydomain.StrategyIntentV1, observedAtMs int64) executiongovernance.AuthorizationDecision
}

type AdapterSelector interface {
	Select(intent strategydomain.StrategyIntentV1, grant executiongovernance.ExecutionGrant) executiongovernance.AdapterSelectionDecision
}

type ExecutionGovernance interface {
	Evaluate(intent strategydomain.StrategyIntentV1, observedAtMs int64) executiongovernance.Outcome
	BoundaryInfo() BoundaryInfo
}
