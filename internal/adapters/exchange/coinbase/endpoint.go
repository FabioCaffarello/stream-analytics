package coinbase

import (
	"encoding/json"
	"strings"

	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const DefaultWSBaseURL = "wss://ws-feed.exchange.coinbase.com"

// BuildEndpoint returns Coinbase websocket endpoint.
func BuildEndpoint(baseURL string) string {
	if strings.TrimSpace(baseURL) != "" {
		return strings.TrimRight(baseURL, "/")
	}
	return DefaultWSBaseURL
}

// BuildSubscriptions returns Coinbase subscribe messages.
func BuildSubscriptions(tickers []string) ([][]byte, *problem.Problem) {
	if len(tickers) == 0 {
		return nil, problem.New(problem.ValidationFailed, "coinbase subscriptions require at least one ticker")
	}

	products := make([]string, 0, len(tickers))
	for i, ticker := range tickers {
		product := normalizeProductID(ticker)
		if product == "" {
			return nil, problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "coinbase: invalid ticker %q", ticker),
				"ticker_index", i,
			)
		}
		products = append(products, product)
	}

	body, err := json.Marshal(map[string]any{
		"type":        "subscribe",
		"product_ids": products,
		"channels":    []string{"matches", "level2_batch", "ticker"},
	})
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "coinbase subscriptions: marshal failed")
	}
	return [][]byte{body}, nil
}

func normalizeProductID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "-") {
		parts := strings.Split(s, "-")
		if len(parts) != 2 {
			return ""
		}
		base := naming.CanonicalInstrument(parts[0])
		quote := naming.CanonicalInstrument(parts[1])
		if base == "" || quote == "" {
			return ""
		}
		return base + "-" + quote
	}
	pair := canonicalPairFromSymbol(s)
	if pair == "" {
		return ""
	}
	return pair
}

func canonicalPairFromSymbol(symbol string) string {
	s := naming.CanonicalInstrument(symbol)
	if s == "" {
		return ""
	}
	for _, quote := range []string{"USDT", "USDC", "USD", "BTC", "ETH", "EUR"} {
		if strings.HasSuffix(s, quote) && len(s) > len(quote) {
			base := strings.TrimSuffix(s, quote)
			return base + "-" + quote
		}
	}
	return ""
}
