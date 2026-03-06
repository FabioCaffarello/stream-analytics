package credentials

import (
	"errors"
	"testing"
	"time"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	executiongovernance "github.com/market-raccoon/internal/core/execution/governance"
)

type staticProvider struct {
	material ProviderMaterial
	err      error
}

func (p staticProvider) ResolveTradeCredentialMaterial() (ProviderMaterial, error) {
	return p.material, p.err
}

func TestEnvProvider_ResolveTradeCredentialMaterial(t *testing.T) {
	t.Setenv("MR_TEST_API_KEY", "key-1")
	t.Setenv("MR_TEST_API_SECRET", "secret-1")

	provider := NewEnvProvider(EnvProviderConfig{
		APIKeyEnv:    "MR_TEST_API_KEY",
		APISecretEnv: "MR_TEST_API_SECRET",
	})
	material, err := provider.ResolveTradeCredentialMaterial()
	if err != nil {
		t.Fatalf("ResolveTradeCredentialMaterial() error = %v", err)
	}
	if material.Credentials.APIKey != "key-1" {
		t.Fatalf("APIKey=%q want=key-1", material.Credentials.APIKey)
	}
	if material.Credentials.APISecret != "secret-1" {
		t.Fatalf("APISecret=%q want=secret-1", material.Credentials.APISecret)
	}
	if material.Scope != ScopeTradeOnly {
		t.Fatalf("Scope=%q want=%q", material.Scope, ScopeTradeOnly)
	}
	if !material.TradeOnly {
		t.Fatal("expected trade-only material")
	}
	if material.ProviderID != ProviderIDEnvStaticV1 {
		t.Fatalf("ProviderID=%q want=%q", material.ProviderID, ProviderIDEnvStaticV1)
	}
}

func TestEnvProvider_ResolveTradeCredentialMaterialMissingValues(t *testing.T) {
	provider := NewEnvProvider(EnvProviderConfig{
		APIKeyEnv:    "MR_TEST_MISSING_API_KEY",
		APISecretEnv: "MR_TEST_MISSING_API_SECRET",
	})
	if _, err := provider.ResolveTradeCredentialMaterial(); err == nil {
		t.Fatal("expected error when credentials env vars are missing")
	}
}

func TestBroker_ResolveCredentialModelsLeaseAndProvenance(t *testing.T) {
	broker := NewBroker(BrokerConfig{
		Boundary:   "execution.adapter",
		AdapterID:  "binance.spot",
		Mode:       "real_adapter_safe",
		ResolverID: ResolverIDTradeBrokerV1,
		ProviderID: ProviderIDEnvStaticV1,
		LeaseTTL:   45 * time.Second,
	}, staticProvider{material: ProviderMaterial{
		Credentials:     TradeCredentials{APIKey: "key-1", APISecret: "secret-1"},
		Scope:           ScopeTradeOnly,
		TradeOnly:       true,
		ProviderID:      ProviderIDEnvStaticV1,
		SourceType:      SourceTypeEnv,
		SourceRef:       "execution.real.binance.trade_api",
		RevocationReady: true,
	}})

	result := broker.ResolveCredential(executiongovernance.CredentialRequirement{
		Required:            true,
		Boundary:            "execution.adapter",
		AdapterID:           "binance.spot",
		Mode:                "real_adapter_safe",
		Scope:               ScopeTradeOnly,
		TradeOnly:           true,
		Venue:               "binance",
		AccountID:           "paper",
		Symbol:              "BTCUSDT",
		AcceptedResolverIDs: []string{ResolverIDTradeBrokerV1},
		AcceptedProviderIDs: []string{ProviderIDEnvStaticV1},
	}, 1_700_000_002_000)
	if result.Status != executiongovernance.CredentialResolutionResolved {
		t.Fatalf("status=%q want=resolved", result.Status)
	}
	if result.Availability != executiongovernance.CredentialAvailabilityAvailable {
		t.Fatalf("availability=%q want=available", result.Availability)
	}
	if !result.Satisfied() {
		t.Fatal("expected resolution to satisfy requirement")
	}
	if result.Credential.Provenance.ProviderID != ProviderIDEnvStaticV1 {
		t.Fatalf("provider_id=%q want=%q", result.Credential.Provenance.ProviderID, ProviderIDEnvStaticV1)
	}
	if result.Credential.Provenance.ResolverID != ResolverIDTradeBrokerV1 {
		t.Fatalf("resolver_id=%q want=%q", result.Credential.Provenance.ResolverID, ResolverIDTradeBrokerV1)
	}
	if result.Credential.Lease.State != executiongovernance.CredentialLeaseStateActive {
		t.Fatalf("lease_state=%q want=active", result.Credential.Lease.State)
	}
	if result.Credential.Lease.ValidUntilMs <= result.Credential.Lease.IssuedAtMs {
		t.Fatalf("lease validity invalid: issued=%d valid_until=%d", result.Credential.Lease.IssuedAtMs, result.Credential.Lease.ValidUntilMs)
	}
}

func TestBroker_DeniesUnsupportedScope(t *testing.T) {
	broker := NewBroker(BrokerConfig{
		Boundary:   "execution.adapter",
		AdapterID:  "binance.spot",
		Mode:       "real_adapter_safe",
		ResolverID: ResolverIDTradeBrokerV1,
		ProviderID: ProviderIDEnvStaticV1,
	}, staticProvider{material: ProviderMaterial{
		Credentials: TradeCredentials{APIKey: "key-1", APISecret: "secret-1"},
		Scope:       ScopeTradeOnly,
		TradeOnly:   true,
		ProviderID:  ProviderIDEnvStaticV1,
	}})

	result := broker.ResolveCredential(executiongovernance.CredentialRequirement{
		Required:            true,
		Boundary:            "execution.adapter",
		AdapterID:           "binance.spot",
		Mode:                "real_adapter_safe",
		Scope:               "withdraw",
		TradeOnly:           true,
		AcceptedResolverIDs: []string{ResolverIDTradeBrokerV1},
		AcceptedProviderIDs: []string{ProviderIDEnvStaticV1},
	}, 1_700_000_002_000)
	if result.Status != executiongovernance.CredentialResolutionDenied {
		t.Fatalf("status=%q want=denied", result.Status)
	}
	if result.Reason != executiondomain.ReasonCredentialsScopeDeniedScopeMismatch {
		t.Fatalf("reason=%q want=%q", result.Reason, executiondomain.ReasonCredentialsScopeDeniedScopeMismatch)
	}
}

func TestBroker_DeniesUnavailableMaterial(t *testing.T) {
	broker := NewBroker(BrokerConfig{
		Boundary:   "execution.adapter",
		AdapterID:  "binance.spot",
		Mode:       "real_adapter_safe",
		ResolverID: ResolverIDTradeBrokerV1,
		ProviderID: ProviderIDEnvStaticV1,
	}, staticProvider{err: errors.New("missing env")})

	result := broker.ResolveCredential(executiongovernance.CredentialRequirement{
		Required:            true,
		Boundary:            "execution.adapter",
		AdapterID:           "binance.spot",
		Mode:                "real_adapter_safe",
		Scope:               ScopeTradeOnly,
		TradeOnly:           true,
		AcceptedResolverIDs: []string{ResolverIDTradeBrokerV1},
		AcceptedProviderIDs: []string{ProviderIDEnvStaticV1},
	}, 1_700_000_002_000)
	if result.Reason != executiondomain.ReasonCredentialsUnavailableMaterialMissing {
		t.Fatalf("reason=%q want=%q", result.Reason, executiondomain.ReasonCredentialsUnavailableMaterialMissing)
	}
	if result.Availability != executiongovernance.CredentialAvailabilityUnavailable {
		t.Fatalf("availability=%q want=unavailable", result.Availability)
	}
}

func TestBroker_AcquireTradeCredentialLeaseReturnsMaterial(t *testing.T) {
	broker := NewBroker(BrokerConfig{
		Boundary:   "execution.adapter",
		AdapterID:  "binance.spot",
		Mode:       "real_adapter_safe",
		ResolverID: ResolverIDTradeBrokerV1,
		ProviderID: ProviderIDEnvStaticV1,
	}, staticProvider{material: ProviderMaterial{
		Credentials: TradeCredentials{APIKey: "key-1", APISecret: "secret-1"},
		Scope:       ScopeTradeOnly,
		TradeOnly:   true,
		ProviderID:  ProviderIDEnvStaticV1,
	}})

	lease, err := broker.AcquireTradeCredentialLease(executiongovernance.CredentialRequirement{
		Required:            true,
		Boundary:            "execution.adapter",
		AdapterID:           "binance.spot",
		Mode:                "real_adapter_safe",
		Scope:               ScopeTradeOnly,
		TradeOnly:           true,
		AcceptedResolverIDs: []string{ResolverIDTradeBrokerV1},
		AcceptedProviderIDs: []string{ProviderIDEnvStaticV1},
	}, 1_700_000_002_000)
	if err != nil {
		t.Fatalf("AcquireTradeCredentialLease() error = %v", err)
	}
	if lease.Material.APIKey != "key-1" {
		t.Fatalf("APIKey=%q want=key-1", lease.Material.APIKey)
	}
	if lease.Credential.Provenance.SourceRef != "execution.real.binance.trade_api" {
		t.Fatalf("source_ref=%q want execution.real.binance.trade_api", lease.Credential.Provenance.SourceRef)
	}
}
