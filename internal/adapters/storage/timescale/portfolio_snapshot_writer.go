package timescale

import (
	"context"
	"encoding/json"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	domain "github.com/market-raccoon/internal/core/portfolio/domain"
	ports "github.com/market-raccoon/internal/core/portfolio/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ ports.AccountSnapshotWriter = (*PgAccountSnapshotWriter)(nil)

// PgAccountSnapshotWriter persists account-scoped aggregation snapshots in Timescale.
type PgAccountSnapshotWriter struct {
	exec adapterstorage.SQLExecutor
}

func NewPgAccountSnapshotWriter(pool *Pool) *PgAccountSnapshotWriter {
	if pool == nil {
		return &PgAccountSnapshotWriter{}
	}
	return &PgAccountSnapshotWriter{exec: pool}
}

func NewPgAccountSnapshotWriterWithExecutor(exec adapterstorage.SQLExecutor) *PgAccountSnapshotWriter {
	return &PgAccountSnapshotWriter{exec: exec}
}

func (w *PgAccountSnapshotWriter) UpsertAccountSnapshot(ctx context.Context, snap domain.AccountSnapshotV1) *problem.Problem {
	if w == nil || w.exec == nil {
		return problem.New(problem.ValidationFailed, "timescale account snapshot writer is nil")
	}
	if p := snap.Validate(); p != nil {
		return p
	}

	venuesJSON, err := json.Marshal(snap.Venues)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal venues failed")
	}
	fillJSON, err := json.Marshal(snap.FillSummary)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal fill_summary failed")
	}

	const upsertSQL = `
INSERT INTO portfolio_account_snapshot (
    snapshot_id, account_id, projected_at_ms,
    total_equity_usd, total_realized_usd, total_unrealized,
    total_margin_used, total_leverage,
    venues, fill_summary
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (account_id, snapshot_id) DO UPDATE SET
    projected_at_ms = EXCLUDED.projected_at_ms,
    total_equity_usd = EXCLUDED.total_equity_usd,
    total_realized_usd = EXCLUDED.total_realized_usd,
    total_unrealized = EXCLUDED.total_unrealized,
    total_margin_used = EXCLUDED.total_margin_used,
    total_leverage = EXCLUDED.total_leverage,
    venues = EXCLUDED.venues,
    fill_summary = EXCLUDED.fill_summary`

	args := []any{
		snap.SnapshotID,
		snap.AccountID,
		snap.ProjectedAtMs,
		snap.TotalEquityUSD,
		snap.TotalRealizedUSD,
		snap.TotalUnrealized,
		snap.TotalMarginUsed,
		snap.TotalLeverage,
		venuesJSON,
		fillJSON,
	}

	if _, p := w.exec.Exec(ctx, upsertSQL, args...); p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale account snapshot upsert failed")
	}
	return nil
}
