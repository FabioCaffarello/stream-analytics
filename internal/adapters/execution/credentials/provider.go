package credentials

import (
	"errors"
	"fmt"
	"strings"
	"time"

	executiongovernance "github.com/market-raccoon/internal/core/execution/governance"
	executionports "github.com/market-raccoon/internal/core/execution/ports"
)

const (
	ResolverIDTradeBrokerV1 = "credentials.trade_broker.v1"
	ProviderIDEnvStaticV1   = "credentials.provider.env.trade_static"
	SourceTypeEnv           = "env"
	ScopeTradeOnly          = "trade_only"
	defaultLeaseTTL         = 30 * time.Second
)

// TradeCredentials holds raw trade-only material. Withdraw/custody remain out of scope.
type TradeCredentials struct {
	APIKey    string
	APISecret string
}

// ProviderMaterial describes raw material plus non-secret provenance metadata.
type ProviderMaterial struct {
	Credentials     TradeCredentials
	Scope           string
	TradeOnly       bool
	ProviderID      string
	SourceType      string
	SourceRef       string
	RevocationReady bool
}

// Provider resolves trade-only credentials for execution adapters.
type Provider interface {
	ResolveTradeCredentialMaterial() (ProviderMaterial, error)
}

// Broker is the hardened boundary shared by governance resolution and runtime lease acquisition.
type Broker interface {
	executionports.CredentialResolver
	AcquireTradeCredentialLease(requirement executiongovernance.CredentialRequirement, observedAtMs int64) (TradeCredentialLease, error)
}

// TradeCredentialLease carries opaque material plus governance-safe metadata.
type TradeCredentialLease struct {
	Credential executiongovernance.ResolvedCredential
	Material   TradeCredentials
}

type LeaseError struct {
	Reason string
	Err    error
}

func (e *LeaseError) Error() string {
	if e == nil {
		return ""
	}
	switch {
	case strings.TrimSpace(e.Reason) == "" && e.Err != nil:
		return e.Err.Error()
	case e.Err == nil:
		return e.Reason
	default:
		return fmt.Sprintf("%s: %v", e.Reason, e.Err)
	}
}

func (e *LeaseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewLeaseError(reason string, err error) error {
	return &LeaseError{
		Reason: strings.TrimSpace(reason),
		Err:    err,
	}
}

func ReasonFromError(err error) string {
	var leaseErr *LeaseError
	if !errors.As(err, &leaseErr) || leaseErr == nil {
		return ""
	}
	return strings.TrimSpace(leaseErr.Reason)
}
