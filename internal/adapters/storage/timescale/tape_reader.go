package timescale

import (
	"context"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

var _ aggports.TapeReader = (*PgTapeReader)(nil)

// PgTapeReader implements ports.TapeReader against Timescale hot storage.
type PgTapeReader struct {
	q pgQuerier
}

func NewPgTapeReader(pool *Pool) *PgTapeReader {
	if pool == nil || pool.Raw() == nil {
		return &PgTapeReader{}
	}
	return &PgTapeReader{q: pool.Raw()}
}

func NewPgTapeReaderWithQuerier(q pgQuerier) *PgTapeReader {
	return &PgTapeReader{q: q}
}

func (r *PgTapeReader) GetTapeRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.TapeWindowV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale tape reader is nil")
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
FROM aggregation_tape
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
  AND window_start >= $4 AND window_start <= $5
ORDER BY window_start ASC
LIMIT $6`

	rows, err := r.q.Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale tape range query failed")
	}
	defer rows.Close()

	out := make([]aggdomain.TapeWindowV1, 0, limit)
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
			return nil, problem.Wrap(err, problem.Internal, "timescale tape range scan failed")
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale tape range rows failed")
	}
	return out, nil
}
