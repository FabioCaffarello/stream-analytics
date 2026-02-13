# Checkpoint: M3-bootstrap

Date: 2026-02-13
Mode: Commit-Driven Development (no feature implementation in this cycle)

## Snapshot Delta (post-M2)

- Head log includes `a12b8b1 feat(delivery): add ws snapshot stream with backpressure`.
- Gates executed and passing:
  - `make docs-check-full`
  - `make invariants-check`
  - `make test-workspace`
  - `make test-replay-golden`
- Runtime allowed roots confirmed in `internal/adapters/jetstream/subject_validation.go`: `aggregation`, `insights`, `marketdata`, `quarantine`.
- Registry governance enforcement confirmed in `scripts/check-registry.sh`.
- Delivery instrument<->symbol normalization path confirmed in:
  - `internal/core/delivery/domain/subject.go`
  - `internal/shared/envelope/subject.go`
  - `docs/contracts/delivery-ws.md`

## Hard Decisions

1. D1: Keep `aggregation.*` enabled in runtime allowed roots.
2. D2: When `producer_bc != owner_bc`, `schema_authority_bc` is required and must be owner or producer.
3. D3: Canonical normalization authority is `BTCUSDT` for bus/internal subjects; `BTC-USDT` is accepted input and canonicalized.

## STOP Condition (Review)

- No STOP raised.
- `docs/contracts/event-bus.md`, `internal/adapters/jetstream/subject_validation.go`, and `scripts/check-registry.sh` are consistent for roots and governance rules.

## Authority Set (locked)

- `docs/rfcs/EXECUTION-SEQUENCE.md`
- `docs/rfcs/RFC-0011-product-parity-marketmonkey.md`
- `docs/architecture/heatmap.md`
- `docs/architecture/volume-profiles.md`
- `docs/contracts/event-bus.md`
- `.context/docs/feature-packs/storage.md`
- `.context/docs/feature-packs/orderbook.md`
- `docs/architecture/TRUTH-MAP.md`
- `docs/contracts/delivery-ws.md`
