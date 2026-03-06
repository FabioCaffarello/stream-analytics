package credentials

import (
	"strings"
	"time"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	executiongovernance "github.com/market-raccoon/internal/core/execution/governance"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

type BrokerConfig struct {
	Boundary   string
	AdapterID  string
	Mode       string
	ResolverID string
	ProviderID string
	SourceType string
	SourceRef  string
	LeaseTTL   time.Duration
}

type tradeCredentialBroker struct {
	provider Provider
	cfg      BrokerConfig
	now      func() time.Time
}

func NewBroker(cfg BrokerConfig, provider Provider) Broker {
	if strings.TrimSpace(cfg.Boundary) == "" {
		cfg.Boundary = "execution.adapter"
	}
	if strings.TrimSpace(cfg.ResolverID) == "" {
		cfg.ResolverID = ResolverIDTradeBrokerV1
	}
	if strings.TrimSpace(cfg.ProviderID) == "" {
		cfg.ProviderID = ProviderIDEnvStaticV1
	}
	if strings.TrimSpace(cfg.SourceType) == "" {
		cfg.SourceType = SourceTypeEnv
	}
	if strings.TrimSpace(cfg.SourceRef) == "" {
		cfg.SourceRef = "execution.real.binance.trade_api"
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = defaultLeaseTTL
	}
	return &tradeCredentialBroker{
		provider: provider,
		cfg:      cfg,
		now:      time.Now,
	}
}

func (b *tradeCredentialBroker) ResolveCredential(requirement executiongovernance.CredentialRequirement, observedAtMs int64) executiongovernance.CredentialResolution {
	_, resolution := b.resolve(requirement, observedAtMs)
	return resolution
}

func (b *tradeCredentialBroker) AcquireTradeCredentialLease(requirement executiongovernance.CredentialRequirement, observedAtMs int64) (TradeCredentialLease, error) {
	lease, resolution := b.resolve(requirement, observedAtMs)
	if resolution.Satisfied() {
		return lease, nil
	}
	return TradeCredentialLease{}, NewLeaseError(resolution.Reason, nil)
}

//nolint:gocyclo // Broker denial taxonomy is intentionally explicit to preserve governance failure classes.
func (b *tradeCredentialBroker) resolve(requirement executiongovernance.CredentialRequirement, observedAtMs int64) (TradeCredentialLease, executiongovernance.CredentialResolution) {
	requirement = b.normalizeRequirement(requirement)
	resolution := executiongovernance.CredentialResolution{
		EvaluatedAtMs: observedAtMs,
		Requirement:   requirement,
	}
	if !requirement.Required {
		resolution.Availability = executiongovernance.CredentialAvailabilityNotRequired
		resolution.Status = executiongovernance.CredentialResolutionNotRequired
		resolution.Credential.Lease.State = executiongovernance.CredentialLeaseStateNotRequired
		return TradeCredentialLease{}, resolution
	}

	if observedAtMs <= 0 {
		observedAtMs = b.nowMs()
		resolution.EvaluatedAtMs = observedAtMs
	}
	if b == nil || b.provider == nil {
		return TradeCredentialLease{}, deniedResolution(resolution, executiongovernance.CredentialAvailabilityUnavailable, executiondomain.ReasonCredentialsUnavailableNoResolver)
	}
	if !requirement.AcceptsResolver(b.cfg.ResolverID) {
		return TradeCredentialLease{}, deniedResolution(resolution, executiongovernance.CredentialAvailabilityAvailable, executiondomain.ReasonCredentialsInvalidResolverUnaccepted)
	}

	material, err := b.provider.ResolveTradeCredentialMaterial()
	if err != nil {
		return TradeCredentialLease{}, deniedResolution(resolution, executiongovernance.CredentialAvailabilityUnavailable, executiondomain.ReasonCredentialsUnavailableMaterialMissing)
	}
	provenance := executiongovernance.CredentialProvenance{
		ResolverID:      strings.TrimSpace(b.cfg.ResolverID),
		ProviderID:      firstNonEmpty(material.ProviderID, b.cfg.ProviderID),
		SourceType:      firstNonEmpty(material.SourceType, b.cfg.SourceType),
		SourceRef:       firstNonEmpty(material.SourceRef, b.cfg.SourceRef),
		RevocationReady: material.RevocationReady,
	}
	if !requirement.AcceptsProvider(provenance.ProviderID) {
		return TradeCredentialLease{}, deniedResolution(resolution, executiongovernance.CredentialAvailabilityAvailable, executiondomain.ReasonCredentialsInvalidProviderUnaccepted)
	}
	if requirement.TradeOnly && !material.TradeOnly {
		return TradeCredentialLease{}, deniedResolution(resolution, executiongovernance.CredentialAvailabilityAvailable, executiondomain.ReasonCredentialsInvalidTradeOnlyRequired)
	}

	scope := firstNonEmpty(material.Scope, ScopeTradeOnly)
	switch {
	case requirement.Boundary != "" && !strings.EqualFold(requirement.Boundary, b.cfg.Boundary):
		return TradeCredentialLease{}, deniedResolution(resolution, executiongovernance.CredentialAvailabilityAvailable, executiondomain.ReasonCredentialsInvalidBoundaryMismatch)
	case requirement.AdapterID != "" && !strings.EqualFold(requirement.AdapterID, b.cfg.AdapterID):
		return TradeCredentialLease{}, deniedResolution(resolution, executiongovernance.CredentialAvailabilityAvailable, executiondomain.ReasonCredentialsInvalidAdapterMismatch)
	case requirement.Mode != "" && !strings.EqualFold(requirement.Mode, b.cfg.Mode):
		return TradeCredentialLease{}, deniedResolution(resolution, executiongovernance.CredentialAvailabilityAvailable, executiondomain.ReasonCredentialsInvalidModeMismatch)
	case requirement.Scope != "" && !strings.EqualFold(requirement.Scope, scope):
		return TradeCredentialLease{}, deniedResolution(resolution, executiongovernance.CredentialAvailabilityAvailable, executiondomain.ReasonCredentialsScopeDeniedScopeMismatch)
	}

	credential := executiongovernance.ResolvedCredential{
		Boundary:  firstNonEmpty(requirement.Boundary, b.cfg.Boundary),
		AdapterID: firstNonEmpty(requirement.AdapterID, b.cfg.AdapterID),
		Mode:      firstNonEmpty(requirement.Mode, b.cfg.Mode),
		Scope:     scope,
		TradeOnly: material.TradeOnly,
		Venue:     strings.ToLower(strings.TrimSpace(requirement.Venue)),
		AccountID: strings.TrimSpace(requirement.AccountID),
		Symbol:    strings.ToUpper(strings.TrimSpace(requirement.Symbol)),
		Lease: executiongovernance.CredentialLease{
			LeaseID:      b.leaseID(requirement, observedAtMs),
			State:        executiongovernance.CredentialLeaseStateActive,
			IssuedAtMs:   observedAtMs,
			ValidUntilMs: observedAtMs + b.cfg.LeaseTTL.Milliseconds(),
		},
		Provenance: provenance,
	}
	if !credential.Lease.ActiveAt(observedAtMs) {
		return TradeCredentialLease{}, deniedResolution(resolution, executiongovernance.CredentialAvailabilityAvailable, executiondomain.ReasonCredentialsLeaseExpired)
	}

	resolution.Availability = executiongovernance.CredentialAvailabilityAvailable
	resolution.Status = executiongovernance.CredentialResolutionResolved
	resolution.Credential = credential
	if !resolution.Satisfied() {
		return TradeCredentialLease{}, deniedResolution(resolution, executiongovernance.CredentialAvailabilityAvailable, executiondomain.ReasonCredentialsLeaseInactive)
	}
	return TradeCredentialLease{
		Credential: credential,
		Material:   material.Credentials,
	}, resolution
}

func (b *tradeCredentialBroker) normalizeRequirement(requirement executiongovernance.CredentialRequirement) executiongovernance.CredentialRequirement {
	requirement.Boundary = strings.TrimSpace(requirement.Boundary)
	requirement.AdapterID = strings.TrimSpace(requirement.AdapterID)
	requirement.Mode = strings.TrimSpace(requirement.Mode)
	requirement.Scope = strings.TrimSpace(requirement.Scope)
	requirement.Venue = strings.ToLower(strings.TrimSpace(requirement.Venue))
	requirement.AccountID = strings.TrimSpace(requirement.AccountID)
	requirement.Symbol = strings.ToUpper(strings.TrimSpace(requirement.Symbol))
	return requirement
}

func (b *tradeCredentialBroker) leaseID(requirement executiongovernance.CredentialRequirement, observedAtMs int64) string {
	return sharedhash.HashFieldsFast(
		"credential-lease",
		b.cfg.ResolverID,
		requirement.Boundary,
		requirement.AdapterID,
		requirement.Mode,
		requirement.Scope,
		requirement.Venue,
		requirement.AccountID,
		requirement.Symbol,
		strings.TrimSpace(time.UnixMilli(observedAtMs).UTC().Format(time.RFC3339Nano)),
	)
}

func (b *tradeCredentialBroker) nowMs() int64 {
	if b == nil || b.now == nil {
		return time.Now().UnixMilli()
	}
	return b.now().UnixMilli()
}

func deniedResolution(
	resolution executiongovernance.CredentialResolution,
	availability executiongovernance.CredentialAvailabilityStatus,
	reason string,
) executiongovernance.CredentialResolution {
	resolution.Availability = availability
	resolution.Status = executiongovernance.CredentialResolutionDenied
	resolution.Reason = reason
	switch reason {
	case executiondomain.ReasonCredentialsLeaseExpired:
		resolution.Credential.Lease.State = executiongovernance.CredentialLeaseStateExpired
	case executiondomain.ReasonCredentialsLeaseInactive:
		resolution.Credential.Lease.State = executiongovernance.CredentialLeaseStateInvalid
	default:
		if resolution.Credential.Lease.State == "" {
			resolution.Credential.Lease.State = executiongovernance.CredentialLeaseStateInvalid
		}
	}
	return resolution
}

func firstNonEmpty(values ...string) string {
	for _, raw := range values {
		if value := strings.TrimSpace(raw); value != "" {
			return value
		}
	}
	return ""
}
