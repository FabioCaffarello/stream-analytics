package clickhouse

import (
	"context"
	"fmt"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.StatsReader = (*ChStatsReader)(nil)

// ChStatsReader implements ports.StatsReader against ClickHouse cold storage.
// Queries use FINAL to avoid returning duplicate rows from ReplacingMergeTree.
type ChStatsReader struct {
	pool *Pool
}

func NewChStatsReader(pool *Pool) *ChStatsReader {
	return &ChStatsReader{pool: pool}
}

func (r *ChStatsReader) GetStatsTimestamps(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse stats reader is nil")
	}

	const querySQL = `
SELECT window_start
FROM aggregation_stats_cold FINAL
WHERE venue = ? AND instrument = ? AND timeframe = ?
  AND window_start >= ? AND window_start <= ?
ORDER BY window_start ASC`

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse stats timestamps query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]int64, 0, 1024)
	seen := make(map[int64]struct{}, 1024)
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "clickhouse stats timestamps scan failed")
		}
		if _, ok := seen[ts]; ok {
			continue
		}
		seen[ts] = struct{}{}
		out = append(out, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse stats timestamps rows failed")
	}
	return out, nil
}

func (r *ChStatsReader) GetStatsRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.StatsWindowV1, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse stats reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       liq_buy_volume, liq_sell_volume, liq_total_volume, liq_count,
       markprice_open, markprice_high, markprice_low, markprice_close,
       funding_rate_avg, funding_rate_last, seq_first, seq_last
FROM aggregation_stats_cold FINAL
WHERE venue = ? AND instrument = ? AND timeframe = ?
  AND window_start >= ? AND window_start <= ?
ORDER BY window_start ASC
LIMIT ?`

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse stats range query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]aggdomain.StatsWindowV1, 0, limit)
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		s, p := scanStatsRow(rows)
		if p != nil {
			return nil, p
		}

		key := fmt.Sprintf("%s|%s|%s|%d", s.Venue, s.Instrument, s.Timeframe, s.WindowStartTs)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse stats range rows failed")
	}
	return out, nil
}

func (r *ChStatsReader) GetFirstStats(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	return r.getBoundaryStats(ctx, venue, instrument, timeframe, "ASC")
}

func (r *ChStatsReader) GetLastStats(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	return r.getBoundaryStats(ctx, venue, instrument, timeframe, "DESC")
}

//nolint:gocyclo // explicit nullable scan mapping keeps adapter behavior obvious.
func (r *ChStatsReader) getBoundaryStats(ctx context.Context, venue, instrument, timeframe, order string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse stats reader is nil")
	}
	if order != "ASC" && order != "DESC" {
		return nil, problem.New(problem.ValidationFailed, "order must be ASC or DESC")
	}

	querySQL := fmt.Sprintf(`
SELECT venue, instrument, timeframe, window_start, window_end,
       liq_buy_volume, liq_sell_volume, liq_total_volume, liq_count,
       markprice_open, markprice_high, markprice_low, markprice_close,
       funding_rate_avg, funding_rate_last, seq_first, seq_last
FROM aggregation_stats_cold FINAL
WHERE venue = ? AND instrument = ? AND timeframe = ?
ORDER BY window_start %s
LIMIT 1`, order)

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, timeframe)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse stats boundary query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, problem.Wrap(err, problem.Unavailable, "clickhouse stats boundary rows failed")
		}
		return nil, nil
	}

	s, p := scanStatsRow(rows)
	if p != nil {
		return nil, p
	}
	return &s, nil
}

// statsRowScanner is the subset of clickhouse.Rows needed to scan a single row.
type statsRowScanner interface {
	Scan(dest ...any) error
}

// scanStatsRow scans a full stats window row and applies nullable float mappings.
func scanStatsRow(row statsRowScanner) (aggdomain.StatsWindowV1, *problem.Problem) {
	var (
		s                                  aggdomain.StatsWindowV1
		markOpen, markHigh, markLow        *float64
		markClose, fundingAvg, fundingLast *float64
	)
	if err := row.Scan(
		&s.Venue,
		&s.Instrument,
		&s.Timeframe,
		&s.WindowStartTs,
		&s.WindowEndTs,
		&s.LiqBuyVolume,
		&s.LiqSellVolume,
		&s.LiqTotalVolume,
		&s.LiqCount,
		&markOpen,
		&markHigh,
		&markLow,
		&markClose,
		&fundingAvg,
		&fundingLast,
		&s.SeqFirst,
		&s.SeqLast,
	); err != nil {
		return s, problem.Wrap(err, problem.Internal, "clickhouse stats scan failed")
	}
	applyNullableStatsFields(&s, markOpen, markHigh, markLow, markClose, fundingAvg, fundingLast)
	s.IsClosed = s.WindowEndTs > s.WindowStartTs
	return s, nil
}

// applyNullableStatsFields copies non-nil float64 pointers into the domain struct.
func applyNullableStatsFields(s *aggdomain.StatsWindowV1, markOpen, markHigh, markLow, markClose, fundingAvg, fundingLast *float64) {
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
}
