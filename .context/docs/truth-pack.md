# Truth Pack

Context interface for `.context/docs`: pointers, invariants, acceptance gates.

## Authoritative Sources

| Theme | Source of truth |
|---|---|
| Program baseline | [`docs/prd/PRD-0001-extreme-runtime.md`](../../docs/prd/PRD-0001-extreme-runtime.md), [`docs/rfcs/EXECUTION-SEQUENCE.md`](../../docs/rfcs/EXECUTION-SEQUENCE.md) |
| Architecture map | [`docs/architecture/TRUTH-MAP.md`](../../docs/architecture/TRUTH-MAP.md), [`docs/architecture/README.md`](../../docs/architecture/README.md) |
| Envelope and subjects | [`docs/contracts/event-bus.md`](../../docs/contracts/event-bus.md), [`docs/adrs/ADR-0002-event-envelope-and-versioning.md`](../../docs/adrs/ADR-0002-event-envelope-and-versioning.md), [`docs/adrs/ADR-0014-stream-partitioning-strategy.md`](../../docs/adrs/ADR-0014-stream-partitioning-strategy.md) |
| Storage boundaries | [`docs/architecture/storage.md`](../../docs/architecture/storage.md), [`docs/adrs/ADR-0006-storage-hot-vs-cold.md`](../../docs/adrs/ADR-0006-storage-hot-vs-cold.md) |
| Orderbook domain | [`docs/architecture/orderbook.md`](../../docs/architecture/orderbook.md), [`docs/adrs/ADR-0005-sequencing-and-time-normalization.md`](../../docs/adrs/ADR-0005-sequencing-and-time-normalization.md) |
| Heatmap domain | [`docs/architecture/heatmap.md`](../../docs/architecture/heatmap.md), [`docs/adrs/ADR-0013-backpressure-overload-policies.md`](../../docs/adrs/ADR-0013-backpressure-overload-policies.md) |
| Volume profiles domain | [`docs/architecture/volume-profiles.md`](../../docs/architecture/volume-profiles.md), [`docs/adrs/ADR-0017-multi-exchange-normalization.md`](../../docs/adrs/ADR-0017-multi-exchange-normalization.md) |
| Liquidations + markprice | [`docs/architecture/liquidations-markprice.md`](../../docs/architecture/liquidations-markprice.md), [`docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md`](../../docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md) |
| Delivery WS contract | [`docs/contracts/delivery-ws.md`](../../docs/contracts/delivery-ws.md), [`docs/adrs/ADR-0007-delivery-ws-sessions.md`](../../docs/adrs/ADR-0007-delivery-ws-sessions.md) |
| Replay determinism | [`docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`](../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md), [`docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md`](../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md) |

## Feature Packs Index

- Storage: [`feature-packs/storage.md`](./feature-packs/storage.md)
- Orderbook: [`feature-packs/orderbook.md`](./feature-packs/orderbook.md)
- Heatmap: [`feature-packs/heatmap.md`](./feature-packs/heatmap.md)
- Volume Profiles: [`feature-packs/volume-profiles.md`](./feature-packs/volume-profiles.md)
- Liquidations + MarkPrice: [`feature-packs/liquidations-markprice.md`](./feature-packs/liquidations-markprice.md)
- Delivery WS: [`feature-packs/delivery-ws.md`](./feature-packs/delivery-ws.md)

## Invariants Index

- Global invariants: [`docs/architecture/system-invariants.md`](../../docs/architecture/system-invariants.md)
- ADR invariants set:
- [`docs/adrs/ADR-0002-event-envelope-and-versioning.md`](../../docs/adrs/ADR-0002-event-envelope-and-versioning.md)
- [`docs/adrs/ADR-0004-bus-nats-jetstream.md`](../../docs/adrs/ADR-0004-bus-nats-jetstream.md)
- [`docs/adrs/ADR-0005-sequencing-and-time-normalization.md`](../../docs/adrs/ADR-0005-sequencing-and-time-normalization.md)
- [`docs/adrs/ADR-0006-storage-hot-vs-cold.md`](../../docs/adrs/ADR-0006-storage-hot-vs-cold.md)
- [`docs/adrs/ADR-0007-delivery-ws-sessions.md`](../../docs/adrs/ADR-0007-delivery-ws-sessions.md)
- [`docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md`](../../docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md)
- [`docs/adrs/ADR-0013-backpressure-overload-policies.md`](../../docs/adrs/ADR-0013-backpressure-overload-policies.md)
- [`docs/adrs/ADR-0014-stream-partitioning-strategy.md`](../../docs/adrs/ADR-0014-stream-partitioning-strategy.md)
- [`docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`](../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)
- [`docs/adrs/ADR-0017-multi-exchange-normalization.md`](../../docs/adrs/ADR-0017-multi-exchange-normalization.md)

## Operational Runbooks

- Observability runbooks: [`docs/observability/runbooks/`](../../docs/observability/runbooks/) — ingest, guardian, websocket, vpvr-overload, bus, consumer-stall
- SLO definitions: [`docs/observability/slo.md`](../../docs/observability/slo.md)
- Operations: [`docs/operations/`](../../docs/operations/) — degradation, local-dev, sharding, cold-path-runbook

## Validation Gates

- Gate targets in [`Makefile`](../../Makefile): `docs-check`, `invariants-check`, `test-workspace`, `test-workspace-race`, `proto-lint`, `proto-breaking`, `soak-check`
- Docs gate scripts: [`scripts/check-doc-headers.sh`](../../scripts/check-doc-headers.sh), [`scripts/check-doc-links.sh`](../../scripts/check-doc-links.sh), [`scripts/check-truth-map.sh`](../../scripts/check-truth-map.sh), [`scripts/check-feature-pack-links.sh`](../../scripts/check-feature-pack-links.sh)
