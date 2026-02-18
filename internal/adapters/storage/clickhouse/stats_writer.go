package clickhouse

import (
	"context"
	"strconv"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
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
    idempotency_key,
    created_at
)`

	markOpen, markHigh, markLow, markClose := statsNullableMarkPrice(s)
	fundingAvg, fundingLast := statsNullableFundingRate(s)

	idempotencyKey := sharedhash.HashFields(
		s.Venue,
		s.Instrument,
		s.Timeframe,
		strconv.FormatInt(s.WindowStartTs, 10),
	)

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

func statsNullableMarkPrice(s aggdomain.StatsWindowV1) (any, any, any, any) {
	if s.MarkPriceOpen <= 0 || s.MarkPriceHigh <= 0 || s.MarkPriceLow <= 0 || s.MarkPriceClose <= 0 {
		return nil, nil, nil, nil
	}
	return s.MarkPriceOpen, s.MarkPriceHigh, s.MarkPriceLow, s.MarkPriceClose
}

func statsNullableFundingRate(s aggdomain.StatsWindowV1) (any, any) {
	if s.FundingRateAvg == 0 && s.FundingRateLast == 0 {
		return nil, nil
	}
	return s.FundingRateAvg, s.FundingRateLast
}
