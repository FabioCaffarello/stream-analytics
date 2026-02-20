# Odin M1 Evidence — Slow-Client Threshold (2026-02-19)

## Implemented
- Session-level slow-client disconnect threshold for WS delivery.
- New config key: `delivery.slow_client_drop_threshold`.
- Runtime wiring from config to WS session actor.
- Metric increment on threshold breach: `ws_drops_total{reason="slow_client_disconnect"}`.

## Code Anchors
- `internal/actors/delivery/runtime/session.go`
- `internal/interfaces/ws/server.go`
- `cmd/server/bootstrap.go`
- `internal/shared/config/schema.go`
- `internal/shared/config/loader.go`

## Tests Added/Updated
- `internal/actors/delivery/runtime/session_backpressure_test.go:TestWSBackpressureSlowClientThresholdDisconnects`
- `internal/interfaces/ws/server_test.go` (threshold propagation assertion)
- `internal/shared/config/loader_test.go` (default + validation)
- `internal/actors/delivery/runtime/session_test.go` (atomic close flag for race-safe assertions)

## Validation Commands
- `go test ./internal/actors/delivery/runtime`
- `go test ./internal/interfaces/ws ./cmd/server`
- `go test ./internal/shared/config`
- `go test -race ./internal/actors/delivery/runtime`
- `make docs-check`
- `make invariants-check`
- `make test-short`
- `make lint`
- `make ci`

## Result
- All commands above passed.
