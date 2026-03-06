package domain

import "strings"

const (
	ReasonCategoryAccepted                = "accepted"
	ReasonCategoryLifecycle               = "lifecycle"
	ReasonCategoryGovernanceDenied        = "governance_denied"
	ReasonCategoryCredentialsUnavailable  = "credentials_unavailable"
	ReasonCategoryCredentialsInvalid      = "credentials_invalid"
	ReasonCategoryCredentialsScopeDenied  = "credentials_scope_denied"
	ReasonCategoryCredentialsLeaseDenied  = "credentials_lease_denied"
	ReasonCategoryAdapterSelectionDenied  = "adapter_selection_denied"
	ReasonCategoryExecutionPolicyRejected = "execution_policy_rejected"
	ReasonCategoryVenueRuntimeFailure     = "venue_runtime_failure"
	ReasonCategoryControlPlane            = "control_plane"
	ReasonCategoryUnknown                 = "unknown"
)

const (
	ReasonGovernanceNoGrant           = "governance_denied_no_grant"
	ReasonGovernanceIntentInvalid     = "governance_denied_intent_invalid"
	ReasonGovernanceGrantExpired      = "governance_denied_grant_expired"
	ReasonGovernanceTTLExpired        = "governance_denied_ttl_expired"
	ReasonGovernanceTTLTooLarge       = "governance_denied_ttl_above_grant_limit"
	ReasonGovernanceSizeTooLarge      = "governance_denied_size_above_grant_limit"
	ReasonGovernanceNotionalTooLarge  = "governance_denied_notional_above_grant_limit"
	ReasonGovernanceSlippageTooLarge  = "governance_denied_slippage_above_grant_limit"
	ReasonGovernanceModeNotAllowed    = "governance_denied_mode_not_allowed"
	ReasonGovernanceSafeModeRequired  = "governance_denied_safe_mode_required"
	ReasonGovernanceTradeOnlyRequired = "governance_denied_trade_only_required"
	ReasonGovernanceVenueNotAllowed   = "governance_denied_venue_not_allowed"
	ReasonGovernanceSymbolNotAllowed  = "governance_denied_symbol_not_allowed"
	ReasonGovernanceAccountNotAllowed = "governance_denied_account_not_allowed"

	ReasonAdapterSelectionDenied       = "adapter_selection_denied"
	ReasonAdapterSelectionUnavailable  = "adapter_selection_denied_unavailable"
	ReasonAdapterSelectionModeMismatch = "adapter_selection_denied_mode_mismatch"

	ReasonCredentialsUnavailableNoResolver      = "credentials_unavailable_no_resolver"
	ReasonCredentialsUnavailableMaterialMissing = "credentials_unavailable_material_missing"
	ReasonCredentialsInvalidResolverUnaccepted  = "credentials_invalid_resolver_unaccepted"
	ReasonCredentialsInvalidProviderUnaccepted  = "credentials_invalid_provider_unaccepted"
	ReasonCredentialsInvalidBoundaryMismatch    = "credentials_invalid_boundary_mismatch"
	ReasonCredentialsInvalidAdapterMismatch     = "credentials_invalid_adapter_mismatch"
	ReasonCredentialsInvalidModeMismatch        = "credentials_invalid_mode_mismatch"
	ReasonCredentialsInvalidTradeOnlyRequired   = "credentials_invalid_trade_only_required"
	ReasonCredentialsScopeDeniedScopeMismatch   = "credentials_scope_denied_scope_mismatch"
	ReasonCredentialsLeaseExpired               = "credentials_lease_expired"
	ReasonCredentialsLeaseInactive              = "credentials_lease_inactive"

	ReasonVenueRuntimeAdapterCallFailed = "venue_runtime_failed_adapter_call"

	ReasonControlPlanePaused           = "control_plane_paused"
	ReasonControlPlaneDrained          = "control_plane_drained"
	ReasonControlPlaneHalted           = "control_plane_halted"
	ReasonControlPlaneStrategyDisabled = "control_plane_strategy_disabled"
	ReasonControlPlaneAdapterDisabled  = "control_plane_adapter_disabled"
	ReasonControlPlaneVenueRestricted  = "control_plane_venue_restricted"
	ReasonControlPlaneSymbolRestricted = "control_plane_symbol_restricted"
)

func ReasonCategory(reason string) string {
	reason = strings.TrimSpace(reason)
	switch {
	case strings.HasPrefix(reason, "accepted_"):
		return ReasonCategoryAccepted
	case strings.HasPrefix(reason, "filled_"),
		strings.HasPrefix(reason, "placed_"),
		strings.HasPrefix(reason, "partially_filled_"),
		strings.HasPrefix(reason, "canceled_"),
		strings.HasPrefix(reason, "expired_"):
		return ReasonCategoryLifecycle
	case strings.HasPrefix(reason, "governance_denied_"):
		return ReasonCategoryGovernanceDenied
	case strings.HasPrefix(reason, "credentials_unavailable"):
		return ReasonCategoryCredentialsUnavailable
	case strings.HasPrefix(reason, "credentials_invalid"):
		return ReasonCategoryCredentialsInvalid
	case strings.HasPrefix(reason, "credentials_scope_denied"):
		return ReasonCategoryCredentialsScopeDenied
	case strings.HasPrefix(reason, "credentials_lease_"):
		return ReasonCategoryCredentialsLeaseDenied
	case strings.HasPrefix(reason, "adapter_selection_denied"):
		return ReasonCategoryAdapterSelectionDenied
	case strings.HasPrefix(reason, "failed_"),
		strings.HasPrefix(reason, "venue_runtime_failed_"):
		return ReasonCategoryVenueRuntimeFailure
	case strings.HasPrefix(reason, "control_plane_"):
		return ReasonCategoryControlPlane
	case strings.HasPrefix(reason, "rejected_"):
		return ReasonCategoryExecutionPolicyRejected
	default:
		return ReasonCategoryUnknown
	}
}
