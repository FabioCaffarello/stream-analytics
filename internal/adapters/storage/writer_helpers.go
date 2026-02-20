// Package storage provides shared helpers for Timescale and ClickHouse writers.
package storage

import (
	"strconv"
	"strings"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

// NullableMarkPrice returns nil for all four OHLC mark-price fields when any
// value is non-positive, signalling the DB column should store NULL.
func NullableMarkPrice(s aggdomain.StatsWindowV1) (open, high, low, close any) {
	if s.MarkPriceOpen <= 0 || s.MarkPriceHigh <= 0 || s.MarkPriceLow <= 0 || s.MarkPriceClose <= 0 {
		return nil, nil, nil, nil
	}
	return s.MarkPriceOpen, s.MarkPriceHigh, s.MarkPriceLow, s.MarkPriceClose
}

// NullableFundingRate returns nil for both funding-rate fields when both are
// zero, signalling the DB column should store NULL.
func NullableFundingRate(s aggdomain.StatsWindowV1) (avg, last any) {
	if s.FundingRateAvg == 0 && s.FundingRateLast == 0 {
		return nil, nil
	}
	return s.FundingRateAvg, s.FundingRateLast
}

// WindowIdempotencyKey builds a deterministic FNV-1a hash for an aggregation
// window keyed by venue, instrument, timeframe and window start timestamp.
// Used by candle and stats writers across both backends.
func WindowIdempotencyKey(venue, instrument, timeframe string, windowStartTs int64) string {
	return sharedhash.HashFieldsFast(
		venue,
		instrument,
		timeframe,
		strconv.FormatInt(windowStartTs, 10),
	)
}

// HeatmapBaseIdempotencyKey builds the artifact-level portion of a heatmap
// idempotency key. Per-cell keys are derived from this base.
func HeatmapBaseIdempotencyKey(venue, instrument, timeframe string, windowStartTs int64, sourceIdempotencyKey string) string {
	return sharedhash.HashFieldsFast(
		venue,
		instrument,
		timeframe,
		strconv.FormatInt(windowStartTs, 10),
		sourceIdempotencyKey,
	)
}

// HeatmapCellIdempotencyKey builds a per-cell idempotency key from the
// artifact base key and cell coordinates.
func HeatmapCellIdempotencyKey(baseKey string, priceLow, priceHigh float64, sizeBucket string) string {
	return sharedhash.HashFieldsFast(
		baseKey,
		strconv.FormatFloat(priceLow, 'f', -1, 64),
		strconv.FormatFloat(priceHigh, 'f', -1, 64),
		strings.ToUpper(strings.TrimSpace(sizeBucket)),
	)
}
