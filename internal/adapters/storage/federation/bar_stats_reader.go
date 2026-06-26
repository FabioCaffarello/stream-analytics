package federation

import (
	"context"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

var _ aggports.BarStatsReader = (*FederatedBarStatsReader)(nil)

type FederatedBarStatsReader struct {
	hot   aggports.BarStatsReader
	cold  aggports.BarStatsReader
	cfg   Config
	nowFn func() int64
}

func NewFederatedBarStatsReader(hot, cold aggports.BarStatsReader, cfg Config) *FederatedBarStatsReader {
	return &FederatedBarStatsReader{hot: hot, cold: cold, cfg: cfg, nowFn: systemNowMs}
}

func (r *FederatedBarStatsReader) GetBarStatsRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.BarStatsWindowV1, *problem.Problem) {
	rt := route(fromMs, toMs, r.cfg.HotWindowMs, r.nowFn)
	switch rt {
	case routeColdOnly:
		return queryOrFallback(ctx, r.cold, r.hot, venue, instrument, timeframe, fromMs, toMs, limit, bsQuery)
	case routeHotOnly:
		return queryOrFallback(ctx, r.hot, r.cold, venue, instrument, timeframe, fromMs, toMs, limit, bsQuery)
	default:
		return mergeRange(ctx, r.hot, r.cold, venue, instrument, timeframe, fromMs, toMs, limit, bsQuery, bsWS)
	}
}

func bsQuery(ctx context.Context, rd aggports.BarStatsReader, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.BarStatsWindowV1, *problem.Problem) {
	return rd.GetBarStatsRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
}

func bsWS(b aggdomain.BarStatsWindowV1) int64 { return b.WindowStartTs }
