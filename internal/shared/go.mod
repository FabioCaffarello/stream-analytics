module github.com/market-raccoon/internal/shared

go 1.25.6

require (
	github.com/market-raccoon/internal/core/evidence v0.0.0
	github.com/market-raccoon/internal/core/insights v0.0.0
	github.com/market-raccoon/internal/core/marketdata v0.0.0
	github.com/market-raccoon/internal/core/signals v0.0.0
	github.com/nats-io/nats.go v1.48.0
	github.com/prometheus/client_golang v1.18.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
)

replace (
	github.com/market-raccoon/internal/core/evidence => ../core/evidence
	github.com/market-raccoon/internal/core/insights => ../core/insights
	github.com/market-raccoon/internal/core/marketdata => ../core/marketdata
	github.com/market-raccoon/internal/core/signals => ../core/signals
)
