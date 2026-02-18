// Package binance provides Binance-specific market-data adapter helpers.
package binance

import (
	"fmt"
	"strings"

	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	// DefaultWSBaseURL is Binance's combined stream websocket endpoint.
	DefaultWSBaseURL = "wss://stream.binance.com:9443/stream"
	// DefaultFuturesWSBaseURL is Binance USD-M futures combined stream endpoint.
	DefaultFuturesWSBaseURL = "wss://fstream.binance.com/stream"
)

// BuildEndpoint builds a Binance combined-stream endpoint with aggTrade+depth per ticker.
// When includeMarkPriceLiquidation=true, markPrice+forceOrder are appended per ticker.
func BuildEndpoint(baseURL string, tickers []string, includeMarkPriceLiquidation bool) (string, *problem.Problem) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultWSBaseURL
	}
	if len(tickers) == 0 {
		return "", problem.New(problem.ValidationFailed, "binance endpoint requires at least one ticker")
	}

	streamsPerTicker := 2
	if includeMarkPriceLiquidation {
		streamsPerTicker = 4
	}
	streams := make([]string, 0, len(tickers)*streamsPerTicker)
	for _, rawTicker := range tickers {
		symbol := strings.ToLower(naming.CanonicalInstrument(rawTicker))
		if symbol == "" {
			return "", problem.Newf(problem.ValidationFailed, "invalid ticker %q", rawTicker)
		}
		streams = append(streams,
			fmt.Sprintf("%s@aggTrade", symbol),
			fmt.Sprintf("%s@depth@100ms", symbol),
		)
		if includeMarkPriceLiquidation {
			streams = append(streams,
				fmt.Sprintf("%s@markPrice", symbol),
				fmt.Sprintf("%s@forceOrder", symbol),
			)
		}
	}

	return fmt.Sprintf("%s?streams=%s", strings.TrimRight(baseURL, "/"), strings.Join(streams, "/")), nil
}
