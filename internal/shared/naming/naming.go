// Package naming provides canonical normalization of venue and instrument identifiers.
// All functions are pure, idempotent, and dependency-free.
package naming

import (
	"strings"
	"unicode"
)

// CanonicalVenue normalizes a venue identifier to uppercase with no surrounding
// whitespace. E.g. "  binance  " → "BINANCE", "Bybit" → "BYBIT".
func CanonicalVenue(venue string) string {
	return strings.ToUpper(strings.TrimSpace(venue))
}

// CanonicalInstrument normalizes an instrument to uppercase, trims whitespace,
// and removes common separators (/, -, _) between the base and quote parts.
// E.g. "btc/usdt" → "BTCUSDT", "BTC-PERP" → "BTCPERP", "eth_usd" → "ETHUSD".
//
// Note: this is a best-effort normalization for matching purposes.
// The canonical representation preserves the uppercase letters and digits only.
func CanonicalInstrument(instrument string) string {
	s := strings.ToUpper(strings.TrimSpace(instrument))
	// Remove common separator characters.
	s = strings.NewReplacer("/", "", "-", "", "_", "", ".", "").Replace(s)
	return s
}

// CanonicalSymbol returns a canonical symbol by combining venue and instrument.
// Format: "<VENUE>:<INSTRUMENT>" — e.g. "BINANCE:BTCUSDT".
func CanonicalSymbol(venue, instrument string) string {
	return CanonicalVenue(venue) + ":" + CanonicalInstrument(instrument)
}

// NormalizeEventType lowercases and trims an event type string.
// E.g. "  MarketData.Trade  " → "marketdata.trade".
func NormalizeEventType(eventType string) string {
	return strings.ToLower(strings.TrimSpace(eventType))
}

// NormalizeTimeframe lowercases and trims a timeframe string.
// E.g. "  5M  " → "5m", "1H" → "1h".
func NormalizeTimeframe(tf string) string {
	return strings.ToLower(strings.TrimSpace(tf))
}

// NormalizeSide lowercases and trims a trade side string.
// E.g. "  BUY  " → "buy", "Sell" → "sell".
func NormalizeSide(side string) string {
	return strings.ToLower(strings.TrimSpace(side))
}

// IsValidIdentifier reports whether s is a non-empty string containing only
// alphanumeric characters, hyphens, underscores, and dots.
// Useful for validating venue/instrument values before canonicalization.
func IsValidIdentifier(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) &&
			r != '-' && r != '_' && r != '.' && r != '/' {
			return false
		}
	}
	return true
}
