package timescale

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.OIHotReadModelStore = (*PgOIWriter)(nil)

// PgOIWriter persists closed open-interest windows in Timescale.
type PgOIWriter struct {
	exec adapterstorage.SQLExecutor
}

func NewPgOIWriter(pool *Pool) *PgOIWriter {
	if pool == nil {
		return &PgOIWriter{}
	}
	return &PgOIWriter{exec: pool}
}

func NewPgOIWriterWithExecutor(exec adapterstorage.SQLExecutor) *PgOIWriter {
	return &PgOIWriter{exec: exec}
}

func (w *PgOIWriter) SaveOI(ctx context.Context, evt aggdomain.OpenInterestClosed) *problem.Problem {
	if w == nil || w.exec == nil {
		return problem.New(problem.ValidationFailed, "timescale oi writer is nil")
	}
	oi := evt.Window
	const upsertSQL = `
INSERT INTO aggregation_oi (
    venue,
    instrument,
    timeframe,
    window_start,
    window_end,
    open_interest,
    delta,
    delta_pct,
    seq,
    ts_ingest,
    idempotency_key
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (venue, instrument, timeframe, window_start) DO NOTHING`

	idempotencyKey := adapterstorage.WindowIdempotencyKey(oi.Venue, oi.Instrument, oi.Timeframe, oi.WindowStartTs)
	args := []any{
		oi.Venue,
		oi.Instrument,
		oi.Timeframe,
		oi.WindowStartTs,
		oi.WindowEndTs,
		oi.OpenInterest,
		oi.Delta,
		oi.DeltaPct,
		oi.Seq,
		oi.TsIngestMs,
		idempotencyKey,
	}
	if _, p := w.exec.Exec(ctx, upsertSQL, args...); p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale oi upsert failed")
	}

	metrics.IncProcessorCommit("oi_hot")
	return nil
}
