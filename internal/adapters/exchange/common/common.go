// Package common provides shared primitives for exchange parser implementations.
// Each exchange adapter (binance, bybit, coinbase, etc.) delegates to these
// functions for boilerplate that is identical across all parsers.
package common

import (
	"strconv"
	"strings"
	"sync"

	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

// ParseMeta carries parser diagnostics for observability.
type ParseMeta struct {
	EventType  string
	SkipReason string
	Problem    *problem.Problem
	WSStream   string
	Ticker     string
}

// SkipReasonFromProblem returns "parse_error" if p is non-nil, otherwise "".
func SkipReasonFromProblem(p *problem.Problem) string {
	if p != nil {
		return "parse_error"
	}
	return ""
}

// NormalizeSide maps raw side strings to canonical "buy"/"sell".
// exchangePrefix is used in the error message (e.g. "binance", "coinbase").
func NormalizeSide(side, exchangePrefix string) (string, *problem.Problem) {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "buy":
		return "buy", nil
	case "sell":
		return "sell", nil
	default:
		return "", problem.Newf(problem.ValidationFailed, "%s: unsupported side %q", exchangePrefix, side)
	}
}

// NormalizeMarketType validates and normalizes a market type string.
// If raw is invalid, defaultType is returned.
func NormalizeMarketType(raw string, defaultType domain.MarketType) string {
	mt, p := domain.NewMarketType(raw)
	if p != nil {
		return defaultType.String()
	}
	return mt.String()
}

// NormalizeMarketTypeSpot normalizes a market type defaulting to Spot.
func NormalizeMarketTypeSpot(raw string) string {
	return NormalizeMarketType(raw, domain.MarketTypeSpot)
}

// NormalizeMarketTypeFutures normalizes a market type defaulting to USDMFutures.
func NormalizeMarketTypeFutures(raw string) string {
	return NormalizeMarketType(raw, domain.MarketTypeUSDMFutures)
}

// BuildTradeIdempotencyKey builds a deterministic idempotency key for trade events.
func BuildTradeIdempotencyKey(venue, instrument, tradeID string) string {
	return "venue=" + venue + "|instrument=" + instrument + "|trade_id=" + tradeID
}

// BuildDepthIdempotencyKey builds a deterministic idempotency key for depth events.
func BuildDepthIdempotencyKey(venue, instrument string, finalUpdateID int64) string {
	return "venue=" + venue + "|instrument=" + instrument + "|final_update_id=" + strconv.FormatInt(finalUpdateID, 10)
}

// BuildMarkPriceIdempotencyKey builds an idempotency key for mark price events.
// Returns "" if sequence <= 0 (no reliable idempotency source).
func BuildMarkPriceIdempotencyKey(venue, instrument string, sequence int64) string {
	if sequence <= 0 {
		return ""
	}
	return "venue=" + venue + "|instrument=" + instrument + "|sequence=" + strconv.FormatInt(sequence, 10)
}

// PairExtractor extracts a "BASE-QUOTE" pair from a venue-specific symbol.
// Returns "" if the symbol cannot be split.
type PairExtractor func(venueSymbol string) string

// metadataPool is a per-exchange metadata cache keyed by "venueSymbol|canonical|marketType".
type metadataPool struct {
	cache sync.Map
}

var globalMetadataPool = &metadataPool{}

// BuildInstrumentMetadata builds (and caches) standard instrument metadata.
// extractPair is an optional exchange-specific pair extractor; pass nil to skip.
func BuildInstrumentMetadata(venueSymbol, canonical, marketType string, extractPair PairExtractor) map[string]string {
	return globalMetadataPool.build(venueSymbol, canonical, marketType, extractPair)
}

func (p *metadataPool) build(venueSymbol, canonical, marketType string, extractPair PairExtractor) map[string]string {
	cacheKey := venueSymbol + "|" + canonical + "|" + marketType
	if val, ok := p.cache.Load(cacheKey); ok {
		return cloneMetadata(val.(map[string]string))
	}

	meta := map[string]string{
		"instrument_venue_symbol": strings.ToUpper(strings.TrimSpace(venueSymbol)),
		"instrument_canonical":    canonical,
		"instrument_market_type":  marketType,
	}
	if extractPair != nil {
		if pair := extractPair(venueSymbol); pair != "" {
			meta["instrument_pair"] = pair
			parts := strings.SplitN(pair, "-", 2)
			if len(parts) == 2 {
				meta["instrument_base"] = parts[0]
				meta["instrument_quote"] = parts[1]
			}
		}
	}

	p.cache.Store(cacheKey, meta)
	return cloneMetadata(meta)
}

func cloneMetadata(src map[string]string) map[string]string {
	cloned := make(map[string]string, len(src))
	for k, v := range src {
		cloned[k] = v
	}
	return cloned
}

// ParseStringLevels parses [][]string price levels (common across Binance, Bybit,
// Coinbase). Each sub-slice must have at least 2 elements: [price, size].
// exchangePrefix is used in error messages (e.g. "binance depthUpdate").
func ParseStringLevels(raw [][]string, exchangePrefix string) ([]domain.PriceLevel, *problem.Problem) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]domain.PriceLevel, 0, len(raw))
	for _, pair := range raw {
		if len(pair) < 2 {
			return nil, problem.Newf(problem.ValidationFailed, "%s: invalid level pair", exchangePrefix)
		}
		price, err := strconv.ParseFloat(pair[0], 64)
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, exchangePrefix+": invalid level price")
		}
		size, err := strconv.ParseFloat(pair[1], 64)
		if err != nil {
			return nil, problem.Wrap(err, problem.ValidationFailed, exchangePrefix+": invalid level size")
		}
		out = append(out, domain.PriceLevel{Price: price, Size: size})
	}
	return out, nil
}

// CanonicalPairFromSuffixList tries to split a canonical instrument into "BASE-QUOTE"
// by matching known quote suffixes. Returns "" if no match found.
// This is the shared pattern used by Binance, Bybit, and Coinbase parsers.
func CanonicalPairFromSuffixList(symbol string, quotes []string) string {
	s := naming.CanonicalInstrument(symbol)
	if s == "" {
		return ""
	}
	for _, quote := range quotes {
		if strings.HasSuffix(s, quote) && len(s) > len(quote) {
			base := strings.TrimSuffix(s, quote)
			return base + "-" + quote
		}
	}
	return ""
}
