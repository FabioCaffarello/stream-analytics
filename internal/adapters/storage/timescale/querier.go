package timescale

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// pgQuerier abstracts pgx multi-row query execution for reader adapters.
// Satisfied by *pgxpool.Pool and test fakes.
type pgQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
