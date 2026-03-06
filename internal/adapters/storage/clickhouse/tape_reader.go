package clickhouse

import (
	"context"
	"fmt"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.TapeReader = (*ChTapeReader)(nil)

// ChTapeReader implements ports.TapeReader against ClickHouse cold storage.
type ChTapeReader struct {
	pool *Pool
}

func NewChTapeReader(pool *Pool) *ChTapeReader {
	return &ChTapeReader{pool: pool}
}

func (r *ChTapeReader) GetTapeRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.TapeWindowV1, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse tape reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       trade_count, buy_count, sell_count,
       buy_volume, sell_volume, total_volume,
       buy_notional, sell_notional,
       vwap_price, max_price, min_price, last_price,
       max_trade_size, rate_trades_per_sec, volume_imbalance,
       seq_last
FROM aggregation_tape_cold FINAL
WHERE venue = ? AND instrument = ? AND timeframe = ?
  AND window_start >= ? AND window_start <= ?
ORDER BY window_start ASC
LIMIT ?`

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse tape range query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]aggdomain.TapeWindowV1, 0, limit)
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		var t aggdomain.TapeWindowV1
		if err := rows.Scan(
			&t.Venue, &t.Instrument, &t.Timeframe,
			&t.WindowStartTs, &t.WindowEndTs,
			&t.TradeCount, &t.BuyCount, &t.SellCount,
			&t.BuyVolume, &t.SellVolume, &t.TotalVolume,
			&t.BuyNotional, &t.SellNotional,
			&t.VwapPrice, &t.MaxPrice, &t.MinPrice, &t.LastPrice,
			&t.MaxTradeSize, &t.RateTradesPerSec, &t.VolumeImbalance,
			&t.LastSeq,
		); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "clickhouse tape range scan failed")
		}
		key := fmt.Sprintf("%s|%s|%s|%d", t.Venue, t.Instrument, t.Timeframe, t.WindowStartTs)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse tape range rows failed")
	}
	return out, nil
}
