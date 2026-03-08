package domain

// ExecutionDecisionRecord captures the full governance evaluation result for a single intent.
// Used for audit trail, compliance, and debugging.
type ExecutionDecisionRecord struct {
	IntentID          string        `json:"intent_id"`
	ObservedAtMs      int64         `json:"observed_at_ms"`
	ControlPlaneGate  string        `json:"control_plane_gate"` // "passed", or denial reason
	AuthorizationGate string        `json:"authorization_gate"` // "authorized", or denial reason
	AdapterGate       string        `json:"adapter_gate"`       // "selected", or denial reason
	CredentialGate    string        `json:"credential_gate"`    // "satisfied", or denial reason
	FinalDecision     string        `json:"final_decision"`     // "dispatched" or "rejected"
	FinalReason       string        `json:"final_reason"`       // empty if dispatched, reason if rejected
	GovernanceRef     GovernanceRef `json:"governance_ref"`
}
