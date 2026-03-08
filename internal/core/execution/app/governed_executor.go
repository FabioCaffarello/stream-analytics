package app

import (
	"math"
	"strconv"
	"strings"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	executionports "github.com/market-raccoon/internal/core/execution/ports"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

type GovernedExecutor struct {
	governance         executionports.ExecutionGovernance
	adapters           map[string]executionports.IntentExecutor
	source             string
	controlPlane       executionports.ControlPlane // optional, nil = no control plane
	idempotency        *idempotencyCache
	adapterHealth      *adapterHealth
	lastDecisionRecord executiondomain.ExecutionDecisionRecord
}

type GovernedExecutorConfig struct {
	Governance   executionports.ExecutionGovernance
	Adapters     map[string]executionports.IntentExecutor
	Source       string
	ControlPlane executionports.ControlPlane // optional
}

func NewGovernedExecutor(cfg GovernedExecutorConfig) *GovernedExecutor {
	adapters := make(map[string]executionports.IntentExecutor, len(cfg.Adapters))
	for raw, executor := range cfg.Adapters {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key == "" || executor == nil {
			continue
		}
		adapters[key] = executor
	}
	source := strings.TrimSpace(cfg.Source)
	if source == "" {
		source = "executor.governance.v1"
	}
	return &GovernedExecutor{
		governance:    cfg.Governance,
		adapters:      adapters,
		source:        source,
		controlPlane:  cfg.ControlPlane,
		idempotency:   newIdempotencyCache(4096),
		adapterHealth: newAdapterHealth(5, 30_000),
	}
}

func (e *GovernedExecutor) BoundaryInfo() executionports.BoundaryInfo {
	if e == nil || e.governance == nil {
		return executionports.BoundaryInfo{}
	}
	return e.governance.BoundaryInfo()
}

func (e *GovernedExecutor) LastDecisionRecord() executiondomain.ExecutionDecisionRecord {
	return e.lastDecisionRecord
}

// AdapterHealthSnapshots returns a read-only view of all tracked adapter circuit states.
func (e *GovernedExecutor) AdapterHealthSnapshots() map[string]AdapterHealthSnapshot {
	if e == nil || e.adapterHealth == nil {
		return nil
	}
	return e.adapterHealth.snapshot()
}

func (e *GovernedExecutor) ExecuteAt(intent strategydomain.StrategyIntentV1, observedAtMs int64) []executiondomain.ExecutionEventV1 {
	rec := executiondomain.ExecutionDecisionRecord{
		IntentID:     intent.IntentID,
		ObservedAtMs: observedAtMs,
	}

	// Idempotency guard: reject duplicate intents before any governance evaluation.
	if e != nil && e.idempotency != nil && e.idempotency.seen(intent.IntentID, observedAtMs) {
		govRef := executiondomain.GovernanceRef{Decision: "denied_duplicate"}
		rec.ControlPlaneGate = "skipped"
		rec.AuthorizationGate = "skipped"
		rec.FinalDecision = "rejected"
		rec.FinalReason = executiondomain.ReasonDuplicateIntent
		rec.GovernanceRef = govRef
		e.lastDecisionRecord = rec
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, executiondomain.ReasonDuplicateIntent, govRef)}
	}

	if e == nil || e.governance == nil {
		govRef := executiondomain.GovernanceRef{Decision: "denied_authorization"}
		rec.ControlPlaneGate = "passed"
		rec.AuthorizationGate = executiondomain.ReasonGovernanceNoGrant
		rec.FinalDecision = "rejected"
		rec.FinalReason = executiondomain.ReasonGovernanceNoGrant
		rec.GovernanceRef = govRef
		e.lastDecisionRecord = rec
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, executiondomain.ReasonGovernanceNoGrant, govRef)}
	}

	if e.controlPlane != nil {
		snap := e.controlPlane.Snapshot()
		allowed, reason := snap.IsExecutionAllowed(
			intent.Strategy.StrategyID,
			"", // adapter not known yet at pre-flight
			intent.Scope.Venue,
			intent.Scope.Symbol,
		)
		if !allowed {
			govRef := executiondomain.GovernanceRef{Decision: "denied_control_plane"}
			rec.ControlPlaneGate = reason
			rec.FinalDecision = "rejected"
			rec.FinalReason = reason
			rec.GovernanceRef = govRef
			e.lastDecisionRecord = rec
			return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, reason, govRef)}
		}
	}
	rec.ControlPlaneGate = "passed"

	outcome := e.governance.Evaluate(intent, observedAtMs)

	if !outcome.Authorization.Authorized {
		govRef := executiondomain.GovernanceRef{
			GrantID:  outcome.Authorization.Grant.GrantID,
			Decision: "denied_authorization",
		}
		rec.AuthorizationGate = outcome.Authorization.Reason
		rec.FinalDecision = "rejected"
		rec.FinalReason = outcome.Authorization.Reason
		rec.GovernanceRef = govRef
		e.lastDecisionRecord = rec
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, outcome.Authorization.Reason, govRef)}
	}
	rec.AuthorizationGate = "authorized"

	if !outcome.Adapter.Selected {
		govRef := executiondomain.GovernanceRef{
			GrantID:   outcome.Authorization.Grant.GrantID,
			AdapterID: outcome.Adapter.AdapterID,
			Mode:      outcome.Adapter.Mode,
			Decision:  "denied_adapter",
		}
		rec.AdapterGate = outcome.Adapter.Reason
		rec.FinalDecision = "rejected"
		rec.FinalReason = outcome.Adapter.Reason
		rec.GovernanceRef = govRef
		e.lastDecisionRecord = rec
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, outcome.Adapter.Reason, govRef)}
	}
	rec.AdapterGate = "selected"

	if !outcome.Credential.Satisfied() {
		govRef := executiondomain.GovernanceRef{
			GrantID:   outcome.Authorization.Grant.GrantID,
			AdapterID: outcome.Adapter.AdapterID,
			Mode:      outcome.Adapter.Mode,
			Decision:  "denied_credential",
		}
		rec.CredentialGate = outcome.Credential.Reason
		rec.FinalDecision = "rejected"
		rec.FinalReason = outcome.Credential.Reason
		rec.GovernanceRef = govRef
		e.lastDecisionRecord = rec
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, outcome.Credential.Reason, govRef)}
	}
	rec.CredentialGate = "satisfied"

	adapterKey := strings.ToLower(strings.TrimSpace(outcome.Adapter.AdapterID))
	executor := e.adapters[adapterKey]
	if executor == nil {
		govRef := executiondomain.GovernanceRef{
			GrantID:   outcome.Authorization.Grant.GrantID,
			AdapterID: outcome.Adapter.AdapterID,
			Mode:      outcome.Adapter.Mode,
			Decision:  "denied_adapter",
		}
		rec.AdapterGate = executiondomain.ReasonAdapterSelectionUnavailable
		rec.FinalDecision = "rejected"
		rec.FinalReason = executiondomain.ReasonAdapterSelectionUnavailable
		rec.GovernanceRef = govRef
		e.lastDecisionRecord = rec
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, executiondomain.ReasonAdapterSelectionUnavailable, govRef)}
	}

	// Circuit breaker: reject if adapter has too many consecutive failures.
	if e.adapterHealth.isTripped(adapterKey, observedAtMs) {
		govRef := executiondomain.GovernanceRef{
			GrantID:   outcome.Authorization.Grant.GrantID,
			AdapterID: outcome.Adapter.AdapterID,
			Mode:      outcome.Adapter.Mode,
			Decision:  "denied_adapter",
		}
		rec.AdapterGate = executiondomain.ReasonAdapterSelectionCircuitOpen
		rec.FinalDecision = "rejected"
		rec.FinalReason = executiondomain.ReasonAdapterSelectionCircuitOpen
		rec.GovernanceRef = govRef
		e.lastDecisionRecord = rec
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, executiondomain.ReasonAdapterSelectionCircuitOpen, govRef)}
	}

	// Dispatched successfully — record allowed decision.
	govRef := executiondomain.GovernanceRef{
		GrantID:   outcome.Authorization.Grant.GrantID,
		AdapterID: outcome.Adapter.AdapterID,
		Mode:      outcome.Adapter.Mode,
		Decision:  "allowed",
	}
	rec.FinalDecision = "dispatched"
	rec.GovernanceRef = govRef
	e.lastDecisionRecord = rec

	results := executor.ExecuteAt(intent, observedAtMs)

	// Update adapter health based on execution results.
	for i := range results {
		switch results[i].Status {
		case executiondomain.ExecutionStatusFailed:
			e.adapterHealth.recordFailure(adapterKey, observedAtMs)
		case executiondomain.ExecutionStatusAccepted,
			executiondomain.ExecutionStatusPlaced,
			executiondomain.ExecutionStatusFilled,
			executiondomain.ExecutionStatusPartiallyFilled:
			e.adapterHealth.recordSuccess(adapterKey)
		}
	}

	return results
}

func (e *GovernedExecutor) rejectEvent(intent strategydomain.StrategyIntentV1, observedAtMs int64, reason string, govRef executiondomain.GovernanceRef) executiondomain.ExecutionEventV1 {
	norm := normalizeIntent(intent, observedAtMs)
	streamKey := executionStreamKey(norm.venue, norm.symbol, norm.accountID)
	rejectSeq := int64(1)
	orderID := sharedhash.HashFieldsFast("governed-order", norm.intentID)

	if strings.TrimSpace(reason) == "" {
		reason = executiondomain.ReasonGovernanceNoGrant
	}
	return executiondomain.ExecutionEventV1{
		EventID:      eventID(norm.intentID, executiondomain.ExecutionStatusRejected, rejectSeq),
		Status:       executiondomain.ExecutionStatusRejected,
		Correlation:  buildGovernedCorrelation(norm, orderID),
		TsEventMs:    norm.nowMs,
		TsExchangeMs: 0,
		ExecutionSeq: stableGovernanceSeq(streamKey),
		Attempt:      1,
		RequestedQty: norm.requestedQty,
		LeavesQty:    math.Abs(norm.requestedQty),
		LimitPrice:   norm.limitPrice,
		Reason:       reason,
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: norm.correlationID,
			TraceID:       norm.traceID,
			Source:        e.source,
			GovernanceRef: govRef,
		},
	}
}

func buildGovernedCorrelation(intent normalizedIntent, orderID string) executiondomain.ExecutionCorrelation {
	clientOrderID := "CID-GOV-" + strconv.FormatInt(intent.nowMs, 10)
	if len(clientOrderID) > 32 {
		clientOrderID = clientOrderID[:32]
	}
	return executiondomain.ExecutionCorrelation{
		IntentID:      intent.intentID,
		OrderID:       orderID,
		VenueOrderID:  "",
		ClientOrderID: clientOrderID,
		Venue:         intent.venue,
		Symbol:        intent.symbol,
		AccountID:     intent.accountID,
	}
}

func stableGovernanceSeq(streamKey string) int64 {
	if strings.TrimSpace(streamKey) == "" {
		return 1
	}
	return 1
}

func NewDefaultGovernedBootstrapExecutor() executionports.IntentExecutor {
	grant := DefaultBootstrapGrant()
	bootstrap := NewBootstrapExecutor(DefaultBootstrapConfig())
	governance := NewStaticExecutionGovernance(StaticExecutionGovernanceConfig{
		Authorizer: StaticCapabilityAuthorizer{Grant: &grant},
		Selector: NewStaticAdapterSelector(AdapterRoute{
			Boundary:  grant.Boundary,
			AdapterID: grant.AdapterID,
			Mode:      grant.Mode,
		}),
		BoundaryInfo: executionports.BoundaryInfo{
			Boundary: grant.Boundary,
			Adapter:  grant.AdapterID,
			Mode:     grant.Mode,
		},
	})
	return NewGovernedExecutor(GovernedExecutorConfig{
		Governance: governance,
		Adapters: map[string]executionports.IntentExecutor{
			grant.AdapterID: bootstrap,
		},
	})
}
