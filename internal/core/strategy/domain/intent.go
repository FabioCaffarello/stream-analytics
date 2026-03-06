package domain

import (
	"math"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	IntentEventType    = "strategy.intent"
	IntentEventVersion = 1
)

type IntentSide string

const (
	IntentSideUnspecified IntentSide = "unspecified"
	IntentSideBuy         IntentSide = "buy"
	IntentSideSell        IntentSide = "sell"
)

type SizingMode string

const (
	SizingModeUnspecified       SizingMode = "unspecified"
	SizingModeBaseQuantity      SizingMode = "base_quantity"
	SizingModeQuoteNotionalUSD  SizingMode = "quote_notional_usd"
	SizingModeTargetExposurePct SizingMode = "target_exposure_pct"
)

type OrderType string

const (
	OrderTypeUnspecified OrderType = "unspecified"
	OrderTypeMarket      OrderType = "market"
	OrderTypeLimit       OrderType = "limit"
)

type TimeInForce string

const (
	TimeInForceUnspecified TimeInForce = "unspecified"
	TimeInForceGTC         TimeInForce = "gtc"
	TimeInForceIOC         TimeInForce = "ioc"
	TimeInForceFOK         TimeInForce = "fok"
)

type StrategyRef struct {
	StrategyID         string `json:"strategy_id"`
	StrategyVersion    string `json:"strategy_version"`
	StrategyInstanceID string `json:"strategy_instance_id"`
}

type IntentScope struct {
	Venue     string `json:"venue"`
	Symbol    string `json:"symbol"`
	AccountID string `json:"account_id"`
}

type SizingIntent struct {
	Mode           SizingMode `json:"mode"`
	Value          float64    `json:"value"`
	MaxNotionalUSD float64    `json:"max_notional_usd"`
}

type ExecutionConstraints struct {
	OrderType      OrderType   `json:"order_type"`
	TimeInForce    TimeInForce `json:"time_in_force"`
	LimitPrice     float64     `json:"limit_price"`
	MaxSlippageBps float64     `json:"max_slippage_bps"`
	PostOnly       bool        `json:"post_only"`
	ReduceOnly     bool        `json:"reduce_only"`
}

type IntentProvenance struct {
	Reason          string   `json:"reason"`
	CorrelationID   string   `json:"correlation_id"`
	TraceID         string   `json:"trace_id"`
	ParentSignalIDs []string `json:"parent_signal_ids"`
	PolicyHash      string   `json:"policy_hash"`
}

type StrategyIntentV1 struct {
	IntentID    string               `json:"intent_id"`
	Strategy    StrategyRef          `json:"strategy"`
	Scope       IntentScope          `json:"scope"`
	Side        IntentSide           `json:"side"`
	Sizing      SizingIntent         `json:"sizing"`
	Constraints ExecutionConstraints `json:"constraints"`
	CreatedAtMs int64                `json:"created_at_ms"`
	ExpiresAtMs int64                `json:"expires_at_ms"`
	Provenance  IntentProvenance     `json:"provenance"`
}

//nolint:gocyclo // explicit branch-per-invariant keeps failures audit-friendly.
func (i StrategyIntentV1) Validate() *problem.Problem {
	if strings.TrimSpace(i.IntentID) == "" {
		return problem.New(problem.ValidationFailed, "intent_id must not be empty")
	}
	if strings.TrimSpace(i.Strategy.StrategyID) == "" {
		return problem.New(problem.ValidationFailed, "strategy.strategy_id must not be empty")
	}
	if strings.TrimSpace(i.Strategy.StrategyVersion) == "" {
		return problem.New(problem.ValidationFailed, "strategy.strategy_version must not be empty")
	}
	if strings.TrimSpace(i.Scope.Venue) == "" {
		return problem.New(problem.ValidationFailed, "scope.venue must not be empty")
	}
	if strings.TrimSpace(i.Scope.Symbol) == "" {
		return problem.New(problem.ValidationFailed, "scope.symbol must not be empty")
	}
	switch i.Side {
	case IntentSideBuy, IntentSideSell:
	default:
		return problem.New(problem.ValidationFailed, "side must be buy|sell")
	}
	switch i.Sizing.Mode {
	case SizingModeBaseQuantity, SizingModeQuoteNotionalUSD, SizingModeTargetExposurePct:
	default:
		return problem.New(problem.ValidationFailed, "sizing.mode must be set")
	}
	if !finitePositive(i.Sizing.Value) {
		return problem.New(problem.ValidationFailed, "sizing.value must be > 0")
	}
	if !finiteNonNegative(i.Sizing.MaxNotionalUSD) {
		return problem.New(problem.ValidationFailed, "sizing.max_notional_usd must be >= 0")
	}
	switch i.Constraints.OrderType {
	case OrderTypeMarket, OrderTypeLimit:
	default:
		return problem.New(problem.ValidationFailed, "constraints.order_type must be market|limit")
	}
	switch i.Constraints.TimeInForce {
	case TimeInForceGTC, TimeInForceIOC, TimeInForceFOK:
	default:
		return problem.New(problem.ValidationFailed, "constraints.time_in_force must be gtc|ioc|fok")
	}
	if !finiteNonNegative(i.Constraints.LimitPrice) {
		return problem.New(problem.ValidationFailed, "constraints.limit_price must be >= 0")
	}
	if i.Constraints.OrderType == OrderTypeLimit && !finitePositive(i.Constraints.LimitPrice) {
		return problem.New(problem.ValidationFailed, "constraints.limit_price must be > 0 for limit orders")
	}
	if !finiteNonNegative(i.Constraints.MaxSlippageBps) {
		return problem.New(problem.ValidationFailed, "constraints.max_slippage_bps must be >= 0")
	}
	if i.CreatedAtMs <= 0 {
		return problem.New(problem.ValidationFailed, "created_at_ms must be > 0")
	}
	if i.ExpiresAtMs <= i.CreatedAtMs {
		return problem.New(problem.ValidationFailed, "expires_at_ms must be > created_at_ms")
	}
	if strings.TrimSpace(i.Provenance.CorrelationID) == "" {
		return problem.New(problem.ValidationFailed, "provenance.correlation_id must not be empty")
	}
	if strings.TrimSpace(i.Provenance.PolicyHash) == "" {
		return problem.New(problem.ValidationFailed, "provenance.policy_hash must not be empty")
	}
	if len(i.Provenance.ParentSignalIDs) == 0 {
		return problem.New(problem.ValidationFailed, "provenance.parent_signal_ids must not be empty")
	}
	for _, id := range i.Provenance.ParentSignalIDs {
		if strings.TrimSpace(id) == "" {
			return problem.New(problem.ValidationFailed, "provenance.parent_signal_ids entries must not be empty")
		}
	}
	return nil
}

func finitePositive(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0
}

func finiteNonNegative(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}
