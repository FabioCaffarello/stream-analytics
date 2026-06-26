package federation

import (
	"context"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

var _ aggports.StatsReader = (*FederatedStatsReader)(nil)

// FederatedStatsReader routes stats queries across hot and cold tiers.
type FederatedStatsReader struct {
	hot   aggports.StatsReader
	cold  aggports.StatsReader
	cfg   Config
	nowFn func() int64
}

func NewFederatedStatsReader(hot, cold aggports.StatsReader, cfg Config) *FederatedStatsReader {
	return &FederatedStatsReader{hot: hot, cold: cold, cfg: cfg, nowFn: systemNowMs}
}

func (r *FederatedStatsReader) GetStatsRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.StatsWindowV1, *problem.Problem) {
	rt := route(fromMs, toMs, r.cfg.HotWindowMs, r.nowFn)
	switch rt {
	case routeColdOnly:
		if r.cold != nil {
			return r.cold.GetStatsRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
		}
		if r.hot != nil {
			return r.hot.GetStatsRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
		}
		return nil, problem.New(problem.Unavailable, "no stats reader available")

	case routeHotOnly:
		if r.hot != nil {
			return r.hot.GetStatsRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
		}
		if r.cold != nil {
			return r.cold.GetStatsRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
		}
		return nil, problem.New(problem.Unavailable, "no stats reader available")

	default: // routeBoth
		var hotRes, coldRes []aggdomain.StatsWindowV1
		if r.hot != nil {
			res, p := r.hot.GetStatsRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
			if p != nil {
				return nil, p
			}
			hotRes = res
		}
		if r.cold != nil {
			res, p := r.cold.GetStatsRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
			if p != nil {
				return nil, p
			}
			coldRes = res
		}
		merged := mergeByWindowStart(hotRes, coldRes, func(s aggdomain.StatsWindowV1) int64 { return s.WindowStartTs })
		return capSlice(merged, limit), nil
	}
}

func (r *FederatedStatsReader) GetStatsTimestamps(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem) {
	rt := route(fromMs, toMs, r.cfg.HotWindowMs, r.nowFn)
	switch rt {
	case routeColdOnly:
		rd := r.cold
		if rd == nil {
			rd = r.hot
		}
		if rd == nil {
			return nil, nil
		}
		return rd.GetStatsTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
	case routeHotOnly:
		rd := r.hot
		if rd == nil {
			rd = r.cold
		}
		if rd == nil {
			return nil, nil
		}
		return rd.GetStatsTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
	default:
		var hotTs, coldTs []int64
		if r.hot != nil {
			ts, p := r.hot.GetStatsTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
			if p != nil {
				return nil, p
			}
			hotTs = ts
		}
		if r.cold != nil {
			ts, p := r.cold.GetStatsTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
			if p != nil {
				return nil, p
			}
			coldTs = ts
		}
		return mergeTimestamps(hotTs, coldTs), nil
	}
}

func (r *FederatedStatsReader) GetFirstStats(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	var coldFirst, hotFirst *aggdomain.StatsWindowV1
	if r.cold != nil {
		s, p := r.cold.GetFirstStats(ctx, venue, instrument, timeframe)
		if p != nil {
			return nil, p
		}
		coldFirst = s
	}
	if r.hot != nil {
		s, p := r.hot.GetFirstStats(ctx, venue, instrument, timeframe)
		if p != nil {
			return nil, p
		}
		hotFirst = s
	}
	return pickStatsEarlier(hotFirst, coldFirst), nil
}

func (r *FederatedStatsReader) GetLastStats(ctx context.Context, venue, instrument, timeframe string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	var coldLast, hotLast *aggdomain.StatsWindowV1
	if r.hot != nil {
		s, p := r.hot.GetLastStats(ctx, venue, instrument, timeframe)
		if p != nil {
			return nil, p
		}
		hotLast = s
	}
	if r.cold != nil {
		s, p := r.cold.GetLastStats(ctx, venue, instrument, timeframe)
		if p != nil {
			return nil, p
		}
		coldLast = s
	}
	return pickStatsLater(hotLast, coldLast), nil
}

func pickStatsEarlier(a, b *aggdomain.StatsWindowV1) *aggdomain.StatsWindowV1 {
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

func pickStatsLater(a, b *aggdomain.StatsWindowV1) *aggdomain.StatsWindowV1 {
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
