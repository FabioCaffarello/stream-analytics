module github.com/market-raccoon/internal/contracts

go 1.25.6

require (
	github.com/market-raccoon/internal/core/evidence v0.0.0
	github.com/market-raccoon/internal/core/execution v0.0.0
	github.com/market-raccoon/internal/core/insights v0.0.0
	github.com/market-raccoon/internal/core/marketdata v0.0.0
	github.com/market-raccoon/internal/core/marketmodel v0.0.0
	github.com/market-raccoon/internal/core/portfolio v0.0.0
	github.com/market-raccoon/internal/core/signals v0.0.0
	github.com/market-raccoon/internal/core/strategy v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
	github.com/nats-io/nats.go v1.48.0
	github.com/prometheus/client_golang v1.18.0
	google.golang.org/protobuf v1.36.11
)

replace (
	github.com/market-raccoon/internal/core/delivery => ../core/delivery
	github.com/market-raccoon/internal/core/evidence => ../core/evidence
	github.com/market-raccoon/internal/core/execution => ../core/execution
	github.com/market-raccoon/internal/core/insights => ../core/insights
	github.com/market-raccoon/internal/core/marketdata => ../core/marketdata
	github.com/market-raccoon/internal/core/marketmodel => ../core/marketmodel
	github.com/market-raccoon/internal/core/portfolio => ../core/portfolio
	github.com/market-raccoon/internal/core/signal => ../core/signal
	github.com/market-raccoon/internal/core/signals => ../core/signals
	github.com/market-raccoon/internal/core/strategy => ../core/strategy
	github.com/market-raccoon/internal/shared => ../shared
	github.com/market-raccoon/internal/actors => ../actors
	github.com/market-raccoon/internal/adapters => ../adapters
)
