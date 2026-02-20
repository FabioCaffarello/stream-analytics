package clickhouse

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.CandleHotReadModelStore = (*ChCandleWriter)(nil)

// ChCandleWriter persists closed candles in ClickHouse for cold analytics.
type ChCandleWriter struct {
	preparer batchPreparer
}

func NewChCandleWriter(pool *Pool) *ChCandleWriter {
	if pool == nil {
		return &ChCandleWriter{}
	}
	return &ChCandleWriter{preparer: pool}
}

func NewChCandleWriterWithPreparer(preparer BatchPreparer) *ChCandleWriter {
	return &ChCandleWriter{preparer: preparer}
}

func (w *ChCandleWriter) SaveCandle(ctx context.Context, evt aggdomain.CandleClosed) *problem.Problem {
	if w == nil || w.preparer == nil {
		return problem.New(problem.ValidationFailed, "clickhouse candle writer is nil")
	}
	c := evt.Candle
	const insertSQL = `
INSERT INTO aggregation_candle_cold (
    venue,
    instrument,
    timeframe,
    window_start,
    window_end,
    open_price,
    high_price,
    low_price,
    close_price,
    volume,
    buy_volume,
    sell_volume,
    trade_count,
    seq_first,
    seq_last,
    idempotency_key
)`

	idempotencyKey := adapterstorage.WindowIdempotencyKey(c.Venue, c.Instrument, c.Timeframe, c.WindowStartTs)

	batch, p := w.preparer.PrepareInsert(ctx, insertSQL)
	if p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse candle prepare batch failed")
	}
	defer func() {
		_ = batch.Close()
	}()

	if p := batch.AppendRow(
		ctx,
		c.Venue,
		c.Instrument,
		c.Timeframe,
		c.WindowStartTs,
		c.WindowEndTs,
		c.Open,
		c.High,
		c.Low,
		c.ClosePrice,
		c.Volume,
		c.BuyVolume,
		c.SellVolume,
		c.TradeCount,
		c.SeqFirst,
		c.SeqLast,
		idempotencyKey,
	); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse candle append failed")
	}
	if _, p := batch.Flush(ctx); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse candle batch send failed")
	}

	metrics.IncProcessorCommit("candle_cold")
	return nil
}
