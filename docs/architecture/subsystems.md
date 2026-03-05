# Subsystem Responsibilities

**Status:** Active
**Date:** 2026-03-05
**Owner:** Governance Doc-First Maintainer
**Relates to:** `docs/architecture/system-invariants.md`, `docs/architecture/TRUTH-MAP.md`,
  `docs/adrs/ADR-0003-actor-runtime.md`, `docs/adrs/ADR-0018-actor-topology-supervision-model.md`,
  `docs/analysis/ARCHITECTURE-DOSSIER-S16-S17.md`

---

## Purpose

Define the authoritative boundary, responsibility, inputs, outputs, and runtime properties for each
subsystem managed by the Guardian. Every row anchors to at least one code file and one IQ evidence
record validated by the baseline IQ Loop run (`artifacts/20260305T160115Z`).

---

## Subsystem Registry

The Guardian (`internal/actors/runtime/guardian.go`) manages the following subsystems in the canonical
order defined by `orderedSubsystems` (`internal/actors/runtime/protocol.go:22-30`):

| # | Subsystem constant | Actor package |
|---|---|---|
| 1 | `SubsystemMarketData` | `internal/actors/marketdata/runtime` |
| 2 | `SubsystemAggregation` | `internal/actors/aggregation/runtime` |
| 3 | `SubsystemDelivery` | `internal/actors/delivery/runtime` |
| 4 | `SubsystemInsights` | `internal/actors/insights/runtime` |
| 5 | `SubsystemEvidence` | `internal/actors/evidence/runtime` |
| 6 | `SubsystemSignals` | `internal/actors/signals/runtime` |
| 7 | `SubsystemStorage` | `internal/actors/storage/runtime` |

Dynamic exchange-level market-data subsystems use the key `marketdata:{exchange}` and bypass the static
`SubsystemMarketData` slot when present (`guardian.go:603-616`).

---

## Subsystem Responsibility Table

### 1 — Consumer / MarketData

| Field | Value |
|---|---|
| **Responsibility** | Consume WebSocket streams per exchange, canonicalize payloads into the Canonical Market Model (CMM), apply backpressure, and publish versioned envelopes to NATS JetStream. |
| **Inputs** | Raw WebSocket messages via `ws.Manager` / `Consumer`. |
| **Outputs** | Canonical envelopes: `marketdata.trade.v1`, `marketdata.bookdelta.v1`, `marketdata.markprice.v1`, `marketdata.liquidation.v1`, etc. |
| **Boundedness** | Queue bounded + `canonicalState` cap: `MaxEntries=20_000`, `TTL=1h`. |
| **Shard key** | `BucketID/ConsumerID` deterministically derived by `ws.Manager`. |
| **Dedup / OOO** | Explicit `duplicate` / `out_of_order` status in ingest path with dedicated counters. |
| **Backpressure** | `Enqueue` with counted `drop` (`ws_backpressure_drops_total`). |
| **Code anchors** | `internal/actors/marketdata/runtime/subsystem.go:63-67,160-177,219-231,333-351,371-387,433-449,455-471,737-756`; `internal/actors/marketdata/runtime/telemetry.go:9-13,72-90,225-251`; `internal/actors/marketdata/ws/manager.go:338-356,503-516`. |
| **IQ baseline** | `skip_unexpected_total=0`, `canonicalization_errors_total=0`, `ws_backpressure_drops_total=0` (report.md:57,153-163). |
| **Health checks** | `skip_unexpected_total`, `canonicalization_errors_total`, `ws_backpressure_drops_total`, `depth_gaps_total`. |

---

### 2 — Processor / Aggregation

| Field | Value |
|---|---|
| **Responsibility** | Transform `marketdata.*` envelopes into aggregated read models: snapshots, candles, stats, tape, heatmap snapshots, and volume-profile snapshots. Closes windows by watermark. |
| **Inputs** | `marketdata.>` (all marketdata subjects). |
| **Outputs** | `aggregation.snapshot.v1`, `aggregation.candle.v1`, `aggregation.stats.v1`, `aggregation.tape.v1`, `insights.heatmap_snapshot.v1`, `insights.volume_profile_snapshot.v1`. |
| **Boundedness** | Window/state caps: `Max*=50_000`, `WindowCap=96`, `LateTolerance=30s`; WS snapshot depth capped at `WsSnapshotDepthCap`. |
| **Determinism** | `WatermarkWindowManager` closes/evicts deterministically. |
| **Backpressure** | Snapshot deferral when processor is behind (`processor.go:208-213`). |
| **Code anchors** | `internal/actors/aggregation/runtime/processor.go:208-213,349-351,1270-1397`; `internal/core/aggregation/app/build_candle.go:63-74,86-89`; `internal/core/aggregation/app/build_stats.go:75-86,98-101`; `internal/core/aggregation/domain/watermark_window.go:28-41,85-127`; `internal/core/aggregation/app/build_tape.go:75-86,98-101`. |
| **IQ baseline** | Wire budgets PASS: p95/p99 under thresholds; `md backlog bounded` PASS (report.md:70-75). |
| **Health checks** | `bus_published_total{event_type="aggregation.*"}`, `ws_wire_bytes`, `ws_publish_to_deliver_latency_seconds`. |

---

### 3 — Delivery / Router / Session

| Field | Value |
|---|---|
| **Responsibility** | Route envelopes from the bus to WebSocket sessions. Apply per-stream coherence, sequencing, backpressure, backfill (`getrange`/`getlast`/`resync`), and capabilities negotiation. |
| **Inputs** | `marketdata.>`, `aggregation.>`, `insights.>`, `signal.>`. |
| **Outputs** | WebSocket frames: `snapshot`, `event`, `batch`, `range`, `ack`, `problem`. |
| **Boundedness** | Router TTL/sweep/cap: `30m`/`1m`/`50_000`. |
| **Shard key** | `ShardKey(SubsystemDelivery, streamID)` token; per-stream ownership. |
| **Monotonicity** | `acceptStreamSeq` with explicit decision; violations instrumented. |
| **Delivery seq** | `nextDeliverySeq` strictly monotonic per stream. |
| **Backpressure** | Policies: `drop_newest` / `drop_oldest` / `priority_drop`; slow-client disconnect. |
| **Wire caps** | `MaxFrameBytes` enforced in batch; handshake publishes effective limits. |
| **Backfill** | Initial candle backfill bounded at `subscribeBackfillLimit=64`. |
| **Code anchors** | `internal/actors/delivery/runtime/router.go:95-101,173-189,483-567,569-587,718-730`; `session_drop_policy.go:15-50,127-155`; `backpressure_strategy.go:95-109`; `session_delivery.go:177-180,227-233`; `session_commands.go:90-108`; `effective_limits.go:35-56,82-115`. |
| **IQ baseline** | Router coherence PASS, `violations_total=0`; queue utilization bounded PASS; drops/backpressure budget PASS (report.md:64,76,78,213-217,268-295). |
| **Health checks** | `delivery_router_coherence_violations_total`, `ws_queue_len/capacity`, `ws_drops_total`, `ws_backpressure_drops_total`. |

---

### 4 — Insights / LEL (Liquidity Evidence Layer)

| Field | Value |
|---|---|
| **Responsibility** | Generate heatmap and VPVR artifacts; detect liquidity evidence events (LEL); apply bounded state with deterministic dedup/replay and multi-replica ownership. |
| **Inputs** | Trades, bookdelta, snapshot, and tape events. |
| **Outputs** | `insights.heatmap_snapshot.v1`, `insights.volume_profile_snapshot.v1`, `liquidity.evidence`. |
| **Boundedness — insights** | Caps on buckets/cells/open windows/payload. |
| **Boundedness — LEL** | `MaxStreamsPerRule`, `MaxStreamsGlobal`, TTL; eviction by capacity/TTL; reject non-monotonic seq. |
| **Determinism** | Deterministic idempotency keys in heatmap and VPVR builders. |
| **Ownership — LEL** | Hash `venue|symbol` → owner replica (`subsystem.go:608-614`). |
| **Code anchors** | `internal/core/insights/app/build_heatmap.go:20-26,91-105,185-200,216-220`; `build_volume_profile.go:90-99,174-186,248-253,274-277`; `lel_engine.go:20-27,48-63,87-99`; `state_store.go:40-50,79-101,106-129`; `internal/actors/evidence/runtime/subsystem.go:608-614`. |
| **IQ baseline** | Evidence/signal subjects PASS; signal→evidence link PASS (report.md:65,219-228,306-319). |
| **Health checks** | `probe_md_canonical_evidence_frames`, `probe_widget_evidence_*`, `signal→evidence link`. |

---

### 5 — Evidence / Signal Engine

| Field | Value |
|---|---|
| **Responsibility** | Consume `liquidity.evidence` and microstructure/regime evidence, apply deterministic rules, dedup/rate-limit, and emit `signal.event` envelopes. |
| **Inputs** | `liquidity.evidence`, `insights.microstructure_evidence`, `insights.regime_evidence`. |
| **Outputs** | `signal.event`. |
| **Topology** | Primary: `cmd/signals` dedicated service. Fallback: embedded processor path via `processor.signals.enabled`. |
| **Shard key / Ownership** | `OwnerReplica(SubsystemSignals, StreamKey)` (`subsystem_owner_policy.go:71-77`). |
| **Monotonicity** | `DecideMonotonic` + reject `duplicate`/`out_of_order`. |
| **Boundedness** | State ownership cap `4096`; circular eviction (`subsystem_owner_policy.go:15-18,115-141`). |
| **Deterministic IDs** | `signal_id`, `correlation_id`, `fingerprint`, `intent_id`, `idempotency_key`. |
| **Dedup / replay** | `seq<=LastSeq` ignores replay; dedup window; tenant limit. |
| **Code anchors** | `internal/core/signal/engine.go:162-235`; `internal/actors/signal/runtime/subsystem.go:486-507`; `subsystem_owner_policy.go:15-18,50-61,71-77,115-141,150-185`; `internal/core/signal/store.go:157-160,177-214`. |
| **IQ baseline** | Seq monotonic PASS, router violations=0; signal→evidence link PASS (report.md:56,82,146-151,306-319). |
| **Health checks** | `signal_emitted_total`, `signal_drop_total`, `ownership_contract_*`, `probe_md_canonical_signal_frames`. |

---

### 6 — Strategist (Signal Composer)

| Field | Value |
|---|---|
| **Responsibility** | Compose derived/correlated signals (regime boost, microstructure correlation), control rate-limits, and maintain a bounded regime cache. |
| **Inputs** | `insights.regime_evidence`, `insights.microstructure_evidence`. |
| **Outputs** | `signal.composite` (via router / publisher). |
| **Topology** | Primary: `cmd/strategist` dedicated service. Fallback: embedded server path via `signals.use_composer`. |
| **Boundedness** | `strategistStateMaxStreams=4096`; regime cache bounded. |
| **Shard key / Ownership** | `OwnerReplica(SubsystemStrategist, venue/instrument/channel/raw)`. |
| **Monotonicity** | `DecideMonotonic` with stale gap window. |
| **Dedup / rate-limit** | `DedupWindow`; cap per key and global/minute. |
| **Composition determinism** | Fixed rules; confidence cap `<=0.99`. |
| **Code anchors** | `internal/actors/signals/runtime/subsystem.go:25-31,340-367,369-376,413-438,440-462`; `internal/core/signals/app/signal_rate_limiter.go:20-27,91-123`; `internal/core/signals/app/compose_signal.go:20-27,101-130,253-260`. |
| **IQ baseline** | Runtime + composer anchors confirmed (dossier §1.5). |
| **Health checks** | `ownership_contract_duplicate/out_of_order/evicted{strategist}`; `signal_drop_total{reason="owner_reject"}`. |

---

### 7 — Store (History / Persistence)

| Field | Value |
|---|---|
| **Responsibility** | Persist aggregated events and insights; serve historical queries (`getrange`/`getlast`) from in-memory store and TimescaleDB. |
| **Inputs** | JetStream consumers on `aggregation.*`, `insights.*` (per filter). |
| **Outputs** | Range/last responses; durable persistence (TimescaleDB / ClickHouse). |
| **Boundedness** | `maxPerSubject=4096` in-memory with tail truncation; PG cap equivalent. |
| **Determinism** | Stable sort by `ts_ingest,seq`; tail pagination. |
| **Fault tolerance** | PG query failure returns `nil,nil` (empty) to avoid session drop. Alias fallback for symbols without `:MARKET_TYPE`. |
| **Code anchors** | `internal/adapters/storage/timescale/delivery_range_store.go:25-33,51-53,75-85,106-112,152-156`; `internal/core/delivery/app/session_usecase.go:53-71`; `cmd/store/bootstrap.go:193-228,247-280`. |
| **IQ baseline** | Store subscription and event routing confirmed (dossier §1.8). |
| **Health checks** | `delivery_range_alias_fallback{hit|miss|error}`, `store_commit_*`, store heartbeat. |

---

### 8 — Server (HTTP / WS Gateway)

| Field | Value |
|---|---|
| **Responsibility** | Expose WebSocket endpoint `/ws` (v1), runtime admin endpoints, Prometheus metrics, and enforce auth/rate-limit policies per IP/key/tenant. |
| **Inputs** | Inbound HTTP/WS connections. |
| **Outputs** | WS sessions; operational APIs: `/healthz`, `/readyz`, `/runtime/*`, `/metrics`. |
| **Boundedness** | Per-tenant/IP/key connection limits; connection defaults. |
| **Route contract** | `/ws` enabled explicitly; `HandleFunc` refuses overrides of critical endpoints; `/ws/marketdata` hard-410 (legacy cutover preserved). |
| **Code anchors** | `internal/interfaces/ws/server.go:183-189,247-275,293-341`; `cmd/server/bootstrap.go:678-680`; `internal/interfaces/http/server.go:186-193`; `internal/interfaces/ws/legacy_handler.go:9-20`. |
| **IQ baseline** | `legacy route requests zero` PASS; smoke `hello/ack`, `legacy-off`, `clean-runtime` PASS (report.md:39,48-50,66-67). |
| **Health checks** | `ws_legacy_requests_total`, `ws_auth_fail`, connection-limit rejects, smoke `legacy-off`. |

---

### 9 — Client (Core/Platform/Widgets)

> The client is an Odin/WASM application; it is not a Guardian-managed subsystem. It is documented here
> for full data-path traceability.

| Field | Value |
|---|---|
| **Responsibility** | Consume Terminal_V1 frames, validate contract (`seq`, `prev_seq`, snapshot metadata), apply bounded reducers, and render widgets under render budgets. |
| **Inputs** | WS frames: `snapshot`, `event`, `batch`, `range`. |
| **Outputs** | `Market_Store`, `Layer Registry`, rendered widgets. |
| **Boundedness** | Rings: `trade=1024`, `candle=32`, `signal=64`; per-type evidence/signal caps. |
| **Contract validation** | Validates `seq gap`, `prev_seq`, `snapshot_seq`, `missing_ts_server`. |
| **Legacy rejection** | Explicit reject for legacy `evidence`/`signal` subjects. |
| **Code anchors** | `client/src/platform/web/marketdata_web.odin:41-48,379-382,569-572,713-720,1743-1771,1835-1894,1952-1964`; `client/src/core/layers/market_store.odin:6-8,77-86`; `client/src/core/services/signal_store.odin:7-8,36-41`. |
| **IQ baseline** | Widget budgets/entries bounded PASS; `prev_seq chaining` PASS (report.md:61,85-99). |
| **Health checks** | `client_prev_seq_violations`, `client_missing_ts_gap`, `probe_widget_*`, `batched_fallback_events`. |

---

## End-to-End Stream Traceability

```
Exchange WS
    │
    ▼
[MarketData] ──(marketdata.trade / bookdelta / markprice / liquidation)──►
                                                                           │
[Aggregation] ◄────────────────────────────────────────────────────────────┘
    │
    ├──(aggregation.tape)──────────────────────────────────────────┐
    ├──(aggregation.candle / stats)────────────────────────────────┤
    ├──(aggregation.snapshot v2)───────────────────────────────────┤
    ├──(insights.heatmap_snapshot)─────────────────────────────────┤
    └──(insights.volume_profile_snapshot)──────────────────────────┤
                                                                    │
[Evidence / LEL] ◄──(trades+bookdelta)                             │
    │                                                               │
    └──(liquidity.evidence)──►                                      │
                              │                                     │
              [Signal Engine]─┘                                     │
                    │                                               │
                    └──(signal.event)──────────────────────────────┤
                                                                    │
              [Strategist]──(signal.composite)────────────────────►┤
                                                                    │
                                                                    ▼
                                                           [Delivery / Router]
                                                                    │
                                                     ┌──────────────┤
                                                     │              │
                                                  [Store]      [WS Session]
                                               (persist +         │
                                                range srv)    [Client WASM]
```

**IQ-validated coverage (baseline `2026-03-05T16:21:18Z`):**

| Path | Status |
|---|---|
| `marketdata.trade → aggregation.tape → tape widget` | PASS |
| `bookdelta → aggregation.snapshot v2 → DOM` | PASS |
| `liquidity.evidence → signal.event → Evidence/Signal widget` | PASS |
| `stats/candle → price overlay` | PASS |

---

## Supervision Model (INV-TOPO-01)

The Guardian (`internal/actors/runtime/guardian.go`) orchestrates all subsystems:

```
Engine
 └── Guardian (runtime.Guardian)
      ├── Subsystem: marketdata
      ├── Subsystem: aggregation
      ├── Subsystem: delivery
      ├── Subsystem: insights
      ├── Subsystem: evidence
      ├── Subsystem: signals
      └── Subsystem: storage
```

**Restart policy** (`internal/actors/runtime/supervisor.go`):

| Parameter | Default |
|---|---|
| `BaseBackoff` | 250 ms |
| `MaxBackoff` | 5 s |
| `RestartWindow` | 30 s |
| `RestartLimit` | 5 failures / window |
| `Cooldown` | 30 s (degraded period) |
| Global restart limit | 5 / 1 min window |

**Invariants:**
- `TOP-1` — Failure in one subsystem does not kill siblings.
- `TOP-2` — Global restart rate limit prevents restart storms.
- `TOP-3` — Session actors clean-close and de-register from router.
- `TOP-4` — Repeated restart cycles maintain goroutine stability (soak).
- `TOP-5` — `Msg-ID` dedup on JetStream prevents double-delivery in dedup window.

Authority: `docs/adrs/ADR-0018-actor-topology-supervision-model.md`, validated by
`internal/actors/runtime/guardian_test.go:99,315,436`.

---

## Changelog

- 2026-03-05: Initial creation as part of S16→S17 architecture documentation consolidation.
  Sources: `docs/analysis/ARCHITECTURE-DOSSIER-S16-S17.md`, `internal/actors/runtime/protocol.go`,
  `internal/actors/runtime/guardian.go`, IQ baseline `artifacts/20260305T160115Z`.
