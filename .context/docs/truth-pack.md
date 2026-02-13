# Truth Pack

Compact bridge from `.context/docs` to canonical `docs/**` and executable gates.

## Authoritative Sources

| Theme | Primary source(s) |
|---|---|
| Program baseline and status | [`docs/prd/PRD-0001-extreme-runtime.md`](../../docs/prd/PRD-0001-extreme-runtime.md), [`docs/rfcs/EXECUTION-SEQUENCE.md`](../../docs/rfcs/EXECUTION-SEQUENCE.md) |
| Single source map and drift control | [`docs/architecture/TRUTH-MAP.md`](../../docs/architecture/TRUTH-MAP.md), [`docs/audits/DRIFT-REPORT-W11.md`](../../docs/audits/DRIFT-REPORT-W11.md) |
| Event envelope, subjects, versioning | [`docs/contracts/event-bus.md`](../../docs/contracts/event-bus.md), [`docs/adrs/ADR-0002-event-envelope-and-versioning.md`](../../docs/adrs/ADR-0002-event-envelope-and-versioning.md), [`docs/adrs/ADR-0014-stream-partitioning-strategy.md`](../../docs/adrs/ADR-0014-stream-partitioning-strategy.md) |
| Delivery WS contract | [`docs/contracts/delivery-ws.md`](../../docs/contracts/delivery-ws.md), [`docs/adrs/ADR-0007-delivery-ws-sessions.md`](../../docs/adrs/ADR-0007-delivery-ws-sessions.md) |
| Storage planes and boundaries | [`docs/architecture/storage.md`](../../docs/architecture/storage.md), [`docs/adrs/ADR-0006-storage-hot-vs-cold.md`](../../docs/adrs/ADR-0006-storage-hot-vs-cold.md) |
| Orderbook processing | [`docs/architecture/orderbook.md`](../../docs/architecture/orderbook.md), [`docs/adrs/ADR-0005-sequencing-and-time-normalization.md`](../../docs/adrs/ADR-0005-sequencing-and-time-normalization.md) |
| Heatmap architecture | [`docs/architecture/heatmap.md`](../../docs/architecture/heatmap.md) |
| Volume profile architecture | [`docs/architecture/volume-profiles.md`](../../docs/architecture/volume-profiles.md), [`docs/adrs/ADR-0017-multi-exchange-normalization.md`](../../docs/adrs/ADR-0017-multi-exchange-normalization.md) |
| Liquidations + markprice | [`docs/architecture/liquidations-markprice.md`](../../docs/architecture/liquidations-markprice.md), [`docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md`](../../docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md) |
| Backpressure policies | [`docs/adrs/ADR-0013-backpressure-overload-policies.md`](../../docs/adrs/ADR-0013-backpressure-overload-policies.md) |
| Replay and deterministic time | [`docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`](../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md), [`docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md`](../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md) |
| Parity v1 rollout and gaps | [`docs/rfcs/RFC-0011-product-parity-marketmonkey.md`](../../docs/rfcs/RFC-0011-product-parity-marketmonkey.md) |

## Feature Packs Index

- Storage: [`feature-packs/storage.md`](./feature-packs/storage.md) -> [`docs/architecture/storage.md`](../../docs/architecture/storage.md)
- Orderbook: [`feature-packs/orderbook.md`](./feature-packs/orderbook.md) -> [`docs/architecture/orderbook.md`](../../docs/architecture/orderbook.md)
- Heatmap: [`feature-packs/heatmap.md`](./feature-packs/heatmap.md) -> [`docs/architecture/heatmap.md`](../../docs/architecture/heatmap.md)
- Volume Profiles: [`feature-packs/volume-profiles.md`](./feature-packs/volume-profiles.md) -> [`docs/architecture/volume-profiles.md`](../../docs/architecture/volume-profiles.md)
- Liquidations + MarkPrice: [`feature-packs/liquidations-markprice.md`](./feature-packs/liquidations-markprice.md) -> [`docs/architecture/liquidations-markprice.md`](../../docs/architecture/liquidations-markprice.md)
- WS Delivery: [`feature-packs/delivery-ws.md`](./feature-packs/delivery-ws.md) -> [`docs/contracts/delivery-ws.md`](../../docs/contracts/delivery-ws.md)

## Invariants Index

- Runtime invariant index: [`docs/architecture/system-invariants.md`](../../docs/architecture/system-invariants.md)
- Architecture source map: [`docs/architecture/TRUTH-MAP.md`](../../docs/architecture/TRUTH-MAP.md)
- Core ADR set for invariants:
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
- [`docs/adrs/ADR-0018-actor-topology-supervision-model.md`](../../docs/adrs/ADR-0018-actor-topology-supervision-model.md)

## Runbook Index

- QA docs in `.context`:
- [`qa/README.md`](./qa/README.md)
- [`qa/getting-started.md`](./qa/getting-started.md)
- [`qa/project-structure.md`](./qa/project-structure.md)
- [`qa/deployment.md`](./qa/deployment.md)
- [`qa/cli-commands.md`](./qa/cli-commands.md)
- [`qa/cli-arguments.md`](./qa/cli-arguments.md)

- Canonical run targets (source: [`Makefile`](../../Makefile)):
- `make docs-check`
- `make invariants-check`
- `make test-workspace`
- `make test-workspace-race`
- `make proto-lint`
- `make proto-breaking`
- `make soak-check`
