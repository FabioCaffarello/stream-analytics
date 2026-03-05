# M4 Throughput & Drop Baseline (2026-03-05)

## Objective
Establish initial throughput/drop baseline after M3 connectivity fixes, using online soak and Playwright stress scenarios.

## Commands Executed
- `make -C client check-wasm-compile`
- `make -C client check-core`
- `make -C client check-widgets-online`
- `SOAK_MULTI=1 make -C client check-widgets-online`
- `npx --prefix tests/playwright playwright test tests/playwright/e2e/stress.spec.ts`

## Results
- `check-wasm-compile`: PASS
- `check-core`: PASS
- `check-widgets-online`: PASS
- `check-widgets-online (SOAK_MULTI=1)`: PASS
- `stress.spec.ts`: 3/3 PASS

### Last soak coverage sample (`SOAK_MULTI=1`)
- `conn=Connected`
- `health=OK`
- `streams=3`
- `drop=0`
- `w[t=24 ob=50/50 st=0 hm=0 vp=0 c=17]`

## Notes
- Heatmap/VPVR remain zero in this local profile and are treated as optional in the default online gate.
- `check-widgets-online` now emits explicit `NOTE` lines when optional channels are zero, to keep visibility without failing liveness gate.
- Trade/orderbook/candle liveness and connection stability passed under default and multi-sub soak.
- M4 instrumentation applied:
  - Status panel now exposes `M4 BUDGETS` with explicit drop-rate budget (`20%`) and render-over-budget alerting.
  - Copy Diagnostics now includes `M4 BUDGETS` and policy skip counters.
  - `drain_marketdata` now applies explicit backpressure policy by event type:
    - keep critical events (`trade/orderbook/candle/range/signal/stats/tape`);
    - degrade `heatmap/vpvr` only when assist policy is active;
    - drop `evidence` at critical pressure (`bp>=3`).
- Cacheless runtime validation via Playwright MCP on `http://127.0.0.1:8090`:
  - cookies/localStorage/sessionStorage/CacheStorage/IndexedDB cleaned before load.
  - network cache explicitly disabled (`Network.setCacheDisabled`).
  - canvas detected (`hasCanvas=true`).

## Next Action
Proceed with M5 decomposition/legacy-exit while keeping M4 budget alerts active during soak regressions.
