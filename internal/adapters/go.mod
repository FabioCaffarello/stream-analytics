module github.com/market-raccoon/internal/adapters

go 1.25.6

require github.com/market-raccoon/internal/shared v0.0.0
require github.com/market-raccoon/internal/core/marketdata v0.0.0

replace github.com/market-raccoon/internal/shared => ../shared
replace github.com/market-raccoon/internal/core/marketdata => ../core/marketdata
