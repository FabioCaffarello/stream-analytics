package federation

import (
	"context"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

var _ aggports.DeltaVolumeReader = (*FederatedDeltaVolumeReader)(nil)

type FederatedDeltaVolumeReader struct {
	hot   aggports.DeltaVolumeReader
	cold  aggports.DeltaVolumeReader
	cfg   Config
	nowFn func() int64
}

func NewFederatedDeltaVolumeReader(hot, cold aggports.DeltaVolumeReader, cfg Config) *FederatedDeltaVolumeReader {
	return &FederatedDeltaVolumeReader{hot: hot, cold: cold, cfg: cfg, nowFn: systemNowMs}
}

func (r *FederatedDeltaVolumeReader) GetDeltaVolumeRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.DeltaVolumeWindowV1, *problem.Problem) {
	rt := route(fromMs, toMs, r.cfg.HotWindowMs, r.nowFn)
	switch rt {
	case routeColdOnly:
		return queryOrFallback(ctx, r.cold, r.hot, venue, instrument, timeframe, fromMs, toMs, limit, dvQuery)
	case routeHotOnly:
		return queryOrFallback(ctx, r.hot, r.cold, venue, instrument, timeframe, fromMs, toMs, limit, dvQuery)
	default:
		return mergeRange(ctx, r.hot, r.cold, venue, instrument, timeframe, fromMs, toMs, limit, dvQuery, dvWS)
	}
}

func dvQuery(ctx context.Context, rd aggports.DeltaVolumeReader, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.DeltaVolumeWindowV1, *problem.Problem) {
	return rd.GetDeltaVolumeRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
}

func dvWS(d aggdomain.DeltaVolumeWindowV1) int64 { return d.WindowStartTs }
