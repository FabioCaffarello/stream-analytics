package app

import "github.com/market-raccoon/internal/core/aggregation/ports"

// AggregationServiceConfig configures all use cases exposed by AggregationService.
type AggregationServiceConfig struct {
	Update    UpdateConfig
	Publisher ports.ArtifactPublisher
	Store     ports.HotReadModelStore
}

// AggregationService is the entrypoint facade for the aggregation bounded context.
type AggregationService struct {
	UpdateBook *UpdateOrderBookFromEvents
}

// NewAggregationService creates all aggregation use cases from a single config.
func NewAggregationService(cfg AggregationServiceConfig) *AggregationService {
	return &AggregationService{
		UpdateBook: NewUpdateOrderBookFromEventsWithConfig(cfg.Publisher, cfg.Store, cfg.Update),
	}
}
