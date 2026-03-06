package clickhouse

import (
	"context"
	"fmt"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.DeltaVolumeReader = (*ChDeltaVolumeReader)(nil)

// ChDeltaVolumeReader implements ports.DeltaVolumeReader against ClickHouse cold storage.
type ChDeltaVolumeReader struct {
	pool *Pool
}

func NewChDeltaVolumeReader(pool *Pool) *ChDeltaVolumeReader {
	return &ChDeltaVolumeReader{pool: pool}
}

func (r *ChDeltaVolumeReader) GetDeltaVolumeRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.DeltaVolumeWindowV1, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse delta_volume reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       buy_volume, sell_volume, delta_volume, seq, ts_ingest
FROM aggregation_delta_volume_cold FINAL
WHERE venue = ? AND instrument = ? AND timeframe = ?
  AND window_start >= ? AND window_start <= ?
ORDER BY window_start ASC
LIMIT ?`

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse delta_volume range query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]aggdomain.DeltaVolumeWindowV1, 0, limit)
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		var dv aggdomain.DeltaVolumeWindowV1
		if err := rows.Scan(
			&dv.Venue, &dv.Instrument, &dv.Timeframe,
			&dv.WindowStartTs, &dv.WindowEndTs,
			&dv.BuyVolume, &dv.SellVolume, &dv.DeltaVolume,
			&dv.Seq, &dv.TsIngestMs,
		); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "clickhouse delta_volume range scan failed")
		}
		key := fmt.Sprintf("%s|%s|%s|%d", dv.Venue, dv.Instrument, dv.Timeframe, dv.WindowStartTs)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, dv)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse delta_volume range rows failed")
	}
	return out, nil
}
