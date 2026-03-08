package timescale

import (
	"context"
	"encoding/json"
	"fmt"

	domain "github.com/market-raccoon/internal/core/portfolio/domain"
	ports "github.com/market-raccoon/internal/core/portfolio/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ ports.PortfolioStateReader = (*PgPortfolioStateReader)(nil)

// PgPortfolioStateReader reads persisted venue-scoped portfolio states from Timescale.
type PgPortfolioStateReader struct {
	q pgQuerier
}

func NewPgPortfolioStateReader(pool *Pool) *PgPortfolioStateReader {
	if pool == nil || pool.Raw() == nil {
		return &PgPortfolioStateReader{}
	}
	return &PgPortfolioStateReader{q: pool.Raw()}
}

func NewPgPortfolioStateReaderWithQuerier(q pgQuerier) *PgPortfolioStateReader {
	return &PgPortfolioStateReader{q: q}
}

const defaultStateLimit = 100

func (r *PgPortfolioStateReader) GetPortfolioStates(ctx context.Context, q ports.PortfolioStateQuery) ([]domain.PortfolioStateV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return nil, problem.New(problem.ValidationFailed, "timescale portfolio state reader is nil")
	}
	if q.AccountID == "" {
		return nil, problem.New(problem.ValidationFailed, "account_id is required")
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultStateLimit
	}

	query := `
SELECT state_id, scope, account_id, venue, projected_at_ms,
       equity_usd, realized_pnl_usd, unrealized_pnl_usd,
       balances, positions, exposures, risk, fill_summary, provenance
FROM portfolio_state
WHERE account_id = $1`

	args := []any{q.AccountID}
	idx := 2

	if q.Venue != "" {
		query += " AND venue = $" + itoa(idx)
		args = append(args, q.Venue)
		idx++
	}
	if q.Symbol != "" {
		query += " AND positions @> $" + itoa(idx) + "::jsonb"
		args = append(args, symbolJSONBFilter(q.Symbol))
		idx++
	}

	query += " ORDER BY projected_at_ms DESC LIMIT $" + itoa(idx)
	args = append(args, limit)

	rows, err := r.q.Query(ctx, query, args...)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale portfolio state query failed")
	}
	defer rows.Close()

	out := make([]domain.PortfolioStateV1, 0, limit)
	for rows.Next() {
		s, p := scanPortfolioState(rows)
		if p != nil {
			return nil, p
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale portfolio state rows failed")
	}
	return out, nil
}

func (r *PgPortfolioStateReader) GetLatestPortfolioState(ctx context.Context, accountID, venue, symbol string) (domain.PortfolioStateV1, *problem.Problem) {
	if r == nil || r.q == nil {
		return domain.PortfolioStateV1{}, problem.New(problem.ValidationFailed, "timescale portfolio state reader is nil")
	}
	if accountID == "" {
		return domain.PortfolioStateV1{}, problem.New(problem.ValidationFailed, "account_id is required")
	}

	query := `
SELECT state_id, scope, account_id, venue, projected_at_ms,
       equity_usd, realized_pnl_usd, unrealized_pnl_usd,
       balances, positions, exposures, risk, fill_summary, provenance
FROM portfolio_state
WHERE account_id = $1`

	args := []any{accountID}
	idx := 2

	if venue != "" {
		query += " AND venue = $" + itoa(idx)
		args = append(args, venue)
		idx++
	}
	if symbol != "" {
		query += " AND positions @> $" + itoa(idx) + "::jsonb"
		args = append(args, symbolJSONBFilter(symbol))
		idx++
	}

	query += " ORDER BY projected_at_ms DESC LIMIT 1"

	rows, err := r.q.Query(ctx, query, args...)
	if err != nil {
		return domain.PortfolioStateV1{}, problem.Wrap(err, problem.Unavailable, "timescale portfolio state latest query failed")
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return domain.PortfolioStateV1{}, problem.Wrap(err, problem.Unavailable, "timescale portfolio state latest rows failed")
		}
		return domain.PortfolioStateV1{}, nil
	}

	return scanPortfolioState(rows)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPortfolioState(row rowScanner) (domain.PortfolioStateV1, *problem.Problem) {
	var s domain.PortfolioStateV1
	var scope string
	var balancesJSON, positionsJSON, exposuresJSON, riskJSON, fillJSON, provJSON []byte

	if err := row.Scan(
		&s.StateID, &scope, &s.AccountID, &s.Venue, &s.ProjectedAtMs,
		&s.EquityUSD, &s.RealizedPnlUSD, &s.UnrealizedPnlUSD,
		&balancesJSON, &positionsJSON, &exposuresJSON, &riskJSON, &fillJSON, &provJSON,
	); err != nil {
		return domain.PortfolioStateV1{}, problem.Wrap(err, problem.Internal, "timescale portfolio state scan failed")
	}

	s.Scope = domain.PortfolioScope(scope)

	if err := json.Unmarshal(balancesJSON, &s.Balances); err != nil {
		return domain.PortfolioStateV1{}, problem.Wrap(err, problem.Internal, "unmarshal balances failed")
	}
	if err := json.Unmarshal(positionsJSON, &s.Positions); err != nil {
		return domain.PortfolioStateV1{}, problem.Wrap(err, problem.Internal, "unmarshal positions failed")
	}
	if err := json.Unmarshal(exposuresJSON, &s.Exposures); err != nil {
		return domain.PortfolioStateV1{}, problem.Wrap(err, problem.Internal, "unmarshal exposures failed")
	}
	if err := json.Unmarshal(riskJSON, &s.Risk); err != nil {
		return domain.PortfolioStateV1{}, problem.Wrap(err, problem.Internal, "unmarshal risk failed")
	}
	if err := json.Unmarshal(fillJSON, &s.FillSummary); err != nil {
		return domain.PortfolioStateV1{}, problem.Wrap(err, problem.Internal, "unmarshal fill_summary failed")
	}
	if err := json.Unmarshal(provJSON, &s.Provenance); err != nil {
		return domain.PortfolioStateV1{}, problem.Wrap(err, problem.Internal, "unmarshal provenance failed")
	}
	return s, nil
}

// symbolJSONBFilter builds a properly JSON-escaped JSONB containment filter
// for the positions column. Uses json.Marshal to safely escape the symbol value.
func symbolJSONBFilter(symbol string) string {
	escaped, err := json.Marshal(symbol)
	if err != nil {
		return fmt.Sprintf(`[{"symbol":"%s"}]`, symbol)
	}
	return fmt.Sprintf(`[{"symbol":%s}]`, escaped)
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
