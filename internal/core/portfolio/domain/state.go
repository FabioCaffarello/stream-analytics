package domain

import (
	"math"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	StateEventType    = "portfolio.state"
	StateEventVersion = 1
)

type PortfolioScope string

const (
	PortfolioScopeUnspecified  PortfolioScope = "unspecified"
	PortfolioScopeGlobal       PortfolioScope = "global"
	PortfolioScopeAccount      PortfolioScope = "account"
	PortfolioScopeVenueAccount PortfolioScope = "venue_account"
)

type BalanceV1 struct {
	Asset     string  `json:"asset"`
	Total     float64 `json:"total"`
	Available float64 `json:"available"`
	Locked    float64 `json:"locked"`
}

type PositionV1 struct {
	Venue         string  `json:"venue"`
	Symbol        string  `json:"symbol"`
	Quantity      float64 `json:"quantity"`
	AvgEntryPrice float64 `json:"avg_entry_price"`
	NotionalUSD   float64 `json:"notional_usd"`
	RealizedPnL   float64 `json:"realized_pnl"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
}

type ExposureV1 struct {
	Symbol           string  `json:"symbol"`
	NetQty           float64 `json:"net_qty"`
	GrossNotionalUSD float64 `json:"gross_notional_usd"`
	Leverage         float64 `json:"leverage"`
}

type RiskSnapshotV1 struct {
	MarginUsedUSD        float64 `json:"margin_used_usd"`
	MarginAvailableUSD   float64 `json:"margin_available_usd"`
	MaintenanceMarginUSD float64 `json:"maintenance_margin_usd"`
	Var95USD             float64 `json:"var_95_usd"`
}

type ProjectionProvenanceV1 struct {
	SourceExecutionEventID string `json:"source_execution_event_id"`
	SourceExecutionSeq     int64  `json:"source_execution_seq"`
	CorrelationID          string `json:"correlation_id"`
	TraceID                string `json:"trace_id"`
	ProjectorVersion       string `json:"projector_version"`
}

type PortfolioStateV1 struct {
	StateID          string                 `json:"state_id"`
	Scope            PortfolioScope         `json:"scope"`
	AccountID        string                 `json:"account_id"`
	Venue            string                 `json:"venue"`
	ProjectedAtMs    int64                  `json:"projected_at_ms"`
	Balances         []BalanceV1            `json:"balances"`
	Positions        []PositionV1           `json:"positions"`
	Exposures        []ExposureV1           `json:"exposures"`
	EquityUSD        float64                `json:"equity_usd"`
	RealizedPnlUSD   float64                `json:"realized_pnl_usd"`
	UnrealizedPnlUSD float64                `json:"unrealized_pnl_usd"`
	Risk             RiskSnapshotV1         `json:"risk"`
	Provenance       ProjectionProvenanceV1 `json:"provenance"`
}

func (s PortfolioStateV1) Validate() *problem.Problem {
	if strings.TrimSpace(s.StateID) == "" {
		return problem.New(problem.ValidationFailed, "state_id must not be empty")
	}
	switch s.Scope {
	case PortfolioScopeGlobal, PortfolioScopeAccount, PortfolioScopeVenueAccount:
	default:
		return problem.New(problem.ValidationFailed, "scope must be set")
	}
	if s.ProjectedAtMs <= 0 {
		return problem.New(problem.ValidationFailed, "projected_at_ms must be > 0")
	}
	if len(s.Positions) == 0 {
		return problem.New(problem.ValidationFailed, "positions must not be empty")
	}
	if len(s.Exposures) == 0 {
		return problem.New(problem.ValidationFailed, "exposures must not be empty")
	}
	if !finite(s.EquityUSD) || !finite(s.RealizedPnlUSD) || !finite(s.UnrealizedPnlUSD) {
		return problem.New(problem.ValidationFailed, "equity/pnl fields must be finite")
	}
	if strings.TrimSpace(s.Provenance.SourceExecutionEventID) == "" {
		return problem.New(problem.ValidationFailed, "provenance.source_execution_event_id must not be empty")
	}
	if s.Provenance.SourceExecutionSeq <= 0 {
		return problem.New(problem.ValidationFailed, "provenance.source_execution_seq must be > 0")
	}
	if strings.TrimSpace(s.Provenance.CorrelationID) == "" {
		return problem.New(problem.ValidationFailed, "provenance.correlation_id must not be empty")
	}
	if strings.TrimSpace(s.Provenance.ProjectorVersion) == "" {
		return problem.New(problem.ValidationFailed, "provenance.projector_version must not be empty")
	}
	return nil
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
