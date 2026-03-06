package federation

import (
	"context"

	"github.com/market-raccoon/internal/shared/problem"
)

// rangeQueryFn abstracts a single-reader range query.
type rangeQueryFn[R any, T any] func(ctx context.Context, rd R, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]T, *problem.Problem)

// queryOrFallback queries the primary reader; if nil, falls back to the other.
func queryOrFallback[R any, T any](
	ctx context.Context,
	primary, fallback R,
	venue, instrument, timeframe string,
	fromMs, toMs int64, limit int,
	qFn rangeQueryFn[R, T],
) ([]T, *problem.Problem) {
	if any(primary) != nil {
		return qFn(ctx, primary, venue, instrument, timeframe, fromMs, toMs, limit)
	}
	if any(fallback) != nil {
		return qFn(ctx, fallback, venue, instrument, timeframe, fromMs, toMs, limit)
	}
	return nil, problem.New(problem.Unavailable, "no reader available")
}

// mergeRange queries both readers and merges results by window_start.
func mergeRange[R any, T any](
	ctx context.Context,
	hot, cold R,
	venue, instrument, timeframe string,
	fromMs, toMs int64, limit int,
	qFn rangeQueryFn[R, T],
	wsFn func(T) int64,
) ([]T, *problem.Problem) {
	var hotRes, coldRes []T
	if any(hot) != nil {
		res, p := qFn(ctx, hot, venue, instrument, timeframe, fromMs, toMs, limit)
		if p != nil {
			return nil, p
		}
		hotRes = res
	}
	if any(cold) != nil {
		res, p := qFn(ctx, cold, venue, instrument, timeframe, fromMs, toMs, limit)
		if p != nil {
			return nil, p
		}
		coldRes = res
	}
	merged := mergeByWindowStart(hotRes, coldRes, wsFn)
	return capSlice(merged, limit), nil
}
