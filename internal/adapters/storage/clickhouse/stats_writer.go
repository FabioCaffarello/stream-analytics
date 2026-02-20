package clickhouse

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.StatsHotReadModelStore = (*ChStatsWriter)(nil)

// ChStatsWriter persists closed stats windows in ClickHouse for cold analytics.
type ChStatsWriter struct {
	preparer batchPreparer
}

func NewChStatsWriter(pool *Pool) *ChStatsWriter {
	if pool == nil {
		return &ChStatsWriter{}
	}
	return &ChStatsWriter{preparer: pool}
}

func NewChStatsWriterWithPreparer(preparer BatchPreparer) *ChStatsWriter {
	return &ChStatsWriter{preparer: preparer}
}

func (w *ChStatsWriter) SaveStats(ctx context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
	if w == nil || w.preparer == nil {
		return problem.New(problem.ValidationFailed, "clickhouse stats writer is nil")
	}
	s := evt.Stats
	const insertSQL = `
INSERT INTO aggregation_stats_cold (
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
)`

	markOpen, markHigh, markLow, markClose := adapterstorage.NullableMarkPrice(s)
	fundingAvg, fundingLast := adapterstorage.NullableFundingRate(s)
	idempotencyKey := adapterstorage.WindowIdempotencyKey(s.Venue, s.Instrument, s.Timeframe, s.WindowStartTs)

	batch, p := w.preparer.PrepareInsert(ctx, insertSQL)
	if p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse stats prepare batch failed")
	}
	defer func() {
		_ = batch.Close()
	}()

	if p := batch.AppendRow(
		ctx,
		s.Venue,
		s.Instrument,
		s.Timeframe,
		s.WindowStartTs,
		s.WindowEndTs,
		s.LiqBuyVolume,
		s.LiqSellVolume,
		s.LiqTotalVolume,
		s.LiqCount,
		markOpen,
		markHigh,
		markLow,
		markClose,
		fundingAvg,
		fundingLast,
		s.SeqFirst,
		s.SeqLast,
		idempotencyKey,
	); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse stats append failed")
	}
	if _, p := batch.Flush(ctx); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse stats batch send failed")
	}

	metrics.IncProcessorCommit("stats_cold")
	return nil
}
