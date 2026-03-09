module github.com/market-raccoon/cmd/strategist

go 1.25.6

require (
	github.com/anthdm/hollywood v1.0.5
	github.com/market-raccoon/internal/actors v0.0.0
	github.com/market-raccoon/internal/adapters v0.0.0
	github.com/market-raccoon/internal/core/strategy v0.0.0
	github.com/market-raccoon/internal/interfaces v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
)

require (
	github.com/DataDog/gostackparse v0.7.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/market-raccoon/internal/core/aggregation v0.0.0 // indirect
	github.com/market-raccoon/internal/core/evidence v0.0.0 // indirect
	github.com/market-raccoon/internal/core/execution v0.0.0 // indirect
	github.com/market-raccoon/internal/core/insights v0.0.0 // indirect
	github.com/market-raccoon/internal/core/marketdata v0.0.0 // indirect
	github.com/market-raccoon/internal/core/marketmodel v0.0.0 // indirect
	github.com/market-raccoon/internal/core/portfolio v0.0.0 // indirect
	github.com/market-raccoon/internal/core/signal v0.0.0 // indirect
	github.com/market-raccoon/internal/core/signals v0.0.0 // indirect
	github.com/market-raccoon/internal/core/workspace v0.0.0 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/nats-io/nats.go v1.48.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/prometheus/client_golang v1.18.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/market-raccoon/internal/actors => ../../internal/actors
	github.com/market-raccoon/internal/adapters => ../../internal/adapters
	github.com/market-raccoon/internal/core/aggregation => ../../internal/core/aggregation
	github.com/market-raccoon/internal/core/delivery => ../../internal/core/delivery
	github.com/market-raccoon/internal/core/evidence => ../../internal/core/evidence
	github.com/market-raccoon/internal/core/execution => ../../internal/core/execution
	github.com/market-raccoon/internal/core/insights => ../../internal/core/insights
	github.com/market-raccoon/internal/core/marketdata => ../../internal/core/marketdata
	github.com/market-raccoon/internal/core/marketmodel => ../../internal/core/marketmodel
	github.com/market-raccoon/internal/core/portfolio => ../../internal/core/portfolio
	github.com/market-raccoon/internal/core/signal => ../../internal/core/signal
	github.com/market-raccoon/internal/core/signals => ../../internal/core/signals
	github.com/market-raccoon/internal/core/strategy => ../../internal/core/strategy
	github.com/market-raccoon/internal/interfaces => ../../internal/interfaces
	github.com/market-raccoon/internal/shared => ../../internal/shared
)

replace github.com/market-raccoon/internal/core/workspace => ../../internal/core/workspace
