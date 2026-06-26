package common

import (
	"math"
	"strings"
)

// ClassifyTradeValidationReason returns a stable, Prometheus-safe reason label
// for rejected trade payloads.
func ClassifyTradeValidationReason(price, size float64, side, tradeID string, tsMs int64) string {
	switch {
	case math.IsNaN(price):
		return "nan_price"
	case math.IsNaN(size):
		return "nan_size"
	case price < 0:
		return "neg_price"
	case size < 0:
		return "neg_size"
	case price == 0:
		return "zero_price"
	case size == 0:
		return "zero_size"
	case strings.TrimSpace(side) == "":
		return "empty_side"
	case strings.TrimSpace(tradeID) == "":
		return "empty_trade_id"
	case tsMs <= 0:
		return "bad_timestamp"
	default:
		return "unknown"
	}
}
