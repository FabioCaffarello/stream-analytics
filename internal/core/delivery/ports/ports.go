package ports

import (
	"context"

	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// RangeItem is transport-agnostic historical output for getrange.
type RangeItem struct {
	Seq      int64
	TsIngest int64
	Payload  []byte
}

// RangeStore resolves historical data for one Subject.
type RangeStore interface {
	GetRange(ctx context.Context, subject domain.Subject, fromMs, toMs int64, limit int) ([]RangeItem, *problem.Problem)
}
