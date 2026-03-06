package timescale

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.DeltaVolumeHotReadModelStore = (*PgDeltaVolumeWriter)(nil)

// PgDeltaVolumeWriter persists closed delta-volume windows in Timescale.
type PgDeltaVolumeWriter struct {
	exec adapterstorage.SQLExecutor
}

func NewPgDeltaVolumeWriter(pool *Pool) *PgDeltaVolumeWriter {
	if pool == nil {
		return &PgDeltaVolumeWriter{}
	}
	return &PgDeltaVolumeWriter{exec: pool}
}

func NewPgDeltaVolumeWriterWithExecutor(exec adapterstorage.SQLExecutor) *PgDeltaVolumeWriter {
	return &PgDeltaVolumeWriter{exec: exec}
}

func (w *PgDeltaVolumeWriter) SaveDeltaVolume(ctx context.Context, evt aggdomain.DeltaVolumeClosed) *problem.Problem {
	if w == nil || w.exec == nil {
		return problem.New(problem.ValidationFailed, "timescale delta_volume writer is nil")
	}
	dv := evt.Window
	const upsertSQL = `
INSERT INTO aggregation_delta_volume (
    venue,
    instrument,
    timeframe,
    window_start,
    window_end,
    buy_volume,
    sell_volume,
    delta_volume,
    seq,
    ts_ingest,
    idempotency_key
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (venue, instrument, timeframe, window_start) DO NOTHING`

	idempotencyKey := adapterstorage.WindowIdempotencyKey(dv.Venue, dv.Instrument, dv.Timeframe, dv.WindowStartTs)
	args := []any{
		dv.Venue,
		dv.Instrument,
		dv.Timeframe,
		dv.WindowStartTs,
		dv.WindowEndTs,
		dv.BuyVolume,
		dv.SellVolume,
		dv.DeltaVolume,
		dv.Seq,
		dv.TsIngestMs,
		idempotencyKey,
	}
	if _, p := w.exec.Exec(ctx, upsertSQL, args...); p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale delta_volume upsert failed")
	}

	metrics.IncProcessorCommit("delta_volume_hot")
	return nil
}
