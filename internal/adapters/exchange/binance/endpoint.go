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
)

// BuildEndpoint builds a Binance combined-stream endpoint with aggTrade+depth per ticker.
// Example: wss://.../stream?streams=btcusdt@aggTrade/btcusdt@depth@100ms
func BuildEndpoint(baseURL string, tickers []string) (string, *problem.Problem) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultWSBaseURL
	}
	if len(tickers) == 0 {
		return "", problem.New(problem.ValidationFailed, "binance endpoint requires at least one ticker")
	}

	streams := make([]string, 0, len(tickers)*2)
	for _, rawTicker := range tickers {
		symbol := strings.ToLower(naming.CanonicalInstrument(rawTicker))
		if symbol == "" {
			return "", problem.Newf(problem.ValidationFailed, "invalid ticker %q", rawTicker)
		}
		streams = append(streams,
			fmt.Sprintf("%s@aggTrade", symbol),
			fmt.Sprintf("%s@depth@100ms", symbol),
		)
	}

	return fmt.Sprintf("%s?streams=%s", strings.TrimRight(baseURL, "/"), strings.Join(streams, "/")), nil
}
