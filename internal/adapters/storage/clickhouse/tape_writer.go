package clickhouse

import (
	"context"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.TapeHotReadModelStore = (*ChTapeWriter)(nil)

// ChTapeWriter persists closed tape windows in ClickHouse cold storage.
type ChTapeWriter struct {
	preparer batchPreparer
}

func NewChTapeWriter(pool *Pool) *ChTapeWriter {
	if pool == nil {
		return &ChTapeWriter{}
	}
	return &ChTapeWriter{preparer: pool}
}

func NewChTapeWriterWithPreparer(preparer BatchPreparer) *ChTapeWriter {
	return &ChTapeWriter{preparer: preparer}
}

func (w *ChTapeWriter) SaveTape(ctx context.Context, evt aggdomain.TapeClosed) *problem.Problem {
	if w == nil || w.preparer == nil {
		return problem.New(problem.ValidationFailed, "clickhouse tape writer is nil")
	}
	t := evt.Window
	const insertSQL = `
INSERT INTO aggregation_tape_cold (
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
)`

	batch, p := w.preparer.PrepareInsert(ctx, insertSQL)
	if p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse tape prepare batch failed")
	}
	defer func() {
		_ = batch.Close()
	}()

	idempotencyKey := adapterstorage.WindowIdempotencyKey(t.Venue, t.Instrument, t.Timeframe, t.WindowStartTs)
	var isBurst uint8
	if evt.IsBurst {
		isBurst = 1
	}
	if p := batch.AppendRow(ctx,
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
		isBurst,
		t.LastSeq,
		idempotencyKey,
	); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse tape append failed")
	}
	if _, p := batch.Flush(ctx); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse tape batch send failed")
	}

	metrics.IncProcessorCommit("tape_cold")
	return nil
}
