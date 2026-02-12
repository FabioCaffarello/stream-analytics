module github.com/market-raccoon/internal/core/delivery

go 1.25.6

require github.com/market-raccoon/internal/shared v0.0.0

require google.golang.org/protobuf v1.32.0 // indirect

replace github.com/market-raccoon/internal/core/marketdata => ../../../internal/core/marketdata

replace github.com/market-raccoon/internal/shared => ../../../internal/shared
