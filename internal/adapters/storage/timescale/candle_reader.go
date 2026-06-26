package timescale

import (
	"context"
	"fmt"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

var _ aggports.CandleReader = (*PgCandleReader)(nil)

// PgCandleReader implements ports.CandleReader against Timescale hot storage.
type PgCandleReader struct {
	q pgQuerier
}

func NewPgCandleReader(pool *Pool) *PgCandleReader {
	if pool == nil || pool.Raw() == nil {
		return &PgCandleReader{}
	}
	return &PgCandleReader{q: pool.Raw()}
}

func NewPgCandleReaderWithQuerier(q pgQuerier) *PgCandleReader {
	return &PgCandleReader{q: q}
}

func (r *PgCandleReader) GetCandleRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.CandleV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale candle reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       open_price, high_price, low_price, close_price,
       volume, buy_volume, sell_volume, trade_count, seq_first, seq_last
FROM aggregation_candle
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
  AND window_start >= $4 AND window_start <= $5
ORDER BY window_start ASC
LIMIT $6`

	rows, err := r.q.Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale candle range query failed")
	}
	defer rows.Close()

	out := make([]aggdomain.CandleV1, 0, limit)
	for rows.Next() {
		var c aggdomain.CandleV1
		if err := rows.Scan(
			&c.Venue, &c.Instrument, &c.Timeframe,
			&c.WindowStartTs, &c.WindowEndTs,
			&c.Open, &c.High, &c.Low, &c.ClosePrice,
			&c.Volume, &c.BuyVolume, &c.SellVolume,
			&c.TradeCount, &c.SeqFirst, &c.SeqLast,
		); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "timescale candle range scan failed")
		}
		c.IsClosed = c.WindowEndTs > c.WindowStartTs
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale candle range rows failed")
	}
	return out, nil
}

func (r *PgCandleReader) GetCandleTimestamps(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale candle reader is nil")
	}

	const querySQL = `
SELECT window_start
FROM aggregation_candle
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
  AND window_start >= $4 AND window_start <= $5
ORDER BY window_start ASC`

	rows, err := r.q.Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale candle timestamps query failed")
	}
	defer rows.Close()

	out := make([]int64, 0, 1024)
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "timescale candle timestamps scan failed")
		}
		out = append(out, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale candle timestamps rows failed")
	}
	return out, nil
}

func (r *PgCandleReader) GetFirstCandle(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	return r.getBoundaryCandle(ctx, venue, instrument, timeframe, "ASC")
}

func (r *PgCandleReader) GetLastCandle(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	return r.getBoundaryCandle(ctx, venue, instrument, timeframe, "DESC")
}

func (r *PgCandleReader) getBoundaryCandle(ctx context.Context, venue, instrument, timeframe, order string) (*aggdomain.CandleV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale candle reader is nil")
	}
	if order != "ASC" && order != "DESC" {
		return nil, problem.New(problem.ValidationFailed, "order must be ASC or DESC")
	}

	querySQL := fmt.Sprintf(`
SELECT venue, instrument, timeframe, window_start, window_end,
       open_price, high_price, low_price, close_price,
       volume, buy_volume, sell_volume, trade_count, seq_first, seq_last
FROM aggregation_candle
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
ORDER BY window_start %s
LIMIT 1`, order)

	rows, err := r.q.Query(ctx, querySQL, venue, instrument, timeframe)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale candle boundary query failed")
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, problem.Wrap(err, problem.Unavailable, "timescale candle boundary rows failed")
		}
		return nil, nil
	}

	var c aggdomain.CandleV1
	if err := rows.Scan(
		&c.Venue, &c.Instrument, &c.Timeframe,
		&c.WindowStartTs, &c.WindowEndTs,
		&c.Open, &c.High, &c.Low, &c.ClosePrice,
		&c.Volume, &c.BuyVolume, &c.SellVolume,
		&c.TradeCount, &c.SeqFirst, &c.SeqLast,
	); err != nil {
		return nil, problem.Wrap(err, problem.Internal, "timescale candle boundary scan failed")
	}
	c.IsClosed = c.WindowEndTs > c.WindowStartTs
	return &c, nil
}
