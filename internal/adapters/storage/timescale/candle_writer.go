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

var _ aggports.CandleHotReadModelStore = (*PgCandleWriter)(nil)

// PgCandleWriter persists closed candles in Timescale.
type PgCandleWriter struct {
	exec adapterstorage.SQLExecutor
}

func NewPgCandleWriter(pool *Pool) *PgCandleWriter {
	if pool == nil {
		return &PgCandleWriter{}
	}
	return &PgCandleWriter{exec: pool}
}

func NewPgCandleWriterWithExecutor(exec adapterstorage.SQLExecutor) *PgCandleWriter {
	return &PgCandleWriter{exec: exec}
}

func (w *PgCandleWriter) SaveCandle(ctx context.Context, evt aggdomain.CandleClosed) *problem.Problem {
	if w == nil || w.exec == nil {
		return problem.New(problem.ValidationFailed, "timescale candle writer is nil")
	}
	c := evt.Candle
	const upsertSQL = `
INSERT INTO aggregation_candle (
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
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT (venue, instrument, timeframe, window_start) DO NOTHING`

	idempotencyKey := sharedhash.HashFields(
		c.Venue,
		c.Instrument,
		c.Timeframe,
		strconv.FormatInt(c.WindowStartTs, 10),
	)

	if _, p := w.exec.Exec(
		ctx,
		upsertSQL,
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
		return problem.Wrap(p, problem.Unavailable, "timescale candle upsert failed")
	}

	metrics.IncProcessorCommit("candle_hot")
	return nil
}
