package timescale

import (
	"context"
	"encoding/json"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	domain "github.com/market-raccoon/internal/core/portfolio/domain"
	ports "github.com/market-raccoon/internal/core/portfolio/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ ports.PortfolioSummaryWriter = (*PgPortfolioSummaryWriter)(nil)

// PgPortfolioSummaryWriter persists global portfolio summary snapshots in Timescale.
type PgPortfolioSummaryWriter struct {
	exec adapterstorage.SQLExecutor
}

func NewPgPortfolioSummaryWriter(pool *Pool) *PgPortfolioSummaryWriter {
	if pool == nil {
		return &PgPortfolioSummaryWriter{}
	}
	return &PgPortfolioSummaryWriter{exec: pool}
}

func NewPgPortfolioSummaryWriterWithExecutor(exec adapterstorage.SQLExecutor) *PgPortfolioSummaryWriter {
	return &PgPortfolioSummaryWriter{exec: exec}
}

func (w *PgPortfolioSummaryWriter) UpsertPortfolioSummary(ctx context.Context, sum domain.PortfolioSummaryV1) *problem.Problem {
	if w == nil || w.exec == nil {
		return problem.New(problem.ValidationFailed, "timescale portfolio summary writer is nil")
	}
	if p := sum.Validate(); p != nil {
		return p
	}

	accountsJSON, err := json.Marshal(sum.Accounts)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal accounts failed")
	}
	fillJSON, err := json.Marshal(sum.FillSummary)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal fill_summary failed")
	}

	const upsertSQL = `
INSERT INTO portfolio_summary (
    summary_id, projected_at_ms,
    global_equity_usd, global_realized_usd, global_unrealized,
    global_margin_used, global_leverage,
    total_position_count, total_open_orders,
    accounts, fill_summary
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (summary_id) DO UPDATE SET
    projected_at_ms = EXCLUDED.projected_at_ms,
    global_equity_usd = EXCLUDED.global_equity_usd,
    global_realized_usd = EXCLUDED.global_realized_usd,
    global_unrealized = EXCLUDED.global_unrealized,
    global_margin_used = EXCLUDED.global_margin_used,
    global_leverage = EXCLUDED.global_leverage,
    total_position_count = EXCLUDED.total_position_count,
    total_open_orders = EXCLUDED.total_open_orders,
    accounts = EXCLUDED.accounts,
    fill_summary = EXCLUDED.fill_summary`

	args := []any{
		sum.SummaryID,
		sum.ProjectedAtMs,
		sum.GlobalEquityUSD,
		sum.GlobalRealizedUSD,
		sum.GlobalUnrealized,
		sum.GlobalMarginUsed,
		sum.GlobalLeverage,
		sum.TotalPositionCount,
		sum.TotalOpenOrders,
		accountsJSON,
		fillJSON,
	}

	if _, p := w.exec.Exec(ctx, upsertSQL, args...); p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale portfolio summary upsert failed")
	}
	return nil
}
