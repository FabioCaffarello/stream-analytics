module github.com/market-raccoon/internal/core/portfolio

go 1.25.6

require (
	github.com/market-raccoon/internal/core/execution v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
)

replace github.com/market-raccoon/internal/core/execution => ../execution

replace github.com/market-raccoon/internal/shared => ../../shared

replace github.com/market-raccoon/internal/core/strategy => ../strategy
