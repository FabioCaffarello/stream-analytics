package app

import (
	"math"
	"strings"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	executiongovernance "github.com/market-raccoon/internal/core/execution/governance"
	executionports "github.com/market-raccoon/internal/core/execution/ports"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
)

type AdapterRoute struct {
	Boundary              string
	AdapterID             string
	Mode                  string
	CredentialRequirement executiongovernance.CredentialRequirement
}

type StaticCapabilityAuthorizer struct {
	Grant *executiongovernance.ExecutionGrant
}

//nolint:gocyclo // Authorization keeps one explicit branch per policy denial for deterministic governance reasons.
func (a StaticCapabilityAuthorizer) Authorize(intent strategydomain.StrategyIntentV1, observedAtMs int64) executiongovernance.AuthorizationDecision {
	if a.Grant == nil {
		return executiongovernance.AuthorizationDecision{Reason: executiondomain.ReasonGovernanceNoGrant}
	}
	grant := *a.Grant
	if strings.TrimSpace(grant.Boundary) == "" || strings.TrimSpace(grant.AdapterID) == "" || strings.TrimSpace(grant.Mode) == "" {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceNoGrant}
	}
	if !grant.SafeMode {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceSafeModeRequired}
	}
	if !grant.TradeOnly {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceTradeOnlyRequired}
	}
	if p := intent.Validate(); p != nil {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceIntentInvalid}
	}

	norm := normalizeIntent(intent, observedAtMs)
	if grant.Lease.ValidUntilMs > 0 && norm.nowMs > grant.Lease.ValidUntilMs {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceGrantExpired}
	}
	if !grant.Scope.AllowsVenue(norm.venue) {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceVenueNotAllowed}
	}
	if !grant.Scope.AllowsSymbol(norm.symbol) {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceSymbolNotAllowed}
	}
	if !grant.Scope.AllowsAccount(norm.accountID) {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceAccountNotAllowed}
	}
	if intent.ExpiresAtMs <= norm.nowMs {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceTTLExpired}
	}
	if grant.Limits.MaxIntentTTLms > 0 && intent.ExpiresAtMs-intent.CreatedAtMs > grant.Limits.MaxIntentTTLms {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceTTLTooLarge}
	}
	if grant.Limits.MaxAbsQuantity > 0 && math.Abs(intent.Sizing.Value) > grant.Limits.MaxAbsQuantity {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceSizeTooLarge}
	}
	if grant.Limits.MaxNotionalUSD > 0 && intentNotionalUSD(intent) > grant.Limits.MaxNotionalUSD {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceNotionalTooLarge}
	}
	if grant.Limits.MaxSlippageBps > 0 && intent.Constraints.MaxSlippageBps > grant.Limits.MaxSlippageBps {
		return executiongovernance.AuthorizationDecision{Grant: grant, Reason: executiondomain.ReasonGovernanceSlippageTooLarge}
	}

	return executiongovernance.AuthorizationDecision{
		Authorized: true,
		Grant:      grant,
	}
}

type StaticAdapterSelector struct {
	routes map[string]AdapterRoute
}

func NewStaticAdapterSelector(routes ...AdapterRoute) *StaticAdapterSelector {
	out := make(map[string]AdapterRoute, len(routes))
	for _, route := range routes {
		key := strings.ToLower(strings.TrimSpace(route.AdapterID))
		if key == "" {
			continue
		}
		out[key] = route
	}
	return &StaticAdapterSelector{routes: out}
}

func (s *StaticAdapterSelector) Select(intent strategydomain.StrategyIntentV1, grant executiongovernance.ExecutionGrant) executiongovernance.AdapterSelectionDecision {
	adapterID := strings.ToLower(strings.TrimSpace(grant.AdapterID))
	if s == nil || len(s.routes) == 0 || adapterID == "" {
		return executiongovernance.AdapterSelectionDecision{Reason: executiondomain.ReasonAdapterSelectionUnavailable}
	}
	route, ok := s.routes[adapterID]
	if !ok {
		return executiongovernance.AdapterSelectionDecision{Reason: executiondomain.ReasonAdapterSelectionUnavailable}
	}
	if !strings.EqualFold(strings.TrimSpace(route.Mode), strings.TrimSpace(grant.Mode)) {
		return executiongovernance.AdapterSelectionDecision{Reason: executiondomain.ReasonAdapterSelectionModeMismatch}
	}

	requirement := route.CredentialRequirement
	requirement.Boundary = strings.TrimSpace(route.Boundary)
	requirement.AdapterID = strings.TrimSpace(route.AdapterID)
	requirement.Mode = strings.TrimSpace(route.Mode)
	requirement.Venue = strings.ToLower(strings.TrimSpace(intent.Scope.Venue))
	requirement.AccountID = strings.TrimSpace(intent.Scope.AccountID)
	requirement.Symbol = strings.ToUpper(strings.TrimSpace(intent.Scope.Symbol))

	return executiongovernance.AdapterSelectionDecision{
		Selected:              true,
		Boundary:              strings.TrimSpace(route.Boundary),
		AdapterID:             strings.TrimSpace(route.AdapterID),
		Mode:                  strings.TrimSpace(route.Mode),
		CredentialRequirement: requirement,
	}
}

type StaticExecutionGovernance struct {
	authorizer         executionports.CapabilityAuthorizer
	selector           executionports.AdapterSelector
	credentialResolver executionports.CredentialResolver
	boundaryInfo       executionports.BoundaryInfo
}

type StaticExecutionGovernanceConfig struct {
	Authorizer         executionports.CapabilityAuthorizer
	Selector           executionports.AdapterSelector
	CredentialResolver executionports.CredentialResolver
	BoundaryInfo       executionports.BoundaryInfo
}

func NewStaticExecutionGovernance(cfg StaticExecutionGovernanceConfig) *StaticExecutionGovernance {
	return &StaticExecutionGovernance{
		authorizer:         cfg.Authorizer,
		selector:           cfg.Selector,
		credentialResolver: cfg.CredentialResolver,
		boundaryInfo:       cfg.BoundaryInfo,
	}
}

func (g *StaticExecutionGovernance) Evaluate(intent strategydomain.StrategyIntentV1, observedAtMs int64) executiongovernance.Outcome {
	outcome := executiongovernance.Outcome{}
	if g == nil || g.authorizer == nil {
		outcome.Authorization = executiongovernance.AuthorizationDecision{Reason: executiondomain.ReasonGovernanceNoGrant}
		outcome.Credential = notRequiredCredentialResolution(executiongovernance.CredentialRequirement{})
		return outcome
	}

	outcome.Authorization = g.authorizer.Authorize(intent, observedAtMs)
	if !outcome.Authorization.Authorized {
		outcome.Credential = notRequiredCredentialResolution(executiongovernance.CredentialRequirement{})
		return outcome
	}

	if g.selector == nil {
		outcome.Adapter = executiongovernance.AdapterSelectionDecision{Reason: executiondomain.ReasonAdapterSelectionUnavailable}
		outcome.Credential = notRequiredCredentialResolution(executiongovernance.CredentialRequirement{})
		return outcome
	}
	outcome.Adapter = g.selector.Select(intent, outcome.Authorization.Grant)
	if !outcome.Adapter.Selected {
		outcome.Credential = notRequiredCredentialResolution(outcome.Adapter.CredentialRequirement)
		return outcome
	}

	if g.credentialResolver == nil {
		outcome.Credential = executiongovernance.CredentialResolution{
			Availability:  executiongovernance.CredentialAvailabilityUnavailable,
			Status:        executiongovernance.CredentialResolutionDenied,
			EvaluatedAtMs: observedAtMs,
			Requirement:   outcome.Adapter.CredentialRequirement,
			Reason:        executiondomain.ReasonCredentialsUnavailableNoResolver,
		}
		if !outcome.Adapter.CredentialRequirement.Required {
			outcome.Credential = notRequiredCredentialResolution(outcome.Adapter.CredentialRequirement)
		}
		return outcome
	}

	outcome.Credential = g.credentialResolver.ResolveCredential(outcome.Adapter.CredentialRequirement, observedAtMs)
	return outcome
}

func (g *StaticExecutionGovernance) BoundaryInfo() executionports.BoundaryInfo {
	if g == nil {
		return executionports.BoundaryInfo{}
	}
	return g.boundaryInfo
}

func DefaultBootstrapGrant() executiongovernance.ExecutionGrant {
	cfg := DefaultBootstrapConfig()
	return executiongovernance.ExecutionGrant{
		GrantID:   "execution.bootstrap.default",
		Boundary:  strings.TrimSpace(cfg.Boundary),
		AdapterID: strings.TrimSpace(cfg.AdapterID),
		Mode:      strings.TrimSpace(cfg.ExecutionMode),
		SafeMode:  true,
		TradeOnly: true,
		Scope: executiongovernance.ExecutionScope{
			AllowAnyVenue:   true,
			AllowAnySymbol:  true,
			AllowAnyAccount: true,
		},
		Limits: executiongovernance.ExecutionLimits{
			MaxIntentTTLms: cfg.MaxIntentTTLms,
			MaxAbsQuantity: cfg.MaxAbsQuantity,
			MaxNotionalUSD: cfg.MaxNotionalUSD,
			MaxSlippageBps: cfg.MaxSlippageBps,
		},
		Provenance: executiongovernance.GrantProvenance{
			Source:   cfg.Source,
			PolicyID: "execution.bootstrap.default",
		},
	}
}

func intentNotionalUSD(intent strategydomain.StrategyIntentV1) float64 {
	switch intent.Sizing.Mode {
	case strategydomain.SizingModeQuoteNotionalUSD:
		return maxFloat(intent.Sizing.Value, 0)
	default:
		return maxFloat(intent.Sizing.MaxNotionalUSD, 0)
	}
}

func maxFloat(v, fallback float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return fallback
	}
	return v
}

func notRequiredCredentialResolution(requirement executiongovernance.CredentialRequirement) executiongovernance.CredentialResolution {
	return executiongovernance.CredentialResolution{
		Availability: executiongovernance.CredentialAvailabilityNotRequired,
		Status:       executiongovernance.CredentialResolutionNotRequired,
		Requirement:  requirement,
		Credential: executiongovernance.ResolvedCredential{
			Lease: executiongovernance.CredentialLease{State: executiongovernance.CredentialLeaseStateNotRequired},
		},
	}
}
