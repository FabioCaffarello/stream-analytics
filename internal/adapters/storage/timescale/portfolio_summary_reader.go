package timescale

import (
	"context"
	"encoding/json"

	domain "github.com/market-raccoon/internal/core/portfolio/domain"
	ports "github.com/market-raccoon/internal/core/portfolio/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ ports.PortfolioSummaryReader = (*PgPortfolioSummaryReader)(nil)

// PgPortfolioSummaryReader reads persisted global portfolio summaries from Timescale.
type PgPortfolioSummaryReader struct {
	q pgQuerier
}

func NewPgPortfolioSummaryReader(pool *Pool) *PgPortfolioSummaryReader {
	if pool == nil || pool.Raw() == nil {
		return &PgPortfolioSummaryReader{}
	}
	return &PgPortfolioSummaryReader{q: pool.Raw()}
}

func NewPgPortfolioSummaryReaderWithQuerier(q pgQuerier) *PgPortfolioSummaryReader {
	return &PgPortfolioSummaryReader{q: q}
}

const defaultSummaryLimit = 100

func (r *PgPortfolioSummaryReader) GetPortfolioSummaries(ctx context.Context, q ports.PortfolioSummaryQuery) ([]domain.PortfolioSummaryV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale portfolio summary reader is nil")
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultSummaryLimit
	}

	query := `
SELECT summary_id, projected_at_ms,
       global_equity_usd, global_realized_usd, global_unrealized,
       global_margin_used, global_leverage,
       total_position_count, total_open_orders,
       accounts, fill_summary
FROM portfolio_summary
WHERE 1=1`

	args := []any{}
	idx := 1

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
		return nil, problem.Wrap(err, problem.Unavailable, "timescale portfolio summary query failed")
	}
	defer rows.Close()

	out := make([]domain.PortfolioSummaryV1, 0, limit)
	for rows.Next() {
		s, p := scanPortfolioSummary(rows)
		if p != nil {
			return nil, p
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale portfolio summary rows failed")
	}
	return out, nil
}

func (r *PgPortfolioSummaryReader) GetLatestPortfolioSummary(ctx context.Context) (domain.PortfolioSummaryV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return domain.PortfolioSummaryV1{}, problem.New(problem.ValidationFailed, "timescale portfolio summary reader is nil")
	}

	const querySQL = `
SELECT summary_id, projected_at_ms,
       global_equity_usd, global_realized_usd, global_unrealized,
       global_margin_used, global_leverage,
       total_position_count, total_open_orders,
       accounts, fill_summary
FROM portfolio_summary
ORDER BY projected_at_ms DESC
LIMIT 1`

	rows, err := r.q.Query(ctx, querySQL)
	if err != nil {
		return domain.PortfolioSummaryV1{}, problem.Wrap(err, problem.Unavailable, "timescale portfolio summary latest query failed")
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return domain.PortfolioSummaryV1{}, problem.Wrap(err, problem.Unavailable, "timescale portfolio summary latest rows failed")
		}
		return domain.PortfolioSummaryV1{}, nil
	}

	return scanPortfolioSummary(rows)
}

func scanPortfolioSummary(row rowScanner) (domain.PortfolioSummaryV1, *problem.Problem) {
	var s domain.PortfolioSummaryV1
	var accountsJSON, fillJSON []byte

	if err := row.Scan(
		&s.SummaryID, &s.ProjectedAtMs,
		&s.GlobalEquityUSD, &s.GlobalRealizedUSD, &s.GlobalUnrealized,
		&s.GlobalMarginUsed, &s.GlobalLeverage,
		&s.TotalPositionCount, &s.TotalOpenOrders,
		&accountsJSON, &fillJSON,
	); err != nil {
		return domain.PortfolioSummaryV1{}, problem.Wrap(err, problem.Internal, "timescale portfolio summary scan failed")
	}

	if err := json.Unmarshal(accountsJSON, &s.Accounts); err != nil {
		return domain.PortfolioSummaryV1{}, problem.Wrap(err, problem.Internal, "unmarshal accounts failed")
	}
	if err := json.Unmarshal(fillJSON, &s.FillSummary); err != nil {
		return domain.PortfolioSummaryV1{}, problem.Wrap(err, problem.Internal, "unmarshal fill_summary failed")
	}
	return s, nil
}
