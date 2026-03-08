package domain

// PortfolioStateQuery specifies a query for venue-scoped portfolio states.
// Mirrors ports.PortfolioStateQuery for protobuf-first wire contracts.
type PortfolioStateQuery struct {
	AccountID string `json:"account_id"`
	Venue     string `json:"venue,omitempty"`
	Symbol    string `json:"symbol,omitempty"`
	Limit     int32  `json:"limit,omitempty"`
}

// AccountSnapshotQuery specifies a query for account-scoped snapshots.
// Mirrors ports.AccountSnapshotQuery for protobuf-first wire contracts.
type AccountSnapshotQuery struct {
	AccountID string `json:"account_id,omitempty"`
	FromMs    int64  `json:"from_ms,omitempty"`
	ToMs      int64  `json:"to_ms,omitempty"`
	Limit     int32  `json:"limit,omitempty"`
}

// PortfolioSummaryQuery specifies a query for global portfolio summaries.
// Mirrors ports.PortfolioSummaryQuery for protobuf-first wire contracts.
type PortfolioSummaryQuery struct {
	FromMs int64 `json:"from_ms,omitempty"`
	ToMs   int64 `json:"to_ms,omitempty"`
	Limit  int32 `json:"limit,omitempty"`
}
