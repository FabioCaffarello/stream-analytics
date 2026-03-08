package domain

import (
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

// AccountSnapshotV1 aggregates all venue-scoped portfolio states under one account.
// This is a read model — not a wire type. Built from accumulated PortfolioStateV1.
type AccountSnapshotV1 struct {
	SnapshotID       string            `json:"snapshot_id"`
	AccountID        string            `json:"account_id"`
	ProjectedAtMs    int64             `json:"projected_at_ms"`
	Venues           []VenuePositionV1 `json:"venues"`
	TotalEquityUSD   float64           `json:"total_equity_usd"`
	TotalRealizedUSD float64           `json:"total_realized_usd"`
	TotalUnrealized  float64           `json:"total_unrealized_usd"`
	TotalMarginUsed  float64           `json:"total_margin_used_usd"`
	TotalLeverage    float64           `json:"total_leverage"`
	FillSummary      FillSummaryV1     `json:"fill_summary"`
}

// VenuePositionV1 groups positions and balances from a single venue within an account.
type VenuePositionV1 struct {
	Venue            string       `json:"venue"`
	Positions        []PositionV1 `json:"positions"`
	Balances         []BalanceV1  `json:"balances"`
	EquityUSD        float64      `json:"equity_usd"`
	RealizedPnlUSD   float64      `json:"realized_pnl_usd"`
	UnrealizedPnlUSD float64      `json:"unrealized_pnl_usd"`
	MarginUsedUSD    float64      `json:"margin_used_usd"`
}

func (s AccountSnapshotV1) Validate() *problem.Problem {
	if strings.TrimSpace(s.SnapshotID) == "" {
		return problem.New(problem.ValidationFailed, "snapshot_id must not be empty")
	}
	if strings.TrimSpace(s.AccountID) == "" {
		return problem.New(problem.ValidationFailed, "account_id must not be empty")
	}
	if s.ProjectedAtMs <= 0 {
		return problem.New(problem.ValidationFailed, "projected_at_ms must be > 0")
	}
	if len(s.Venues) == 0 {
		return problem.New(problem.ValidationFailed, "venues must not be empty")
	}
	if !finite(s.TotalEquityUSD) || !finite(s.TotalRealizedUSD) || !finite(s.TotalUnrealized) {
		return problem.New(problem.ValidationFailed, "aggregate USD fields must be finite")
	}
	return nil
}

// PortfolioSummaryV1 aggregates all accounts into a global operational view.
// This is a read model — not a wire type.
type PortfolioSummaryV1 struct {
	SummaryID          string             `json:"summary_id"`
	ProjectedAtMs      int64              `json:"projected_at_ms"`
	Accounts           []AccountSummaryV1 `json:"accounts"`
	GlobalEquityUSD    float64            `json:"global_equity_usd"`
	GlobalRealizedUSD  float64            `json:"global_realized_usd"`
	GlobalUnrealized   float64            `json:"global_unrealized_usd"`
	GlobalMarginUsed   float64            `json:"global_margin_used_usd"`
	GlobalLeverage     float64            `json:"global_leverage"`
	TotalPositionCount int32              `json:"total_position_count"`
	TotalOpenOrders    int32              `json:"total_open_orders"`
	FillSummary        FillSummaryV1      `json:"fill_summary"`
}

// AccountSummaryV1 is a lightweight per-account summary within a global portfolio view.
type AccountSummaryV1 struct {
	AccountID        string  `json:"account_id"`
	VenueCount       int32   `json:"venue_count"`
	PositionCount    int32   `json:"position_count"`
	EquityUSD        float64 `json:"equity_usd"`
	RealizedPnlUSD   float64 `json:"realized_pnl_usd"`
	UnrealizedPnlUSD float64 `json:"unrealized_pnl_usd"`
}

func (s PortfolioSummaryV1) Validate() *problem.Problem {
	if strings.TrimSpace(s.SummaryID) == "" {
		return problem.New(problem.ValidationFailed, "summary_id must not be empty")
	}
	if s.ProjectedAtMs <= 0 {
		return problem.New(problem.ValidationFailed, "projected_at_ms must be > 0")
	}
	if !finite(s.GlobalEquityUSD) || !finite(s.GlobalRealizedUSD) || !finite(s.GlobalUnrealized) {
		return problem.New(problem.ValidationFailed, "global USD fields must be finite")
	}
	return nil
}
