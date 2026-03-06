package timescale

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.BarStatsHotReadModelStore = (*PgBarStatsWriter)(nil)

// PgBarStatsWriter persists closed bar-statistics windows in Timescale.
type PgBarStatsWriter struct {
	exec adapterstorage.SQLExecutor
}

func NewPgBarStatsWriter(pool *Pool) *PgBarStatsWriter {
	if pool == nil {
		return &PgBarStatsWriter{}
	}
	return &PgBarStatsWriter{exec: pool}
}

func NewPgBarStatsWriterWithExecutor(exec adapterstorage.SQLExecutor) *PgBarStatsWriter {
	return &PgBarStatsWriter{exec: exec}
}

func (w *PgBarStatsWriter) SaveBarStats(ctx context.Context, evt aggdomain.BarStatsClosed) *problem.Problem {
	if w == nil || w.exec == nil {
		return problem.New(problem.ValidationFailed, "timescale bar_stats writer is nil")
	}
	b := evt.Window
	const upsertSQL = `
INSERT INTO aggregation_bar_stats (
    venue,
    instrument,
    timeframe,
    window_start,
    window_end,
    trade_count,
    buy_count,
    sell_count,
    total_volume,
    buy_volume,
    sell_volume,
    vwap_price,
    last_price,
    max_price,
    min_price,
    imbalance,
    is_burst,
    seq,
    ts_ingest,
    idempotency_key
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
ON CONFLICT (venue, instrument, timeframe, window_start) DO NOTHING`

	idempotencyKey := adapterstorage.WindowIdempotencyKey(b.Venue, b.Instrument, b.Timeframe, b.WindowStartTs)
	args := []any{
		b.Venue,
		b.Instrument,
		b.Timeframe,
		b.WindowStartTs,
		b.WindowEndTs,
		b.TradeCount,
		b.BuyCount,
		b.SellCount,
		b.TotalVolume,
		b.BuyVolume,
		b.SellVolume,
		b.VwapPrice,
		b.LastPrice,
		b.MaxPrice,
		b.MinPrice,
		b.Imbalance,
		b.IsBurst,
		b.Seq,
		b.TsIngestMs,
		idempotencyKey,
	}
	if _, p := w.exec.Exec(ctx, upsertSQL, args...); p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale bar_stats upsert failed")
	}

	metrics.IncProcessorCommit("bar_stats_hot")
	return nil
}
