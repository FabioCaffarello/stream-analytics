package clickhouse

import (
	"context"
	"fmt"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.OIReader = (*ChOIReader)(nil)

// ChOIReader implements ports.OIReader against ClickHouse cold storage.
type ChOIReader struct {
	pool *Pool
}

func NewChOIReader(pool *Pool) *ChOIReader {
	return &ChOIReader{pool: pool}
}

func (r *ChOIReader) GetOIRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.OpenInterestWindowV1, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse oi reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       open_interest, delta, delta_pct, seq, ts_ingest
FROM aggregation_oi_cold FINAL
WHERE venue = ? AND instrument = ? AND timeframe = ?
  AND window_start >= ? AND window_start <= ?
ORDER BY window_start ASC
LIMIT ?`

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse oi range query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]aggdomain.OpenInterestWindowV1, 0, limit)
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		var oi aggdomain.OpenInterestWindowV1
		if err := rows.Scan(
			&oi.Venue, &oi.Instrument, &oi.Timeframe,
			&oi.WindowStartTs, &oi.WindowEndTs,
			&oi.OpenInterest, &oi.Delta, &oi.DeltaPct,
			&oi.Seq, &oi.TsIngestMs,
		); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "clickhouse oi range scan failed")
		}
		key := fmt.Sprintf("%s|%s|%s|%d", oi.Venue, oi.Instrument, oi.Timeframe, oi.WindowStartTs)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, oi)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse oi range rows failed")
	}
	return out, nil
}
