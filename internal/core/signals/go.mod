module github.com/market-raccoon/internal/core/signals

go 1.25.6

require (
	github.com/market-raccoon/internal/core/evidence v0.0.0
	github.com/market-raccoon/internal/shared v0.0.0
)

replace (
	github.com/market-raccoon/internal/core/evidence => ../../../internal/core/evidence
	github.com/market-raccoon/internal/core/marketmodel => ../marketmodel
	github.com/market-raccoon/internal/shared => ../../../internal/shared
)

replace github.com/market-raccoon/internal/core/signal => ../signal

replace github.com/market-raccoon/internal/actors => ../../actors

replace github.com/market-raccoon/internal/core/delivery => ../delivery
