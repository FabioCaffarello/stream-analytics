package timescale

import (
	"context"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.DeltaVolumeReader = (*PgDeltaVolumeReader)(nil)

// PgDeltaVolumeReader implements ports.DeltaVolumeReader against Timescale hot storage.
type PgDeltaVolumeReader struct {
	q pgQuerier
}

func NewPgDeltaVolumeReader(pool *Pool) *PgDeltaVolumeReader {
	if pool == nil || pool.Raw() == nil {
		return &PgDeltaVolumeReader{}
	}
	return &PgDeltaVolumeReader{q: pool.Raw()}
}

func NewPgDeltaVolumeReaderWithQuerier(q pgQuerier) *PgDeltaVolumeReader {
	return &PgDeltaVolumeReader{q: q}
}

func (r *PgDeltaVolumeReader) GetDeltaVolumeRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.DeltaVolumeWindowV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale delta_volume reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       buy_volume, sell_volume, delta_volume, seq, ts_ingest
FROM aggregation_delta_volume
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
  AND window_start >= $4 AND window_start <= $5
ORDER BY window_start ASC
LIMIT $6`

	rows, err := r.q.Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale delta_volume range query failed")
	}
	defer rows.Close()

	out := make([]aggdomain.DeltaVolumeWindowV1, 0, limit)
	for rows.Next() {
		var dv aggdomain.DeltaVolumeWindowV1
		if err := rows.Scan(
			&dv.Venue, &dv.Instrument, &dv.Timeframe,
			&dv.WindowStartTs, &dv.WindowEndTs,
			&dv.BuyVolume, &dv.SellVolume, &dv.DeltaVolume,
			&dv.Seq, &dv.TsIngestMs,
		); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "timescale delta_volume range scan failed")
		}
		out = append(out, dv)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale delta_volume range rows failed")
	}
	return out, nil
}
