# Runtime Sequencing Model

**Status:** Active
**Date:** 2026-03-05
**Owner:** Governance Doc-First Maintainer
**Relates to:** `docs/adrs/ADR-0005-sequencing-and-time-normalization.md`,
  `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`,
  `docs/adrs/ADR-0014-stream-partitioning-strategy.md`,
  `docs/adrs/ADR-0018-actor-topology-supervision-model.md`,
  `docs/analysis/s17-p2-policy-map.md`,
  `docs/analysis/ARCHITECTURE-DOSSIER-S16-S17.md`,
  `docs/architecture/system-invariants.md`

---

## Purpose

Document how the system establishes ordering guarantees from raw WebSocket messages
through the bus, aggregation, delivery, and client — including the policies that govern
deduplication, monotonic sequencing, ownership assignment, and replay determinism.

---

## 1. Time Primitives

| Field | Source | Semantics |
|---|---|---|
| `ts_exchange` | Exchange-provided | Native exchange timestamp; may be unreliable/skewed. |
| `ts_ingest` | MarketData subsystem | Wall-clock at canonicalization; monotonic within a consumer shard. |
| `ts_server` | Delivery / Frame builder | Server-side timestamp attached to every emitted WS frame. **Mandatory.** Missing `ts_server` is a P0 invariant violation. |
| `seq` | Per-stream sequence counter | Strictly monotonic integer assigned per stream key. |
| `prev_seq` | Delivery frame | Previous sequence in the delivery chain; enables client-side gap detection. |
| `watermark` | Aggregation watermark | Window event-time boundary used by `WatermarkWindowManager`. |

**Invariant INV-DET-01:** `internal/core/*` must never call `time.Now()` directly. Time injection must
flow from outside the domain layer. Authority: `ADR-0015`; gate: `make invariants-check`.

---

## 2. Stream Identity and Partitioning

Every event stream is uniquely identified by the tuple:

```
venue | instrument | market_type
```

Examples: `BINANCE|BTC-USDT:PERPETUAL`, `BYBIT|ETH-USDT:SPOT`.

This identity is:
- Enforced at ingest parse time by the MarketData subsystem (CMM normalization, ADR-0011).
- Propagated as the JetStream subject suffix `{venue_lower}.{instrument_alnum_upper}`.
- Used as the shard key for ownership hashing across multiple replicas.

**Invariant INV-MEX-01:** Stream identity must include `venue + instrument + market_type`.
Authority: `docs/adrs/ADR-0017-multi-exchange-normalization.md`; validated by
`internal/core/marketdata/domain/instrument_stream.go:30`.

---

## 3. Subject Taxonomy (INV-BUS-01)

Subject format:
```
{event_family}.{event_type}.v{version}.{venue_lower}.{instrument_alnum_upper}
```

Examples:
- `marketdata.trade.v1.binance.BTCUSDT`
- `aggregation.candle.v1.bybit.ETHUSDT`
- `insights.heatmap_snapshot.v1.binance.BTCUSDT`
- `signal.event.v1.binance.BTCUSDT`

Wildcard patterns used for consumer subscriptions:
- `marketdata.>` — all marketdata events
- `aggregation.>` — all aggregated artifacts
- `insights.>` — all insight artifacts

**Invariant INV-BUS-01:** Subject taxonomy must maintain valid family/versioning.
Authority: `docs/adrs/ADR-0014-stream-partitioning-strategy.md`;
gate: `make test-workspace` via `internal/adapters/jetstream/subject_validation.go`.

---

## 4. Ownership Contract (Cross-Subsystem)

When `PROCESSOR_REPLICAS > 1`, each stream is owned by exactly one replica per subsystem.
Ownership is computed by a deterministic hash:

```
ownerIndex = hash(salt(subsystem) | venue | instrument | channel | timeframe) % replicaCount
```

**Subsystem salts are independent.** Delivery, Signal, and Strategist use different salts so
each subsystem independently computes its owner without coordination.

| Subsystem | Salt / Key | Policy file |
|---|---|---|
| Delivery | `SubsystemDelivery` | `internal/actors/delivery/runtime/router.go:718-730` |
| Signal | `SubsystemSignals` | `internal/actors/signal/runtime/subsystem_owner_policy.go:71-77` |
| Strategist | `SubsystemStrategist` | `internal/actors/signals/runtime/subsystem.go:369-376` |

**Key invariant:** Non-owner replicas reject events with reason `owner_reject` and must not emit
or mutate stream state. Double-emit is prevented by the combination of owner gating and monotonic
dedup.

Authority: `docs/analysis/s17-p2-policy-map.md`, `docs/adrs/ADR-0018-actor-topology-supervision-model.md`.

---

## 5. Monotonic Sequencing (`DecideMonotonic`)

All sequencing decisions flow through `ownership.DecideMonotonic`
(`internal/shared/ownership/monotonic.go`). The function maps a candidate event against the
last accepted state for a stream:

| Input condition | Decision | Reason |
|---|---|---|
| `candidateSeq <= 0` or empty stream key | `drop` | `seq_invalid` |
| `candidateSeq == lastSeq` | `drop` | `replay_duplicate` |
| `candidateSeq > lastSeq` (advancing) | `accept` | — (watermark regression tolerated) |
| `candidateSeq < lastSeq`, within stale gap window | `drop` | `stale_event` |
| `candidateSeq < lastSeq`, severe non-monotonic | `drop` | `seq_non_monotonic` |
| Owner change, lower/equal seq at handoff boundary | `convert_to_resync` | `owner_change` |

**Thin adapters** apply the same logic per subsystem:
- **Delivery** — `defaultSeqPolicy.Decide` (`internal/actors/delivery/runtime/seq_policy.go`): detects
  snapshots by `eventType` containing `"snapshot"`. Preserves reject/coherence reasons.
- **Signal** — `acceptMonotonicProgress` (`subsystem_owner_policy.go:150-185`): replay classified as
  `duplicate`; stale/OOO classified with ownership reason.
- **Strategist** — `acceptMonotonic` (`subsystem.go:440-462`): advancing seq accepted even if watermark regresses.

**IQ baseline validation:** `seq monotonic` PASS, `router coherence violations_total=0`
(report.md:56,146-151,213-217).

---

## 6. Delivery Sequence Chain (`prev_seq`)

Every WS frame delivered to the client carries:
- `seq` — monotonically increasing delivery sequence for the stream.
- `prev_seq` — the `seq` of the last frame sent for the same stream.

The client validates the chain:
```
frame.prev_seq == last_delivered_seq   →  OK
frame.prev_seq != last_delivered_seq   →  gap detected → client_prev_seq_violations++
```

**IQ baseline:** `prev_seq chaining` PASS (report.md:61).

---

## 7. Deduplication Windows

| Layer | Mechanism | Window / Cap |
|---|---|---|
| **JetStream (bus)** | `Nats-Msg-Id` header with `idempotency_key` | Configurable per-stream dedup window |
| **Signal engine** | `seq <= LastSeq` drop; dedup window store | Per-stream, per-tenant rate limit |
| **Strategist** | `DecideMonotonic` + dedup window | Per-stream key |
| **Delivery router** | `acceptStreamSeq` | Per-stream, per-session coherence |
| **Client** | `prev_seq` chain; `snapshot_seq` gate | Frame-level; reset on new snapshot |

---

## 8. Replay and Backfill Determinism

The replay subsystem (`internal/shared/replay/`) is deliberately kept **offline** (no NATS dependency):

**Invariant INV-REP-01.** Authority: `ADR-0015`; gate: `make invariants-check`.

Replay guarantees:
- `player.go` validates strictly monotonic `seq` per stream; aborts on first violation.
- `sequencer.go` uses canonical key `venue|instrument:market_type` and a monotonic queue.
- `jetstream_reader.go` produces `ReplaySummary.InputSHA` for canonical integrity.
- Only `download | gaps` backfill modes are permitted; exchange allow-list enforced.

Golden-replay tests: `internal/shared/replay/golden_test.go:TestGoldenReplay`,
`cmd/consumer/replay_test.go:TestReplayIngestGolden1000`.

---

## 9. Startup Sequencing

The Guardian starts subsystems in the canonical `orderedSubsystems` order
(`internal/actors/runtime/protocol.go:22-30`). A subsystem is considered "ready" after its
first successful spawn (optimistic v1 model).

**`ReadyQuery` flow:**
```
HTTP /readyz  →  Guardian ReadyQuery  →  ReadyResponse{Ready, Pending}
```
`Ready=true` only when all `ExpectedSubsystems` have been spawned at least once.

Config loading is validated at startup per `ADR-0010`: strict validation, no silent defaults for
required fields.

---

## 10. Runtime Invariants Sequencing Summary

| Invariant | Rule | Gate |
|---|---|---|
| INV-DET-01 | `core/*` never calls `time.Now()` | `make invariants-check` |
| INV-MEX-01 | Stream identity = `venue+instrument+market_type` | `make test-workspace-race` |
| INV-BUS-01 | Subject taxonomy family/version valid | `make test-workspace` |
| INV-ACK-01 | JetStream ingest maintains ACK/NAK/TERM | `make test-workspace` |
| INV-TOPO-01 | Guardian enforces readiness per expected subsystems + restart budget | `make test-workspace-race` |
| INV-REP-01 | `internal/shared/replay` stays offline (no NATS) | `make invariants-check` |

For the full invariant index see `docs/architecture/system-invariants.md`.

---

## Changelog

- 2026-03-05: Initial creation. Sources: `docs/analysis/s17-p2-policy-map.md`,
  `docs/analysis/ARCHITECTURE-DOSSIER-S16-S17.md`, `docs/adrs/ADR-0005`, `ADR-0015`,
  `ADR-0014`, `ADR-0018`, runtime code anchors, IQ baseline `artifacts/20260305T160115Z`.
