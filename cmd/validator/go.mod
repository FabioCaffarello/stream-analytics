module github.com/FabioCaffarello/stream-analytics/cmd/validator

go 1.25.6

require (
	github.com/FabioCaffarello/stream-analytics/internal/adapters v0.0.0
	github.com/FabioCaffarello/stream-analytics/internal/application v0.0.0
	github.com/FabioCaffarello/stream-analytics/internal/shared v0.0.0
)

require (
	github.com/FabioCaffarello/stream-analytics/internal/contracts v0.0.0 // indirect
	github.com/FabioCaffarello/stream-analytics/internal/core/aggregation v0.0.0 // indirect
	github.com/FabioCaffarello/stream-analytics/internal/core/evidence v0.0.0 // indirect
	github.com/FabioCaffarello/stream-analytics/internal/core/insights v0.0.0 // indirect
	github.com/FabioCaffarello/stream-analytics/internal/core/marketdata v0.0.0 // indirect
	github.com/FabioCaffarello/stream-analytics/internal/core/marketmodel v0.0.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/nats-io/nats.go v1.48.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/prometheus/client_golang v1.18.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace (
	github.com/FabioCaffarello/stream-analytics/internal/actors => ../../internal/actors
	github.com/FabioCaffarello/stream-analytics/internal/adapters => ../../internal/adapters
	github.com/FabioCaffarello/stream-analytics/internal/application => ../../internal/application
	github.com/FabioCaffarello/stream-analytics/internal/contracts => ../../internal/contracts
	github.com/FabioCaffarello/stream-analytics/internal/core/aggregation => ../../internal/core/aggregation
	github.com/FabioCaffarello/stream-analytics/internal/core/delivery => ../../internal/core/delivery
	github.com/FabioCaffarello/stream-analytics/internal/core/evidence => ../../internal/core/evidence
	github.com/FabioCaffarello/stream-analytics/internal/core/insights => ../../internal/core/insights
	github.com/FabioCaffarello/stream-analytics/internal/core/marketdata => ../../internal/core/marketdata
	github.com/FabioCaffarello/stream-analytics/internal/core/marketmodel => ../../internal/core/marketmodel
	github.com/FabioCaffarello/stream-analytics/internal/core/workspace => ../../internal/core/workspace
	github.com/FabioCaffarello/stream-analytics/internal/interfaces => ../../internal/interfaces
	github.com/FabioCaffarello/stream-analytics/internal/shared => ../../internal/shared
)
