package storage

import (
	"context"
	"errors"
	"time"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/observability"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
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
	started := time.Now()
	if c == nil {
		p := problem.New(problem.ValidationFailed, "snapshot committer is nil")
		observability.SetCommitterErr(p)
		metrics.IncProcessorCommit("failed")
		return p
	}
	if c.hot == nil {
		p := problem.New(problem.ValidationFailed, "hot writer is nil")
		observability.SetHotErr(p)
		observability.SetCommitterErr(p)
		metrics.IncProcessorCommit("failed")
		return p
	}
	if c.cold == nil {
		p := problem.New(problem.ValidationFailed, "cold writer is nil")
		observability.SetColdErr(p)
		observability.SetCommitterErr(p)
		metrics.IncProcessorCommit("failed")
		return p
	}
	if p := c.hot.Save(ctx, snap); p != nil {
		observability.SetHotErr(p)
		observability.SetCommitterErr(p)
		metrics.IncProcessorCommit("failed")
		metrics.ObserveProcessorCommitLatency(time.Since(started))
		return p
	}
	observability.SetHotOk()
	if p := c.cold.Save(ctx, snap); p != nil {
		observability.SetColdErr(p)
		observability.SetCommitterErr(p)
		metrics.IncProcessorCommit("failed")
		metrics.ObserveProcessorCommitLatency(time.Since(started))
		return p
	}
	observability.SetColdOk()
	observability.SetCommitterOk()
	metrics.IncProcessorCommit("ok")
	metrics.ObserveProcessorCommitLatency(time.Since(started))
	return nil
}

// CommitAndAck enforces ack-on-commit boundary: ack is called only after
// both storage writes have committed successfully.
func CommitAndAck(ctx context.Context, committer *SnapshotCommitter, snap aggdomain.SnapshotProduced, ack func() error) *problem.Problem {
	if committer == nil {
		return problem.New(problem.ValidationFailed, "snapshot committer is nil")
	}
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
