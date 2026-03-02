# Refactor Stage Close Checklist

Status: completed
Updated: 2026-03-02

## Mandatory Checklist

- [x] No URL params for operational config
- [x] No legacy connection bar / old Connection Settings wiring
- [x] No widget parsing raw messages
- [x] No widget IO calls
- [x] Ring buffers only on continuous data path
- [x] ParseArena and FrameArena ownership rules enforced
- [x] Protocol gate (`HELLO/proto_ver/capabilities`) active
- [x] Parser fails loudly on protocol mismatch (no silent fallback)
- [x] Orderbook snapshot-before-delta invariant enforced
- [x] Playwright E2E on `:8090` passed with screenshots/log evidence
- [x] 15 min soak evidence captured (native RSS budget)
- [x] wasm heap behavior documented (stable or bounded-growth mitigation)

## Evidence

- Docker up/build:
  - `make up PROCESSOR_REPLICAS=2` completed with all services healthy/started.
- Native soak (15 min):
  - `client/build/soak-mem-stage-close-20260302-v2/mem-contract-summary.txt`
  - `client/build/soak-mem-stage-close-20260302-v2/summary.txt`
  - Result: `PASS`, `rss_growth_kb=0`, `window_growth_kb=0`, budget `64MB total / 24MB sustained`.
- Playwright E2E:
  - Screenshots:
    - `client/build/e2e-8090-01-live-btc.png`
    - `client/build/e2e-8090-02-live-eth.png`
    - `client/build/e2e-8090-03-live-btc-return.png`
  - Console logs:
    - `client/build/e2e-8090-console.log`
  - Runtime checks:
    - HELLO gate accepted (`hello_ok`)
    - stream migration BTC -> ETH -> BTC without crash
    - widgets updated (`trades/stats/candles/heatmap/vpvr` probes advanced)
    - orderbook coherent reason surfaced when unavailable (`DESYNC: snapshot stale (Resync)`).

## WASM Heap Note

- Direct browser heap telemetry is environment-dependent; this stage uses bounded stores/rings and arena resets in the hot path.
- During E2E run, probe counters remained bounded (`trades cap=256`, `stats cap=64`) with stable behavior across stream switches.
