package timescale

import (
	"context"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.CVDReader = (*PgCVDReader)(nil)

// PgCVDReader implements ports.CVDReader against Timescale hot storage.
type PgCVDReader struct {
	q pgQuerier
}

func NewPgCVDReader(pool *Pool) *PgCVDReader {
	if pool == nil || pool.Raw() == nil {
		return &PgCVDReader{}
	}
	return &PgCVDReader{q: pool.Raw()}
}

func NewPgCVDReaderWithQuerier(q pgQuerier) *PgCVDReader {
	return &PgCVDReader{q: q}
}

func (r *PgCVDReader) GetCVDRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.CVDWindowV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale cvd reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       delta_volume, cvd, seq, ts_ingest
FROM aggregation_cvd
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
  AND window_start >= $4 AND window_start <= $5
ORDER BY window_start ASC
LIMIT $6`

	rows, err := r.q.Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale cvd range query failed")
	}
	defer rows.Close()

	out := make([]aggdomain.CVDWindowV1, 0, limit)
	for rows.Next() {
		var c aggdomain.CVDWindowV1
		if err := rows.Scan(
			&c.Venue, &c.Instrument, &c.Timeframe,
			&c.WindowStartTs, &c.WindowEndTs,
			&c.DeltaVolume, &c.CVD,
			&c.Seq, &c.TsIngestMs,
		); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "timescale cvd range scan failed")
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale cvd range rows failed")
	}
	return out, nil
}
