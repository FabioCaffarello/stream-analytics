package app

import "github.com/market-raccoon/internal/core/aggregation/ports"

// AggregationServiceConfig configures all use cases exposed by AggregationService.
type AggregationServiceConfig struct {
	Update      UpdateConfig
	Candle      BuildCandleConfig
	Stats       BuildStatsConfig
	Publisher   ports.ArtifactPublisher
	Store       ports.HotReadModelStore
	CandleStore ports.CandleHotReadModelStore
	StatsStore  ports.StatsHotReadModelStore
}

// AggregationService is the entrypoint facade for the aggregation bounded context.
type AggregationService struct {
	UpdateBook *UpdateOrderBookFromEvents
	Candle     *BuildCandleFromEvents
	Stats      *BuildStatsFromEvents
}

// NewAggregationService creates all aggregation use cases from a single config.
func NewAggregationService(cfg AggregationServiceConfig) *AggregationService {
	return &AggregationService{
		UpdateBook: NewUpdateOrderBookFromEventsWithConfig(cfg.Publisher, cfg.Store, cfg.Update),
		Candle:     NewBuildCandleFromEvents(cfg.Publisher, cfg.CandleStore, cfg.Candle),
		Stats:      NewBuildStatsFromEvents(cfg.Publisher, cfg.StatsStore, cfg.Stats),
	}
}
