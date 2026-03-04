package common

import (
	"math"
	"testing"
)

func TestClassifyTradeValidationReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   struct {
			price   float64
			size    float64
			side    string
			tradeID string
			tsMs    int64
		}
		want string
	}{
		{name: "nan price", in: struct {
			price   float64
			size    float64
			side    string
			tradeID string
			tsMs    int64
		}{price: math.NaN(), size: 1, side: "buy", tradeID: "1", tsMs: 1}, want: "nan_price"},
		{name: "nan size", in: struct {
			price   float64
			size    float64
			side    string
			tradeID string
			tsMs    int64
		}{price: 1, size: math.NaN(), side: "buy", tradeID: "1", tsMs: 1}, want: "nan_size"},
		{name: "negative price", in: struct {
			price   float64
			size    float64
			side    string
			tradeID string
			tsMs    int64
		}{price: -1, size: 1, side: "buy", tradeID: "1", tsMs: 1}, want: "neg_price"},
		{name: "negative size", in: struct {
			price   float64
			size    float64
			side    string
			tradeID string
			tsMs    int64
		}{price: 1, size: -1, side: "buy", tradeID: "1", tsMs: 1}, want: "neg_size"},
		{name: "zero price", in: struct {
			price   float64
			size    float64
			side    string
			tradeID string
			tsMs    int64
		}{price: 0, size: 1, side: "buy", tradeID: "1", tsMs: 1}, want: "zero_price"},
		{name: "zero size", in: struct {
			price   float64
			size    float64
			side    string
			tradeID string
			tsMs    int64
		}{price: 1, size: 0, side: "buy", tradeID: "1", tsMs: 1}, want: "zero_size"},
		{name: "empty side", in: struct {
			price   float64
			size    float64
			side    string
			tradeID string
			tsMs    int64
		}{price: 1, size: 1, side: "", tradeID: "1", tsMs: 1}, want: "empty_side"},
		{name: "empty trade id", in: struct {
			price   float64
			size    float64
			side    string
			tradeID string
			tsMs    int64
		}{price: 1, size: 1, side: "buy", tradeID: "", tsMs: 1}, want: "empty_trade_id"},
		{name: "bad timestamp", in: struct {
			price   float64
			size    float64
			side    string
			tradeID string
			tsMs    int64
		}{price: 1, size: 1, side: "buy", tradeID: "1", tsMs: 0}, want: "bad_timestamp"},
		{name: "unknown", in: struct {
			price   float64
			size    float64
			side    string
			tradeID string
			tsMs    int64
		}{price: 1, size: 1, side: "buy", tradeID: "1", tsMs: 1}, want: "unknown"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyTradeValidationReason(tc.in.price, tc.in.size, tc.in.side, tc.in.tradeID, tc.in.tsMs)
			if got != tc.want {
				t.Fatalf("reason=%q want=%q", got, tc.want)
			}
		})
	}
}
