package ports

import (
	"context"

	"github.com/market-raccoon/internal/core/portfolio/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// PortfolioStateQuery specifies a query for venue-scoped portfolio states.
type PortfolioStateQuery struct {
	AccountID string // required — scopes the query
	Venue     string // optional — filter by venue (empty = all venues)
	Symbol    string // optional — filter by instrument
	Limit     int    // max results (0 = default cap)
}

// PortfolioStateReader reads persisted venue-scoped portfolio states.
type PortfolioStateReader interface {
	GetPortfolioStates(ctx context.Context, q PortfolioStateQuery) ([]domain.PortfolioStateV1, *problem.Problem)
	GetLatestPortfolioState(ctx context.Context, accountID, venue, symbol string) (domain.PortfolioStateV1, *problem.Problem)
}

// AccountSnapshotQuery specifies a query for account-scoped snapshots.
type AccountSnapshotQuery struct {
	AccountID string
	FromMs    int64 // inclusive lower bound
	ToMs      int64 // exclusive upper bound
	Limit     int
}

// AccountSnapshotReader reads persisted account-level snapshots.
type AccountSnapshotReader interface {
	GetAccountSnapshots(ctx context.Context, q AccountSnapshotQuery) ([]domain.AccountSnapshotV1, *problem.Problem)
	GetLatestAccountSnapshot(ctx context.Context, accountID string) (domain.AccountSnapshotV1, *problem.Problem)
}

// PortfolioSummaryQuery specifies a query for global portfolio summaries.
type PortfolioSummaryQuery struct {
	FromMs int64
	ToMs   int64
	Limit  int
}

// PortfolioSummaryReader reads persisted global portfolio summaries.
type PortfolioSummaryReader interface {
	GetPortfolioSummaries(ctx context.Context, q PortfolioSummaryQuery) ([]domain.PortfolioSummaryV1, *problem.Problem)
	GetLatestPortfolioSummary(ctx context.Context) (domain.PortfolioSummaryV1, *problem.Problem)
}
