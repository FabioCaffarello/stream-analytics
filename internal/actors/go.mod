module github.com/market-raccoon/internal/actors

go 1.25.6

require (
	github.com/anthdm/hollywood v1.0.5
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/market-raccoon/internal/adapters v0.0.0
	github.com/market-raccoon/internal/contracts v0.0.0
	github.com/market-raccoon/internal/core/aggregation v0.0.0
	github.com/market-raccoon/internal/core/delivery v0.0.0
	github.com/market-raccoon/internal/core/evidence v0.0.0
	github.com/market-raccoon/internal/core/execution v0.0.0
	github.com/market-raccoon/internal/core/insights v0.0.0
	github.com/market-raccoon/internal/core/marketdata v0.0.0
	github.com/market-raccoon/internal/core/marketmodel v0.0.0
	github.com/market-raccoon/internal/core/portfolio v0.0.0
	github.com/market-raccoon/internal/core/signal v0.0.0
	github.com/market-raccoon/internal/core/signals v0.0.0
	github.com/market-raccoon/internal/core/strategy v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
	github.com/prometheus/client_golang v1.18.0
	go.opentelemetry.io/otel v1.40.0
)

require (
	github.com/DataDog/gostackparse v0.7.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/market-raccoon/internal/adapters => ../adapters
	github.com/market-raccoon/internal/contracts => ../contracts
	github.com/market-raccoon/internal/core/aggregation => ../core/aggregation
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
)
