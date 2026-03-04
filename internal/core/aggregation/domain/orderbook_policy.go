package domain

import "github.com/market-raccoon/internal/shared/problem"

// OrderBookLimitsPolicy centralizes order book boundedness limits.
type OrderBookLimitsPolicy struct {
	// MaxLevelsPerSide caps bid and ask levels independently.
	// Values <= 0 mean unbounded.
	MaxLevelsPerSide int
}

// NewOrderBookLimitsPolicy validates and builds the boundedness policy.
func NewOrderBookLimitsPolicy(maxLevelsPerSide int) (OrderBookLimitsPolicy, *problem.Problem) {
	if maxLevelsPerSide < 0 {
		return OrderBookLimitsPolicy{}, problem.New(problem.ValidationFailed, "max_levels must be >= 0")
	}
	return OrderBookLimitsPolicy{MaxLevelsPerSide: maxLevelsPerSide}, nil
}

// IsBounded reports whether hard-cap pruning is enabled.
func (p OrderBookLimitsPolicy) IsBounded() bool {
	return p.MaxLevelsPerSide > 0
}
