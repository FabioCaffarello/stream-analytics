package timescale

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.StatsHotReadModelStore = (*PgStatsWriter)(nil)

// PgStatsWriter persists closed stats windows in Timescale.
type PgStatsWriter struct {
	exec adapterstorage.SQLExecutor
}

func NewPgStatsWriter(pool *Pool) *PgStatsWriter {
	if pool == nil {
		return &PgStatsWriter{}
	}
	return &PgStatsWriter{exec: pool}
}

func NewPgStatsWriterWithExecutor(exec adapterstorage.SQLExecutor) *PgStatsWriter {
	return &PgStatsWriter{exec: exec}
}

func (w *PgStatsWriter) SaveStats(ctx context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
	if w == nil || w.exec == nil {
		return problem.New(problem.ValidationFailed, "timescale stats writer is nil")
	}
	s := evt.Stats
	const upsertSQL = `
INSERT INTO aggregation_stats (
    venue,
    instrument,
    timeframe,
    window_start,
    window_end,
    liq_buy_volume,
    liq_sell_volume,
    liq_total_volume,
    liq_count,
    markprice_open,
    markprice_high,
    markprice_low,
    markprice_close,
    funding_rate_avg,
    funding_rate_last,
    seq_first,
    seq_last,
    idempotency_key
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
ON CONFLICT (venue, instrument, timeframe, window_start) DO NOTHING`

	args, _, p := adapterstorage.MarshalStats(ctx, s)
	if p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale stats marshal failed")
	}
	if _, p := w.exec.Exec(ctx, upsertSQL, args...); p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale stats upsert failed")
	}

	metrics.IncProcessorCommit("stats_hot")
	return nil
}
