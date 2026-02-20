package storage

import (
	"context"
	"strings"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/codec"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/problem"
)

// UpsertAggregationSnapshot writes a SnapshotProduced to Timescale via Exec.
// It centralizes marshaling and common error wrapping used by Timescale/ClickHouse writers.
func UpsertAggregationSnapshot(ctx context.Context, exec SQLExecutor, snap aggdomain.SnapshotProduced) *problem.Problem {
	if exec == nil {
		return problem.New(problem.ValidationFailed, "sql executor is nil")
	}

	bidsJSON, asksJSON, p := MarshalAggregationSnapshot(ctx, snap)
	if p != nil {
		return p
	}

	const upsertSQL = `
INSERT INTO aggregation_orderbook_snapshot (
    venue,
    instrument,
    seq,
    bids_json,
    asks_json,
    created_at
) VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (venue, instrument, seq) DO NOTHING`

	if _, p := exec.Exec(ctx, upsertSQL,
		snap.BookID.Venue,
		snap.BookID.Instrument,
		snap.Seq,
		bidsJSON,
		asksJSON,
	); p != nil {
		return p
	}
	return nil
}

// MarshalAggregationSnapshot marshals bids and asks into JSON bytes for storage writers.
func MarshalAggregationSnapshot(_ context.Context, snap aggdomain.SnapshotProduced) ([]byte, []byte, *problem.Problem) {
	bidsJSON, err := codec.Marshal(snap.Bids)
	if err != nil {
		return nil, nil, problem.Wrap(err, problem.Internal, "marshal bids failed")
	}
	asksJSON, err := codec.Marshal(snap.Asks)
	if err != nil {
		return nil, nil, problem.Wrap(err, problem.Internal, "marshal asks failed")
	}
	return bidsJSON, asksJSON, nil
}

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
	return sharedhash.NewFieldHasher().
		String(venue).
		String(instrument).
		String(timeframe).
		Int64(windowStartTs).
		Hex()
}

// HeatmapBaseIdempotencyKey builds the artifact-level portion of a heatmap
// idempotency key. Per-cell keys are derived from this base.
func HeatmapBaseIdempotencyKey(venue, instrument, timeframe string, windowStartTs int64, sourceIdempotencyKey string) string {
	return sharedhash.NewFieldHasher().
		String(venue).
		String(instrument).
		String(timeframe).
		Int64(windowStartTs).
		String(sourceIdempotencyKey).
		Hex()
}

// HeatmapCellIdempotencyKey builds a per-cell idempotency key from the
// artifact base key and cell coordinates.
func HeatmapCellIdempotencyKey(baseKey string, priceLow, priceHigh float64, sizeBucket string) string {
	return sharedhash.NewFieldHasher().
		String(baseKey).
		Float64(priceLow).
		Float64(priceHigh).
		String(strings.ToUpper(strings.TrimSpace(sizeBucket))).
		Hex()
}
