package domain

// TradingStatus indicates the operational readiness of a venue for execution.
type TradingStatus string

const (
	TradingStatusEnabled  TradingStatus = "enabled"  // Active, no restrictions.
	TradingStatusDegraded TradingStatus = "degraded" // Active, but adapters disabled or venue restricted.
	TradingStatusDisabled TradingStatus = "disabled" // Paused or drained — no new executions.
	TradingStatusHalted   TradingStatus = "halted"   // Emergency stop — all rejected.
)

// VenueReadiness is the per-venue trading readiness assessment.
type VenueReadiness struct {
	Venue           string        `json:"venue"`
	TradingStatus   TradingStatus `json:"trading_status"`
	PositionCount   int32         `json:"position_count"`
	EquityUSD       float64       `json:"equity_usd"`
	LastProjectedMs int64         `json:"last_projected_ms"`
	Stale           bool          `json:"stale"`
	Restricted      bool          `json:"restricted"` // venue excluded by allowlist override
}

// AccountReadiness is the per-account trading readiness view.
type AccountReadiness struct {
	AccountID     string           `json:"account_id"`
	Venues        []VenueReadiness `json:"venues"`
	EquityUSD     float64          `json:"equity_usd"`
	PositionCount int32            `json:"position_count"`
	Stale         bool             `json:"stale"`
}

// ControlPlaneReadiness is the control plane section of the readiness surface.
type ControlPlaneReadiness struct {
	State               string   `json:"state"`
	SimulationProfile   string   `json:"simulation_profile,omitempty"`
	DisabledStrategies  []string `json:"disabled_strategies"`
	DisabledAdapters    []string `json:"disabled_adapters"`
	AllowlistRestricted bool     `json:"allowlist_restricted"`
	RestrictedVenues    []string `json:"restricted_venues,omitempty"`
	RestrictedSymbols   []string `json:"restricted_symbols,omitempty"`
	UpdatedAtMs         int64    `json:"updated_at_ms"`
}

// TradingReadinessV1 is the composed trading readiness surface.
// It integrates control plane state with portfolio staleness assessment.
// Portfolio does NOT own this — it is a query-side composition at the boundary.
type TradingReadinessV1 struct {
	ControlPlane         ControlPlaneReadiness `json:"control_plane"`
	Accounts             []AccountReadiness    `json:"accounts"`
	SafetyFlags          []string              `json:"safety_flags"`
	EvaluatedAtMs        int64                 `json:"evaluated_at_ms"`
	StalenessThresholdMs int64                 `json:"staleness_threshold_ms"` // server-authoritative threshold for portfolio staleness
}

// BaseTradingStatus derives the base trading status from a control plane state.
func BaseTradingStatus(state ControlState) TradingStatus {
	switch state {
	case ControlStateActive:
		return TradingStatusEnabled
	case ControlStatePaused, ControlStateDrained:
		return TradingStatusDisabled
	case ControlStateHalted:
		return TradingStatusHalted
	default:
		return TradingStatusHalted // fail-closed
	}
}
