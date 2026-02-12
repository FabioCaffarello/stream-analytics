module github.com/market-raccoon/internal/actors

go 1.25.6

require (
	github.com/anthdm/hollywood v1.0.5
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/market-raccoon/internal/core/aggregation v0.0.0
	github.com/market-raccoon/internal/core/delivery v0.0.0
	github.com/market-raccoon/internal/core/marketdata v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
)

require (
	github.com/DataDog/gostackparse v0.7.0 // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	google.golang.org/protobuf v1.32.0 // indirect
)

replace (
	github.com/market-raccoon/internal/core/aggregation => ../core/aggregation
	github.com/market-raccoon/internal/core/delivery    => ../core/delivery
	github.com/market-raccoon/internal/core/marketdata  => ../core/marketdata
	github.com/market-raccoon/internal/shared           => ../shared
)
