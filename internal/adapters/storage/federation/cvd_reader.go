package federation

import (
	"context"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

var _ aggports.CVDReader = (*FederatedCVDReader)(nil)

type FederatedCVDReader struct {
	hot   aggports.CVDReader
	cold  aggports.CVDReader
	cfg   Config
	nowFn func() int64
}

func NewFederatedCVDReader(hot, cold aggports.CVDReader, cfg Config) *FederatedCVDReader {
	return &FederatedCVDReader{hot: hot, cold: cold, cfg: cfg, nowFn: systemNowMs}
}

func (r *FederatedCVDReader) GetCVDRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.CVDWindowV1, *problem.Problem) {
	rt := route(fromMs, toMs, r.cfg.HotWindowMs, r.nowFn)
	switch rt {
	case routeColdOnly:
		return queryOrFallback(ctx, r.cold, r.hot, venue, instrument, timeframe, fromMs, toMs, limit, cvdQuery)
	case routeHotOnly:
		return queryOrFallback(ctx, r.hot, r.cold, venue, instrument, timeframe, fromMs, toMs, limit, cvdQuery)
	default:
		return mergeRange(ctx, r.hot, r.cold, venue, instrument, timeframe, fromMs, toMs, limit, cvdQuery, cvdWS)
	}
}

func cvdQuery(ctx context.Context, rd aggports.CVDReader, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.CVDWindowV1, *problem.Problem) {
	return rd.GetCVDRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
}

func cvdWS(c aggdomain.CVDWindowV1) int64 { return c.WindowStartTs }
