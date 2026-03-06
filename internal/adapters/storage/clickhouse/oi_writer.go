package clickhouse

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.OIHotReadModelStore = (*ChOIWriter)(nil)

// ChOIWriter persists closed open-interest windows in ClickHouse cold storage.
type ChOIWriter struct {
	preparer batchPreparer
}

func NewChOIWriter(pool *Pool) *ChOIWriter {
	if pool == nil {
		return &ChOIWriter{}
	}
	return &ChOIWriter{preparer: pool}
}

func NewChOIWriterWithPreparer(preparer BatchPreparer) *ChOIWriter {
	return &ChOIWriter{preparer: preparer}
}

func (w *ChOIWriter) SaveOI(ctx context.Context, evt aggdomain.OpenInterestClosed) *problem.Problem {
	if w == nil || w.preparer == nil {
		return problem.New(problem.ValidationFailed, "clickhouse oi writer is nil")
	}
	oi := evt.Window
	const insertSQL = `
INSERT INTO aggregation_oi_cold (
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
)`

	batch, p := w.preparer.PrepareInsert(ctx, insertSQL)
	if p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse oi prepare batch failed")
	}
	defer func() {
		_ = batch.Close()
	}()

	idempotencyKey := adapterstorage.WindowIdempotencyKey(oi.Venue, oi.Instrument, oi.Timeframe, oi.WindowStartTs)
	if p := batch.AppendRow(ctx,
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
	); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse oi append failed")
	}
	if _, p := batch.Flush(ctx); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse oi batch send failed")
	}

	metrics.IncProcessorCommit("oi_cold")
	return nil
}
