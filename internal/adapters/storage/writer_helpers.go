package storage

import (
	"context"
	"strings"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	insightsports "github.com/market-raccoon/internal/core/insights/ports"
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

// MarshalCandle builds argument list and idempotency key for candle writers.
func MarshalCandle(_ context.Context, c aggdomain.CandleV1) ([]any, string, *problem.Problem) {
	if p := c.Validate(); p != nil {
		return nil, "", p
	}
	idempotencyKey := WindowIdempotencyKey(c.Venue, c.Instrument, c.Timeframe, c.WindowStartTs)
	args := []any{
		c.Venue,
		c.Instrument,
		c.Timeframe,
		c.WindowStartTs,
		c.WindowEndTs,
		c.Open,
		c.High,
		c.Low,
		c.ClosePrice,
		c.Volume,
		c.BuyVolume,
		c.SellVolume,
		c.TradeCount,
		c.SeqFirst,
		c.SeqLast,
		idempotencyKey,
	}
	return args, idempotencyKey, nil
}

// MarshalStats builds argument list and idempotency key for stats writers.
func MarshalStats(_ context.Context, s aggdomain.StatsWindowV1) ([]any, string, *problem.Problem) {
	if p := s.Validate(); p != nil {
		return nil, "", p
	}
	markOpen, markHigh, markLow, markClose := NullableMarkPrice(s)
	fundingAvg, fundingLast := NullableFundingRate(s)
	idempotencyKey := WindowIdempotencyKey(s.Venue, s.Instrument, s.Timeframe, s.WindowStartTs)
	args := []any{
		s.Venue,
		s.Instrument,
		s.Timeframe,
		s.WindowStartTs,
		s.WindowEndTs,
		s.LiqBuyVolume,
		s.LiqSellVolume,
		s.LiqTotalVolume,
		s.LiqCount,
		markOpen,
		markHigh,
		markLow,
		markClose,
		fundingAvg,
		fundingLast,
		s.SeqFirst,
		s.SeqLast,
		idempotencyKey,
	}
	return args, idempotencyKey, nil
}

// MarshalHeatmapCells returns a slice of argument lists (one per cell) for heatmap writers.
func MarshalHeatmapCells(_ context.Context, artifact insightsdomain.HeatmapArtifactV1, sourceIdempotencyKey string) ([][]any, *problem.Problem) {
	if p := artifact.Validate(); p != nil {
		return nil, p
	}
	if strings.TrimSpace(sourceIdempotencyKey) == "" {
		return nil, problem.New(problem.ValidationFailed, "heatmap source idempotency key must not be empty")
	}
	baseKey := HeatmapBaseIdempotencyKey(artifact.Venue, artifact.Instrument, artifact.Timeframe, artifact.WindowStartTs, sourceIdempotencyKey)
	out := make([][]any, 0, len(artifact.Cells))
	for _, cell := range artifact.Cells {
		idempotencyKey := HeatmapCellIdempotencyKey(baseKey, cell.PriceBucketLow, cell.PriceBucketHigh, cell.SizeBucket)
		args := []any{
			artifact.Venue,
			artifact.Instrument,
			artifact.Timeframe,
			artifact.WindowStartTs,
			artifact.WindowEndTs,
			cell.PriceBucketLow,
			cell.PriceBucketHigh,
			cell.SizeBucket,
			cell.BidLiquidity,
			cell.AskLiquidity,
			cell.TradeVolume,
			cell.SeqMin,
			cell.SeqMax,
			cell.Samples,
			sourceIdempotencyKey,
			idempotencyKey,
		}
		out = append(out, args)
	}
	return out, nil
}

// UpsertVolumeProfileBucket performs dedup + upsert for a VolumeProfileBucketUpsert.
// It centralizes the operation-dedup and upsert SQL for Timescale writers.
func UpsertVolumeProfileBucket(ctx context.Context, exec SQLExecutor, upsert insightsports.VolumeProfileBucketUpsert, operationID string) *problem.Problem {
	if exec == nil {
		return problem.New(problem.ValidationFailed, "sql executor is nil")
	}
	const operationDedupSQL = `
INSERT INTO aggregation_volume_profile_oplog (
    operation_id,
    venue,
    instrument,
    timeframe,
    window_start_ts
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (operation_id) DO NOTHING`

	rows, p := exec.Exec(
		ctx,
		operationDedupSQL,
		operationID,
		upsert.Venue,
		upsert.Instrument,
		upsert.Timeframe,
		upsert.WindowStartTs,
	)
	if p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale volume profile dedup failed")
	}
	if rows == 0 {
		// Duplicate operation detected; noop.
		return nil
	}

	const upsertSQL = `
INSERT INTO aggregation_volume_profile (
    venue,
    instrument,
    timeframe,
    window_start_ts,
    bucket_low,
    bucket_high,
    buy_volume,
    sell_volume,
    total_volume,
    seq_min,
    seq_max,
    last_operation_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (venue, instrument, timeframe, window_start_ts, bucket_low, bucket_high) DO UPDATE
SET buy_volume = aggregation_volume_profile.buy_volume + EXCLUDED.buy_volume,
    sell_volume = aggregation_volume_profile.sell_volume + EXCLUDED.sell_volume,
    total_volume = aggregation_volume_profile.total_volume + EXCLUDED.total_volume,
    seq_min = LEAST(aggregation_volume_profile.seq_min, EXCLUDED.seq_min),
    seq_max = GREATEST(aggregation_volume_profile.seq_max, EXCLUDED.seq_max),
    last_operation_id = EXCLUDED.last_operation_id,
    updated_at = NOW()`

	if _, p := exec.Exec(
		ctx,
		upsertSQL,
		upsert.Venue,
		upsert.Instrument,
		upsert.Timeframe,
		upsert.WindowStartTs,
		upsert.BucketLow,
		upsert.BucketHigh,
		upsert.BuyVolume,
		upsert.SellVolume,
		upsert.TotalVolume,
		upsert.SeqMin,
		upsert.SeqMax,
		operationID,
	); p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale volume profile upsert failed")
	}
	return nil
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
