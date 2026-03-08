package domain

import "testing"

func TestExecutionDecisionRecord_ConstructionDispatched(t *testing.T) {
	rec := ExecutionDecisionRecord{
		IntentID:          "intent-abc",
		ObservedAtMs:      1_700_000_002_000,
		ControlPlaneGate:  "passed",
		AuthorizationGate: "authorized",
		AdapterGate:       "selected",
		CredentialGate:    "satisfied",
		FinalDecision:     "dispatched",
		FinalReason:       "",
		GovernanceRef: GovernanceRef{
			GrantID:   "grant-1",
			AdapterID: "binance.spot",
			Mode:      "real_adapter_safe",
			Decision:  "allowed",
		},
	}
	if rec.IntentID != "intent-abc" {
		t.Fatalf("intent_id=%q want=%q", rec.IntentID, "intent-abc")
	}
	if rec.FinalDecision != "dispatched" {
		t.Fatalf("final_decision=%q want=%q", rec.FinalDecision, "dispatched")
	}
	if rec.FinalReason != "" {
		t.Fatalf("final_reason=%q want empty", rec.FinalReason)
	}
	if rec.GovernanceRef.GrantID != "grant-1" {
		t.Fatalf("governance_ref.grant_id=%q want=%q", rec.GovernanceRef.GrantID, "grant-1")
	}
	if rec.GovernanceRef.Decision != "allowed" {
		t.Fatalf("governance_ref.decision=%q want=%q", rec.GovernanceRef.Decision, "allowed")
	}
}

func TestExecutionDecisionRecord_ConstructionRejected(t *testing.T) {
	rec := ExecutionDecisionRecord{
		IntentID:          "intent-xyz",
		ObservedAtMs:      1_700_000_003_000,
		ControlPlaneGate:  "passed",
		AuthorizationGate: ReasonGovernanceSizeTooLarge,
		AdapterGate:       "",
		CredentialGate:    "",
		FinalDecision:     "rejected",
		FinalReason:       ReasonGovernanceSizeTooLarge,
		GovernanceRef: GovernanceRef{
			GrantID:   "grant-2",
			AdapterID: "",
			Mode:      "",
			Decision:  "denied_authorization",
		},
	}
	if rec.FinalDecision != "rejected" {
		t.Fatalf("final_decision=%q want=%q", rec.FinalDecision, "rejected")
	}
	if rec.FinalReason != ReasonGovernanceSizeTooLarge {
		t.Fatalf("final_reason=%q want=%q", rec.FinalReason, ReasonGovernanceSizeTooLarge)
	}
	if rec.GovernanceRef.Decision != "denied_authorization" {
		t.Fatalf("governance_ref.decision=%q want=%q", rec.GovernanceRef.Decision, "denied_authorization")
	}
}

func TestExecutionDecisionRecord_ZeroValue(t *testing.T) {
	var rec ExecutionDecisionRecord
	if rec.IntentID != "" {
		t.Fatalf("zero value intent_id=%q want empty", rec.IntentID)
	}
	if rec.FinalDecision != "" {
		t.Fatalf("zero value final_decision=%q want empty", rec.FinalDecision)
	}
	if rec.GovernanceRef.GrantID != "" {
		t.Fatalf("zero value governance_ref.grant_id=%q want empty", rec.GovernanceRef.GrantID)
	}
}

func TestGovernanceRef_ZeroValue(t *testing.T) {
	var ref GovernanceRef
	if ref.GrantID != "" || ref.AdapterID != "" || ref.Mode != "" || ref.Decision != "" {
		t.Fatal("zero value GovernanceRef should have all empty fields")
	}
}
