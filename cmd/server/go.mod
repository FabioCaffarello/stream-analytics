module github.com/market-raccoon/cmd/server

go 1.25.6

require (
	github.com/anthdm/hollywood v1.0.5
	github.com/market-raccoon/internal/actors v0.0.0
	github.com/market-raccoon/internal/adapters v0.0.0
	github.com/market-raccoon/internal/interfaces v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
)

require (
	github.com/DataDog/gostackparse v0.7.0 // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	google.golang.org/protobuf v1.32.0 // indirect
)

replace (
	github.com/market-raccoon/internal/actors => ../../internal/actors
	github.com/market-raccoon/internal/adapters => ../../internal/adapters
	github.com/market-raccoon/internal/interfaces => ../../internal/interfaces
	github.com/market-raccoon/internal/shared => ../../internal/shared
)
