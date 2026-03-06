package federation

import (
	"context"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

var _ aggports.TapeReader = (*FederatedTapeReader)(nil)

type FederatedTapeReader struct {
	hot   aggports.TapeReader
	cold  aggports.TapeReader
	cfg   Config
	nowFn func() int64
}

func NewFederatedTapeReader(hot, cold aggports.TapeReader, cfg Config) *FederatedTapeReader {
	return &FederatedTapeReader{hot: hot, cold: cold, cfg: cfg, nowFn: systemNowMs}
}

func (r *FederatedTapeReader) GetTapeRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.TapeWindowV1, *problem.Problem) {
	rt := route(fromMs, toMs, r.cfg.HotWindowMs, r.nowFn)
	switch rt {
	case routeColdOnly:
		return queryOrFallback(ctx, r.cold, r.hot, venue, instrument, timeframe, fromMs, toMs, limit, tapeQuery)
	case routeHotOnly:
		return queryOrFallback(ctx, r.hot, r.cold, venue, instrument, timeframe, fromMs, toMs, limit, tapeQuery)
	default:
		return mergeRange(ctx, r.hot, r.cold, venue, instrument, timeframe, fromMs, toMs, limit, tapeQuery, tapeWS)
	}
}

func tapeQuery(ctx context.Context, rd aggports.TapeReader, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]aggdomain.TapeWindowV1, *problem.Problem) {
	return rd.GetTapeRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
}

func tapeWS(t aggdomain.TapeWindowV1) int64 { return t.WindowStartTs }
