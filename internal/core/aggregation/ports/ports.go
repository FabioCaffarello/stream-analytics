// Package ports defines secondary port interfaces for the aggregation context.
package ports

import (
	"context"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// ArtifactPublisher publishes derived artifacts (snapshots, events) to the bus.
type ArtifactPublisher interface {
	PublishSnapshot(ctx context.Context, snap domain.SnapshotProduced) *problem.Problem
	PublishInconsistent(ctx context.Context, evt domain.OrderBookInconsistentDetected) *problem.Problem
	PublishCandleClosed(ctx context.Context, evt domain.CandleClosed) *problem.Problem
	PublishStatsClosed(ctx context.Context, evt domain.StatsWindowClosed) *problem.Problem
	PublishTapeClosed(ctx context.Context, evt domain.TapeClosed) *problem.Problem
}

// HotReadModelStore is the write port for the in-memory hot read model.
// Implementations keep the latest snapshot for low-latency reads.
type HotReadModelStore interface {
	Save(ctx context.Context, snap domain.SnapshotProduced) *problem.Problem
}

// ColdReadModelStore archives immutable snapshots for replay/analytics.
// Implementations are expected to enforce idempotency at write boundary.
type ColdReadModelStore interface {
	Save(ctx context.Context, snap domain.SnapshotProduced) *problem.Problem
}

// CandleHotReadModelStore writes closed candles to the hot read model.
type CandleHotReadModelStore interface {
	SaveCandle(ctx context.Context, evt domain.CandleClosed) *problem.Problem
}

// StatsHotReadModelStore writes closed stats windows to the hot read model.
type StatsHotReadModelStore interface {
	SaveStats(ctx context.Context, evt domain.StatsWindowClosed) *problem.Problem
}

// TapeHotReadModelStore writes closed tape windows to the hot read model.
type TapeHotReadModelStore interface {
	SaveTape(ctx context.Context, evt domain.TapeClosed) *problem.Problem
}
