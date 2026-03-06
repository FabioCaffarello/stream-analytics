package clickhouse

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.CVDHotReadModelStore = (*ChCVDWriter)(nil)

// ChCVDWriter persists closed CVD windows in ClickHouse cold storage.
type ChCVDWriter struct {
	preparer batchPreparer
}

func NewChCVDWriter(pool *Pool) *ChCVDWriter {
	if pool == nil {
		return &ChCVDWriter{}
	}
	return &ChCVDWriter{preparer: pool}
}

func NewChCVDWriterWithPreparer(preparer BatchPreparer) *ChCVDWriter {
	return &ChCVDWriter{preparer: preparer}
}

func (w *ChCVDWriter) SaveCVD(ctx context.Context, evt aggdomain.CVDClosed) *problem.Problem {
	if w == nil || w.preparer == nil {
		return problem.New(problem.ValidationFailed, "clickhouse cvd writer is nil")
	}
	c := evt.Window
	const insertSQL = `
INSERT INTO aggregation_cvd_cold (
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
)`

	batch, p := w.preparer.PrepareInsert(ctx, insertSQL)
	if p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse cvd prepare batch failed")
	}
	defer func() {
		_ = batch.Close()
	}()

	idempotencyKey := adapterstorage.WindowIdempotencyKey(c.Venue, c.Instrument, c.Timeframe, c.WindowStartTs)
	if p := batch.AppendRow(ctx,
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
	); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse cvd append failed")
	}
	if _, p := batch.Flush(ctx); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse cvd batch send failed")
	}

	metrics.IncProcessorCommit("cvd_cold")
	return nil
}
