// Package ports defines storage write interfaces for the portfolio context.
package ports

import (
	"context"

	"github.com/market-raccoon/internal/core/portfolio/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// PortfolioStateWriter persists venue-scoped portfolio state projections.
type PortfolioStateWriter interface {
	UpsertPortfolioState(ctx context.Context, state domain.PortfolioStateV1) *problem.Problem
}

// AccountSnapshotWriter persists account-scoped aggregation snapshots.
type AccountSnapshotWriter interface {
	UpsertAccountSnapshot(ctx context.Context, snap domain.AccountSnapshotV1) *problem.Problem
}

// PortfolioSummaryWriter persists global portfolio summary snapshots.
type PortfolioSummaryWriter interface {
	UpsertPortfolioSummary(ctx context.Context, sum domain.PortfolioSummaryV1) *problem.Problem
}
