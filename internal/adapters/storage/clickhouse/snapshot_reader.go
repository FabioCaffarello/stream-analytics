package clickhouse

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

var _ aggports.SnapshotReader = (*ChSnapshotReader)(nil)

// ChSnapshotReader implements ports.SnapshotReader against ClickHouse cold storage.
// Queries use FINAL to avoid returning duplicate rows from ReplacingMergeTree.
type ChSnapshotReader struct {
	queryer snapshotQueryer
}

func NewChSnapshotReader(pool *Pool) *ChSnapshotReader {
	if pool == nil || pool.Conn() == nil {
		return &ChSnapshotReader{}
	}
	return &ChSnapshotReader{queryer: poolSnapshotQueryer{pool: pool}}
}

func NewChSnapshotReaderWithQueryer(queryer snapshotQueryer) *ChSnapshotReader {
	return &ChSnapshotReader{queryer: queryer}
}

type snapshotRows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error
}

type snapshotQueryer interface {
	Query(ctx context.Context, query string, args ...any) (snapshotRows, error)
}

type poolSnapshotQueryer struct {
	pool *Pool
}

func (q poolSnapshotQueryer) Query(ctx context.Context, query string, args ...any) (snapshotRows, error) {
	if q.pool == nil || q.pool.Conn() == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse snapshot reader is nil")
	}
	return q.pool.Conn().Query(ctx, query, args...)
}

var _ snapshotRows = (driver.Rows)(nil)

func (r *ChSnapshotReader) GetSnapshotTimestamps(ctx context.Context, venue, instrument string, fromMs, toMs int64) ([]int64, *problem.Problem) {
	if r == nil || r.queryer == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse snapshot reader is nil")
	}

	const querySQL = `
SELECT toUnixTimestamp64Milli(created_at) AS ts
FROM aggregation_orderbook_snapshot_cold FINAL
WHERE venue = ? AND instrument = ?
  AND toUnixTimestamp64Milli(created_at) >= ?
  AND toUnixTimestamp64Milli(created_at) <= ?
ORDER BY ts ASC`

	rows, err := r.queryer.Query(ctx, querySQL, venue, instrument, fromMs, toMs)
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
