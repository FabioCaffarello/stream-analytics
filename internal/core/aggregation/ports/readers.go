// Package ports defines secondary port interfaces for the aggregation context.
package ports

import (
	"context"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// CandleRange represents a time-bounded candle query result.
type CandleRange struct {
	Venue      string
	Instrument string
	Timeframe  string
	FromMs     int64
	ToMs       int64
}

// CandleReader queries cold candle storage for historical data.
type CandleReader interface {
	// GetCandleRange returns candles in [fromMs, toMs] ordered by window_start ASC.
	GetCandleRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]domain.CandleV1, *problem.Problem)

	// GetCandleTimestamps returns only window_start timestamps for gap detection.
	// Much cheaper than GetCandleRange and avoids transferring full candle data.
	GetCandleTimestamps(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem)

	// GetFirstCandle returns the earliest candle by window_start.
	GetFirstCandle(ctx context.Context, venue, instrument, timeframe string) (*domain.CandleV1, *problem.Problem)

	// GetLastCandle returns the latest candle by window_start.
	GetLastCandle(ctx context.Context, venue, instrument, timeframe string) (*domain.CandleV1, *problem.Problem)
}

// StatsReader queries cold stats storage for historical data.
type StatsReader interface {
	// GetStatsTimestamps returns only window_start timestamps for gap detection.
	GetStatsTimestamps(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem)

	// GetStatsRange returns stats windows in [fromMs, toMs] ordered by window_start ASC.
	GetStatsRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]domain.StatsWindowV1, *problem.Problem)

	// GetFirstStats returns the earliest stats window by window_start.
	GetFirstStats(ctx context.Context, venue, instrument, timeframe string) (*domain.StatsWindowV1, *problem.Problem)

	// GetLastStats returns the latest stats window by window_start.
	GetLastStats(ctx context.Context, venue, instrument, timeframe string) (*domain.StatsWindowV1, *problem.Problem)
}

// SnapshotReader queries cold orderbook snapshot storage.
type SnapshotReader interface {
	// GetSnapshotTimestamps returns snapshot timestamps for gap detection.
	GetSnapshotTimestamps(ctx context.Context, venue, instrument string, fromMs, toMs int64) ([]int64, *problem.Problem)
}
