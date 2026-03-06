package federation

import (
	"context"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.CandleReader = (*FederatedCandleReader)(nil)

// FederatedCandleReader routes candle queries across hot (Pg) and cold (CH)
// storage tiers based on a configurable time boundary.
type FederatedCandleReader struct {
	hot   aggports.CandleReader
	cold  aggports.CandleReader
	cfg   Config
	nowFn func() int64
}

func NewFederatedCandleReader(hot, cold aggports.CandleReader, cfg Config) *FederatedCandleReader {
	return &FederatedCandleReader{hot: hot, cold: cold, cfg: cfg, nowFn: systemNowMs}
}

func (r *FederatedCandleReader) GetCandleRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.CandleV1, *problem.Problem) {
	rt := route(fromMs, toMs, r.cfg.HotWindowMs, r.nowFn)
	switch rt {
	case routeColdOnly:
		if r.cold != nil {
			return r.cold.GetCandleRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
		}
		if r.hot != nil {
			return r.hot.GetCandleRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
		}
		return nil, problem.New(problem.Unavailable, "no candle reader available")

	case routeHotOnly:
		if r.hot != nil {
			return r.hot.GetCandleRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
		}
		if r.cold != nil {
			return r.cold.GetCandleRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
		}
		return nil, problem.New(problem.Unavailable, "no candle reader available")

	default: // routeBoth
		hotRes, coldRes, p := r.queryBothRanges(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
		if p != nil {
			return nil, p
		}
		merged := mergeByWindowStart(hotRes, coldRes, func(c aggdomain.CandleV1) int64 { return c.WindowStartTs })
		return capSlice(merged, limit), nil
	}
}

func (r *FederatedCandleReader) queryBothRanges(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.CandleV1, []aggdomain.CandleV1, *problem.Problem) {
	var hotRes, coldRes []aggdomain.CandleV1
	if r.hot != nil {
		res, p := r.hot.GetCandleRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
		if p != nil {
			return nil, nil, p
		}
		hotRes = res
	}
	if r.cold != nil {
		res, p := r.cold.GetCandleRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
		if p != nil {
			return nil, nil, p
		}
		coldRes = res
	}
	return hotRes, coldRes, nil
}

func (r *FederatedCandleReader) GetCandleTimestamps(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem) {
	rt := route(fromMs, toMs, r.cfg.HotWindowMs, r.nowFn)
	switch rt {
	case routeColdOnly:
		if r.cold != nil {
			return r.cold.GetCandleTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
		}
		if r.hot != nil {
			return r.hot.GetCandleTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
		}
		return nil, nil
	case routeHotOnly:
		if r.hot != nil {
			return r.hot.GetCandleTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
		}
		if r.cold != nil {
			return r.cold.GetCandleTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
		}
		return nil, nil
	default:
		var hotTs, coldTs []int64
		if r.hot != nil {
			ts, p := r.hot.GetCandleTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
			if p != nil {
				return nil, p
			}
			hotTs = ts
		}
		if r.cold != nil {
			ts, p := r.cold.GetCandleTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
			if p != nil {
				return nil, p
			}
			coldTs = ts
		}
		return mergeTimestamps(hotTs, coldTs), nil
	}
}

func (r *FederatedCandleReader) GetFirstCandle(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	// First candle is most likely in cold store (oldest data).
	var coldFirst, hotFirst *aggdomain.CandleV1
	if r.cold != nil {
		c, p := r.cold.GetFirstCandle(ctx, venue, instrument, timeframe)
		if p != nil {
			return nil, p
		}
		coldFirst = c
	}
	if r.hot != nil {
		c, p := r.hot.GetFirstCandle(ctx, venue, instrument, timeframe)
		if p != nil {
			return nil, p
		}
		hotFirst = c
	}
	return pickEarlier(hotFirst, coldFirst), nil
}

func (r *FederatedCandleReader) GetLastCandle(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.CandleV1, *problem.Problem) {
	// Last candle is most likely in hot store (freshest data).
	var coldLast, hotLast *aggdomain.CandleV1
	if r.hot != nil {
		c, p := r.hot.GetLastCandle(ctx, venue, instrument, timeframe)
		if p != nil {
			return nil, p
		}
		hotLast = c
	}
	if r.cold != nil {
		c, p := r.cold.GetLastCandle(ctx, venue, instrument, timeframe)
		if p != nil {
			return nil, p
		}
		coldLast = c
	}
	return pickLater(hotLast, coldLast), nil
}

func pickEarlier(a, b *aggdomain.CandleV1) *aggdomain.CandleV1 {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.WindowStartTs <= b.WindowStartTs {
		return a
	}
	return b
}

func pickLater(a, b *aggdomain.CandleV1) *aggdomain.CandleV1 {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.WindowStartTs >= b.WindowStartTs {
		return a
	}
	return b
}
