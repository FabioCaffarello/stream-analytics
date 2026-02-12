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
}

// HotReadModelStore is the write port for the in-memory hot read model.
// Implementations keep the latest snapshot for low-latency reads.
type HotReadModelStore interface {
	Save(ctx context.Context, snap domain.SnapshotProduced) *problem.Problem
}
