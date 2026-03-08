package domain

import (
	"math"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	EventType    = "execution.event"
	EventVersion = 1
)

type ExecutionStatus string

const (
	ExecutionStatusUnspecified     ExecutionStatus = "unspecified"
	ExecutionStatusAccepted        ExecutionStatus = "accepted"
	ExecutionStatusRejected        ExecutionStatus = "rejected"
	ExecutionStatusPlaced          ExecutionStatus = "placed"
	ExecutionStatusPartiallyFilled ExecutionStatus = "partially_filled"
	ExecutionStatusFilled          ExecutionStatus = "filled"
	ExecutionStatusCanceled        ExecutionStatus = "canceled"
	ExecutionStatusExpired         ExecutionStatus = "expired"
	ExecutionStatusFailed          ExecutionStatus = "failed"
)

type ExecutionCorrelation struct {
	IntentID      string `json:"intent_id"`
	OrderID       string `json:"order_id"`
	VenueOrderID  string `json:"venue_order_id"`
	ClientOrderID string `json:"client_order_id"`
	Venue         string `json:"venue"`
	Symbol        string `json:"symbol"`
	AccountID     string `json:"account_id"`
}

type GovernanceRef struct {
	GrantID   string `json:"grant_id"`
	AdapterID string `json:"adapter_id"`
	Mode      string `json:"mode"`
	Decision  string `json:"decision"` // "allowed", "denied_authorization", "denied_adapter", "denied_credential", "denied_control_plane"
}

type ExecutionProvenance struct {
	CorrelationID string        `json:"correlation_id"`
	TraceID       string        `json:"trace_id"`
	Source        string        `json:"source"`
	GovernanceRef GovernanceRef `json:"governance_ref"`
}

type ExecutionEventV1 struct {
	EventID             string               `json:"event_id"`
	Status              ExecutionStatus      `json:"status"`
	Correlation         ExecutionCorrelation `json:"correlation"`
	TsEventMs           int64                `json:"ts_event_ms"`
	TsExchangeMs        int64                `json:"ts_exchange_ms"`
	ExecutionSeq        int64                `json:"execution_seq"`
	Attempt             int32                `json:"attempt"`
	RequestedQty        float64              `json:"requested_qty"`
	CumulativeFilledQty float64              `json:"cumulative_filled_qty"`
	LastFillQty         float64              `json:"last_fill_qty"`
	LeavesQty           float64              `json:"leaves_qty"`
	LimitPrice          float64              `json:"limit_price"`
	AvgFillPrice        float64              `json:"avg_fill_price"`
	LastFillPrice       float64              `json:"last_fill_price"`
	Reason              string               `json:"reason"`
	Provenance          ExecutionProvenance  `json:"provenance"`
}

//nolint:gocyclo // explicit checks keep runtime validation deterministic.
func (e ExecutionEventV1) Validate() *problem.Problem {
	if strings.TrimSpace(e.EventID) == "" {
		return problem.New(problem.ValidationFailed, "event_id must not be empty")
	}
	switch e.Status {
	case ExecutionStatusAccepted,
		ExecutionStatusRejected,
		ExecutionStatusPlaced,
		ExecutionStatusPartiallyFilled,
		ExecutionStatusFilled,
		ExecutionStatusCanceled,
		ExecutionStatusExpired,
		ExecutionStatusFailed:
	default:
		return problem.New(problem.ValidationFailed, "status must be set")
	}
	if strings.TrimSpace(e.Correlation.IntentID) == "" {
		return problem.New(problem.ValidationFailed, "correlation.intent_id must not be empty")
	}
	if strings.TrimSpace(e.Correlation.OrderID) == "" {
		return problem.New(problem.ValidationFailed, "correlation.order_id must not be empty")
	}
	if strings.TrimSpace(e.Correlation.Venue) == "" {
		return problem.New(problem.ValidationFailed, "correlation.venue must not be empty")
	}
	if strings.TrimSpace(e.Correlation.Symbol) == "" {
		return problem.New(problem.ValidationFailed, "correlation.symbol must not be empty")
	}
	if e.TsEventMs <= 0 {
		return problem.New(problem.ValidationFailed, "ts_event_ms must be > 0")
	}
	if e.ExecutionSeq <= 0 {
		return problem.New(problem.ValidationFailed, "execution_seq must be > 0")
	}
	if e.Attempt <= 0 {
		return problem.New(problem.ValidationFailed, "attempt must be > 0")
	}
	if !finite(e.RequestedQty) {
		return problem.New(problem.ValidationFailed, "requested_qty must be finite")
	}
	if e.Status != ExecutionStatusRejected && e.RequestedQty == 0 {
		return problem.New(problem.ValidationFailed, "requested_qty must be non-zero except for rejected status")
	}
	if !finite(e.CumulativeFilledQty) || !finite(e.LastFillQty) || !finite(e.LeavesQty) {
		return problem.New(problem.ValidationFailed, "filled/leaves quantities must be finite")
	}
	if e.Status == ExecutionStatusRejected {
		if e.CumulativeFilledQty != 0 || e.LastFillQty != 0 {
			return problem.New(problem.ValidationFailed, "rejected status must not carry fill quantities")
		}
	}
	if !finiteNonNegative(e.LimitPrice) || !finiteNonNegative(e.AvgFillPrice) || !finiteNonNegative(e.LastFillPrice) {
		return problem.New(problem.ValidationFailed, "price fields must be finite and >= 0")
	}
	if strings.TrimSpace(e.Provenance.CorrelationID) == "" {
		return problem.New(problem.ValidationFailed, "provenance.correlation_id must not be empty")
	}
	if strings.TrimSpace(e.Provenance.Source) == "" {
		return problem.New(problem.ValidationFailed, "provenance.source must not be empty")
	}
	return nil
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func finiteNonNegative(v float64) bool {
	return finite(v) && v >= 0
}
