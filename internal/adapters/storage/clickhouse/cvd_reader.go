package clickhouse

import (
	"context"
	"fmt"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.CVDReader = (*ChCVDReader)(nil)

// ChCVDReader implements ports.CVDReader against ClickHouse cold storage.
type ChCVDReader struct {
	pool *Pool
}

func NewChCVDReader(pool *Pool) *ChCVDReader {
	return &ChCVDReader{pool: pool}
}

func (r *ChCVDReader) GetCVDRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.CVDWindowV1, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse cvd reader is nil")
	}
	if limit <= 0 {
		return nil, problem.New(problem.ValidationFailed, "limit must be > 0")
	}

	const querySQL = `
SELECT venue, instrument, timeframe, window_start, window_end,
       delta_volume, cvd, seq, ts_ingest
FROM aggregation_cvd_cold FINAL
WHERE venue = ? AND instrument = ? AND timeframe = ?
  AND window_start >= ? AND window_start <= ?
ORDER BY window_start ASC
LIMIT ?`

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, timeframe, fromMs, toMs, limit)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse cvd range query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]aggdomain.CVDWindowV1, 0, limit)
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		var c aggdomain.CVDWindowV1
		if err := rows.Scan(
			&c.Venue, &c.Instrument, &c.Timeframe,
			&c.WindowStartTs, &c.WindowEndTs,
			&c.DeltaVolume, &c.CVD,
			&c.Seq, &c.TsIngestMs,
		); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "clickhouse cvd range scan failed")
		}
		key := fmt.Sprintf("%s|%s|%s|%d", c.Venue, c.Instrument, c.Timeframe, c.WindowStartTs)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse cvd range rows failed")
	}
	return out, nil
}
