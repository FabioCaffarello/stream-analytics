# IQ Loop Runtime Invariants

**Status:** Active
**Date:** 2026-03-05
**Owner:** Governance Doc-First Maintainer
**Baseline IQ run:** `artifacts/20260305T160115Z` (HEAD `e1afc642`, `overall_pass=true`)
**Relates to:** `docs/architecture/system-invariants.md`, `docs/analysis/ARCHITECTURE-DOSSIER-S16-S17.md`,
  `docs/architecture/subsystems.md`

---

## Purpose

Document the Top-10 runtime properties that the IQ Loop validates on every run. Each entry maps:
- the property name as used in IQ reports
- the code anchor that enforces it
- the guardrail metric or check
- the failure mode
- the rollback action

This document is the runtime-evidence layer for `docs/architecture/system-invariants.md`.

---

## IQ Validation Command

```bash
PROCESSOR_REPLICAS=2 ./scripts/iq_loop.sh
```

Required artifacts per run (attach to PR):
```
artifacts/<ts>/evidence/summary.json
artifacts/<ts>/evidence/report.md
artifacts/<ts>/evidence/server.metrics.prom
artifacts/<ts>/evidence/logs/{server,consumer,processor}.log
artifacts/<ts>/evidence/logs/playwright-smoke.json
```

---

## Top-10 Runtime Properties

### P1 — `ts_server` Field Mandatory

| Field | Value |
|---|---|
| **Rule** | Every WS frame must carry a non-zero `ts_server`. |
| **Code enforcement** | `client/src/platform/web/marketdata_web.odin:1835-1837` (client validation) |
| **Guardrail** | `missing_ts_server == 0` |
| **IQ check** | `ts_server present` PASS (report.md:55) |
| **Failure mode** | Frame without `ts_server` → `client_missing_ts_gap` incremented; client gap alarm. |
| **Rollback** | Restore canonical `ts_server` serialization in frame builder / encoder. |

---

### P2 — Delivery Sequence Monotonic

| Field | Value |
|---|---|
| **Rule** | Each stream's delivery sequence must be strictly monotonically increasing. |
| **Code enforcement** | `internal/actors/delivery/runtime/router.go:483-567` (`acceptStreamSeq`) |
| **Guardrail** | `delivery_router_coherence_violations_total == 0` |
| **IQ check** | `seq monotonic` PASS (report.md:56) |
| **Failure mode** | Out-of-order delivery → coherence broken → client state corruption. |
| **Rollback** | Revert seq/ownership policy change to last stable commit. |

---

### P3 — `prev_seq` Chaining Valid

| Field | Value |
|---|---|
| **Rule** | `frame.prev_seq` must equal the last delivered `seq` for the same stream. |
| **Code enforcement** | `client/src/platform/web/marketdata_web.odin:1871-1875` |
| **Guardrail** | `client_prev_seq_violations == 0` |
| **IQ check** | `prev_seq chaining` PASS (report.md:61) |
| **Failure mode** | Broken prev_seq chain → inconsistent snapshot/event chain in client state. |
| **Rollback** | Revert delta frame builder or `prev_seq` assignment logic. |

---

### P4 — Legacy Subjects Rejected

| Field | Value |
|---|---|
| **Rule** | Legacy `evidence.*` and `signal.*` subjects must be explicitly rejected by the client. |
| **Code enforcement** | `client/src/platform/web/marketdata_web.odin:1952-1964` |
| **Guardrail** | `canonical evidence/signal subjects` PASS |
| **IQ check** | Canonical subjects PASS (report.md:65) |
| **Failure mode** | Downgrade to legacy subject → duplicate state or wrong schema deserialization. |
| **Rollback** | Revert `accept_legacy_*` flags / client parser to reject legacy subjects. |

---

### P5 — `/ws/marketdata` Never Used (Legacy Route 410)

| Field | Value |
|---|---|
| **Rule** | The legacy WebSocket route `/ws/marketdata` must always return HTTP 410 Gone. |
| **Code enforcement** | `internal/interfaces/ws/legacy_handler.go:9-20` |
| **Guardrail** | `ws_legacy_requests_total == 0` |
| **IQ check** | `legacy route requests zero` PASS (report.md:66-67) |
| **Failure mode** | Client or patch reintroduces legacy route → legacy subject traffic resurfaces. |
| **Rollback** | Restore `410 Gone` handler; remove any rewrite/compat routes. |

---

### P6 — No Compat Fallback Path Hit

| Field | Value |
|---|---|
| **Rule** | The batch fastpath must not fall back to the synthetic compat parser at baseline. |
| **Code enforcement** | `client/src/platform/web/marketdata_web.odin:1764-1771` (`batched_fallback_events`) |
| **Guardrail** | `no fallback/compat path hit` PASS |
| **IQ check** | `no fallback/compat path hit` PASS (report.md:69) |
| **Failure mode** | Parser leaves fastpath → `batched_fallback_events` grows → performance regression. |
| **Rollback** | Revert batching/decoder change that caused fastpath exit. |

---

### P7 — Wire Latency Within Budget

| Field | Value |
|---|---|
| **Rule** | `ws_publish_to_deliver_latency_seconds` p95/p99 must stay within baseline thresholds. |
| **Code enforcement** | `internal/actors/delivery/runtime/session_delivery.go:177-180` |
| **Guardrail** | Baseline: `md_parse_p95_us=1000`, `md_apply_p95_us=200` (summary.json:11-14) |
| **IQ check** | Wire latency budgets PASS (report.md:70) |
| **Failure mode** | Latency increase over threshold → slow clients accumulate backpressure / drops. |
| **Rollback** | Revert change that increased serialization / queueing depth. |

---

### P8 — Wire Bytes Within Budget

| Field | Value |
|---|---|
| **Rule** | Payload bytes per frame must stay within `MaxFrameBytes` cap. |
| **Code enforcement** | `internal/actors/delivery/runtime/session_delivery.go:177-180,227-233` |
| **Guardrail** | p95/p99 byte budget below threshold (report.md:71) |
| **IQ check** | Wire bytes budgets PASS (report.md:71) |
| **Failure mode** | Oversized payload → `frame_too_large`, drop, or client disconnect. |
| **Rollback** | Revert schema payload expansion; reapply depth cap. |

---

### P9 — Zero Drops / Backpressure at Baseline

| Field | Value |
|---|---|
| **Rule** | At steady-state baseline, `ws_backpressure_drops_total == 0`. |
| **Code enforcement** | `internal/actors/delivery/runtime/session_drop_policy.go:15-50` |
| **Guardrail** | `ws_backpressure_drops_total <= 0`; `queue utilization == 0.0` |
| **IQ check** | Drops/backpressure budget PASS; queue utilization PASS (report.md:76-79) |
| **Failure mode** | Burst traffic → drops / slow-client disconnect. |
| **Rollback** | Revert queue size or drop-policy threshold change. |

---

### P10 — Client Backlog Bounded

| Field | Value |
|---|---|
| **Rule** | All client ring buffers, stream stores, and widget budgets must stay within their caps. |
| **Code enforcement** | `client/src/platform/web/marketdata_web.odin:41-43,713-720`; `market_store.odin:6-8`; `signal_store.odin:7-8` |
| **Guardrail** | `md backlog bounded` PASS; widget entries bounded PASS (report.md:74-75,85-99) |
| **IQ check** | Widget budgets PASS; backlog bounded PASS (report.md:74-75,85-99) |
| **Failure mode** | Uncapped ring or store → unbounded memory growth / render budget violation. |
| **Rollback** | Revert cap/ring mutations; re-sync with IQ gates after change. |

---

## Invariant Governance Matrix

| Property | Subsystem | Metric | IQ check | Failure risk |
|---|---|---|---|---|
| P1 `ts_server` | Delivery / Frame Builder | `missing_ts_server` | P0 | Frame unusable by client |
| P2 Seq monotonic | Delivery Router | `delivery_router_coherence_violations_total` | P0 | Stream state corruption |
| P3 `prev_seq` chain | Delivery → Client | `client_prev_seq_violations` | P0 | Snapshot chain inconsistency |
| P4 Legacy subjects OFF | Client | (canonical subjects check) | P0 | Dual-decode / wrong schema |
| P5 Legacy route 410 | Server | `ws_legacy_requests_total` | P0 | Legacy traffic resurface |
| P6 No compat fallback | Client | `batched_fallback_events` | P1 | Performance regression |
| P7 Wire latency | Delivery | `ws_publish_to_deliver_latency_seconds` | P1 | Drop cascade |
| P8 Wire bytes | Delivery | (frame byte histogram) | P1 | Oversized frame drop |
| P9 Zero drops baseline | Delivery | `ws_backpressure_drops_total` | P1 | Data loss under burst |
| P10 Backlog bounded | Client | `md_backlog_bounded` | P1 | OOM / render stall |

---

## Anti-Legacy Rules (Hard Blocks)

These conditions must remain true on every IQ run. Any PR that causes a regression must be rejected:

1. `/ws/marketdata` must remain `410 Gone`.
2. Legacy `evidence.*` and `signal.*` subjects must remain rejected by client.
3. `compat_fallback_zero` / `no fallback/compat path hit` — counters must remain zero.
4. `ws_legacy_requests_total` must remain zero.
5. `canonicalization_errors_total` must remain zero.

---

## Risk Register (Top-5, IQ-validated)

| Risk | Blast radius | Detection | Minimum mitigation |
|---|---|---|---|
| R1 — IQ gate profile drift | Release without catching regression | `compat_fallback_zero`, `stats canonical`, `legacy route zero` (report.md:66-70) | Freeze CI IQ env profile |
| R2 — Ownership contract drift | OOO / owner_reject / event loss | `seq monotonic`, `router coherence` ownership counters | Cross-subsystem property tests |
| R3 — Fragmented cap/eviction | Gradual boundedness breakdown | `md backlog bounded`, widget bounded (report.md:74-75) | Cap matrix + IQ validation |
| R4 — Per-stream scorecard missing | Rollout without degraded-channel visibility | Stream budgets in IQ report | Add scorecard to IQ final output |
| R5 — Legacy cutover regression | Re-introduction of legacy traffic | `legacy route zero`, `canonical evidence/signal` | Negative test assertions |

---

## Changelog

- 2026-03-05: Initial creation from `docs/analysis/ARCHITECTURE-DOSSIER-S16-S17.md` §2 and §4.
  Baseline IQ run: `artifacts/20260305T160115Z`.
