package timescale

import (
	"context"
	"encoding/json"

	domain "github.com/market-raccoon/internal/core/portfolio/domain"
	ports "github.com/market-raccoon/internal/core/portfolio/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ ports.AccountSnapshotReader = (*PgAccountSnapshotReader)(nil)

// PgAccountSnapshotReader reads persisted account-level snapshots from Timescale.
type PgAccountSnapshotReader struct {
	q pgQuerier
}

func NewPgAccountSnapshotReader(pool *Pool) *PgAccountSnapshotReader {
	if pool == nil || pool.Raw() == nil {
		return &PgAccountSnapshotReader{}
	}
	return &PgAccountSnapshotReader{q: pool.Raw()}
}

func NewPgAccountSnapshotReaderWithQuerier(q pgQuerier) *PgAccountSnapshotReader {
	return &PgAccountSnapshotReader{q: q}
}

const defaultSnapshotLimit = 100

func (r *PgAccountSnapshotReader) GetAccountSnapshots(ctx context.Context, q ports.AccountSnapshotQuery) ([]domain.AccountSnapshotV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale account snapshot reader is nil")
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultSnapshotLimit
	}

	query := `
SELECT snapshot_id, account_id, projected_at_ms,
       total_equity_usd, total_realized_usd, total_unrealized,
       total_margin_used, total_leverage,
       venues, fill_summary
FROM portfolio_account_snapshot
WHERE 1=1`

	args := []any{}
	idx := 1

	if q.AccountID != "" {
		query += " AND account_id = $" + itoa(idx)
		args = append(args, q.AccountID)
		idx++
	}
	if q.FromMs > 0 {
		query += " AND projected_at_ms >= $" + itoa(idx)
		args = append(args, q.FromMs)
		idx++
	}
	if q.ToMs > 0 {
		query += " AND projected_at_ms < $" + itoa(idx)
		args = append(args, q.ToMs)
		idx++
	}

	query += " ORDER BY projected_at_ms DESC LIMIT $" + itoa(idx)
	args = append(args, limit)

	rows, err := r.q.Query(ctx, query, args...)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale account snapshot query failed")
	}
	defer rows.Close()

	out := make([]domain.AccountSnapshotV1, 0, limit)
	for rows.Next() {
		s, p := scanAccountSnapshot(rows)
		if p != nil {
			return nil, p
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale account snapshot rows failed")
	}
	return out, nil
}

func (r *PgAccountSnapshotReader) GetLatestAccountSnapshot(ctx context.Context, accountID string) (domain.AccountSnapshotV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return domain.AccountSnapshotV1{}, problem.New(problem.ValidationFailed, "timescale account snapshot reader is nil")
	}
	if accountID == "" {
		return domain.AccountSnapshotV1{}, problem.New(problem.ValidationFailed, "account_id is required")
	}

	const querySQL = `
SELECT snapshot_id, account_id, projected_at_ms,
       total_equity_usd, total_realized_usd, total_unrealized,
       total_margin_used, total_leverage,
       venues, fill_summary
FROM portfolio_account_snapshot
WHERE account_id = $1
ORDER BY projected_at_ms DESC
LIMIT 1`

	rows, err := r.q.Query(ctx, querySQL, accountID)
	if err != nil {
		return domain.AccountSnapshotV1{}, problem.Wrap(err, problem.Unavailable, "timescale account snapshot latest query failed")
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return domain.AccountSnapshotV1{}, problem.Wrap(err, problem.Unavailable, "timescale account snapshot latest rows failed")
		}
		return domain.AccountSnapshotV1{}, nil
	}

	return scanAccountSnapshot(rows)
}

func scanAccountSnapshot(row rowScanner) (domain.AccountSnapshotV1, *problem.Problem) {
	var s domain.AccountSnapshotV1
	var venuesJSON, fillJSON []byte

	if err := row.Scan(
		&s.SnapshotID, &s.AccountID, &s.ProjectedAtMs,
		&s.TotalEquityUSD, &s.TotalRealizedUSD, &s.TotalUnrealized,
		&s.TotalMarginUsed, &s.TotalLeverage,
		&venuesJSON, &fillJSON,
	); err != nil {
		return domain.AccountSnapshotV1{}, problem.Wrap(err, problem.Internal, "timescale account snapshot scan failed")
	}

	if err := json.Unmarshal(venuesJSON, &s.Venues); err != nil {
		return domain.AccountSnapshotV1{}, problem.Wrap(err, problem.Internal, "unmarshal venues failed")
	}
	if err := json.Unmarshal(fillJSON, &s.FillSummary); err != nil {
		return domain.AccountSnapshotV1{}, problem.Wrap(err, problem.Internal, "unmarshal fill_summary failed")
	}
	return s, nil
}
