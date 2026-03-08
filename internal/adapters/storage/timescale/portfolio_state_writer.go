package timescale

import (
	"context"
	"encoding/json"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	domain "github.com/market-raccoon/internal/core/portfolio/domain"
	ports "github.com/market-raccoon/internal/core/portfolio/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ ports.PortfolioStateWriter = (*PgPortfolioStateWriter)(nil)

// PgPortfolioStateWriter persists venue-scoped portfolio state projections in Timescale.
type PgPortfolioStateWriter struct {
	exec adapterstorage.SQLExecutor
}

func NewPgPortfolioStateWriter(pool *Pool) *PgPortfolioStateWriter {
	if pool == nil {
		return &PgPortfolioStateWriter{}
	}
	return &PgPortfolioStateWriter{exec: pool}
}

func NewPgPortfolioStateWriterWithExecutor(exec adapterstorage.SQLExecutor) *PgPortfolioStateWriter {
	return &PgPortfolioStateWriter{exec: exec}
}

func (w *PgPortfolioStateWriter) UpsertPortfolioState(ctx context.Context, state domain.PortfolioStateV1) *problem.Problem {
	if w == nil || w.exec == nil {
		return problem.New(problem.ValidationFailed, "timescale portfolio state writer is nil")
	}
	if p := state.Validate(); p != nil {
		return p
	}

	balancesJSON, err := json.Marshal(state.Balances)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal balances failed")
	}
	positionsJSON, err := json.Marshal(state.Positions)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal positions failed")
	}
	exposuresJSON, err := json.Marshal(state.Exposures)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal exposures failed")
	}
	riskJSON, err := json.Marshal(state.Risk)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal risk failed")
	}
	fillJSON, err := json.Marshal(state.FillSummary)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal fill_summary failed")
	}
	provJSON, err := json.Marshal(state.Provenance)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal provenance failed")
	}

	const upsertSQL = `
INSERT INTO portfolio_state (
    state_id, scope, account_id, venue, projected_at_ms,
    equity_usd, realized_pnl_usd, unrealized_pnl_usd,
    balances, positions, exposures, risk, fill_summary, provenance
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (account_id, venue, state_id) DO UPDATE SET
    scope = EXCLUDED.scope,
    projected_at_ms = EXCLUDED.projected_at_ms,
    equity_usd = EXCLUDED.equity_usd,
    realized_pnl_usd = EXCLUDED.realized_pnl_usd,
    unrealized_pnl_usd = EXCLUDED.unrealized_pnl_usd,
    balances = EXCLUDED.balances,
    positions = EXCLUDED.positions,
    exposures = EXCLUDED.exposures,
    risk = EXCLUDED.risk,
    fill_summary = EXCLUDED.fill_summary,
    provenance = EXCLUDED.provenance`

	args := []any{
		state.StateID,
		string(state.Scope),
		state.AccountID,
		state.Venue,
		state.ProjectedAtMs,
		state.EquityUSD,
		state.RealizedPnlUSD,
		state.UnrealizedPnlUSD,
		balancesJSON,
		positionsJSON,
		exposuresJSON,
		riskJSON,
		fillJSON,
		provJSON,
	}

	if _, p := w.exec.Exec(ctx, upsertSQL, args...); p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale portfolio state upsert failed")
	}
	return nil
}
