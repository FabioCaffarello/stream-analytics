package naming_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/naming"
)

func TestCanonicalVenue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"binance", "BINANCE"},
		{"  Bybit  ", "BYBIT"},
		{"OKEX", "OKEX"},
		{"kraken", "KRAKEN"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := naming.CanonicalVenue(tc.input); got != tc.want {
				t.Errorf("CanonicalVenue(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCanonicalVenue_idempotent(t *testing.T) {
	v := naming.CanonicalVenue("binance")
	if naming.CanonicalVenue(v) != v {
		t.Error("CanonicalVenue must be idempotent")
	}
}

func TestCanonicalInstrument(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"BTC/USDT", "BTCUSDT"},
		{"btc-perp", "BTCPERP"},
		{"eth_usd", "ETHUSD"},
		{"SOL.USDT", "SOLUSDT"},
		{"  BTC/USD  ", "BTCUSD"},
		{"BTCUSDT", "BTCUSDT"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := naming.CanonicalInstrument(tc.input); got != tc.want {
				t.Errorf("CanonicalInstrument(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCanonicalInstrument_idempotent(t *testing.T) {
	v := naming.CanonicalInstrument("btc/usdt")
	if naming.CanonicalInstrument(v) != v {
		t.Error("CanonicalInstrument must be idempotent")
	}
}

func TestCanonicalSymbol(t *testing.T) {
	got := naming.CanonicalSymbol("binance", "BTC/USDT")
	want := "BINANCE:BTCUSDT"
	if got != want {
		t.Errorf("CanonicalSymbol = %q; want %q", got, want)
	}
}

func TestNormalizeEventType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"MarketData.Trade", "marketdata.trade"},
		{"  AGGREGATION.ORDERBOOK  ", "aggregation.orderbook"},
		{"insights.liquidity_shift", "insights.liquidity_shift"},
	}
	for _, tc := range tests {
		if got := naming.NormalizeEventType(tc.input); got != tc.want {
			t.Errorf("NormalizeEventType(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestIsValidIdentifier(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"binance", true},
		{"BTC-PERP", true},
		{"eth_usd", true},
		{"BTC/USDT", true},
		{"sol.usdt", true},
		{"", false},
		{"   ", false},
		{"BTC USDT", false},
		{"BTC@USDT", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := naming.IsValidIdentifier(tc.input)
			if got != tc.valid {
				t.Errorf("IsValidIdentifier(%q) = %v; want %v", tc.input, got, tc.valid)
			}
		})
	}
}
