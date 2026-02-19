# PRD-0002 - Backend Stable & Odin-Ready

**Status:** Active
**Owner:** Chief Architect
**Date:** 2026-02-19
**Last updated:** 2026-02-19
**Relates to:** `docs/prd/PRD-0001-extreme-runtime.md`, `docs/rfcs/RFC-0011-product-parity-marketmonkey.md`, `docs/rfcs/EXECUTION-SEQUENCE.md`, `docs/architecture/AUTHORITY-MAP.md`

---

## Objective

Define the acceptance criteria for declaring the Market Raccoon backend **stable** — meaning it can run continuously via `docker compose` without operator intervention — and **Odin-ready** — meaning a desktop client (Odin) can connect via WebSocket and consume live market data streams with the same functional coverage that MarketMonkey's backend provides today.

## Goals

1. **G1 — Compose-up-and-forget.** `make up-core` starts the full backend (consumer, processor, server, store + infra) and all services reach `/readyz` healthy within 60 seconds. No manual config edits required beyond `.env` overrides.
2. **G2 — Odin can subscribe and receive data.** An Odin client connecting to `wss://server:8080/ws` with a valid API key can subscribe to all stable subjects and receive ordered, bounded-queue event frames in JSON or proto.
3. **G3 — Five exchanges live.** Binance spot, Binance futures, Bybit, Coinbase, and HyperLiquid — all parsed, normalized, and published to the event bus.
4. **G4 — Orderbook + VPVR + cross-venue signals delivered.** Orderbook snapshots, volume profile snapshots/deltas, and cross-venue trade/spread signals are routable via WS delivery.
5. **G5 — Cold path operational.** The `store` binary persists aggregation snapshots to ClickHouse with ack-on-commit semantics. Gap detection is available as a CLI tool.
6. **G6 — SLOs hold under sustained load.** The three SLOs (ingest 99.9%, delivery p99 < 250 ms, data loss 99.99%) hold for a 10M-event multi-exchange soak.

## Non-Goals

- **Candle aggregation (OHLCV).** W14 scope. Not required for Odin v0 — candle rendering can be client-side from trade stream until backend candle pipeline is ready.
- **Stats aggregation (liq/funding/markprice per TF).** W14 scope. Same rationale.
- **Heatmap delivery pipeline.** Domain model exists but heatmap writers, delivery wiring, and storage are deferred. Odin v0 does not render heatmaps.
- **Timescale `getrange` durable source.** `getrange` uses in-memory buffer only. Historical range queries beyond the buffer window are out of scope.
- **Funding rate standalone pipeline.** Funding rate is embedded in markprice/liquidation flow (GD-13). No standalone subject.
- **Production TLS certificates.** TLS is implemented but ships with self-signed defaults. Certificate provisioning is an operator concern.
- **Odin client implementation.** This PRD covers backend readiness only.

## Current State (as-of 2026-02-19)

| Capability | Status | Gap for Stable |
|---|---|---|
| 5-exchange consumer (Binance spot/futures, Bybit, Coinbase, HyperLiquid) | Implemented | None — parsers + endpoints tested |
| WS delivery (subscribe/unsubscribe/getrange, JSON + proto opt-in) | Implemented | Slow-client disconnect threshold missing |
| Auth (API key) + rate limiting (token bucket) | Implemented | None |
| Orderbook aggregation + snapshot delivery | Implemented | None |
| VPVR builder + snapshot/delta delivery | Implemented | None |
| Cross-venue trade snapshot + spread signal | Implemented | None |
| MarkPrice + liquidation ingestion + dedup | Implemented | None |
| Cold-path writer (JetStream -> ClickHouse) | Implemented | Read ports (SELECT) missing |
| Backfill binary (`cmd/backfill`) | Stub | No source code — C3 scope |
| Gap detection tool | Missing | Depth gaps logged only — C3 scope |
| Docker Compose (infra + core + obs profiles) | Implemented | Needs smoke-test gate |
| Candle aggregation | Not started | Non-goal for Odin v0 |
| Stats aggregation | Not started | Non-goal for Odin v0 |
| Heatmap delivery pipeline | Not started | Non-goal for Odin v0 |

## Functional Requirements

### FR-1: Compose Lifecycle

| ID | Requirement | Verification |
|---|---|---|
| FR-1.1 | `make up-core` brings all services to healthy within 60s | **TODO:** `scripts/smoke-compose.sh` (does not exist yet — must be created before M1) |
| FR-1.2 | `make down` stops all services and releases volumes cleanly | Manual + CI |
| FR-1.3 | All binaries read config from mounted JSONC; no hardcoded defaults leak | Config loader tests |
| FR-1.4 | Shard env vars (`SHARD_INDEX`, `SHARD_COUNT`) propagate correctly | E2E integration test |
| FR-1.5 | Infra healthchecks (NATS, TimescaleDB, ClickHouse) gate app startup via `depends_on: condition: service_healthy` | Compose file review |

### FR-2: WS Delivery Contract (Odin API Surface)

| ID | Requirement | Verification |
|---|---|---|
| FR-2.1 | Client connects via `wss://server:8080/ws` with `Authorization: Bearer <api_key>` | `TestWSAuth_ValidKey` |
| FR-2.2 | Subscribe/unsubscribe/getrange ops work per `docs/contracts/delivery-ws.md` | Session integration tests |
| FR-2.3 | Event frames contain `type`, `subject`, `seq`, `ts_ingest`, `payload` | Wire contract tests |
| FR-2.4 | Per-subject `seq` ordering preserved within a session (WS-3) | `TestSession_SeqOrdering` |
| FR-2.5 | Slow client receives `drop_newest` drops with observable `queue_full` reason | Backpressure soak |
| FR-2.6 | Unsubscribe + disconnect release all routing state (WS-5) | Leak detection test |
| FR-2.7 | Proto opt-in via `?format=proto` or `X-Delivery-Format: proto` header | Proto delivery test |

### FR-3: Exchange Coverage

| ID | Requirement | Verification |
|---|---|---|
| FR-3.1 | Binance spot: trade + bookdelta + markprice + liquidation | Parser tests |
| FR-3.2 | Binance futures: trade + bookdelta + markprice + liquidation | Parser tests |
| FR-3.3 | Bybit: trade + bookdelta + markprice + liquidation | Parser tests |
| FR-3.4 | Coinbase: trade + bookdelta | Parser tests |
| FR-3.5 | HyperLiquid: trade + bookdelta + liquidation | Parser tests |
| FR-3.6 | Canonical instrument normalization across all exchanges (ADR-0017) | `TestCanonicalNormalization_*` |

### FR-4: Subjects Routable to Odin

Stable subjects that Odin can subscribe to at launch:

| WS Subject Pattern | Source |
|---|---|
| `marketdata.trade/{venue}/{symbol}/raw` | Consumer |
| `marketdata.bookdelta/{venue}/{symbol}/raw` | Consumer |
| `marketdata.markprice/{venue}/{symbol}/raw` | Consumer |
| `marketdata.liquidation/{venue}/{symbol}/raw` | Consumer |
| `aggregation.snapshot/{venue}/{symbol}/raw` | Processor |
| `insights.crossvenue.trade_snapshot/global/{symbol}/raw` | Processor |
| `insights.crossvenue.spread_signal/global/{symbol}/raw` | Processor |
| `insights.volume_profile_snapshot/{venue}/{symbol}/raw` | Processor |
| `insights.volume_profile_delta/{venue}/{symbol}/raw` | Processor |

### FR-5: Cold Path

| ID | Requirement | Verification |
|---|---|---|
| FR-5.1 | Store binary consumes JetStream and writes to ClickHouse with ack-on-commit | `TestIngestConformance_AckNakTermGoldenTable` |
| FR-5.2 | Quarantine subjects routed to dead-letter, not silently dropped | Conformance test |
| FR-5.3 | Backfill binary downloads Binance agg trades and produces JSONL fixtures (C3) | `TestBackfill_ProducesValidFixture` |
| FR-5.4 | Gap detector exits non-zero when gaps found (C3) | `TestGapDetector_ReturnsGaps` |

## Performance Budgets

Source of truth: `docs/perf/performance-budgets.md`. PRD-0001 is authoritative for SLO targets.

| Metric | Budget | Gate |
|---|---|---|
| Ingest p95 | <= 500 us | `BenchmarkIngest` |
| Ingest p99 | < 10 ms (PRD-0001) | Soak assertion |
| Delivery WS p99 | < 250 ms (SLO-2) | `ws_send_latency_ms_bucket` |
| Cold-path commit p95 | <= 10 ms | `TestStoreSoak_ColdPathLatencyBudgets` |
| Orderbook snapshot e2e | <= 15 us/op | `BenchmarkE2E_IngestToOrderbookSnapshot` |
| Heap delta (10M soak, 4 exchanges) | <= 1 GB | Soak assertion |
| Goroutine drift (pipeline + delivery) | <= 48 | Soak assertion |
| Active orderbooks cardinality | <= 4,096 | BoundedMap eviction |
| Active instrument streams | <= 4,096 | Config `max_instruments` |
| Compose full-stack boot | < 60 s | **TODO:** `scripts/smoke-compose.sh` |

## Mandatory Tests

These tests MUST pass before the backend is declared stable.

### Gate 1: Unit + Integration (CI-fast)

```bash
make test-workspace           # all modules, short mode
make test-workspace-race      # race detector
make invariants-check         # system invariants
make proto-lint               # protobuf schema
make proto-breaking           # protobuf backward compat
make docs-check               # doc headers, links, truth-map, registry
```

### Gate 2: Soak (CI-nightly)

```bash
make soak-check               # WS lifecycle + VPVR overload
make soak-pipeline            # 10M multi-exchange pipeline (C4)
make soak-roundtrip           # cold-path write+read (C4)
```

### Gate 3: Compose Smoke

```bash
make up-core
scripts/smoke-compose.sh      # waits for /readyz on all 4 binaries
make down
```

> **TODO:** `scripts/smoke-compose.sh` does not exist yet. Must be created before M1.
> Script requirements: (1) wait up to 60 s for `/readyz` 200 on consumer, processor, server, store;
> (2) exit 0 on all healthy, exit 1 on timeout; (3) wire into a `make smoke` target.

### Gate 4: Delivery Contract

| Test | File |
|---|---|
| `TestParseSubject` | `internal/core/delivery/domain/subject_test.go` |
| `TestSession_parseSubscribeUnsubscribeGetRange` | `internal/actors/delivery/runtime/session_test.go` |
| `TestRouter_subscribeUnsubscribeAndBroadcast` | `internal/actors/delivery/runtime/router_test.go` |
| `TestWSBackpressureSlowClientDropPolicy` | `internal/actors/delivery/runtime/session_backpressure_test.go` |
| `TestWSAuth_ValidKey` + `TestWSAuth_InvalidKey` | `internal/interfaces/http/auth_test.go` |
| `TestWSRateLimit_TokenBucket` | `internal/interfaces/http/ratelimit_test.go` |

### Gate 5: Exchange Parsers

| Test | File |
|---|---|
| `TestParseMessage_*` (Binance) | `internal/adapters/exchange/binance/parser_test.go` |
| `TestParseMessage_*` (Bybit) | `internal/adapters/exchange/bybit/parser_test.go` |
| `TestParseMessage_*` (Coinbase) | `internal/adapters/exchange/coinbase/parser_test.go` |
| `TestParseMessage_*` (HyperLiquid) | `internal/adapters/exchange/hyperliquid/parser_test.go` |

## Release Checklist

| # | Item | Owner | Status | Anchor |
|---|---|---|---|---|
| 1 | Gate 1 passes on `main` | CI | Done | `make ci` (`Makefile`) |
| 2 | Gate 2 soak evidence committed to `.context/evidence/` | Dev | Done | `make soak-pipeline` (`Makefile`); evidence: `.context/evidence/c4-pipeline-soak.txt` |
| 3 | Gate 3 compose smoke passes locally and in CI | Dev | Done | `scripts/smoke-compose.sh`; `make up-core` + `make smoke` (`Makefile`) |
| 4 | Gate 4 delivery contract tests green | Dev | Done | `internal/actors/delivery/runtime/*_test.go`; `internal/interfaces/http/{auth,ratelimit}_test.go` |
| 5 | Gate 5 all exchange parsers green | Dev | Done | `internal/adapters/exchange/{binance,bybit,coinbase,hyperliquid}/parser_test.go` |
| 6 | `deploy/configs/*.jsonc` reviewed — no `CHANGE_ME` tokens | Dev | Done | `deploy/configs/server.jsonc` (no `CHANGE_ME` tokens) |
| 7 | Alert rules pass `promtool check rules` | Dev | Done | `promtool check rules deploy/observability/prometheus/alerts.rules.yml deploy/observability/prometheus/shard-alerts.rules.yml` |
| 8 | ClickHouse migrations run without error on fresh DB | Dev | Done | `sql/clickhouse/migrations/` validated on fresh compose volume (`make down -v` + `make up-core`) |
| 9 | TimescaleDB migrations run without error on fresh DB | Dev | Done | `sql/timescale/migrations/` validated on fresh compose volume (`make down -v` + `make up-core`) |
| 10 | PRD-0002 status changed to `Active` | Architect | Done | This file, line 3 |
| 11 | Tag `v0.1.0-stable` created on `main` | Architect | Pending | `git tag v0.1.0-stable` |

## Open Risks

| ID | Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|---|
| R-01 | `getrange` backed by in-memory buffer only — Odin cannot query historical data beyond buffer window | Medium | High | Document limitation in Odin client. Timescale durable range is deferred per `codebase-modernization-baseline.md`. |
| R-02 | No candle/stats streams — Odin must render candles client-side from trade events | Medium | Certain | Odin v0 implements client-side OHLCV from trade stream. Backend candle pipeline (W14) is a fast-follow. |
| R-03 | Slow-client disconnect threshold not implemented — misbehaving Odin instance accumulates drops silently | Low | Medium | `drop_newest` policy bounds memory. Add `ws_drops_total` alert. Implement disconnect-on-threshold as fast-follow. |
| R-04 | Cold-path read ports missing — backfill/gap-detection blocked until C3 | Low | High | C3 is next execution wave. Does not block Odin WS delivery. |
| R-05 | Proto delivery flags disabled by default — Odin must use JSON unless flags toggled | Low | Low | JSON is sufficient for Odin v0. Proto activation is an operator toggle, not a code change. |
| R-06 | Single-shard consumer — scaling beyond 1 consumer requires `SHARD_COUNT` > 1 and multiple replicas | Low | Low | Shard infra implemented and tested. Default single-shard is sufficient for < 200 instruments. |
| R-07 | `content_type` field not emitted in WS event frames | Low | Certain | Odin client infers type from subject. Field is a planned optional extension. |

## Milestones

| Milestone | Scope | Depends On | Exit Criteria | Anchor |
|---|---|---|---|---|
| **M0 — CI Green on main** | All Gate 1 tests pass, no flaky failures | — | `make ci` green for 5 consecutive runs | `Makefile` (`ci` target) |
| **M1 — Compose Smoke** | `make up-core` boots to healthy; smoke script passes | M0 | Gate 3 green | `Makefile` (`up-core`); `scripts/smoke-compose.sh` |
| **M2 — Delivery Contract Hardened** | All Gate 4 tests pass; slow-client drop metrics wired | M0 | Gate 4 green + `ws_drops_total` metric exists | `internal/actors/delivery/runtime/`; `internal/interfaces/http/{auth,ratelimit}_test.go` |
| **M3 — Cold-Path Operational (C3)** | Backfill binary + gap detector + cold-path read ports | M0 | FR-5.3, FR-5.4 green | `cmd/backfill/` (stub); `.context/prompts/codex-prompt-C3-operational-tooling.md` |
| **M4 — Multi-Exchange Soak (C4)** | 10M-event soak with 4 exchanges, budget assertions | M1, M2 | Gate 2 green; evidence in `.context/evidence/c4-pipeline-soak.txt` | `.context/prompts/codex-prompt-C4-production-soak.md` |
| **M5 — Backend Stable** | All gates pass; release checklist complete | M1, M2, M3, M4 | Tag `v0.1.0-stable`; PRD-0002 status `Active` | Release Checklist (above) |
| **M6 — Odin v0 Connected** | Odin client connects, subscribes to FR-4 subjects, renders live data | M5 | Manual acceptance by product owner | — |

## Evidence

- `docs/prd/PRD-0001-extreme-runtime.md` — program baseline
- `docs/rfcs/RFC-0011-product-parity-marketmonkey.md` — parity gap analysis
- `docs/contracts/delivery-ws.md` — WS wire contract
- `docs/contracts/subject-registry.yaml` — subject inventory
- `docs/observability/slo.md` — SLO definitions
- `docs/perf/performance-budgets.md` — performance budgets
- `docs/operations/cold-path-runbook.md` — cold-path operational guide
- `deploy/compose/docker-compose.yml` — compose topology
- `deploy/configs/*.jsonc` — runtime configurations

## Changelog

- 2026-02-19 (gates-1-3-7-10):
  - Gate 1 marked `Done` after `make ci` passed.
  - Gate 2 marked `Done` with new soak evidence in `.context/evidence/c4-pipeline-soak.txt`.
  - Gate 3 marked `Done` after `make up-core` + `make smoke` passed (script exists and is executable).
  - Gate 7 marked `Done` after `promtool check rules` passed for active alert rule files.
  - Gate 8/9 marked `Done` after fresh-volume compose bootstrap validated ClickHouse/Timescale migration tables.
  - PRD status promoted from `Draft` to `Active` (Gate 10 done).
  - Gate 11 remains pending because release tag must be created on `main`.
- 2026-02-19 (gate-5-6):
  - Gate 5 marked `Done` after parser suites for Binance/Bybit/Coinbase/HyperLiquid passed.
  - Gate 6 marked `Done` after removing last `CHANGE_ME` token from `deploy/configs/server.jsonc`.
- 2026-02-19 (gate-4):
  - Added WS auth contract tests in `internal/interfaces/http/auth_test.go`.
  - Added WS rate-limit token bucket test in `internal/interfaces/http/ratelimit_test.go`.
  - Gate 4 checklist item updated to `Done`; removed stale TODO anchors for auth/rate-limit test files.
- 2026-02-19 (audit):
  - Gate 3: marked `scripts/smoke-compose.sh` as TODO (file does not exist yet).
  - Gate 4: marked `auth_test.go` and `ratelimit_test.go` as TODO (files do not exist yet).
  - Performance Budgets: annotated smoke-compose row as TODO.
  - Release Checklist: added Anchor column with real paths; flagged `CHANGE_ME` in server.jsonc.
  - Milestones: added Anchor column linking gates to Makefile targets and prompt files.
  - Added `docs/architecture/AUTHORITY-MAP.md` to Relates-to.
- 2026-02-19: Initial draft.
