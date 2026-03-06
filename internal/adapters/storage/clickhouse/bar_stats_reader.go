package clickhouse

import (
	"context"
	"fmt"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.BarStatsReader = (*ChBarStatsReader)(nil)

// ChBarStatsReader implements ports.BarStatsReader against ClickHouse cold storage.
type ChBarStatsReader struct {
	pool *Pool
}

func NewChBarStatsReader(pool *Pool) *ChBarStatsReader {
	return &ChBarStatsReader{pool: pool}
}

func (r *ChBarStatsReader) GetBarStatsRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.BarStatsWindowV1, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse bar_stats reader is nil")
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
FROM aggregation_bar_stats_cold FINAL
WHERE venue = ? AND instrument = ? AND timeframe = ?
  AND window_start >= ? AND window_start <= ?
ORDER BY window_start ASC
LIMIT ?`

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse bar_stats range query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]aggdomain.BarStatsWindowV1, 0, limit)
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		var b aggdomain.BarStatsWindowV1
		var isBurst uint8
		if err := rows.Scan(
			&b.Venue, &b.Instrument, &b.Timeframe,
			&b.WindowStartTs, &b.WindowEndTs,
			&b.TradeCount, &b.BuyCount, &b.SellCount,
			&b.TotalVolume, &b.BuyVolume, &b.SellVolume,
			&b.VwapPrice, &b.LastPrice, &b.MaxPrice, &b.MinPrice,
			&b.Imbalance, &isBurst, &b.Seq, &b.TsIngestMs,
		); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "clickhouse bar_stats range scan failed")
		}
		b.IsBurst = isBurst != 0
		key := fmt.Sprintf("%s|%s|%s|%d", b.Venue, b.Instrument, b.Timeframe, b.WindowStartTs)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse bar_stats range rows failed")
	}
	return out, nil
}
