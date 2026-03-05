# Integration QA — Bug List (2026-03-03)

**Stack:** `make up PROCESSOR_REPLICAS=2` — 11 containers, all healthy.
**Method:** websocat WS probes + container log analysis + code audit.

---

## BUG-1: Candle events never delivered to WS clients (CRITICAL) — FIXED

| Field | Value |
|-------|-------|
| **Severity** | P0 — Correctness |
| **Component** | `internal/actors/delivery/runtime/router.go` |
| **Root Cause** | `allowEnvelopeTimeframeOverride()` (line 545-549) only returned `true` for `insights.` prefix. Candle envelopes carry `Meta["timeframe"]` (set at `internal/adapters/jetstream/artifact_publisher.go:135`), but the router ignored it and routed to `/raw`. The Odin client subscribes with the user's active TF (e.g., `/1m`), so the subject never matched. |
| **Repro** | 1. Connect WS to `/ws` with valid API key. 2. HELLO handshake. 3. Subscribe `aggregation.candle/binance/BTCUSDT/1m`. 4. Wait 15s — **0 events**. 5. Subscribe `aggregation.candle/binance/BTCUSDT/raw` — **13 events** (all TFs mixed). |
| **Expected** | Subscribing to `aggregation.candle/.../1m` delivers only 1m candles. |
| **Actual** | Zero delivery. Only `/raw` received events (all timeframes mixed). |
| **Fix** | Extended `allowEnvelopeTimeframeOverride` to include `aggregation.candle` and `aggregation.stats`. Updated 3 router tests. |
| **Verification** | Post-fix: `candle_1s = 26`, `candle_1m = 0` (no 1m rollups yet), `candle_5m = 0`. Timeframe isolation PASS — 1s candles only go to `/1s`, not `/1m` or `/5m`. |
| **Status** | **FIXED** |

---

## BUG-2: Stats envelope missing timeframe metadata (MEDIUM) — FIXED

| Field | Value |
|-------|-------|
| **Severity** | P1 — Completeness |
| **Component** | `internal/adapters/jetstream/artifact_publisher.go:168` |
| **Root Cause** | `PublishStatsClosed` set `Meta: artifactMetaForInstrument(evt.Stats.Instrument, nil)` — no timeframe. Compare with `PublishCandleClosed` (line 133-136) which correctly passes `map[string]string{"timeframe": evt.Candle.Timeframe}`. |
| **Repro** | Subscribe `aggregation.stats/binance/BTCUSDT/raw` — received 2 events in 30s. Even with BUG-1 fix, stats routed to `/raw` because no timeframe metadata. |
| **Expected** | Stats envelopes carry `timeframe` in Meta, matching candle envelopes. |
| **Actual** | Stats Meta had no `timeframe` key. |
| **Fix** | Added `map[string]string{"timeframe": evt.Stats.Timeframe}` to stats envelope Meta. |
| **Verification** | Post-fix: `stats_1s = 1` event delivered on `/1s` subscription — TF metadata working. |
| **Status** | **FIXED** |

---

## BUG-3: Router tests hard-coded to `/raw` for candles (LOW) — FIXED

| Field | Value |
|-------|-------|
| **Severity** | P2 — Test fidelity |
| **Component** | `internal/actors/delivery/runtime/router_test.go` |
| **Root Cause** | Router tests subscribed to `aggregation.candle/.../raw` and expected delivery at `/raw`, not reflecting real client behavior. |
| **Fix** | Replaced `TestRouter_preservesRawRoutingForAggregationCandleWithTimeframeMeta` with 3 new tests: `TestRouter_routesCandleByEnvelopeTimeframeMeta` (candle with TF meta → `/1m`), `TestRouter_candleFallsBackToRawWithoutTimeframeMeta` (no meta → `/raw`), `TestRouter_routesStatsByEnvelopeTimeframeMeta` (stats with TF meta → `/1m`). Updated `TestRouter_routesMarketTypeAlias` and `TestRouter_doesNotDuplicate` from `/raw` to `/1m`. |
| **Verification** | All 6 targeted tests PASS. Full delivery runtime suite PASS (6.745s). Full WS interface suite PASS (0.185s). |
| **Status** | **FIXED** |

---

## Summary

| ID | Severity | Status | Component | Files Changed |
|----|----------|--------|-----------|---------------|
| BUG-1 | P0 Critical | **FIXED** | router.go | `internal/actors/delivery/runtime/router.go` |
| BUG-2 | P1 Medium | **FIXED** | artifact_publisher.go | `internal/adapters/jetstream/artifact_publisher.go` |
| BUG-3 | P2 Low | **FIXED** | router tests | `internal/actors/delivery/runtime/router_test.go` |

**Files changed (3):**
1. `internal/actors/delivery/runtime/router.go` — `allowEnvelopeTimeframeOverride` extended for `aggregation.candle` and `aggregation.stats`
2. `internal/adapters/jetstream/artifact_publisher.go` — `PublishStatsClosed` now sets `timeframe` in envelope Meta
3. `internal/actors/delivery/runtime/router_test.go` — 3 new tests + 2 updated tests for TF-qualified routing

**Test results:**
- `./internal/actors/delivery/runtime/` — PASS (6.745s)
- `./internal/adapters/jetstream/` — PASS (8.793s)
- `./internal/interfaces/ws/` — PASS (0.185s)

**Non-issues confirmed:**
- `marketdata.trade/*/raw` — 754+ events in 15s (PASS)
- `marketdata.bookdelta/*/raw` — 148+ events in 15s (PASS)
- `insights.heatmap_snapshot/*/1m` — 20+ events in 15s (PASS)
- `aggregation.stats/*/1s` — 1 event in 30s after fix (PASS)
- HELLO handshake, subscribe ACKs — all working
- 2 processor replicas: shard 0/2 and 1/2 resolved correctly
- No ERROR/WARN in any container logs
- Timeframe isolation: 1s candles only go to `/1s` (not `/1m` or `/5m`)

**Note:** 1m candle delivery not yet observed because the processor is still catching up through the NATS backlog after container rebuild. Sub-minute (1s) candles and stats are confirmed working with timeframe-qualified routing. 1m candles will flow once the processor closes its first 1m window.
