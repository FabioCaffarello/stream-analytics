package domain

import "github.com/market-raccoon/internal/shared/problem"

// UpdateSequence carries Binance depth delta sequence boundaries (U/u).
type UpdateSequence struct {
	First int64
	Final int64
}

// NewUpdateSequence validates a depth update sequence pair.
func NewUpdateSequence(first, final int64) (UpdateSequence, *problem.Problem) {
	if first <= 0 {
		return UpdateSequence{}, problem.WithDetail(
			problem.New(problem.ValidationFailed, "update_sequence.first must be > 0"),
			"field", "update_sequence.first",
		)
	}
	if final <= 0 {
		return UpdateSequence{}, problem.WithDetail(
			problem.New(problem.ValidationFailed, "update_sequence.final must be > 0"),
			"field", "update_sequence.final",
		)
	}
	if final < first {
		return UpdateSequence{}, problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "update_sequence.final must be >= first (first=%d final=%d)", first, final),
			"field", "update_sequence",
		)
	}
	return UpdateSequence{First: first, Final: final}, nil
}
