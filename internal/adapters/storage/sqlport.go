package storage

import (
	"context"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// SQLExecutor abstracts a SQL execution surface for adapter tests.
type SQLExecutor interface {
	Exec(ctx context.Context, query string, args ...any) (int64, *problem.Problem)
	QueryRow(ctx context.Context, query string, args ...any) Row
}

// Row abstracts a single-row scan result.
type Row interface {
	Scan(dest ...any) error
}

// BatchInserter abstracts append/flush lifecycle for batch inserts.
type BatchInserter interface {
	AppendRow(ctx context.Context, values ...any) *problem.Problem
	Flush(ctx context.Context) (int64, *problem.Problem)
	Close() *problem.Problem
}
