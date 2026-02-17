package app

import "github.com/market-raccoon/internal/core/marketdata/ports"

// MarketDataServiceConfig configures all use cases exposed by MarketDataService.
type MarketDataServiceConfig struct {
	Ingest IngestConfig
	Clock  ports.Clock
	Seq    ports.Sequencer
	Pub    ports.EventPublisher
}

// MarketDataService is the entrypoint facade for the marketdata bounded context.
type MarketDataService struct {
	Ingest *IngestMarketData
}

// NewMarketDataService creates all marketdata use cases from a single config.
func NewMarketDataService(cfg MarketDataServiceConfig) *MarketDataService {
	return &MarketDataService{
		Ingest: NewIngestMarketDataWithConfig(cfg.Clock, cfg.Seq, cfg.Pub, cfg.Ingest),
	}
}
