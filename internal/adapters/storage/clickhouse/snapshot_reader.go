package clickhouse

import (
	"context"

	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.SnapshotReader = (*ChSnapshotReader)(nil)

// ChSnapshotReader implements ports.SnapshotReader against ClickHouse cold storage.
// Queries use FINAL to avoid returning duplicate rows from ReplacingMergeTree.
type ChSnapshotReader struct {
	pool *Pool
}

func NewChSnapshotReader(pool *Pool) *ChSnapshotReader {
	return &ChSnapshotReader{pool: pool}
}

func (r *ChSnapshotReader) GetSnapshotTimestamps(ctx context.Context, venue, instrument string, fromMs, toMs int64) ([]int64, *problem.Problem) {
	if r == nil || r.pool == nil || r.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse snapshot reader is nil")
	}

	const querySQL = `
SELECT toUnixTimestamp64Milli(created_at) AS ts
FROM aggregation_orderbook_snapshot_cold FINAL
WHERE venue = ? AND instrument = ?
  AND toUnixTimestamp64Milli(created_at) >= ?
  AND toUnixTimestamp64Milli(created_at) <= ?
ORDER BY ts ASC`

	rows, err := r.pool.Conn().Query(ctx, querySQL, venue, instrument, fromMs, toMs)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse snapshot timestamps query failed")
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]int64, 0, 1024)
	seen := make(map[int64]struct{}, 1024)
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "clickhouse snapshot timestamps scan failed")
		}
		if _, ok := seen[ts]; ok {
			continue
		}
		seen[ts] = struct{}{}
		out = append(out, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse snapshot timestamps rows failed")
	}
	return out, nil
}
