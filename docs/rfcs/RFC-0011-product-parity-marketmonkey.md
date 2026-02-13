# RFC-0011 - Product Parity v1 (MarketMonkey-Inspired)

**Status:** Draft
**Owner:** Product Architect
**Last updated:** 2026-02-13
**Date:** 2026-02-13
**Author:** Product Architect
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0004-bus-nats-jetstream.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`, `docs/adrs/ADR-0018-actor-topology-supervision-model.md`, `docs/contracts/event-bus.md`

## Objetivo

Consolidate, in doc-first mode, parity v1 planning for MarketMonkey-inspired core features:
- Storage hot/cold per BC
- Orderbook snapshots + delivery contract
- Heatmap derivation + persistence
- Volume profile (VPVR) + range aggregations
- Liquidations/MarkPrice end-to-end

## Escopo

- Define contracts, invariants, data planes, storage/replay/observability.
- Provide executable roadmap in P0/P1/P2 with testable criteria.
- Register implementation gaps explicitly (TODO skeleton paths).

## Nao-Escopo

- No runtime/adapter feature implementation in this cycle.
- No new protobuf contract without `proto/registry.json` + Buf gates.
- No change to regulatory risk posture (insights remain non-execution).

## Design

### Feature Set v1 (required order)

1. Storage cold-path (ClickHouse) + hot-path (Timescale) per BC
2. Orderbook snapshots + delivery contract
3. Heatmap derivation + persistence
4. Volume profile (VPVR) + range aggregations
5. Liquidations/MarkPrice end-to-end

### Inputs/Outputs per Feature

| Feature | Input events (`marketdata.*`) | Derived events (`aggregation.*` / `insights.*` / `delivery.*`) | Storage (hot/cold) | Keys / idempotency |
|---|---|---|---|---|
| Storage hot/cold | `marketdata.trade`, `marketdata.bookdelta`, `marketdata.markprice`, `marketdata.liquidation` | existing: `insights.crossvenue.trade_snapshot`, `insights.crossvenue.spread_signal`; planned: `aggregation.snapshot`, `insights.heatmap.bucket`, `insights.volume_profile.snapshot` | `timescale.*_hot` / `clickhouse.*_cold` per BC | `(type,version,venue,instrument,seq,idempotency_key)` |
| Orderbook | `marketdata.bookdelta` (+ optional `marketdata.trade`) | planned: `aggregation.snapshot`, `aggregation.orderbook.inconsistent`; WS delivery via `aggregation.snapshot/...` | `timescale.aggregation_orderbook_snapshot_hot` / `clickhouse.aggregation_orderbook_snapshot_cold` | `(venue,instrument,seq,snapshot_version)` + `idempotency_key` |
| Heatmap | `marketdata.bookdelta`, `marketdata.trade` | planned: `insights.heatmap.bucket`; WS delivery `insights.heatmap/...` | `timescale.insights_heatmap_bucket_hot` / `clickhouse.insights_heatmap_bucket_cold` | `(venue,instrument,timeframe,price_bucket,window_start_ts,seq_max)` |
| Volume Profiles | `marketdata.trade` (+ optional `marketdata.bookdelta`) | planned: `insights.volume_profile.snapshot`, `insights.volume_profile.delta`; WS delivery `insights.volume_profile/...` | `timescale.insights_volume_profile_hot` / `clickhouse.insights_volume_profile_cold` | `(venue,instrument,timeframe,price_bucket,window_start_ts)` |
| Liquidations/MarkPrice | `marketdata.markprice`, `marketdata.liquidation` | planned: `insights.liquidation.markprice_snapshot`; WS delivery `marketdata.markprice/...` and `marketdata.liquidation/...` | `timescale.marketdata_markprice_hot`, `timescale.marketdata_liquidation_hot` / `clickhouse.marketdata_markprice_cold`, `clickhouse.marketdata_liquidation_cold` | strong field-hash + `seq` |

### Critical Invariants

- `PAR-1`: single writer per `(venue,instrument,market_type)`.
- `PAR-2`: bounded queues/mailboxes/writers (no unbounded data path).
- `PAR-3`: explicit ack semantics (`ack-on-commit`, never `ack-on-enqueue`).
- `PAR-4`: end-to-end replay determinism (same input -> same output).
- `PAR-5`: contract versioning by envelope + append-only registry.
- `PAR-6`: subsystem supervision with failure isolation.

### ADR Alignment Notes

- Subject taxonomy remains `{event}.v{version}.{venue}.{instrument}`.
- Registry authority remains `proto/registry.json` + `internal/shared/contracts/*`.
- New planned types require registry update + Buf checks before runtime rollout.

## Rollout

### P0 (Contract and Storage Foundation)

Scope:
- `docs/architecture/storage.md`
- `docs/contracts/delivery-ws.md`
- taxonomy/registry alignment for planned types

Testable acceptance criteria:
- `make invariants-check` passes
- `make test-workspace` passes
- replay golden plan defined per artifact
- minimum observability matrix filled (lag/drop/queue depth)

### P1 (Orderbook + Heatmap)

Scope:
- `docs/architecture/orderbook.md`
- `docs/architecture/heatmap.md`

Testable acceptance criteria:
- replay golden spec by window defined
- boundedness/backpressure tests defined
- slow-consumer/drop policy defined in delivery contract
- race test plan defined for session/router/orderbook

### P2 (Volume Profiles + Liquidations/MarkPrice)

Scope:
- `docs/architecture/volume-profiles.md`
- `docs/architecture/liquidations-markprice.md`

Testable acceptance criteria:
- strong dedup keys documented and testable
- cardinality risk and mitigation explicitly bounded
- soak/race plan defined
- schema/buf compatibility criteria defined for new contracts

## Implementation Roadmap (P0/P1/P2)

| Priority | Deliverables | Required gates | Done when |
|---|---|---|---|
| P0 | Storage + Delivery contracts + consolidated RFC | `make invariants-check`, `make test-workspace` | docs approved, invariants closed, truth-map updated |
| P1 | Orderbook + Heatmap docs + acceptance specs | `make invariants-check`, `make test-workspace`, `make test-workspace-race` | replay/backpressure/observability criteria closed |
| P2 | VPVR + Liquidations/MarkPrice docs + rollout readiness | `make invariants-check`, `make test-workspace`, `make test-workspace-race`, `make soak-check` | dedup/cardinality/soak criteria closed |

## Test Plan

Mandatory cross-cut validation:
```bash
make invariants-check
make test-workspace
```

Additional risk validation:
```bash
make test-workspace-race
make soak-check
```

Schema compatibility (when new contracts are materialized):
```bash
make proto-lint
make proto-breaking
```

Golden replay targets (when features are implemented):
- `TestOrderbookReplayGoldenDeterministic`
- `TestHeatmapReplayGoldenMatrixHash`
- `TestVPVRReplayGoldenWindow`
- `TestMarkPriceLiquidationReplayGolden`

## Acceptance

- Feature set v1 documented in required order.
- Inputs/outputs/storage/keys declared per feature.
- Critical invariants declared and aligned to ADRs.
- P0/P1/P2 roadmap with testable criteria defined.
- Implementation gaps explicitly marked as TODO paths (no code implementation in this cycle).

## Evidence Hooks

Target docs:
- `docs/architecture/storage.md`
- `docs/architecture/orderbook.md`
- `docs/architecture/heatmap.md`
- `docs/architecture/volume-profiles.md`
- `docs/architecture/liquidations-markprice.md`
- `docs/contracts/delivery-ws.md`

Current code/test references:
- `internal/core/aggregation/app/update_orderbook.go`
- `internal/core/aggregation/app/golden_replay_test.go`
- `internal/core/insights/app/join_crossvenue_trades.go`
- `internal/actors/delivery/runtime/session.go`
- `internal/adapters/jetstream/subject_validation.go`
- `internal/shared/contracts/authority_manifest.go`

## Failure Modes

- New contract without registry/schema gate:
  - Mitigation: block rollout until `proto-lint/proto-breaking` pass.
- Drift between docs and runtime:
  - Mitigation: update `TRUTH-MAP` and add `ADR-REVISION NOTE` when needed.
- Delivery/storage saturation:
  - Mitigation: explicit bounded queue policy, per-reason drops, alerts.
- Non-deterministic replay:
  - Mitigation: feature-level golden tests with stable checksum.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| taxonomy expansion without subject validator alignment | High | ADR revision note + subject validation gate |
| high cardinality in heatmap/VPVR | High | hard caps + coarsening + payload budgets |
| ack before durable commit | High | global ack-on-commit rule |
| hot/cold divergence | Medium | periodic reconcile by checksum |

## Changelog

- 2026-02-13:
- RFC created to consolidate parity v1 in doc-first mode with roadmap P0/P1/P2.
- No feature implementation in this cycle.
