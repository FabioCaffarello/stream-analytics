package timescale

import (
	"context"

	adapterstorage "github.com/FabioCaffarello/stream-analytics/internal/adapters/storage"
	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

var _ aggports.TapeHotReadModelStore = (*PgTapeWriter)(nil)

// PgTapeWriter persists closed tape windows in Timescale.
type PgTapeWriter struct {
	exec adapterstorage.SQLExecutor
}

func NewPgTapeWriter(pool *Pool) *PgTapeWriter {
	if pool == nil {
		return &PgTapeWriter{}
	}
	return &PgTapeWriter{exec: pool}
}

func NewPgTapeWriterWithExecutor(exec adapterstorage.SQLExecutor) *PgTapeWriter {
	return &PgTapeWriter{exec: exec}
}

func (w *PgTapeWriter) SaveTape(ctx context.Context, evt aggdomain.TapeClosed) *problem.Problem {
	if w == nil || w.exec == nil {
		return problem.New(problem.ValidationFailed, "timescale tape writer is nil")
	}
	t := evt.Window
	const upsertSQL = `
INSERT INTO aggregation_tape (
    venue,
    instrument,
    timeframe,
    window_start,
    window_end,
    trade_count,
    buy_count,
    sell_count,
    buy_volume,
    sell_volume,
    total_volume,
    buy_notional,
    sell_notional,
    vwap_price,
    max_price,
    min_price,
    last_price,
    max_trade_size,
    rate_trades_per_sec,
    volume_imbalance,
    is_burst,
    seq_last,
    idempotency_key
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
ON CONFLICT (venue, instrument, timeframe, window_start) DO NOTHING`

	idempotencyKey := adapterstorage.WindowIdempotencyKey(t.Venue, t.Instrument, t.Timeframe, t.WindowStartTs)
	args := []any{
		t.Venue,
		t.Instrument,
		t.Timeframe,
		t.WindowStartTs,
		t.WindowEndTs,
		t.TradeCount,
		t.BuyCount,
		t.SellCount,
		t.BuyVolume,
		t.SellVolume,
		t.TotalVolume,
		t.BuyNotional,
		t.SellNotional,
		t.VwapPrice,
		t.MaxPrice,
		t.MinPrice,
		t.LastPrice,
		t.MaxTradeSize,
		t.RateTradesPerSec,
		t.VolumeImbalance,
		evt.IsBurst,
		t.LastSeq,
		idempotencyKey,
	}
	if _, p := w.exec.Exec(ctx, upsertSQL, args...); p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale tape upsert failed")
	}

	metrics.IncProcessorCommit("tape_hot")
	return nil
}
