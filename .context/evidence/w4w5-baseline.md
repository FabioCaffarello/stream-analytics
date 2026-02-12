# W4/W5 Baseline (2026-02-12)

## Module list
Source: `./scripts/list-modules.sh`

- ./cmd/consumer
- ./cmd/processor
- ./cmd/server
- ./cmd/store
- ./internal/actors
- ./internal/adapters
- ./internal/core/aggregation
- ./internal/core/delivery
- ./internal/core/insights
- ./internal/core/marketdata
- ./internal/interfaces
- ./internal/shared

## go test ./... baseline
- Full logs: `.context/evidence/w4w5-baseline-tests.txt`
- Result: all modules pass except `./cmd/store` (`./...` matches no packages)

## go test -race ./... baseline
- Full logs: `.context/evidence/w4w5-baseline-race.txt`
- Result: all modules pass except `./cmd/store` (`./...` matches no packages)

## Runtime goroutines/heap baseline
- `/metrics` endpoint: not available yet (pre-W4)
- `/debug/pprof/*` endpoint: not available yet (pre-W4)
- Baseline capture status: unavailable at this checkpoint; will be captured after W4 endpoint wiring.
