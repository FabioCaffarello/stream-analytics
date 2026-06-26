---
description: Odin Cockpit tour — 13 widget types, 8 technical indicators, workspace split-tree, and the 5-layer stream health pipeline.
---

# Cockpit — Odin Client

The Stream Analytics cockpit is a cross-platform market data terminal written in
[Odin](https://odin-lang.org/), compiled to both WASM (browser) and native binaries (GLFW/SDL2).
It connects to the backend server via the Terminal_V1 WebSocket protocol and renders all output
directly to a `<canvas>` element — no DOM widgets.

![Odin Cockpit — full layout](../assets/showcase/screenshots/client-cockpit-full.png)

!!! info "Odin Language"

    The cockpit is built with Odin (`dev-2026-02`), a separate toolchain from the Go backend.
    The WASM build is compiled with `make -C client build-wasm` and served via nginx on `:8090`.
    No Go tooling applies to the client — it has its own strict DAG: `ports → services → layers → app`.

---

## Widget Types

The cockpit supports 13 widget types, each rendering to a canvas region:

| Widget | Description |
|--------|-------------|
| **Candle Chart** | OHLCV candlestick chart across 9 selectable timeframes |
| **Orderbook** | Live depth-of-market bid/ask ladder with size visualisation |
| **Tape (Trades)** | Real-time trade tape with side, price, and size |
| **Heatmap** | Price-level volume distribution map over a rolling window |
| **VPVR** | Volume Profile Volume Rate — per-level buy/sell volume histogram |
| **Stats** | Funding rate, open interest, mark price, liquidation counters |
| **Liquidations** | Real-time and aggregated liquidation events by side |
| **Mark Price** | Live mark price with deviation from last trade |
| **Funding Rate** | Current and predicted funding rate |
| **DOM** | Depth of Market — cumulative bid/ask volume at each level |
| **Evidence** | Liquidity Evidence Layer detections (wall, sweep, stack, …) |
| **Compare** | Side-by-side multi-instrument comparison pane |
| **Split Tree** | Workspace split-tree management widget |

---

## Technical Indicators

Eight technical indicators can be overlaid on the candle chart:

| Indicator | Description |
|-----------|-------------|
| **RSI** | Relative Strength Index |
| **MACD** | Moving Average Convergence Divergence |
| **Funding Rate** | Exchange-reported funding rate overlay |
| **Liquidation Counter** | Cumulative liquidation volume indicator |
| **Trade Counter** | Trade-count delta per candle |
| **CVD** | Cumulative Volume Delta (buy minus sell) |
| **VWAP** | Volume-Weighted Average Price |
| **OI** | Open Interest overlay |

---

## Stream Health Pipeline

Every active data stream passes through a 5-layer health pipeline. Latency is tracked at each
layer via WASM probe exports (`probe_*`) accessible from Playwright tests and the browser console:

```
Transport  →  Parse  →  Apply  →  Render  →  Display
   (WS)      (CMM)    (state)   (canvas)    (RAF)
```

| Layer | Probe | Budget |
|-------|-------|--------|
| Transport | `probe_md_transport_mode` | N/A (mode flag) |
| Parse | `probe_md_parse_time_p95_us` | < 50 µs p95 |
| Apply | `probe_md_apply_time_p95_us` | < 100 µs p95 |
| Render | `probe_widget_stats_render_p95_us` | < 2 ms p95 |
| Display | `probe_active_live_*` | frame-budgeted |

Probe values are exported from Odin WASM at runtime and available via `window.__mr_wasm_exports`.

---

## Workspace Split-Tree

Workspaces are modelled as a binary split-tree. Each node is either a split container (horizontal
or vertical) or a leaf pane hosting a widget. Workspace state is persisted to `localStorage` with
schema versioning — migrations run automatically on load if `probe_layout_migrated` returns 1.

---

## Client Session Protocol

The cockpit uses the Terminal_V1 protocol over WebSocket:

{% include "../architecture/diagrams/sequence-client-session.md" %}

---

![Cockpit after data seeding](../assets/showcase/screenshots/client-seeded.png)
