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
	governance   executionports.ExecutionGovernance
	adapters     map[string]executionports.IntentExecutor
	source       string
	controlPlane executionports.ControlPlane // optional, nil = no control plane
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
		governance:   cfg.Governance,
		adapters:     adapters,
		source:       source,
		controlPlane: cfg.ControlPlane,
	}
}

func (e *GovernedExecutor) BoundaryInfo() executionports.BoundaryInfo {
	if e == nil || e.governance == nil {
		return executionports.BoundaryInfo{}
	}
	return e.governance.BoundaryInfo()
}

func (e *GovernedExecutor) ExecuteAt(intent strategydomain.StrategyIntentV1, observedAtMs int64) []executiondomain.ExecutionEventV1 {
	if e == nil || e.governance == nil {
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, executiondomain.ReasonGovernanceNoGrant)}
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
			return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, reason)}
		}
	}

	outcome := e.governance.Evaluate(intent, observedAtMs)
	if !outcome.Authorization.Authorized {
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, outcome.Authorization.Reason)}
	}
	if !outcome.Adapter.Selected {
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, outcome.Adapter.Reason)}
	}
	if !outcome.Credential.Satisfied() {
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, outcome.Credential.Reason)}
	}

	adapterKey := strings.ToLower(strings.TrimSpace(outcome.Adapter.AdapterID))
	executor := e.adapters[adapterKey]
	if executor == nil {
		return []executiondomain.ExecutionEventV1{e.rejectEvent(intent, observedAtMs, executiondomain.ReasonAdapterSelectionUnavailable)}
	}
	return executor.ExecuteAt(intent, observedAtMs)
}

func (e *GovernedExecutor) rejectEvent(intent strategydomain.StrategyIntentV1, observedAtMs int64, reason string) executiondomain.ExecutionEventV1 {
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
