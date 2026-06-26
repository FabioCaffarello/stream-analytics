package domain

import (
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// MarketType classifies which Binance market an instrument belongs to.
type MarketType string

const (
	MarketTypeSpot         MarketType = "SPOT"
	MarketTypeUSDMFutures  MarketType = "USD_M_FUTURES"
	MarketTypeCOINMFutures MarketType = "COIN_M_FUTURES"
)

// NewMarketType validates and normalizes market type labels.
func NewMarketType(raw string) (MarketType, *problem.Problem) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	switch MarketType(v) {
	case MarketTypeSpot, MarketTypeUSDMFutures, MarketTypeCOINMFutures:
		return MarketType(v), nil
	default:
		return "", problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "market_type must be SPOT|USD_M_FUTURES|COIN_M_FUTURES, got %q", raw),
			"field", "market_type",
		)
	}
}

func (m MarketType) String() string { return string(m) }
