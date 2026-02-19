package clickhouse

import (
	"context"
	"fmt"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.CandleReader = (*ChCandleReader)(nil)

// ChCandleReader implements ports.CandleReader against ClickHouse cold storage.
// Queries use FINAL to avoid returning duplicate rows from ReplacingMergeTree.
type ChCandleReader struct {
	pool *Pool
}

func NewChCandleReader(pool *Pool) *ChCandleReader {
	return &ChCandleReader{pool: pool}
}

func (r *ChCandleReader) GetCandleTimestamps(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse candle reader is nil")
	}

	const querySQL = `
SELECT window_start
FROM aggregation_candle_cold FINAL
WHERE venue = ? AND instrument = ? AND timeframe = ?
  AND window_start >= ? AND window_start <= ?
ORDER BY window_start ASC`

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse candle timestamps query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]int64, 0, 1024)
	seen := make(map[int64]struct{}, 1024)
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "clickhouse candle timestamps scan failed")
		}
		if _, ok := seen[ts]; ok {
			continue
		}
		seen[ts] = struct{}{}
		out = append(out, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse candle timestamps rows failed")
	}
	return out, nil
}

func (r *ChCandleReader) GetCandleRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.CandleV1, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse candle reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       open_price, high_price, low_price, close_price,
       volume, buy_volume, sell_volume, trade_count, seq_first, seq_last
FROM aggregation_candle_cold FINAL
WHERE venue = ? AND instrument = ? AND timeframe = ?
  AND window_start >= ? AND window_start <= ?
ORDER BY window_start ASC
LIMIT ?`

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse candle range query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]aggdomain.CandleV1, 0, limit)
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		var c aggdomain.CandleV1
		if err := rows.Scan(
			&c.Venue,
			&c.Instrument,
			&c.Timeframe,
			&c.WindowStartTs,
			&c.WindowEndTs,
			&c.Open,
			&c.High,
			&c.Low,
			&c.ClosePrice,
			&c.Volume,
			&c.BuyVolume,
			&c.SellVolume,
			&c.TradeCount,
			&c.SeqFirst,
			&c.SeqLast,
		); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "clickhouse candle range scan failed")
		}
		c.IsClosed = c.WindowEndTs > c.WindowStartTs

		key := fmt.Sprintf("%s|%s|%s|%d", c.Venue, c.Instrument, c.Timeframe, c.WindowStartTs)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse candle range rows failed")
	}
	return out, nil
}

func (r *ChCandleReader) GetFirstCandle(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	return r.getBoundaryCandle(ctx, venue, instrument, timeframe, "ASC")
}

func (r *ChCandleReader) GetLastCandle(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	return r.getBoundaryCandle(ctx, venue, instrument, timeframe, "DESC")
}

func (r *ChCandleReader) getBoundaryCandle(ctx context.Context, venue, instrument, timeframe, order string) (*aggdomain.CandleV1, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse candle reader is nil")
	}
	if order != "ASC" && order != "DESC" {
		return nil, problem.New(problem.ValidationFailed, "order must be ASC or DESC")
	}

	querySQL := fmt.Sprintf(`
SELECT venue, instrument, timeframe, window_start, window_end,
       open_price, high_price, low_price, close_price,
       volume, buy_volume, sell_volume, trade_count, seq_first, seq_last
FROM aggregation_candle_cold FINAL
WHERE venue = ? AND instrument = ? AND timeframe = ?
ORDER BY window_start %s
LIMIT 1`, order)

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, timeframe)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse candle boundary query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, problem.Wrap(err, problem.Unavailable, "clickhouse candle boundary rows failed")
		}
		return nil, nil
	}

	var c aggdomain.CandleV1
	if err := rows.Scan(
		&c.Venue,
		&c.Instrument,
		&c.Timeframe,
		&c.WindowStartTs,
		&c.WindowEndTs,
		&c.Open,
		&c.High,
		&c.Low,
		&c.ClosePrice,
		&c.Volume,
		&c.BuyVolume,
		&c.SellVolume,
		&c.TradeCount,
		&c.SeqFirst,
		&c.SeqLast,
	); err != nil {
		return nil, problem.Wrap(err, problem.Internal, "clickhouse candle boundary scan failed")
	}
	c.IsClosed = c.WindowEndTs > c.WindowStartTs
	return &c, nil
}
