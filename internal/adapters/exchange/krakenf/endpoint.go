package krakenf

import (
	"encoding/json"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

const DefaultWSBaseURL = "wss://futures.kraken.com/ws/v1"

// BuildEndpoint returns Kraken Futures websocket endpoint.
func BuildEndpoint(baseURL string) string {
	if strings.TrimSpace(baseURL) != "" {
		return strings.TrimRight(baseURL, "/")
	}
	return DefaultWSBaseURL
}

// BuildSubscriptions returns Kraken Futures subscribe messages.
func BuildSubscriptions(tickers []string) ([][]byte, *problem.Problem) {
	if len(tickers) == 0 {
		return nil, problem.New(problem.ValidationFailed, "krakenf subscriptions require at least one ticker")
	}

	products := make([]string, 0, len(tickers))
	for i, ticker := range tickers {
		productID := normalizeProductID(ticker)
		if productID == "" {
			return nil, problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "krakenf: invalid ticker %q", ticker),
				"ticker_index", i,
			)
		}
		products = append(products, productID)
	}

	feeds := []string{"trade", "book", "ticker"}
	msgs := make([][]byte, 0, len(feeds))
	for _, feed := range feeds {
		body, err := json.Marshal(map[string]any{
			"event":       "subscribe",
			"feed":        feed,
			"product_ids": products,
		})
		if err != nil {
			return nil, problem.Wrap(err, problem.Internal, "krakenf subscriptions: marshal failed")
		}
		msgs = append(msgs, body)
	}
	return msgs, nil
}

func normalizeProductID(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "PI_") || strings.HasPrefix(s, "PF_") || strings.HasPrefix(s, "FI_") {
		return s
	}

	canonical := naming.CanonicalInstrument(s)
	if canonical == "" {
		return ""
	}
	for _, quote := range []string{"USDT", "USD", "EUR", "BTC", "ETH"} {
		if strings.HasSuffix(canonical, quote) && len(canonical) > len(quote) {
			base := strings.TrimSuffix(canonical, quote)
			if base == "BTC" {
				base = "XBT"
			}
			return "PI_" + base + quote
		}
	}
	return ""
}
