package governance

import "strings"

type ExecutionScope struct {
	AllowAnyVenue   bool
	AllowAnySymbol  bool
	AllowAnyAccount bool

	AllowedVenues   map[string]struct{}
	AllowedSymbols  map[string]struct{}
	AllowedAccounts map[string]struct{}
}

func (s ExecutionScope) AllowsVenue(venue string) bool {
	if s.AllowAnyVenue {
		return true
	}
	_, ok := s.AllowedVenues[strings.ToLower(strings.TrimSpace(venue))]
	return ok
}

func (s ExecutionScope) AllowsSymbol(symbol string) bool {
	if s.AllowAnySymbol {
		return true
	}
	_, ok := s.AllowedSymbols[strings.ToUpper(strings.TrimSpace(symbol))]
	return ok
}

func (s ExecutionScope) AllowsAccount(accountID string) bool {
	if s.AllowAnyAccount {
		return true
	}
	_, ok := s.AllowedAccounts[strings.TrimSpace(accountID)]
	return ok
}

type ExecutionLimits struct {
	MaxIntentTTLms int64
	MaxAbsQuantity float64
	MaxNotionalUSD float64
	MaxSlippageBps float64
}

type ExecutionLease struct {
	LeaseID      string
	ValidUntilMs int64
}

type GrantProvenance struct {
	Source   string
	PolicyID string
}

type ExecutionGrant struct {
	GrantID    string
	Boundary   string
	AdapterID  string
	Mode       string
	SafeMode   bool
	TradeOnly  bool
	Scope      ExecutionScope
	Limits     ExecutionLimits
	Lease      ExecutionLease
	Provenance GrantProvenance
}

type AuthorizationDecision struct {
	Authorized bool
	Grant      ExecutionGrant
	Reason     string
}

type CredentialRequirement struct {
	Required            bool
	Boundary            string
	AdapterID           string
	Mode                string
	Scope               string
	TradeOnly           bool
	Venue               string
	AccountID           string
	Symbol              string
	AcceptedResolverIDs []string
	AcceptedProviderIDs []string
}

func (r CredentialRequirement) AcceptsResolver(resolverID string) bool {
	if !r.Required {
		return true
	}
	resolverID = strings.ToLower(strings.TrimSpace(resolverID))
	if resolverID == "" || len(r.AcceptedResolverIDs) == 0 {
		return false
	}
	for _, raw := range r.AcceptedResolverIDs {
		if strings.ToLower(strings.TrimSpace(raw)) == resolverID {
			return true
		}
	}
	return false
}

func (r CredentialRequirement) AcceptsProvider(providerID string) bool {
	if !r.Required {
		return true
	}
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	if providerID == "" || len(r.AcceptedProviderIDs) == 0 {
		return false
	}
	for _, raw := range r.AcceptedProviderIDs {
		if strings.ToLower(strings.TrimSpace(raw)) == providerID {
			return true
		}
	}
	return false
}

type CredentialAvailabilityStatus string

const (
	CredentialAvailabilityUnspecified CredentialAvailabilityStatus = "unspecified"
	CredentialAvailabilityNotRequired CredentialAvailabilityStatus = "not_required"
	CredentialAvailabilityAvailable   CredentialAvailabilityStatus = "available"
	CredentialAvailabilityUnavailable CredentialAvailabilityStatus = "unavailable"
)

type CredentialResolutionStatus string

const (
	CredentialResolutionUnspecified CredentialResolutionStatus = "unspecified"
	CredentialResolutionNotRequired CredentialResolutionStatus = "not_required"
	CredentialResolutionResolved    CredentialResolutionStatus = "resolved"
	CredentialResolutionDenied      CredentialResolutionStatus = "denied"
)

type CredentialLeaseState string

const (
	CredentialLeaseStateUnspecified CredentialLeaseState = "unspecified"
	CredentialLeaseStateNotRequired CredentialLeaseState = "not_required"
	CredentialLeaseStateActive      CredentialLeaseState = "active"
	CredentialLeaseStateExpired     CredentialLeaseState = "expired"
	CredentialLeaseStateRevoked     CredentialLeaseState = "revoked"
	CredentialLeaseStateInvalid     CredentialLeaseState = "invalid"
)

type CredentialLease struct {
	LeaseID      string
	State        CredentialLeaseState
	IssuedAtMs   int64
	ValidUntilMs int64
}

func (l CredentialLease) ActiveAt(observedAtMs int64) bool {
	if l.State != CredentialLeaseStateActive {
		return false
	}
	if observedAtMs > 0 && l.ValidUntilMs > 0 && observedAtMs > l.ValidUntilMs {
		return false
	}
	return true
}

type CredentialProvenance struct {
	ResolverID      string
	ProviderID      string
	SourceType      string
	SourceRef       string
	RevocationReady bool
}

type ResolvedCredential struct {
	Boundary   string
	AdapterID  string
	Mode       string
	Scope      string
	TradeOnly  bool
	Venue      string
	AccountID  string
	Symbol     string
	Lease      CredentialLease
	Provenance CredentialProvenance
}

//nolint:gocyclo // Credential fitness keeps each boundary/scope/provenance check explicit for fail-closed behavior.
func (c ResolvedCredential) FitsAt(requirement CredentialRequirement, observedAtMs int64) bool {
	if !requirement.Required {
		return true
	}
	if strings.TrimSpace(c.Boundary) == "" || strings.TrimSpace(c.AdapterID) == "" || strings.TrimSpace(c.Mode) == "" {
		return false
	}
	if requirement.Boundary != "" && !strings.EqualFold(strings.TrimSpace(requirement.Boundary), strings.TrimSpace(c.Boundary)) {
		return false
	}
	if requirement.AdapterID != "" && !strings.EqualFold(strings.TrimSpace(requirement.AdapterID), strings.TrimSpace(c.AdapterID)) {
		return false
	}
	if requirement.Mode != "" && !strings.EqualFold(strings.TrimSpace(requirement.Mode), strings.TrimSpace(c.Mode)) {
		return false
	}
	if requirement.TradeOnly && !c.TradeOnly {
		return false
	}
	if requirement.Scope != "" && !strings.EqualFold(strings.TrimSpace(requirement.Scope), strings.TrimSpace(c.Scope)) {
		return false
	}
	if requirement.Venue != "" && !strings.EqualFold(strings.TrimSpace(requirement.Venue), strings.TrimSpace(c.Venue)) {
		return false
	}
	if requirement.AccountID != "" && strings.TrimSpace(requirement.AccountID) != strings.TrimSpace(c.AccountID) {
		return false
	}
	if requirement.Symbol != "" && !strings.EqualFold(strings.TrimSpace(requirement.Symbol), strings.TrimSpace(c.Symbol)) {
		return false
	}
	if !requirement.AcceptsResolver(c.Provenance.ResolverID) {
		return false
	}
	if !requirement.AcceptsProvider(c.Provenance.ProviderID) {
		return false
	}
	return c.Lease.ActiveAt(observedAtMs)
}

type CredentialResolution struct {
	Availability  CredentialAvailabilityStatus
	Status        CredentialResolutionStatus
	EvaluatedAtMs int64
	Requirement   CredentialRequirement
	Credential    ResolvedCredential
	Reason        string
}

func (r CredentialResolution) Satisfied() bool {
	if r.Status == CredentialResolutionNotRequired {
		return true
	}
	if r.Status != CredentialResolutionResolved || r.Availability != CredentialAvailabilityAvailable {
		return false
	}
	return r.Credential.FitsAt(r.Requirement, r.EvaluatedAtMs)
}

type AdapterSelectionDecision struct {
	Selected              bool
	Boundary              string
	AdapterID             string
	Mode                  string
	CredentialRequirement CredentialRequirement
	Reason                string
}

type Outcome struct {
	Authorization AuthorizationDecision
	Adapter       AdapterSelectionDecision
	Credential    CredentialResolution
}

func (o Outcome) Allowed() bool {
	return o.Authorization.Authorized && o.Adapter.Selected && o.Credential.Satisfied()
}
