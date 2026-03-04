package app

import (
	"context"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/result"
)

// AggregationServiceConfig configures all use cases exposed by AggregationService.
type AggregationServiceConfig struct {
	Update      UpdateConfig
	Candle      BuildCandleConfig
	Stats       BuildStatsConfig
	Tape        BuildTapeConfig
	Publisher   ports.ArtifactPublisher
	Store       ports.HotReadModelStore
	CandleStore ports.CandleHotReadModelStore
	StatsStore  ports.StatsHotReadModelStore
	TapeStore   ports.TapeHotReadModelStore
}

// AggregationService is the entrypoint facade for the aggregation bounded context.
type AggregationService struct {
	UpdateBook *UpdateOrderBookFromEvents
	Candle     *BuildCandleFromEvents
	Stats      *BuildStatsFromEvents
	Tape       *BuildTapeFromTrades
	Funding    *BuildFundingRateFromEvents
}

// NewAggregationService creates all aggregation use cases from a single config.
func NewAggregationService(cfg AggregationServiceConfig) *AggregationService {
	statsUC := NewBuildStatsFromEvents(cfg.Publisher, cfg.StatsStore, cfg.Stats)
	return &AggregationService{
		UpdateBook: NewUpdateOrderBookFromEventsWithConfig(cfg.Publisher, cfg.Store, cfg.Update),
		Candle:     NewBuildCandleFromEvents(cfg.Publisher, cfg.CandleStore, cfg.Candle),
		Stats:      statsUC,
		Tape:       NewBuildTapeFromTrades(cfg.Publisher, cfg.TapeStore, cfg.Tape),
		Funding:    NewBuildFundingRateFromEvents(statsUC),
	}
}

// SnapshotOrderBook returns a read-only snapshot for one orderbook key.
func (s *AggregationService) SnapshotOrderBook(
	_ context.Context,
	key domain.BookID,
) result.Result[domain.SnapshotProduced] {
	if s == nil || s.UpdateBook == nil {
		return result.FailProblem[domain.SnapshotProduced](
			problem.New(problem.ValidationFailed, "aggregation snapshot query is not configured"),
		)
	}
	snap, p := s.UpdateBook.Snapshot(key.Venue, key.Instrument)
	if p != nil {
		return result.FailProblem[domain.SnapshotProduced](p)
	}
	return result.Ok(snap)
}
