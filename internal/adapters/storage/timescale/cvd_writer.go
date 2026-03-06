package timescale

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.CVDHotReadModelStore = (*PgCVDWriter)(nil)

// PgCVDWriter persists closed CVD windows in Timescale.
type PgCVDWriter struct {
	exec adapterstorage.SQLExecutor
}

func NewPgCVDWriter(pool *Pool) *PgCVDWriter {
	if pool == nil {
		return &PgCVDWriter{}
	}
	return &PgCVDWriter{exec: pool}
}

func NewPgCVDWriterWithExecutor(exec adapterstorage.SQLExecutor) *PgCVDWriter {
	return &PgCVDWriter{exec: exec}
}

func (w *PgCVDWriter) SaveCVD(ctx context.Context, evt aggdomain.CVDClosed) *problem.Problem {
	if w == nil || w.exec == nil {
		return problem.New(problem.ValidationFailed, "timescale cvd writer is nil")
	}
	c := evt.Window
	const upsertSQL = `
INSERT INTO aggregation_cvd (
    venue,
    instrument,
    timeframe,
    window_start,
    window_end,
    delta_volume,
    cvd,
    seq,
    ts_ingest,
    idempotency_key
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (venue, instrument, timeframe, window_start) DO NOTHING`

	idempotencyKey := adapterstorage.WindowIdempotencyKey(c.Venue, c.Instrument, c.Timeframe, c.WindowStartTs)
	args := []any{
		c.Venue,
		c.Instrument,
		c.Timeframe,
		c.WindowStartTs,
		c.WindowEndTs,
		c.DeltaVolume,
		c.CVD,
		c.Seq,
		c.TsIngestMs,
		idempotencyKey,
	}
	if _, p := w.exec.Exec(ctx, upsertSQL, args...); p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale cvd upsert failed")
	}

	metrics.IncProcessorCommit("cvd_hot")
	return nil
}
