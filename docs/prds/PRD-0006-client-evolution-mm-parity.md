# PRD-0006 — Client Evolution: MarketMonkey Feature Parity & Beyond

**Status:** Draft
**Date:** 2026-02-28
**Owner:** Client UI/UX
**Relates to:** [RFC-0014](../rfcs/RFC-0014-client-ui-interaction-architecture-marketmonkey-reference.md), [PRD-0003](PRD-0003-mm-backend-parity.md), [PRD-0004](PRD-0004-backend-evolution-production-hardening.md)

---

## Problem

The Market Raccoon client is a production-ready terminal with 8 widgets, 5 technical indicators, and 3 layout modes across ~15,947 LOC. However, gap analysis against the MarketMonkey reference reveals **6 critical feature gaps** that professional crypto traders rely on daily:

1. **No Footprint charts** — per-bar volume distribution with imbalance detection is the #1 tool for order flow analysis. MM has both standard footprint and delta footprint modes.
2. **No DOM (Depth of Market)** — the 6-column depth ladder with market fill tracking, VWAP/TWAP, and heatmap coloring is essential for scalpers. MM's DOM is its most sophisticated widget.
3. **No funding rate / liquidation chart overlays** — despite MR backend streaming this data, the client only shows current values in the stats widget. MM renders these as time-series chart layers.
4. **Incomplete drawing tools** — MR has horizontal lines only; MM has rectangles, color pickers, and persistent serialization across sessions.
5. **No per-layer settings UI** — MR offers toggle on/off; MM allows inline configuration (colormap, thresholds, imbalance ratio) per overlay by clicking the layer name.
6. **No server-driven market discovery** — MR uses static config; MM receives available exchanges/symbols from the server at startup.

Meanwhile, MR already **exceeds MM** in 5 technical indicators (MA, BB, VWAP, RSI, MACD), compare mode, focus mode, web/WASM, synthetic fallback, sidebar controls, and 6-exchange breadth. The goal is to close the remaining gaps while preserving these advantages.

## Goals

**G1 — Order Flow Parity.** Deliver footprint charts (standard + delta) and DOM widget so traders have complete order flow analysis tools, matching MM's core strength.

**G2 — Chart Layer Completeness.** Add funding rate and liquidation layers as time-series sub-plots, using data already streamed by the backend.

**G3 — Annotation Parity.** Extend drawing tools with rectangles, color pickers, and persistent serialization so users can annotate charts across sessions.

**G4 — Configuration Depth.** Add inline per-layer settings UI so traders can tune overlay parameters (colormap, thresholds, periods) without leaving the chart context.

**G5 — Dynamic Market Discovery.** Enable server-driven exchange/symbol lists so the client auto-discovers available markets at startup.

## Non-Goals

- **Backend protocol changes.** No new WS message types or CBOR encoding (deferred to PRD-0004).
- **Custom indicator scripting.** No user-defined formulas or Pine-like language.
- **Backtesting framework.** No historical replay or strategy simulation.
- **Alerts / notifications.** Deferred to future PRD.
- **Multi-monitor / viewport detachment.** MR's grid system is sufficient; ImGui docking not adopted.

## Requirements

### Functional Requirements

| ID | Requirement | Priority | Acceptance Criteria |
|----|-------------|----------|---------------------|
| **FR-1** | Funding rate chart layer as sub-plot below candle chart | P0 | Line chart rendering from `stats.funding` per snapshot; toggleable via `H` key and sidebar |
| **FR-2** | Liquidation volume chart layer as sub-plot | P0 | Stacked buy/sell bars from `stats.liq_v_buy/liq_v_sell`; same toggle mechanism as FR-1 |
| **FR-3** | Drawing tool: rectangles (drag-to-define price/time zones) | P0 | Click+drag on chart creates zone; draggable corners; delete key removes |
| **FR-4** | Drawing tool: color picker per annotation | P0 | Inline color selector on click-select of any draw tool |
| **FR-5** | Drawing tool persistence to settings | P0 | Tools survive app restart via settings_store serialization |
| **FR-6** | DOM widget with 6-column depth ladder | P0 | Market buys, bids, price, asks, sells, extra columns; live orderbook + trade data |
| **FR-7** | DOM heatmap coloring (Viridis gradient for bid/ask sizes) | P1 | Normalized volume → color intensity per row |
| **FR-8** | DOM VWAP/TWAP indicators | P1 | Calculated from trade stream; displayed as price annotations in extra column |
| **FR-9** | DOM dynamic price grid recentering | P0 | Grid recenters when price moves beyond ±2 grouping thresholds from center |
| **FR-10** | DOM market buy/sell fill tracking | P1 | Accumulate trade flow per price level; display as volume bars |
| **FR-11** | Footprint chart type (standard) | P0 | Per-candle buy/sell volume bars at each price level; adaptive binning |
| **FR-12** | Footprint chart type (delta) | P0 | Delta = buy - sell per bin; magnitude-normalized bars; color by direction |
| **FR-13** | Footprint imbalance detection | P1 | Highlight bins where buy/sell exceeds neighbor by configurable ratio |
| **FR-14** | Footprint POC (Point of Control) marking | P1 | Outline rect on highest-volume bin per candle |
| **FR-15** | Per-layer inline settings UI | P1 | Click layer name in sidebar → expand config (period, sigma, colormap, thresholds) |
| **FR-16** | High/Low price labels on candle chart | P1 | Leader lines from highest/lowest visible candle to price axis labels |
| **FR-17** | Loading state indicators per widget | P2 | Visual feedback when fetching historical data (GetRange, initial subscription) |
| **FR-18** | Server-driven market discovery endpoint | P2 | HTTP GET `/api/v1/markets` returns available exchanges + symbols + tick sizes |
| **FR-19** | Dynamic exchange/symbol menu from discovery | P2 | Replace static config with server-provided market list in stream picker |

### Non-Functional Requirements

| ID | Requirement | Metric |
|----|-------------|--------|
| **NF-1** | No frame budget regression from new layers | Candle chart with all overlays + footprint renders < 8ms at 60 FPS |
| **NF-2** | DOM update latency | < 1 frame (16ms) from orderbook event to rendered grid |
| **NF-3** | Drawing tools serialization roundtrip | Save + load 50 annotations in < 5ms |
| **NF-4** | Zero allocation in steady-state rendering | All new widgets use ring buffers or pre-allocated arrays |
| **NF-5** | Web/native parity for all new features | Every FR works identically in GLFW native and WASM web |

## Milestones

### M1 — Chart Overlay Layers: Funding Rate + Liquidation (P0)

**Rationale:** Lowest complexity, highest immediate value. Data already arrives via `stats` stream. MM implementation is ~150 LOC total. Unblocks trader analysis workflows that currently require mental calculation from stats widget.

**Deliverables:**
- `client/src/core/widgets/indicator_funding.odin` — Funding rate line sub-plot
- `client/src/core/widgets/indicator_liq.odin` — Liquidation stacked bar sub-plot
- Updates to `app/actions.odin` — `H` key for funding rate toggle, `J` key for liquidation toggle
- Updates to `app/settings.odin` — `show_funding`, `show_liq` persistence
- Updates to `app/build_ui.odin` — sidebar layer toggles for new layers
- Updates to `core/ui/sidebar.odin` — 2 new layer toggles

**Data Source:** `Stats_Frame.funding`, `Stats_Frame.liq_v_buy`, `Stats_Frame.liq_v_sell` — already parsed in `message_parser.odin`.

**Gate:**
```bash
make -C client check-core
make -C client build-native
make -C client build-wasm
make -C client check-widgets
```

**Acceptance:**
- FR-1, FR-2 validated
- Funding rate line renders below candle chart with auto-scaled Y-axis
- Liq bars render as stacked green/red bars with auto-scaled Y-axis
- Both toggle on/off independently via keyboard + sidebar
- Settings persist across restart
- No frame budget regression (NF-1)

---

### M2 — Drawing Tools v2: Rectangles, Color Picker, Persistence (P0)

**Rationale:** Annotations are the #1 interaction pattern for technical traders. Current horizontal lines lack color customization and don't persist. Rectangles enable zone marking (support/resistance, order blocks). Low complexity (~200 LOC new code).

**Deliverables:**
- Updates to `client/src/core/widgets/chart_draw_tools.odin`:
  - `Draw_Rectangle` struct (x1,y1,x2,y2 in candle-index + price coords)
  - Drag-to-create interaction (mouse down → drag → release)
  - Corner drag for resize
  - Color picker on selection (rendered as inline 6-color palette)
  - Delete key removes selected tool
- Updates to `client/src/core/services/settings_store.odin`:
  - `draw_tools_json` key with serialized tool array
  - Load on startup, save on every mutation
- Updates to `client/src/core/widgets/candle_widget.odin`:
  - Render rectangles as semi-transparent filled rects
  - Render horizontal lines with user-chosen color

**Gate:**
```bash
make -C client check-core
make -C client build-native
make -C client build-wasm
```

**Acceptance:**
- FR-3, FR-4, FR-5 validated
- Double-click still creates horizontal line; Shift+drag creates rectangle
- Click any tool → color palette appears; select color → applied immediately
- All tools survive app restart (native: file, web: localStorage)
- 50 annotations render without frame drop (NF-3)

---

### M3 — DOM Widget: Depth of Market (P0)

**Rationale:** DOM is the most important scalper tool missing from MR. It displays the full order book as a price ladder with real-time market flow. MM's DOM at ~370 LOC is their most sophisticated widget. MR already receives all required data (orderbook + trades).

**Deliverables:**
- `client/src/core/widgets/dom_widget.odin` (~400 LOC estimated):
  - 6-column table: [Market Buys | Bids | Price | Asks | Market Sells | Extra]
  - Volume aggregation by configurable price grouping (5 presets like orderbook)
  - Dynamic price grid with auto-recentering (±2 groups from center)
  - Viridis heatmap coloring for bid/ask volume intensity
  - Market buy/sell fill tracking from trade stream
  - VWAP/TWAP calculation and display in extra column
- `client/src/core/services/dom_store.odin` (~150 LOC estimated):
  - Ring buffer for trade fills per price level
  - VWAP/TWAP accumulator state
  - Price grid state (center, grouping, sorted levels)
- Updates to `app/build_ui.odin` — DOM as assignable widget type in cell context menu
- Updates to `app/app.odin` — `Widget_Type.DOM` enum variant
- Updates to `core/ui/grid.odin` — DOM in default Analysis layout preset

**Data Source:** Same orderbook and trade streams already subscribed. No new backend requirements.

**Gate:**
```bash
make -C client check-core
make -C client build-native
make -C client build-wasm
make -C client check-widgets
```

**Acceptance:**
- FR-6, FR-7, FR-8, FR-9, FR-10 validated
- DOM renders live orderbook depth with real-time trade fills
- Price grid recenters smoothly when price moves
- Heatmap coloring correctly normalizes volume → intensity
- VWAP/TWAP update per trade and display in extra column
- DOM usable in any grid cell via context menu
- No allocation in steady-state (NF-4)

---

### M4 — Footprint Charts: Standard + Delta (P0)

**Rationale:** Footprint is the defining feature for professional order flow traders. It shows intra-bar volume distribution — where buyers and sellers were active within each candle. This is the most complex milestone (~350 LOC, nested rendering loops, imbalance detection).

**Data Strategy:** MR backend does not currently stream per-level volume breakdowns per candle. Two options:
- **Option A (Client-side):** Accumulate trade-by-trade data into per-candle price buckets on the client. Trades already arrive in real-time. Store as `Footprint_Store` ring buffer alongside `Candle_Store`.
- **Option B (Backend):** Add a new `aggregation.footprint` stream with pre-bucketed data per candle interval. Deferred to PRD-0007 if client-side proves insufficient.

**Recommended: Option A** — client-side accumulation from trade stream. This avoids backend changes and is consistent with MR's synthetic fallback pattern.

**Deliverables:**
- `client/src/core/services/footprint_store.odin` (~200 LOC):
  - Per-candle volume accumulator: `[price_level] → {buy_vol, sell_vol}`
  - Ring buffer of footprint entries aligned to candle windows
  - Adaptive bin size calculation (≥5px per level, respecting tick size)
  - Push from trade stream, keyed by `window_start_ts`
- `client/src/core/widgets/chart_footprint.odin` (~350 LOC):
  - Standard footprint: side-by-side buy/sell bars per price level per candle
  - Delta footprint: single bar per level, magnitude = |buy - sell|, color by direction
  - Imbalance detection: highlight when buy/sell exceeds neighbor by configurable ratio
  - POC marking: outline rect on highest-volume bin per candle
  - Text labels when zoom level permits (bin height > 14px)
- Updates to `candle_widget.odin` — footprint as chart type option (via `Chart_Type` enum)
- Updates to `app/settings.odin` — `chart_type` persistence (candlestick/line/heiken_ashi/footprint/footprint_delta)

**Gate:**
```bash
make -C client check-core
make -C client build-native
make -C client build-wasm
make -C client check-widgets
```

**Acceptance:**
- FR-11, FR-12, FR-13, FR-14 validated
- Footprint renders per-level buy/sell volumes inside each candle bar
- Delta mode shows net direction with magnitude
- Imbalances highlighted with configurable ratio (default 3.0x)
- POC outlined on highest-volume level per candle
- Text labels appear when zoomed in sufficiently
- Frame budget < 8ms with 60 visible footprint candles (NF-1)

---

### M5 — Per-Layer Settings UI + Chart Polish (P1)

**Rationale:** Professional traders need to tune indicator parameters without navigating to a separate settings page. High/low labels and loading states are expected polish items.

**Deliverables:**
- Updates to `core/ui/sidebar.odin`:
  - Click layer name → expand inline settings panel per layer
  - MA: period input (number), type selector (EMA/SMA)
  - BB: period, sigma inputs
  - RSI: period input, overbought/oversold thresholds
  - MACD: fast/slow/signal period inputs
  - Heatmap: intensity preset + colormap selector
  - VPVR: bin size override
  - Footprint: imbalance ratio input
- `client/src/core/widgets/chart_labels.odin` (~80 LOC):
  - High/low price labels with leader lines on candle chart
  - Auto-positioned to avoid overlap with current price line
- Updates to widget rendering:
  - Loading spinner/badge when `is_fetching` flag set per stream view
  - Skeleton state for empty widgets pre-data

**Gate:**
```bash
make -C client check-core
make -C client build-native
make -C client build-wasm
```

**Acceptance:**
- FR-15, FR-16, FR-17 validated
- Inline settings expand/collapse per layer in sidebar
- Changed parameters apply immediately to chart rendering
- Settings persist via settings_store
- High/low labels render correctly with leader lines
- Loading indicators visible during initial data fetch

---

### M6 — Market Discovery (P2)

**Rationale:** Dynamic market lists eliminate hardcoded configs and enable the client to auto-adapt when new exchanges or symbols are added to the backend.

**Deliverables:**
- Backend: `GET /api/v1/markets` endpoint in `internal/interfaces/http/`:
  - Returns `{exchanges: [{name, symbols: [{ticker, tick_size, market_type}]}]}`
  - Populated from server config at startup
- Client: `client/src/core/services/market_discovery.odin`:
  - HTTP fetch on startup (native: HTTP client, web: fetch API)
  - Cached market list with fallback to static config
- Updates to stream picker (`app/build_ui.odin`):
  - Render discovered exchanges → symbols in picker overlay
  - Per-symbol tick size used for orderbook grouping and DOM grid

**Gate:**
```bash
make -C client check-core
make -C client build-native
make -C client build-wasm
make test MODULE=./internal/interfaces
```

**Acceptance:**
- FR-18, FR-19 validated
- Client fetches market list on startup; falls back to static config on failure
- Stream picker shows all available exchanges and symbols
- New symbols added to backend appear in client without client rebuild

---

## Execution Sequence

```
M1 (Funding/Liq layers) ──→ M2 (Drawing Tools v2) ──→ M3 (DOM Widget) ──→ M4 (Footprint) ──→ M5 (Layer Settings) ──→ M6 (Market Discovery)
     ~200 LOC                   ~250 LOC                  ~550 LOC            ~550 LOC           ~300 LOC               ~200 LOC + backend
```

Total estimated: **~2,050 client LOC** + ~150 backend LOC

Dependencies:
- M1-M4 are independent of backend changes (client-only)
- M5 depends on M1+M4 being complete (layers must exist to configure)
- M6 requires a small backend endpoint

## Risk Register

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Footprint client-side accumulation exceeds memory budget | High | Low | Ring buffer cap (750 candles × 50 levels = 37,500 entries); trade buffer already bounded |
| DOM recentering flicker on volatile markets | Medium | Medium | Hysteresis: require ≥2 consecutive frames beyond threshold before recenter |
| Drawing tools serialization format changes break saved data | Medium | Low | Version field in serialized JSON; migration on load |
| Per-layer settings increase sidebar complexity beyond usable | Medium | Medium | Collapse by default; expand only on click; max 4 params per layer |
| Footprint rendering exceeds frame budget on large zoom-out | High | Medium | Cull off-screen levels; skip text labels at low zoom; batch draw calls |

## Success Criteria

1. **All P0 requirements (FR-1 through FR-14) pass acceptance gates**
2. **Native + WASM parity** for every new feature (NF-5)
3. **Zero frame budget regression** (NF-1: < 8ms with full overlay stack)
4. **Zero new TODO/FIXME/HACK markers** in codebase
5. **Settings persistence roundtrip** for all new features
6. **Soak test**: 30-minute session with all overlays active, DOM running, footprint on — no leak, no crash, no stale data

## Changelog

- 2026-02-28: PRD created from comprehensive gap analysis (MR client 15,947 LOC vs MM reference)
