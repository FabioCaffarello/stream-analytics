package timescale

import (
	"context"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.BarStatsReader = (*PgBarStatsReader)(nil)

// PgBarStatsReader implements ports.BarStatsReader against Timescale hot storage.
type PgBarStatsReader struct {
	q pgQuerier
}

func NewPgBarStatsReader(pool *Pool) *PgBarStatsReader {
	if pool == nil || pool.Raw() == nil {
		return &PgBarStatsReader{}
	}
	return &PgBarStatsReader{q: pool.Raw()}
}

func NewPgBarStatsReaderWithQuerier(q pgQuerier) *PgBarStatsReader {
	return &PgBarStatsReader{q: q}
}

func (r *PgBarStatsReader) GetBarStatsRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.BarStatsWindowV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale bar_stats reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       trade_count, buy_count, sell_count,
       total_volume, buy_volume, sell_volume,
       vwap_price, last_price, max_price, min_price,
       imbalance, is_burst, seq, ts_ingest
FROM aggregation_bar_stats
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
  AND window_start >= $4 AND window_start <= $5
ORDER BY window_start ASC
LIMIT $6`

	rows, err := r.q.Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale bar_stats range query failed")
	}
	defer rows.Close()

	out := make([]aggdomain.BarStatsWindowV1, 0, limit)
	for rows.Next() {
		var b aggdomain.BarStatsWindowV1
		if err := rows.Scan(
			&b.Venue, &b.Instrument, &b.Timeframe,
			&b.WindowStartTs, &b.WindowEndTs,
			&b.TradeCount, &b.BuyCount, &b.SellCount,
			&b.TotalVolume, &b.BuyVolume, &b.SellVolume,
			&b.VwapPrice, &b.LastPrice, &b.MaxPrice, &b.MinPrice,
			&b.Imbalance, &b.IsBurst, &b.Seq, &b.TsIngestMs,
		); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "timescale bar_stats range scan failed")
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale bar_stats range rows failed")
	}
	return out, nil
}
