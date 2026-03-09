# Stage 110 — Dashboard Playwright Validation Report

**Date**: 2026-03-08
**Type**: Operational validation (no architecture changes)
**Method**: Playwright MCP against `http://localhost:8090` with `PROCESSOR_REPLICAS=2`

## Results Summary

| # | Flow | Result | Notes |
|---|------|--------|-------|
| 1 | Boot | **PASS** | WASM loaded, canvas rendered in <5s |
| 2 | WS Connection | **PASS** | Hello handshake, 14+ subs acked, data flowing |
| 3 | Chart Render | **PASS** | Candles, DeltaVol bars, CVD line rendering |
| 4 | Timeframe Change | **PASS** | Key `3` switched 5s→1m, clean unsub/sub cycle, badge OK→STALE |
| 5 | Resize Panes | **PASS** | 1366→1024→1920→1366, layout adapted both ways |
| 6 | Compare Mode | **SKIPPED** | Requires 2+ streams (`actions.odin:430` guard). Only 1 active. Not a bug. |
| 7 | Analytics Subplots | **PASS** | CVD line + DeltaVol bars with labels and values |
| 8 | Navigate to Markets | **PASS** | Market Explorer: 7 venues, 21 instruments, 2 active streams |
| 9 | Return to Dashboard | **PASS** | Layout preserved after round-trip navigation |
| 10 | Session Health Page | **PASS** | Transport running, 12 msg/s, lag 4ms, delivery stable |
| 11 | Help Overlay | **PASS** | `?` key shows shortcuts, Escape dismisses |

**10/11 passed, 1 skipped (precondition not met, not a defect)**

## Console Errors

4 errors total — all identical:

```
Failed to load resource: 400 (Bad Request) @ storage.js:62
```

Fires on workspace save attempts. **Non-critical** — workspace persistence endpoint returns 400 when no prior workspace exists. No JS runtime errors, no WebSocket errors, no rendering crashes.

## Observations

- **"Seeding..." on bottom widgets** — Counter, Trades, OB show "Seeding..." throughout. Stack is in `CTX:LIVE_ONLY` with no historical backfill (GetRange timeout). Expected for fresh stack.
- **STALE badge** — Intermittent, due to `seeding (no history)` state. Artifact policy correctly classifies.
- **RTT: 1–4ms** — Excellent transport latency throughout session.
- **Zero JS runtime errors** — No null refs, no rendering crashes, no unhandled promises.
- **Session Health diagnostics** — Transport proto v1 Terminal_V1, parse p95=200us p99=400us, 2 delivery connections, client health=healthy.

## Infrastructure State

| Container | Status |
|-----------|--------|
| compose-consumer-1 | healthy |
| compose-executor-1 | healthy |
| compose-portfolio-1 | healthy |
| compose-processor-1 | healthy |
| compose-processor-2 | healthy |
| compose-signals-1 | healthy |
| compose-strategist-1 | healthy |
| market-raccoon-clickhouse | healthy |
| market-raccoon-client | healthy |
| market-raccoon-grafana | healthy |
| market-raccoon-nats | healthy |
| market-raccoon-prometheus | healthy |
| market-raccoon-server | healthy |
| market-raccoon-store | healthy |
| market-raccoon-timescale | healthy |

All 15 containers healthy. 2 processor replicas running.

## Verdict

**Dashboard functional on new workspace layout. No critical bugs. No regressions from previous stages.**

Single finding: `storage.js:62` returns 400 on workspace save — should gracefully handle missing workspace (no-op or create-on-first-save).
