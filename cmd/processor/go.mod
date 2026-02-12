module github.com/market-raccoon/cmd/processor

go 1.25.6

require (
	github.com/anthdm/hollywood v1.0.5
	github.com/market-raccoon/internal/actors v0.0.0
	github.com/market-raccoon/internal/adapters v0.0.0
	github.com/market-raccoon/internal/core/aggregation v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
)

replace (
	github.com/market-raccoon/internal/actors          => ../../internal/actors
	github.com/market-raccoon/internal/adapters         => ../../internal/adapters
	github.com/market-raccoon/internal/core/aggregation => ../../internal/core/aggregation
	github.com/market-raccoon/internal/shared           => ../../internal/shared
)
