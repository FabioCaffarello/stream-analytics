# Storage Architecture (Hot/Cold per BC)

**Status:** Draft
**Owner:** Product Architect
**Last updated:** 2026-02-18
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0004-bus-nats-jetstream.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`, `docs/adrs/ADR-0018-actor-topology-supervision-model.md`

## Purpose

Define parity-v1 storage boundaries without introducing runtime features in this cycle:
- current hot path in memory (authoritative for realtime delivery, per ADR-0006 amendment);
- planned durable hot extension (Timescale);
- planned cold analytical extension (ClickHouse).

Execution note (2026-02-18):
- Timescale implementation is explicitly out of scope for the current codebase modernization cycle.
- Timescale items remain documented as planned, while delivery focuses on boundary hardening and domain closure without new Timescale adapters.

## Terminology (canonical)

- `instrument`: canonical key in envelope/domain (ADR-0011: `BTCUSDT`).
- `symbol`: WS-facing token in delivery subjects (for example `BTC-USDT`).
- `subject`: bus routing key `{event}.v{version}.{venue}.{instrument}`.
- `stream`: JetStream stream/filter using validated subject patterns.
- `envelope`: canonical event wrapper from ADR-0002.
- `payload`: versioned schema body decoded from `content_type`.

## Data Planes

### Plane A: Event Plane (current input authority)

Current registered marketdata events (`proto/registry.json`):
- `marketdata.trade.v1.{venue}.{instrument}`
- `marketdata.bookdelta.v1.{venue}.{instrument}`
- `marketdata.markprice.v1.{venue}.{instrument}`
- `marketdata.liquidation.v1.{venue}.{instrument}`

Current derived events in runtime:
- `insights.crossvenue.trade_snapshot.v1.global.{instrument}`
- `insights.crossvenue.spread_signal.v1.global.{instrument}`

Planned derived events for storage fanout (not implemented in this cycle):
- `aggregation.snapshot.v1.{venue}.{instrument}` (subject root alignment tracked in `docs/rfcs/archive/ADR-REVISIONS-patch-plan.md`, NOTE-001)
- `insights.<heatmap_event>.v1.{venue}.{instrument}` (TBD registry key under existing taxonomy)
- `insights.<volume_profile_event>.v1.{venue}.{instrument}` (TBD registry key under existing taxonomy)

### Plane B: L0 Hot Read Model (existing)

Current storage write-port in aggregation:
- `internal/core/aggregation/ports/ports.go` (`HotReadModelStore.Save`)

Current behavior:
- in-memory latest snapshot model for low-latency delivery;
- bounded state via `BoundedMap` in runtime/app layers;
- no durable storage adapter in repository yet.

### Plane C: L1 Durable Hot (planned)

Planned Timescale layer for short/medium query windows:
- `timescale.marketdata_ticks_hot`
- `timescale.aggregation_orderbook_snapshot_hot`
- `timescale.insights_heatmap_bucket_hot`
- `timescale.insights_volume_profile_hot`
- `timescale.marketdata_markprice_hot`
- `timescale.marketdata_liquidation_hot`

Current cycle policy:
- no new Timescale implementation work;
- existing in-repo placeholders/stubs are treated as non-production adapters.

### Plane D: L2 Cold Analytics (planned)

Planned ClickHouse layer for history/backfill/rebuild:
- `clickhouse.marketdata_ticks_cold`
- `clickhouse.aggregation_orderbook_snapshot_cold`
- `clickhouse.insights_heatmap_bucket_cold`
- `clickhouse.insights_volume_profile_cold`
- `clickhouse.marketdata_markprice_cold`
- `clickhouse.marketdata_liquidation_cold`

Recommended partitioning:
- `toDate(ts_ingest)` + bucket by `(venue, instrument)`.

## Contracts

Envelope fields for persistence semantics (ADR-0002):
- mandatory: `type`, `version`, `venue`, `instrument`, `ts_ingest`, `seq`, `idempotency_key`, `payload`
- optional/advisory: `ts_exchange`, `meta`
- codec discriminator: `content_type` (`application/json` current default, protobuf opt-in)

Subject examples (existing taxonomy):
- `marketdata.trade.v1.binance.BTCUSDT`
- `marketdata.bookdelta.v1.binance.BTCUSDT`
- `insights.crossvenue.trade_snapshot.v1.global.BTCUSDT`
- `quarantine.v1.binance.BTCUSDT`

Ack semantics for persistence boundaries:
1. `ACK` only after required durable commit boundary.
2. `NAK` for transient failure with bounded retry/jitter.
3. `TERM` for poison/invalid contract; route to `quarantine.v1.*`.

## Invariants

- `STO-1`: single writer per partition key (`venue`, `instrument`, and `market_type` when present).
- `STO-2`: bounded queues/mailboxes in all writer stages.
- `STO-3`: idempotency by `idempotency_key` and monotonic `seq`.
- `STO-4`: deterministic ordering by `(ts_ingest, seq)` inside each partition.
- `STO-5`: replay over equivalent input must produce identical artifacts/checksums.
- `STO-6`: no `ack-on-enqueue`; only `ack-on-commit`.

### W2 Cold-Path Correctness Contract (ClickHouse)

- Success semantics: no silent success. Every storage commit outcome must be surfaced as `*problem.Problem` on failure.
- Durable boundary: `ACK` happens only after the required durable commit boundary succeeds (`ack-on-commit`).
- Canonical idempotency key for cold upsert/dedup: deterministic key from canonical subject + `(venue,instrument,seq[,source_idempotency_key])`.
- Forbidden key source: do not reuse publish dedup-only markers such as heatmap `seqMax` as the canonical snapshot upsert key.
- Replay safety: reprocessing the same input window/events must converge to identical final state with zero duplicate commits.
- Subject safety: cold-path consumers/writers must stay aligned with registry taxonomy `{event}.v{version}.{venue}.{instrument}`.
- Port contract: storage ports and adapters return `*problem.Problem` (never `error`) at domain boundaries.
- Minimum observability proof under load: lag, insert errors, retries, duplicates detected, and queue depth.

## Implementation Matrix

| Feature | Status | Evidence | Tests |
|---|---|---|---|
| L0 in-memory hot read model | Existing | `internal/core/aggregation/ports/ports.go`, `internal/core/aggregation/app/update_orderbook.go` | `internal/core/aggregation/app/update_orderbook_test.go` |
| ACK/NAK/TERM boundary in ingest | Existing | `internal/adapters/jetstream/consumer.go` | `internal/adapters/jetstream/ingest_conformance_test.go:TestIngestConformance_AckNakTermGoldenTable` |
| Replay determinism foundation | Existing | `internal/shared/replay/player.go`, `internal/shared/replay/sequencer.go` | `internal/shared/replay/golden_test.go:TestGoldenReplay`, `cmd/consumer/replay_test.go:TestReplayIngestGolden1000` |
| L1 Timescale writers | TODO | `internal/adapters/storage/timescale/` (TODO) | `internal/adapters/storage/timescale/writer_test.go` (TODO) |
| L2 ClickHouse writers | TODO | `internal/adapters/storage/clickhouse/` (TODO) | `internal/adapters/storage/clickhouse/writer_test.go` (TODO) |
| Storage reconciliation/rebuilder | TODO | `internal/adapters/storage/replay/rebuilder.go` (TODO) | `internal/adapters/storage/replay/rebuilder_test.go` (TODO) |

## Backpressure

Storage-side policy target (ADR-0013 aligned):
1. bounded queues per partition;
2. throttle when queue depth exceeds threshold;
3. `NAK` with bounded retry budget on transient failures;
4. `TERM` + quarantine on poison payload.

Current observable coverage:
- ingest disposition counters and lag are available in JetStream adapter;
- storage-specific queue metrics remain TODO until storage adapters exist.

## Replay Strategy

- primary source: fixture/JetStream replay stack in `internal/shared/replay` and `internal/adapters/jetstream/replay_source.go`;
- deterministic ordering: `(ts_ingest, seq)` per partition;
- parity rule: repeated replay over same fixture must be byte-stable in golden outputs.

## Observability

Minimum required for parity closure:
- lag (`bus_consumer_lag` existing on bus path)
- drop/disposition reason (`*_drop_total` / ack disposition counters; storage-specific TODO)
- queue depth (storage writer queue depth TODO)
- poison/quarantine counters (existing ingest path, storage path TODO)

Planned storage metrics namespace (TODO):
- `storage_writer_queue_depth{bc,plane}`
- `storage_write_latency_ms{bc,plane}`
- `storage_commit_total{bc,plane,status}`
- `storage_drop_total{bc,plane,reason}`
- `storage_replay_lag_ms{bc}`

## Acceptance Tests

Existing tests (current evidence):
- `internal/adapters/jetstream/ingest_conformance_test.go:TestIngestConformance_AckNakTermGoldenTable`
- `internal/shared/replay/golden_test.go:TestGoldenReplay`
- `internal/shared/replay/golden_test.go:TestGoldenReplayByteStable50Runs`
- `cmd/consumer/replay_test.go:TestReplayIngestGolden1000`

Tests to create for storage adapters (no implementation in this cycle):
- `internal/adapters/storage/storage_integration_test.go:TestStorageHotIdempotencyByIdempotencyKey` (TODO)
- `internal/adapters/storage/storage_integration_test.go:TestStorageColdUpsertDeterministicReplay` (TODO)
- `internal/adapters/storage/storage_integration_test.go:TestStorageAckOnCommit_NotOnEnqueue` (TODO)
- `internal/adapters/storage/storage_integration_test.go:TestStorageBackpressureNakAndJitterPolicy` (TODO)
- `internal/adapters/storage/storage_integration_test.go:TestStoragePoisonRoutesToQuarantine` (TODO)
- `internal/adapters/storage/storage_integration_test.go:TestStorageSingleWriterPerPartition` (TODO)

## Evidence Hooks

Current evidence:
- `internal/core/aggregation/ports/ports.go`
- `internal/core/aggregation/app/update_orderbook.go`
- `internal/adapters/jetstream/consumer.go`
- `internal/adapters/jetstream/ingest_conformance_test.go`
- `internal/shared/replay/player.go`
- `internal/shared/replay/golden_test.go`

TODO hooks (skeleton only):
- `internal/core/storage/ports/ports.go` (TODO)
- `internal/core/storage/app/persist_hot_path.go` (TODO)
- `internal/core/storage/app/persist_cold_path.go` (TODO)
- `internal/adapters/storage/timescale/writer.go` (TODO)
- `internal/adapters/storage/clickhouse/writer.go` (TODO)
- `internal/adapters/storage/replay/rebuilder.go` (TODO)
- `internal/adapters/storage/storage_integration_test.go` (TODO)

## Failure Modes

- Premature ack before durable boundary:
  - Mitigation: enforce `ack-on-commit` only.
- Partition skew (`venue,instrument,market_type`) saturation:
  - Mitigation: bounded queues + throttling + partition-level alerts.
- Durable sink unavailable (Timescale/ClickHouse):
  - Mitigation: bounded retries, explicit `NAK`, no silent drop.
- Poison payload loop:
  - Mitigation: `TERM` + `quarantine.v1.*` + DLQ telemetry.
- Replay divergence between runs:
  - Mitigation: mandatory golden replay checksums before rollout.
