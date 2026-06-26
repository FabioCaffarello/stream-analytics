module github.com/FabioCaffarello/stream-analytics/cmd/emulator

go 1.25.6

require (
	github.com/FabioCaffarello/stream-analytics/internal/adapters v0.0.0
	github.com/FabioCaffarello/stream-analytics/internal/application v0.0.0
	github.com/FabioCaffarello/stream-analytics/internal/shared v0.0.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/nats-io/nats.go v1.48.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/segmentio/kafka-go v0.4.49 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace github.com/FabioCaffarello/stream-analytics/internal/adapters => ../../internal/adapters

replace github.com/FabioCaffarello/stream-analytics/internal/application => ../../internal/application

replace github.com/FabioCaffarello/stream-analytics/internal/shared => ../../internal/shared
