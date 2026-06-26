package federation

import (
	"context"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

var _ aggports.OIReader = (*FederatedOIReader)(nil)

type FederatedOIReader struct {
	hot   aggports.OIReader
	cold  aggports.OIReader
	cfg   Config
	nowFn func() int64
}

func NewFederatedOIReader(hot, cold aggports.OIReader, cfg Config) *FederatedOIReader {
	return &FederatedOIReader{hot: hot, cold: cold, cfg: cfg, nowFn: systemNowMs}
}

func (r *FederatedOIReader) GetOIRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.OpenInterestWindowV1, *problem.Problem) {
	rt := route(fromMs, toMs, r.cfg.HotWindowMs, r.nowFn)
	switch rt {
	case routeColdOnly:
		return queryOrFallback(ctx, r.cold, r.hot, venue, instrument, timeframe, fromMs, toMs, limit, oiQuery)
	case routeHotOnly:
		return queryOrFallback(ctx, r.hot, r.cold, venue, instrument, timeframe, fromMs, toMs, limit, oiQuery)
	default:
		return mergeRange(ctx, r.hot, r.cold, venue, instrument, timeframe, fromMs, toMs, limit, oiQuery, oiWS)
	}
}

func oiQuery(ctx context.Context, rd aggports.OIReader, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.OpenInterestWindowV1, *problem.Problem) {
	return rd.GetOIRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
}

func oiWS(o aggdomain.OpenInterestWindowV1) int64 { return o.WindowStartTs }
