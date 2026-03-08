package domain

// EquityCurvePointV1 represents a single point on a temporal equity curve.
// Built from historical account snapshots or portfolio summaries.
type EquityCurvePointV1 struct {
	TimestampMs   int64   `json:"timestamp_ms"`
	EquityUSD     float64 `json:"equity_usd"`
	RealizedUSD   float64 `json:"realized_usd"`
	UnrealizedUSD float64 `json:"unrealized_usd"`
	MarginUsedUSD float64 `json:"margin_used_usd"`
	PositionCount int32   `json:"position_count"`
	DrawdownPct   float64 `json:"drawdown_pct"`
}

// ReconciliationSeverity indicates the severity of a reconciliation finding.
type ReconciliationSeverity string

const (
	SeverityInfo    ReconciliationSeverity = "info"
	SeverityWarning ReconciliationSeverity = "warning"
	SeverityError   ReconciliationSeverity = "error"
)

// ReconciliationFindingKind classifies the type of inconsistency detected.
type ReconciliationFindingKind string

const (
	FindingSeqGap          ReconciliationFindingKind = "seq_gap"
	FindingEquityDrift     ReconciliationFindingKind = "equity_drift"
	FindingStaleProjection ReconciliationFindingKind = "stale_projection"
	FindingOrphanState     ReconciliationFindingKind = "orphan_state"
	FindingDuplicateState  ReconciliationFindingKind = "duplicate_state"
	FindingPnLMismatch     ReconciliationFindingKind = "pnl_mismatch"
)

// ReconciliationFinding describes a single detected inconsistency.
type ReconciliationFinding struct {
	Kind        ReconciliationFindingKind `json:"kind"`
	Severity    ReconciliationSeverity    `json:"severity"`
	AccountID   string                    `json:"account_id,omitempty"`
	Venue       string                    `json:"venue,omitempty"`
	Symbol      string                    `json:"symbol,omitempty"`
	Message     string                    `json:"message"`
	ExpectedSeq int64                     `json:"expected_seq,omitempty"`
	ActualSeq   int64                     `json:"actual_seq,omitempty"`
	DriftUSD    float64                   `json:"drift_usd,omitempty"`
	TimestampMs int64                     `json:"timestamp_ms,omitempty"`
}

// ReconciliationReportV1 is the output of a reconciliation check run.
// It is read-only — it never alters portfolio state.
type ReconciliationReportV1 struct {
	ReportID       string                  `json:"report_id"`
	RunAtMs        int64                   `json:"run_at_ms"`
	AccountID      string                  `json:"account_id,omitempty"`
	ScopeDesc      string                  `json:"scope_desc"`
	Findings       []ReconciliationFinding `json:"findings"`
	TotalStates    int32                   `json:"total_states"`
	TotalSnapshots int32                   `json:"total_snapshots"`
	CheckedFromMs  int64                   `json:"checked_from_ms"`
	CheckedToMs    int64                   `json:"checked_to_ms"`
	Healthy        bool                    `json:"healthy"`
}

// ReconciliationQuery specifies the scope of a reconciliation check.
type ReconciliationQuery struct {
	AccountID string `json:"account_id"`
	FromMs    int64  `json:"from_ms,omitempty"`
	ToMs      int64  `json:"to_ms,omitempty"`
}
