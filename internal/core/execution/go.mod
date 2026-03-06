module github.com/market-raccoon/internal/core/execution

go 1.25.6

require (
	github.com/market-raccoon/internal/core/strategy v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
)

replace github.com/market-raccoon/internal/core/strategy => ../strategy

replace github.com/market-raccoon/internal/shared => ../../shared

replace github.com/market-raccoon/internal/core/portfolio => ../portfolio
