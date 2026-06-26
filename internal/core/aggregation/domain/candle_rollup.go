package domain

import (
	"sort"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/validation"
)

// RollupCandle aggregates a slice of closed lower-timeframe candles into one
// higher-timeframe candle. Source candles must all share the same venue and
// instrument, must be closed, and must fit within a single target-interval
// window. The returned candle is closed with WindowEndTs = windowStart + intervalMs.
//
// This function is used for historical replay / backfill. The live path uses
// the streaming ApplyClosedCandle method instead.
func RollupCandle(source []CandleV1, toInterval string) (CandleV1, *problem.Problem) {
	if p := validation.NonEmptySliceLen("source", len(source)); p != nil {
		return CandleV1{}, p
	}
	intervalMs, p := TimeframeToMs(toInterval)
	if p != nil {
		return CandleV1{}, p
	}

	// Sort by window start to ensure deterministic fold order.
	sorted := make([]CandleV1, len(source))
	copy(sorted, source)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].WindowStartTs < sorted[j].WindowStartTs
	})

	first := sorted[0]
	if !first.IsClosed {
		return CandleV1{}, problem.New(problem.ValidationFailed, "all source candles must be closed")
	}

	venue := first.Venue
	instrument := first.Instrument
	windowStart := bucketStartDomain(first.WindowStartTs, intervalMs)

	candle, p := NewCandleV1(venue, instrument, toInterval, windowStart)
	if p != nil {
		return CandleV1{}, p
	}

	for i := range sorted {
		src := sorted[i]
		if !src.IsClosed {
			return CandleV1{}, problem.New(problem.ValidationFailed, "all source candles must be closed")
		}
		if src.Venue != venue {
			return CandleV1{}, problem.New(problem.ValidationFailed, "all source candles must have the same venue")
		}
		if src.Instrument != instrument {
			return CandleV1{}, problem.New(problem.ValidationFailed, "all source candles must have the same instrument")
		}
		srcWindowStart := bucketStartDomain(src.WindowStartTs, intervalMs)
		if srcWindowStart != windowStart {
			return CandleV1{}, problem.New(problem.ValidationFailed, "all source candles must belong to the same target window")
		}
		if p := candle.ApplyClosedCandle(src); p != nil {
			return CandleV1{}, p
		}
	}

	if p := candle.Close(windowStart + intervalMs); p != nil {
		return CandleV1{}, p
	}
	return *candle, nil
}

func bucketStartDomain(tsMs, windowMs int64) int64 {
	if windowMs <= 0 {
		return tsMs
	}
	return (tsMs / windowMs) * windowMs
}
