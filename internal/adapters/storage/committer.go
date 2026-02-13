package storage

import (
	"context"
	"errors"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

// SnapshotCommitter persists aggregation snapshots in hot and cold paths.
// Commit is successful only after both writes succeed.
type SnapshotCommitter struct {
	hot  aggports.HotReadModelStore
	cold aggports.ColdReadModelStore
}

func NewSnapshotCommitter(hot aggports.HotReadModelStore, cold aggports.ColdReadModelStore) *SnapshotCommitter {
	return &SnapshotCommitter{hot: hot, cold: cold}
}

func (c *SnapshotCommitter) Commit(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	if c == nil {
		return problem.New(problem.ValidationFailed, "snapshot committer is nil")
	}
	if c.hot == nil {
		return problem.New(problem.ValidationFailed, "hot writer is nil")
	}
	if c.cold == nil {
		return problem.New(problem.ValidationFailed, "cold writer is nil")
	}
	if p := c.hot.Save(ctx, snap); p != nil {
		return p
	}
	if p := c.cold.Save(ctx, snap); p != nil {
		return p
	}
	return nil
}

// CommitAndAck enforces ack-on-commit boundary: ack is called only after
// both storage writes have committed successfully.
func CommitAndAck(ctx context.Context, committer *SnapshotCommitter, snap aggdomain.SnapshotProduced, ack func() error) *problem.Problem {
	if p := committer.Commit(ctx, snap); p != nil {
		return p
	}
	if ack == nil {
		return nil
	}
	if err := ack(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return problem.WithRetryable(problem.Wrap(err, problem.Unavailable, "ack after commit failed"))
		}
		return problem.Wrap(err, problem.Unavailable, "ack after commit failed")
	}
	return nil
}
