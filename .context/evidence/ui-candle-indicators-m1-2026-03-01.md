# UI Candle and Indicator Layer Hardening (M1) - Evidence

Date: 2026-03-01

## Build and Runtime
- `make -C client build` -> PASS (native + wasm built, `web/app.wasm` updated).
- `make up PROCESSOR_REPLICAS=2` -> PASS (compose healthy, client served on `:8090`).

## Automated UI Soak (native online probe)
- Command:
  - `WS_URL=ws://127.0.0.1:8090/ws SOAK_SECONDS=10 SOAK_LOG_MS=500 MIN_WIDGET_COUNT=1 bash client/scripts/check-widgets-online.sh`
- Result: PASS
- Coverage sample:
  - `w[t=256 ob=40/35 st=64 hm=1 vp=75 c=18]`
- Health status:
  - `health=OK`

## Browser Validation (Playwright on :8090)
- URL: `http://127.0.0.1:8090`
- WebSocket ACK includes candle timeframe subject:
  - `aggregation.candle/binance/BTCUSDT:SPOT/1m`
- Probe output (`window.__mr_widget_probe()`):
  - `t=231`
  - `obA=50`, `obB=50`
  - `st=64`
  - `hm=1`
  - `vp=100`
  - `c=18`

## Backend and Metrics Tests
- `go test ./internal/core/delivery/app` -> PASS
- `go test ./internal/shared/metrics` -> PASS

## Quality Gates
- `make test-short` -> PASS
- `make invariants-check` -> PASS
- `make fmt && make lint` -> PASS
- `make ci` -> PASS (`CI_EXIT_CODE=0`)
- `make docs-check` -> PASS

## Documentation Updates
- Updated WS delivery contract for candle/getrange semantics:
  - `to_ms` as canonical range upper bound
  - `end_ts` backward compatibility
  - alias fallback (`SYMBOL:MARKET_TYPE` -> `SYMBOL`)
- Updated candle architecture and feature-pack docs to reflect range alias compatibility and fallback behavior.
