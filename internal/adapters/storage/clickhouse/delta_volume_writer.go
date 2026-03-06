package clickhouse

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.DeltaVolumeHotReadModelStore = (*ChDeltaVolumeWriter)(nil)

// ChDeltaVolumeWriter persists closed delta-volume windows in ClickHouse cold storage.
type ChDeltaVolumeWriter struct {
	preparer batchPreparer
}

func NewChDeltaVolumeWriter(pool *Pool) *ChDeltaVolumeWriter {
	if pool == nil {
		return &ChDeltaVolumeWriter{}
	}
	return &ChDeltaVolumeWriter{preparer: pool}
}

func NewChDeltaVolumeWriterWithPreparer(preparer BatchPreparer) *ChDeltaVolumeWriter {
	return &ChDeltaVolumeWriter{preparer: preparer}
}

func (w *ChDeltaVolumeWriter) SaveDeltaVolume(ctx context.Context, evt aggdomain.DeltaVolumeClosed) *problem.Problem {
	if w == nil || w.preparer == nil {
		return problem.New(problem.ValidationFailed, "clickhouse delta_volume writer is nil")
	}
	dv := evt.Window
	const insertSQL = `
INSERT INTO aggregation_delta_volume_cold (
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
)`

	batch, p := w.preparer.PrepareInsert(ctx, insertSQL)
	if p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse delta_volume prepare batch failed")
	}
	defer func() {
		_ = batch.Close()
	}()

	idempotencyKey := adapterstorage.WindowIdempotencyKey(dv.Venue, dv.Instrument, dv.Timeframe, dv.WindowStartTs)
	if p := batch.AppendRow(ctx,
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
	); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse delta_volume append failed")
	}
	if _, p := batch.Flush(ctx); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse delta_volume batch send failed")
	}

	metrics.IncProcessorCommit("delta_volume_cold")
	return nil
}
