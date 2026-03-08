package domain

// PortfolioEventContract describes one portfolio event type.
type PortfolioEventContract struct {
	Type       string // stable event type string
	Version    int    // schema version
	OwnerBC    string // bounded context that owns the schema
	ProducerBC string // bounded context that emits the event
}

// PortfolioEventCatalog is the canonical registry of all portfolio event types.
//
// Governance rules:
//   - OwnerBC owns the schema; ProducerBC may differ when an actor wraps the projector.
//   - Version must match delivery envelope_policy registration.
//   - Adding a new entry here requires a matching delivery policy entry.
var PortfolioEventCatalog = []PortfolioEventContract{
	{Type: StateEventType, Version: StateEventVersion, OwnerBC: "portfolio", ProducerBC: "portfolio"},
	{Type: AccountSnapshotEventType, Version: AccountSnapshotEventVersion, OwnerBC: "portfolio", ProducerBC: "portfolio"},
	{Type: SummaryEventType, Version: SummaryEventVersion, OwnerBC: "portfolio", ProducerBC: "portfolio"},
}

// PortfolioEventCatalogByType returns a map keyed by event type for O(1) lookup.
func PortfolioEventCatalogByType() map[string]PortfolioEventContract {
	m := make(map[string]PortfolioEventContract, len(PortfolioEventCatalog))
	for _, e := range PortfolioEventCatalog {
		m[e.Type] = e
	}
	return m
}

const (
	// AccountSnapshotEventType is the stable event type for account-level snapshots.
	AccountSnapshotEventType = "portfolio.account_snapshot"
	// AccountSnapshotEventVersion is the schema version for account snapshots.
	AccountSnapshotEventVersion = 1

	// SummaryEventType is the stable event type for global portfolio summaries.
	SummaryEventType = "portfolio.summary"
	// SummaryEventVersion is the schema version for portfolio summaries.
	SummaryEventVersion = 1
)

// PortfolioReadModelCatalog lists non-wire read model types produced by this context.
// These are query-side projections with protobuf contracts for wire queries.
var PortfolioReadModelCatalog = []string{
	AccountSnapshotEventType,
	SummaryEventType,
}
