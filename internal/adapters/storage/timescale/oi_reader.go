package timescale

import (
	"context"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

var _ aggports.OIReader = (*PgOIReader)(nil)

// PgOIReader implements ports.OIReader against Timescale hot storage.
type PgOIReader struct {
	q pgQuerier
}

func NewPgOIReader(pool *Pool) *PgOIReader {
	if pool == nil || pool.Raw() == nil {
		return &PgOIReader{}
	}
	return &PgOIReader{q: pool.Raw()}
}

func NewPgOIReaderWithQuerier(q pgQuerier) *PgOIReader {
	return &PgOIReader{q: q}
}

func (r *PgOIReader) GetOIRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.OpenInterestWindowV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale oi reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       open_interest, delta, delta_pct, seq, ts_ingest
FROM aggregation_oi
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
  AND window_start >= $4 AND window_start <= $5
ORDER BY window_start ASC
LIMIT $6`

	rows, err := r.q.Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale oi range query failed")
	}
	defer rows.Close()

	out := make([]aggdomain.OpenInterestWindowV1, 0, limit)
	for rows.Next() {
		var oi aggdomain.OpenInterestWindowV1
		if err := rows.Scan(
			&oi.Venue, &oi.Instrument, &oi.Timeframe,
			&oi.WindowStartTs, &oi.WindowEndTs,
			&oi.OpenInterest, &oi.Delta, &oi.DeltaPct,
			&oi.Seq, &oi.TsIngestMs,
		); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "timescale oi range scan failed")
		}
		out = append(out, oi)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale oi range rows failed")
	}
	return out, nil
}
