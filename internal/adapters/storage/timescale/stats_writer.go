package timescale

import (
	"context"
	"strconv"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
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

	markOpen, markHigh, markLow, markClose := nullableMarkPrice(s)
	fundingAvg, fundingLast := nullableFundingRate(s)

	idempotencyKey := sharedhash.HashFields(
		s.Venue,
		s.Instrument,
		s.Timeframe,
		strconv.FormatInt(s.WindowStartTs, 10),
	)

	if _, p := w.exec.Exec(
		ctx,
		upsertSQL,
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
		return problem.Wrap(p, problem.Unavailable, "timescale stats upsert failed")
	}

	metrics.IncProcessorCommit("stats_hot")
	return nil
}

func nullableMarkPrice(s aggdomain.StatsWindowV1) (any, any, any, any) {
	if s.MarkPriceOpen <= 0 || s.MarkPriceHigh <= 0 || s.MarkPriceLow <= 0 || s.MarkPriceClose <= 0 {
		return nil, nil, nil, nil
	}
	return s.MarkPriceOpen, s.MarkPriceHigh, s.MarkPriceLow, s.MarkPriceClose
}

func nullableFundingRate(s aggdomain.StatsWindowV1) (any, any) {
	if s.FundingRateAvg == 0 && s.FundingRateLast == 0 {
		return nil, nil
	}
	return s.FundingRateAvg, s.FundingRateLast
}
