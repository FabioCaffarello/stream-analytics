// Package bybit provides Bybit-specific market-data adapter helpers.
package bybit

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	// DefaultSpotWSBaseURL is Bybit's v5 public spot websocket endpoint.
	DefaultSpotWSBaseURL = "wss://stream.bybit.com/v5/public/spot"
	// DefaultUSDMFuturesWSBaseURL is Bybit's v5 public linear websocket endpoint.
	DefaultUSDMFuturesWSBaseURL = "wss://stream.bybit.com/v5/public/linear"
	// DefaultCOINMFuturesWSBaseURL is Bybit's v5 public inverse websocket endpoint.
	DefaultCOINMFuturesWSBaseURL = "wss://stream.bybit.com/v5/public/inverse"
)

// BuildEndpoint builds a Bybit endpoint for the provided market type.
// Bybit subscribes via JSON messages after connect, but we still validate tickers
// here to keep config errors fail-fast and deterministic.
func BuildEndpoint(baseURL string, tickers []string, marketType string) (string, *problem.Problem) {
	mt, p := domain.NewMarketType(marketType)
	if p != nil {
		return "", problem.WithDetail(
			problem.WithDetail(problem.New(problem.ValidationFailed, "bybit endpoint: invalid market type"), "reason", "invalid_market_type"),
			"market_type", marketType,
		)
	}
	if len(tickers) == 0 {
		return "", problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit endpoint requires at least one ticker"),
			"reason", "missing_tickers",
		)
	}
	for i, rawTicker := range tickers {
		if naming.CanonicalInstrument(rawTicker) == "" {
			return "", problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "invalid ticker %q", rawTicker),
				"ticker_index", i,
			)
		}
	}

	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURLForMarketType(mt)
	}
	return strings.TrimRight(baseURL, "/"), nil
}

// BuildSubscriptions returns Bybit subscribe frames for trade+bookdelta topics.
func BuildSubscriptions(tickers []string) ([][]byte, *problem.Problem) {
	if len(tickers) == 0 {
		return nil, problem.WithDetail(
			problem.New(problem.ValidationFailed, "bybit subscriptions require at least one ticker"),
			"reason", "missing_tickers",
		)
	}

	args := make([]string, 0, len(tickers)*2)
	for i, rawTicker := range tickers {
		symbol := naming.CanonicalInstrument(rawTicker)
		if symbol == "" {
			return nil, problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "invalid ticker %q", rawTicker),
				"ticker_index", i,
			)
		}
		args = append(args,
			fmt.Sprintf("publicTrade.%s", symbol),
			fmt.Sprintf("orderbook.50.%s", symbol),
		)
	}

	body, err := json.Marshal(map[string]any{
		"op":   "subscribe",
		"args": args,
	})
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "bybit subscriptions: marshal failed")
	}
	return [][]byte{body}, nil
}

func defaultBaseURLForMarketType(marketType domain.MarketType) string {
	switch marketType {
	case domain.MarketTypeSpot:
		return DefaultSpotWSBaseURL
	case domain.MarketTypeUSDMFutures:
		return DefaultUSDMFuturesWSBaseURL
	case domain.MarketTypeCOINMFutures:
		return DefaultCOINMFuturesWSBaseURL
	default:
		return DefaultSpotWSBaseURL
	}
}
