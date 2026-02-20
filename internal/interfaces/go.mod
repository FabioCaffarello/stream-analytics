module github.com/market-raccoon/internal/interfaces

go 1.25.6

require (
	github.com/anthdm/hollywood v1.0.5
	github.com/gorilla/websocket v1.5.3
	github.com/market-raccoon/internal/actors v0.0.0
	github.com/market-raccoon/internal/core/delivery v0.0.0
	github.com/market-raccoon/internal/core/insights v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
	github.com/prometheus/client_golang v1.18.0
)

require (
	github.com/DataDog/gostackparse v0.7.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/market-raccoon/internal/core/marketdata v0.0.0 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	golang.org/x/sys v0.40.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/market-raccoon/internal/actors => ../actors
	github.com/market-raccoon/internal/core/aggregation => ../core/aggregation
	github.com/market-raccoon/internal/core/delivery => ../core/delivery
	github.com/market-raccoon/internal/core/insights => ../core/insights
	github.com/market-raccoon/internal/core/marketdata => ../core/marketdata
	github.com/market-raccoon/internal/shared => ../shared
)
