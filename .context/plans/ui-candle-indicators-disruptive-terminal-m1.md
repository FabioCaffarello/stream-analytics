---
status: completed
progress: 100
generated: 2026-03-01
title: UI Candle and Indicator Layer Hardening (M1)
owner: feature-developer
workflow: PREVC
phase: C
---

# UI Candle and Indicator Layer Hardening (M1)

> Establish deterministic candle and indicator rendering parity on web and native as the base for a disruptive terminal UX.

## Scope
- Stabilize candle subject taxonomy and timeframe propagation (subscribe + getrange + lazy load).
- Guarantee historical range retrieval for symbol aliases (`SYMBOL:MARKET_TYPE`) in delivery service.
- Add observability and regression gates for UI widget hydration (`candles`, `heatmap`, `vpvr`, `stats`, `orderbook`, `trades`).
- Define architecture runway for higher-order indicators and rendering performance upgrades.

## Dependencies
| Dependency | Type | Status |
|-----------|------|--------|
| subject-taxonomy-stabilization.md | informs | done |
| backend-subminute-hardening-execution.md | informs | pending |
| deploy/nginx/client.conf WS proxy contract | blocks | done |
| Delivery range store schema compatibility | blocks | pending |

## Phases

### P - Plan
- [x] Scope confirmed against MarketMonkey-inspired UX targets.
- [x] Timeframe and subject contract documented (`raw` vs `1m/5m/...`).
- [x] Data compatibility matrix drafted (`symbol`, `symbol:market_type`) for getrange.

### R - Review
- [x] Technical review of delivery/UI contracts for subject and range fallback behavior.
- [x] Validate indicator semantics per timeframe (candle overlays and panel indicators).
- [x] Confirm observability KPIs and acceptance probes for browser and native.

### E - Execute
- [x] `client/src/core/util`: make candle channels timeframe-aware at subject builder level.
- [x] `client/src/platform/web` and `client/src/platform/native`: resubscribe timeframe-sensitive channels on TF switch, including candles.
- [x] `client/src/core/app/stream_views`: request getrange using active timeframe and support lazy-load via `to_ms`.
- [x] `internal/core/delivery/app`: fallback getrange lookup from `SYMBOL:MARKET_TYPE` to canonical `SYMBOL`.
- [x] Add and pass unit tests for alias fallback and range behavior.
- [x] Add UI runtime validation script checks for candle hydration count and indicator coverage.
- [x] Tests pass: `make -C client build` and `go test ./internal/core/delivery/app`.

### V - Validate
- [x] Gate: `make test-short`.
- [x] Gate: `make fmt && make lint`.
- [x] Gate: `make invariants-check` (internal changes present).
- [x] Capture evidence from:
  - `WS_URL=ws://127.0.0.1:8090/ws SOAK_SECONDS=12 bash client/scripts/check-widgets-online.sh`
  - Playwright probe on `http://127.0.0.1:8090` using `window.__mr_widget_probe()`
- [x] Save evidence in `.context/evidence/`.

### C - Complete
- [x] Docs updated in `docs/` and `.context/docs/feature-packs/` (if contract changed).
- [x] Run full gate `make ci`.
- [x] Plan status updated to `completed`.

## Acceptance Criteria
1. Candle layer hydrates historical + live data on web and native with active timeframe subjects (`aggregation.candle/.../<tf>`), verified by soak and Playwright probes.
2. getrange returns non-empty data for alias symbols when canonical history exists, verified by `go test ./internal/core/delivery/app`.
3. TF switch causes deterministic resubscribe for candles, heatmap, and VPVR with no stale subject leakage, verified by ack logs and widget probe counters.
4. UI indicator panels (`heatmap`, `vpvr`, `stats`) remain synchronized with candle timeframe and stream context after stream switching and lazy-load.

## Risks
| Risk | Mitigation |
|------|-----------|
| Subject taxonomy drift between adapters and backend | Centralize subject builder usage and add contract tests on subject strings. |
| Alias fallback masks data hygiene issues in persistence | Keep fallback one-shot and add metric/log counter for alias fallback hit-rate. |
| UI regressions from TF/stream switching | Add deterministic probe assertions in soak and Playwright smoke path. |
| Performance degradation under heavy event rates | Track queue drops/backlog and introduce frame budget + staged emit policy checks. |

## Data Compatibility Matrix
| Client Symbol Input | WS Subject | Range Store Canonical Lookup | Fallback Path |
|---|---|---|---|
| `BTCUSDT` | `aggregation.candle/binance/BTCUSDT/1m` | `BTCUSDT` | none |
| `BTCUSDT:SPOT` | `aggregation.candle/binance/BTCUSDT:SPOT/1m` | `BTCUSDT:SPOT` | one-shot fallback to `BTCUSDT` when empty |

## Review Notes
- Technical review completed in runtime validation loop (native soak + Playwright probe).
- Contract review artifacts updated in delivery WS and candle architecture docs.
