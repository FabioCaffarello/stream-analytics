package domain

import "testing"

func TestIsRetryable_TransientReasons(t *testing.T) {
	retryable := []struct {
		name   string
		reason string
	}{
		{"lease_expired", ReasonCredentialsLeaseExpired},
		{"lease_inactive", ReasonCredentialsLeaseInactive},
		{"material_missing", ReasonCredentialsUnavailableMaterialMissing},
		{"control_plane_paused", ReasonControlPlanePaused},
		{"control_plane_drained", ReasonControlPlaneDrained},
		{"venue_adapter_call_failed", ReasonVenueRuntimeAdapterCallFailed},
		{"adapter_circuit_open", ReasonAdapterSelectionCircuitOpen},
	}

	for _, tc := range retryable {
		t.Run(tc.name, func(t *testing.T) {
			if !IsRetryable(tc.reason) {
				t.Errorf("expected IsRetryable(%q) = true", tc.reason)
			}
		})
	}
}

func TestIsRetryable_PermanentReasons(t *testing.T) {
	permanent := []struct {
		name   string
		reason string
	}{
		// governance denied
		{"governance_no_grant", ReasonGovernanceNoGrant},
		{"governance_intent_invalid", ReasonGovernanceIntentInvalid},
		{"governance_grant_expired", ReasonGovernanceGrantExpired},
		{"governance_ttl_expired", ReasonGovernanceTTLExpired},
		{"governance_ttl_too_large", ReasonGovernanceTTLTooLarge},
		{"governance_size_too_large", ReasonGovernanceSizeTooLarge},
		{"governance_notional_too_large", ReasonGovernanceNotionalTooLarge},
		{"governance_slippage_too_large", ReasonGovernanceSlippageTooLarge},
		{"governance_mode_not_allowed", ReasonGovernanceModeNotAllowed},
		{"governance_safe_mode_required", ReasonGovernanceSafeModeRequired},
		{"governance_trade_only_required", ReasonGovernanceTradeOnlyRequired},
		{"governance_venue_not_allowed", ReasonGovernanceVenueNotAllowed},
		{"governance_symbol_not_allowed", ReasonGovernanceSymbolNotAllowed},
		{"governance_account_not_allowed", ReasonGovernanceAccountNotAllowed},

		// adapter selection denied
		{"adapter_denied", ReasonAdapterSelectionDenied},
		{"adapter_unavailable", ReasonAdapterSelectionUnavailable},
		{"adapter_mode_mismatch", ReasonAdapterSelectionModeMismatch},

		// credentials invalid (structural)
		{"cred_invalid_resolver", ReasonCredentialsInvalidResolverUnaccepted},
		{"cred_invalid_provider", ReasonCredentialsInvalidProviderUnaccepted},
		{"cred_invalid_boundary", ReasonCredentialsInvalidBoundaryMismatch},
		{"cred_invalid_adapter", ReasonCredentialsInvalidAdapterMismatch},
		{"cred_invalid_mode", ReasonCredentialsInvalidModeMismatch},
		{"cred_invalid_trade_only", ReasonCredentialsInvalidTradeOnlyRequired},

		// credentials scope denied
		{"cred_scope_denied", ReasonCredentialsScopeDeniedScopeMismatch},

		// credentials unavailable (non-retryable: no resolver)
		{"cred_unavailable_no_resolver", ReasonCredentialsUnavailableNoResolver},

		// control plane terminal/operator-action
		{"control_plane_halted", ReasonControlPlaneHalted},
		{"control_plane_strategy_disabled", ReasonControlPlaneStrategyDisabled},
		{"control_plane_adapter_disabled", ReasonControlPlaneAdapterDisabled},
		{"control_plane_venue_restricted", ReasonControlPlaneVenueRestricted},
		{"control_plane_symbol_restricted", ReasonControlPlaneSymbolRestricted},

		// execution policy rejected (prefix-based reasons)
		{"rejected_example", "rejected_max_notional_exceeded"},
	}

	for _, tc := range permanent {
		t.Run(tc.name, func(t *testing.T) {
			if IsRetryable(tc.reason) {
				t.Errorf("expected IsRetryable(%q) = false", tc.reason)
			}
		})
	}
}

func TestIsRetryable_EmptyAndUnknown(t *testing.T) {
	cases := []struct {
		name   string
		reason string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"unknown_prefix", "something_unexpected"},
		{"unknown_gibberish", "xyzzy"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsRetryable(tc.reason) {
				t.Errorf("expected IsRetryable(%q) = false (fail-closed)", tc.reason)
			}
		})
	}
}

// TestIsRetryable_ExhaustiveConstants verifies every declared reason constant
// is covered by one of the test tables above. If a new constant is added to
// reason.go without a corresponding test entry here, this test will fail.
func TestIsRetryable_ExhaustiveConstants(t *testing.T) {
	allConstants := []string{
		// governance
		ReasonGovernanceNoGrant,
		ReasonGovernanceIntentInvalid,
		ReasonGovernanceGrantExpired,
		ReasonGovernanceTTLExpired,
		ReasonGovernanceTTLTooLarge,
		ReasonGovernanceSizeTooLarge,
		ReasonGovernanceNotionalTooLarge,
		ReasonGovernanceSlippageTooLarge,
		ReasonGovernanceModeNotAllowed,
		ReasonGovernanceSafeModeRequired,
		ReasonGovernanceTradeOnlyRequired,
		ReasonGovernanceVenueNotAllowed,
		ReasonGovernanceSymbolNotAllowed,
		ReasonGovernanceAccountNotAllowed,

		// adapter selection
		ReasonAdapterSelectionDenied,
		ReasonAdapterSelectionUnavailable,
		ReasonAdapterSelectionModeMismatch,
		ReasonAdapterSelectionCircuitOpen,

		// credentials unavailable
		ReasonCredentialsUnavailableNoResolver,
		ReasonCredentialsUnavailableMaterialMissing,

		// credentials invalid
		ReasonCredentialsInvalidResolverUnaccepted,
		ReasonCredentialsInvalidProviderUnaccepted,
		ReasonCredentialsInvalidBoundaryMismatch,
		ReasonCredentialsInvalidAdapterMismatch,
		ReasonCredentialsInvalidModeMismatch,
		ReasonCredentialsInvalidTradeOnlyRequired,

		// credentials scope
		ReasonCredentialsScopeDeniedScopeMismatch,

		// credentials lease
		ReasonCredentialsLeaseExpired,
		ReasonCredentialsLeaseInactive,

		// venue runtime
		ReasonVenueRuntimeAdapterCallFailed,

		// control plane
		ReasonControlPlanePaused,
		ReasonControlPlaneDrained,
		ReasonControlPlaneHalted,
		ReasonControlPlaneStrategyDisabled,
		ReasonControlPlaneAdapterDisabled,
		ReasonControlPlaneVenueRestricted,
		ReasonControlPlaneSymbolRestricted,
	}

	expectedRetryable := map[string]bool{
		ReasonCredentialsLeaseExpired:               true,
		ReasonCredentialsLeaseInactive:              true,
		ReasonCredentialsUnavailableMaterialMissing: true,
		ReasonControlPlanePaused:                    true,
		ReasonControlPlaneDrained:                   true,
		ReasonVenueRuntimeAdapterCallFailed:         true,
		ReasonAdapterSelectionCircuitOpen:           true,
	}

	for _, reason := range allConstants {
		got := IsRetryable(reason)
		want := expectedRetryable[reason]
		if got != want {
			t.Errorf("IsRetryable(%q) = %v, want %v", reason, got, want)
		}
	}
}

func TestIsRetryable_WhitespaceHandling(t *testing.T) {
	// Retryable reason with surrounding whitespace should still be retryable.
	if !IsRetryable("  " + ReasonControlPlanePaused + "  ") {
		t.Error("expected whitespace-padded retryable reason to return true")
	}

	// Permanent reason with surrounding whitespace should still be permanent.
	if IsRetryable("  " + ReasonControlPlaneHalted + "  ") {
		t.Error("expected whitespace-padded permanent reason to return false")
	}
}
