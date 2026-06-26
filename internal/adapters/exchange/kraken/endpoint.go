package kraken

import (
	"encoding/json"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

const DefaultWSBaseURL = "wss://ws.kraken.com/v2"

// BuildEndpoint returns Kraken websocket endpoint.
func BuildEndpoint(baseURL string) string {
	if strings.TrimSpace(baseURL) != "" {
		return strings.TrimRight(baseURL, "/")
	}
	return DefaultWSBaseURL
}

// BuildSubscriptions returns Kraken subscribe messages for trade+book+ticker.
func BuildSubscriptions(tickers []string) ([][]byte, *problem.Problem) {
	if len(tickers) == 0 {
		return nil, problem.New(problem.ValidationFailed, "kraken subscriptions require at least one ticker")
	}

	symbols := make([]string, 0, len(tickers))
	for i, ticker := range tickers {
		symbol := normalizeSubscriptionSymbol(ticker)
		if symbol == "" {
			return nil, problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "kraken: invalid ticker %q", ticker),
				"ticker_index", i,
			)
		}
		symbols = append(symbols, symbol)
	}

	channels := []string{"trade", "book", "ticker"}
	msgs := make([][]byte, 0, len(channels))
	for _, channel := range channels {
		params := map[string]any{
			"channel": channel,
			"symbol":  symbols,
		}
		if channel == "book" {
			params["depth"] = 25
		}
		body, err := json.Marshal(map[string]any{
			"method": "subscribe",
			"params": params,
		})
		if err != nil {
			return nil, problem.Wrap(err, problem.Internal, "kraken subscriptions: marshal failed")
		}
		msgs = append(msgs, body)
	}
	return msgs, nil
}

func normalizeSubscriptionSymbol(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}

	for _, sep := range []string{"/", "-", "_"} {
		if !strings.Contains(s, sep) {
			continue
		}
		parts := strings.Split(s, sep)
		if len(parts) != 2 {
			return ""
		}
		base := normalizeAsset(parts[0])
		quote := normalizeAsset(parts[1])
		if base == "" || quote == "" {
			return ""
		}
		return base + "/" + quote
	}

	canonical := naming.CanonicalInstrument(s)
	if canonical == "" {
		return ""
	}
	for _, quote := range []string{"USDT", "USDC", "USD", "EUR", "BTC", "ETH"} {
		if strings.HasSuffix(canonical, quote) && len(canonical) > len(quote) {
			base := normalizeAsset(strings.TrimSuffix(canonical, quote))
			if base == "" {
				return ""
			}
			return base + "/" + normalizeAsset(quote)
		}
	}
	return ""
}

func normalizeAsset(raw string) string {
	asset := naming.CanonicalInstrument(raw)
	if asset == "" {
		return ""
	}
	if asset == "XBT" {
		return "BTC"
	}
	return asset
}
