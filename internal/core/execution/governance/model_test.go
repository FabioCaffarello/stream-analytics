package governance

import "testing"

func TestCredentialResolution_SatisfiedRequiresActiveLeaseAndAcceptedProvenance(t *testing.T) {
	requirement := CredentialRequirement{
		Required:            true,
		Boundary:            "execution.adapter",
		AdapterID:           "binance.spot",
		Mode:                "real_adapter_safe",
		Scope:               "trade_only",
		TradeOnly:           true,
		Venue:               "binance",
		AccountID:           "paper",
		Symbol:              "BTCUSDT",
		AcceptedResolverIDs: []string{"credentials.trade_broker.v1"},
		AcceptedProviderIDs: []string{"credentials.provider.env.trade_static"},
	}

	resolution := CredentialResolution{
		Availability:  CredentialAvailabilityAvailable,
		Status:        CredentialResolutionResolved,
		EvaluatedAtMs: 1_700_000_002_000,
		Requirement:   requirement,
		Credential: ResolvedCredential{
			Boundary:  "execution.adapter",
			AdapterID: "binance.spot",
			Mode:      "real_adapter_safe",
			Scope:     "trade_only",
			TradeOnly: true,
			Venue:     "binance",
			AccountID: "paper",
			Symbol:    "BTCUSDT",
			Lease: CredentialLease{
				LeaseID:      "lease-1",
				State:        CredentialLeaseStateActive,
				IssuedAtMs:   1_700_000_002_000,
				ValidUntilMs: 1_700_000_032_000,
			},
			Provenance: CredentialProvenance{
				ResolverID: "credentials.trade_broker.v1",
				ProviderID: "credentials.provider.env.trade_static",
			},
		},
	}
	if !resolution.Satisfied() {
		t.Fatal("expected active matching lease to satisfy requirement")
	}

	resolution.Credential.Lease.ValidUntilMs = 1_700_000_001_999
	if resolution.Satisfied() {
		t.Fatal("expected expired lease to fail closed")
	}

	resolution.Credential.Lease.ValidUntilMs = 1_700_000_032_000
	resolution.Credential.Provenance.ProviderID = "credentials.provider.other"
	if resolution.Satisfied() {
		t.Fatal("expected unaccepted provider provenance to fail closed")
	}
}

func TestCredentialResolution_NotRequired(t *testing.T) {
	resolution := CredentialResolution{
		Availability: CredentialAvailabilityNotRequired,
		Status:       CredentialResolutionNotRequired,
		Requirement:  CredentialRequirement{Required: false},
		Credential: ResolvedCredential{
			Lease: CredentialLease{State: CredentialLeaseStateNotRequired},
		},
	}
	if !resolution.Satisfied() {
		t.Fatal("expected not-required credential resolution to satisfy flow")
	}
}
