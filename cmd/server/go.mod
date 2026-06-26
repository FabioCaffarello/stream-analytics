module github.com/FabioCaffarello/stream-analytics/cmd/server

go 1.25.6

require (
	github.com/FabioCaffarello/stream-analytics/internal/actors v0.0.0
	github.com/FabioCaffarello/stream-analytics/internal/adapters v0.0.0
	github.com/FabioCaffarello/stream-analytics/internal/application v0.0.0
	github.com/FabioCaffarello/stream-analytics/internal/contracts v0.0.0
	github.com/FabioCaffarello/stream-analytics/internal/core/aggregation v0.0.0
	github.com/FabioCaffarello/stream-analytics/internal/core/delivery v0.0.0
	github.com/FabioCaffarello/stream-analytics/internal/core/workspace v0.0.0
	github.com/FabioCaffarello/stream-analytics/internal/interfaces v0.0.0-00010101000000-000000000000
	github.com/FabioCaffarello/stream-analytics/internal/shared v0.0.0
	github.com/anthdm/hollywood v1.0.5
	github.com/gorilla/websocket v1.5.3
	github.com/prometheus/client_golang v1.18.0
)

require (
	github.com/ClickHouse/ch-go v0.65.1 // indirect
	github.com/ClickHouse/clickhouse-go/v2 v2.34.0 // indirect
	github.com/DataDog/gostackparse v0.7.0 // indirect
	github.com/FabioCaffarello/stream-analytics/internal/core/evidence v0.0.0 // indirect
	github.com/FabioCaffarello/stream-analytics/internal/core/insights v0.0.0 // indirect
	github.com/FabioCaffarello/stream-analytics/internal/core/marketdata v0.0.0 // indirect
	github.com/FabioCaffarello/stream-analytics/internal/core/marketmodel v0.0.0 // indirect
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.8.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/nats-io/nats.go v1.48.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/paulmach/orb v0.12.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/segmentio/kafka-go v0.4.49 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.40.0 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
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
	github.com/FabioCaffarello/stream-analytics/internal/interfaces => ../../internal/interfaces
	github.com/FabioCaffarello/stream-analytics/internal/shared => ../../internal/shared
)

replace github.com/FabioCaffarello/stream-analytics/internal/core/workspace => ../../internal/core/workspace
