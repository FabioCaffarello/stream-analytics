module github.com/market-raccoon/internal/interfaces

go 1.25.6

require (
	github.com/anthdm/hollywood v1.0.5
	github.com/gorilla/websocket v1.5.3
	github.com/market-raccoon/internal/actors v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
)

require (
	github.com/DataDog/gostackparse v0.7.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/market-raccoon/internal/core/delivery v0.0.0 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/prometheus/client_golang v1.18.0 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	golang.org/x/sys v0.15.0 // indirect
	google.golang.org/protobuf v1.32.0 // indirect
)

replace (
	github.com/market-raccoon/internal/actors => ../actors
	github.com/market-raccoon/internal/core/aggregation => ../core/aggregation
	github.com/market-raccoon/internal/core/delivery => ../core/delivery
	github.com/market-raccoon/internal/core/marketdata => ../core/marketdata
	github.com/market-raccoon/internal/shared => ../shared
)
