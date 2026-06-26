package app

import (
	"context"

	mddomain "github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// BuildFundingRateFromEvents is a thin adapter use-case that converts incoming
// mark-price events (which include funding rate) into stats build requests and
// delegates to the existing BuildStatsFromEvents use case.
type BuildFundingRateFromEvents struct {
	statsUC *BuildStatsFromEvents
}

// NewBuildFundingRateFromEvents constructs the adapter. statsUC must be non-nil.
func NewBuildFundingRateFromEvents(statsUC *BuildStatsFromEvents) *BuildFundingRateFromEvents {
	return &BuildFundingRateFromEvents{statsUC: statsUC}
}

// Execute maps a marketdata MarkPriceTickV1 into a BuildStatsRequest and runs
// the underlying BuildStatsFromEvents use case.
func (uc *BuildFundingRateFromEvents) Execute(ctx context.Context, venue, instrument string, seq int64, tsIngest int64, payload mddomain.MarkPriceTickV1) (BuildStatsResponse, *problem.Problem) {
	if uc == nil || uc.statsUC == nil {
		return BuildStatsResponse{}, problem.New(problem.Internal, "build funding: stats use-case not configured")
	}
	req := BuildStatsRequest{
		Venue:       venue,
		Instrument:  instrument,
		Kind:        StatsInputFundingRate,
		Seq:         seq,
		TsIngest:    tsIngest,
		FundingRate: payload.FundingRate,
		MarkPrice:   payload.MarkPrice,
	}
	return uc.statsUC.Execute(ctx, req)
}
