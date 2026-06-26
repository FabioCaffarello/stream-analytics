package hyperliquid

import (
	"encoding/json"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

const DefaultWSBaseURL = "wss://api.hyperliquid.xyz/ws"

// BuildEndpoint returns the WS endpoint. HyperLiquid uses a single fixed endpoint.
func BuildEndpoint(baseURL string) string {
	if strings.TrimSpace(baseURL) != "" {
		return strings.TrimRight(baseURL, "/")
	}
	return DefaultWSBaseURL
}

// BuildSubscriptions returns per-ticker per-channel subscribe messages.
func BuildSubscriptions(tickers []string) ([][]byte, *problem.Problem) {
	if len(tickers) == 0 {
		return nil, problem.New(problem.ValidationFailed, "hyperliquid subscriptions require at least one ticker")
	}

	msgs := make([][]byte, 0, len(tickers)*2)
	for i, ticker := range tickers {
		coin := toCoinName(ticker)
		if coin == "" {
			return nil, problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "hyperliquid: invalid ticker %q", ticker),
				"ticker_index", i,
			)
		}
		for _, ch := range []string{"trades", "l2Book"} {
			body, err := json.Marshal(map[string]any{
				"method": "subscribe",
				"subscription": map[string]any{
					"type": ch,
					"coin": coin,
				},
			})
			if err != nil {
				return nil, problem.Wrap(err, problem.Internal, "hyperliquid subscriptions: marshal failed")
			}
			msgs = append(msgs, body)
		}
	}
	return msgs, nil
}

// BuildSubscriptionsWithMarkPrice returns per-ticker subscribe messages for
// trades + l2Book, plus a single allMids subscription for mark price data.
func BuildSubscriptionsWithMarkPrice(tickers []string) ([][]byte, *problem.Problem) {
	msgs, p := BuildSubscriptions(tickers)
	if p != nil {
		return nil, p
	}
	allMidsSub, err := json.Marshal(map[string]any{
		"method": "subscribe",
		"subscription": map[string]any{
			"type": "allMids",
		},
	})
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "hyperliquid allMids subscription: marshal failed")
	}
	msgs = append(msgs, allMidsSub)
	return msgs, nil
}

// ToCoinName extracts the coin base symbol from a canonical ticker
// (e.g., "BTCUSDT" → "BTC", "ETHPERP" → "ETH").
func ToCoinName(ticker string) string {
	return toCoinName(ticker)
}

func toCoinName(ticker string) string {
	s := naming.CanonicalInstrument(ticker)
	if s == "" {
		return ""
	}
	for _, suffix := range []string{"USDT", "USDC", "USD", "PERP"} {
		if strings.HasSuffix(s, suffix) && len(s) > len(suffix) {
			return strings.TrimSuffix(s, suffix)
		}
	}
	return s
}
