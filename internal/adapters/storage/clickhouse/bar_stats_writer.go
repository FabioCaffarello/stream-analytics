package clickhouse

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.BarStatsHotReadModelStore = (*ChBarStatsWriter)(nil)

// ChBarStatsWriter persists closed bar-statistics windows in ClickHouse cold storage.
type ChBarStatsWriter struct {
	preparer batchPreparer
}

func NewChBarStatsWriter(pool *Pool) *ChBarStatsWriter {
	if pool == nil {
		return &ChBarStatsWriter{}
	}
	return &ChBarStatsWriter{preparer: pool}
}

func NewChBarStatsWriterWithPreparer(preparer BatchPreparer) *ChBarStatsWriter {
	return &ChBarStatsWriter{preparer: preparer}
}

func (w *ChBarStatsWriter) SaveBarStats(ctx context.Context, evt aggdomain.BarStatsClosed) *problem.Problem {
	if w == nil || w.preparer == nil {
		return problem.New(problem.ValidationFailed, "clickhouse bar_stats writer is nil")
	}
	b := evt.Window
	const insertSQL = `
INSERT INTO aggregation_bar_stats_cold (
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
)`

	batch, p := w.preparer.PrepareInsert(ctx, insertSQL)
	if p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse bar_stats prepare batch failed")
	}
	defer func() {
		_ = batch.Close()
	}()

	idempotencyKey := adapterstorage.WindowIdempotencyKey(b.Venue, b.Instrument, b.Timeframe, b.WindowStartTs)
	var isBurst uint8
	if b.IsBurst {
		isBurst = 1
	}
	if p := batch.AppendRow(ctx,
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
		isBurst,
		b.Seq,
		b.TsIngestMs,
		idempotencyKey,
	); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse bar_stats append failed")
	}
	if _, p := batch.Flush(ctx); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse bar_stats batch send failed")
	}

	metrics.IncProcessorCommit("bar_stats_cold")
	return nil
}
