module github.com/market-raccoon/cmd/emulator

go 1.25.6

require (
	github.com/market-raccoon/internal/adapters v0.0.0
	github.com/market-raccoon/internal/application v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
)

replace github.com/market-raccoon/internal/adapters => ../../internal/adapters

replace github.com/market-raccoon/internal/application => ../../internal/application

replace github.com/market-raccoon/internal/shared => ../../internal/shared
