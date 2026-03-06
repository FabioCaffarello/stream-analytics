package timescale

import (
	"context"
	"fmt"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.StatsReader = (*PgStatsReader)(nil)

// PgStatsReader implements ports.StatsReader against Timescale hot storage.
type PgStatsReader struct {
	q pgQuerier
}

func NewPgStatsReader(pool *Pool) *PgStatsReader {
	if pool == nil || pool.Raw() == nil {
		return &PgStatsReader{}
	}
	return &PgStatsReader{q: pool.Raw()}
}

func NewPgStatsReaderWithQuerier(q pgQuerier) *PgStatsReader {
	return &PgStatsReader{q: q}
}

func (r *PgStatsReader) GetStatsTimestamps(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale stats reader is nil")
	}

	const querySQL = `
SELECT window_start
FROM aggregation_stats
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
  AND window_start >= $4 AND window_start <= $5
ORDER BY window_start ASC`

	rows, err := r.q.Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale stats timestamps query failed")
	}
	defer rows.Close()

	out := make([]int64, 0, 1024)
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "timescale stats timestamps scan failed")
		}
		out = append(out, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale stats timestamps rows failed")
	}
	return out, nil
}

func (r *PgStatsReader) GetStatsRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.StatsWindowV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale stats reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       liq_buy_volume, liq_sell_volume, liq_total_volume, liq_count,
       markprice_open, markprice_high, markprice_low, markprice_close,
       funding_rate_avg, funding_rate_last, seq_first, seq_last
FROM aggregation_stats
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
  AND window_start >= $4 AND window_start <= $5
ORDER BY window_start ASC
LIMIT $6`

	rows, err := r.q.Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale stats range query failed")
	}
	defer rows.Close()

	out := make([]aggdomain.StatsWindowV1, 0, limit)
	for rows.Next() {
		s, p := scanPgStatsRow(rows)
		if p != nil {
			return nil, p
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale stats range rows failed")
	}
	return out, nil
}

func (r *PgStatsReader) GetFirstStats(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	return r.getBoundaryStats(ctx, venue, instrument, timeframe, "ASC")
}

func (r *PgStatsReader) GetLastStats(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	return r.getBoundaryStats(ctx, venue, instrument, timeframe, "DESC")
}

func (r *PgStatsReader) getBoundaryStats(ctx context.Context, venue, instrument, timeframe, order string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale stats reader is nil")
	}
	if order != "ASC" && order != "DESC" {
		return nil, problem.New(problem.ValidationFailed, "order must be ASC or DESC")
	}

	querySQL := fmt.Sprintf(`
SELECT venue, instrument, timeframe, window_start, window_end,
       liq_buy_volume, liq_sell_volume, liq_total_volume, liq_count,
       markprice_open, markprice_high, markprice_low, markprice_close,
       funding_rate_avg, funding_rate_last, seq_first, seq_last
FROM aggregation_stats
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
ORDER BY window_start %s
LIMIT 1`, order)

	rows, err := r.q.Query(ctx, querySQL, venue, instrument, timeframe)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale stats boundary query failed")
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, problem.Wrap(err, problem.Unavailable, "timescale stats boundary rows failed")
		}
		return nil, nil
	}

	s, p := scanPgStatsRow(rows)
	if p != nil {
		return nil, p
	}
	return &s, nil
}

type pgStatsRowScanner interface {
	Scan(dest ...any) error
}

func scanPgStatsRow(row pgStatsRowScanner) (aggdomain.StatsWindowV1, *problem.Problem) {
	var (
		s                                  aggdomain.StatsWindowV1
		markOpen, markHigh, markLow        *float64
		markClose, fundingAvg, fundingLast *float64
	)
	if err := row.Scan(
		&s.Venue, &s.Instrument, &s.Timeframe,
		&s.WindowStartTs, &s.WindowEndTs,
		&s.LiqBuyVolume, &s.LiqSellVolume, &s.LiqTotalVolume, &s.LiqCount,
		&markOpen, &markHigh, &markLow, &markClose,
		&fundingAvg, &fundingLast,
		&s.SeqFirst, &s.SeqLast,
	); err != nil {
		return s, problem.Wrap(err, problem.Internal, "timescale stats scan failed")
	}
	if markOpen != nil {
		s.MarkPriceOpen = *markOpen
	}
	if markHigh != nil {
		s.MarkPriceHigh = *markHigh
	}
	if markLow != nil {
		s.MarkPriceLow = *markLow
	}
	if markClose != nil {
		s.MarkPriceClose = *markClose
	}
	if fundingAvg != nil {
		s.FundingRateAvg = *fundingAvg
	}
	if fundingLast != nil {
		s.FundingRateLast = *fundingLast
	}
	s.IsClosed = s.WindowEndTs > s.WindowStartTs
	return s, nil
}
