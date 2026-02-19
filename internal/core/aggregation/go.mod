module github.com/market-raccoon/internal/core/aggregation

go 1.25.6

require (
	github.com/market-raccoon/internal/adapters v0.0.0
	github.com/market-raccoon/internal/core/marketdata v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/market-raccoon/internal/core/insights v0.0.0 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/prometheus/client_golang v1.18.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	golang.org/x/sys v0.40.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/market-raccoon/internal/adapters => ../../../internal/adapters
	github.com/market-raccoon/internal/core/insights => ../../../internal/core/insights
	github.com/market-raccoon/internal/core/marketdata => ../../../internal/core/marketdata
	github.com/market-raccoon/internal/shared => ../../../internal/shared
)
