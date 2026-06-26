# Stream Analytics Client — Technical Roadmap (Phases 6.8 → 8.0)

> **Owner:** Client Architecture Team
> **Created:** 2026-02-21
> **Status:** APPROVED — ready for execution
> **Branch:** `feat/client-odin-start`

## Strategic Vision

The MR Odin client has an **A+ architecture** (hexagonal, RCL-based, zero-alloc stores) but is at **~55% feature parity** with MarketMonkey's client. The core is pure and portable — the gaps are in platform wiring (web 40%), chart rendering (no candlestick), input handling (no drag/zoom), and layout management (hardcoded positions).

**Competitive thesis:** MR's architecture is *structurally superior* to MM's tightly-coupled ImGui/ImPlot approach. The RCL abstraction enables web/native from a single codebase — something MM cannot do. The roadmap below converts this architectural advantage into user-visible feature parity, then surpasses MM.

**Inviolable principles:**
- Core stays pure: zero imports from `platform/`, `deps/`, vendor. Enforced by `check-core-imports.sh`.
- Zero-alloc steady-state: all hot-path stores use fixed arrays or ring buffers.
- Deterministic rendering: same input → same RCL → bit-identical output hash.

---

## Track Legend

| Track | Code | Scope |
|-------|------|-------|
| Paridade Web/Native | **PAR** | web ↔ native convergence |
| Input/Interaction | **INP** | mouse, keyboard, drag, zoom |
| Dataflow Real | **DAT** | WebSocket, subscriptions, stores |
| Asset Pipeline | **AST** | fonts, DPI, themes, icons |
| Observabilidade/Perf | **OBS** | metrics, culling, profiling |

---

## Phase Overview

```
6.8  RCL Golden Render Spec + Hash Gate          [PAR]
6.9  Web Canvas2D Feature Complete               [PAR]
7.0  Input Unification + rAF Loop                [INP][PAR]
7.1  WebSocket Bridge + Subscription Manager     [DAT][PAR]
7.2  Candlestick Chart Core                      [INP][DAT]
7.3  Layout Engine + Widget Orchestrator          [PAR][INP]
7.4  Chart Overlays (Heatmap/VPVR/Volume)        [DAT]
7.5  Asset Pipeline (Fonts/DPI/Theme)             [AST][PAR]
7.6  Drawing Tools + State Persistence            [INP][DAT]
7.7  Visible-Range Culling + Frame Budget         [OBS]
7.8  Stats/Funding/Liquidations Layers            [DAT]
7.9  Menu System + Config UI                      [INP][AST]
8.0  MM Competitive Parity Gate                   [ALL]
```

---

## Dependency Graph

```
6.8 ──→ 6.9 ──→ 7.0 ──→ 7.1 ──→ 7.2 ──→ 7.3 ──→ 7.9 ──→ 8.0
                  │              ↗   ↘
                  └──────────────     7.4 ──→ 7.7
                                       ↓        ↑
                                      7.8 ───────┘
                        7.5 (parallel after 7.3)
                        7.6 (parallel after 7.2 + 7.0)
```

**Critical path:** 6.8 → 6.9 → 7.0 → 7.1 → 7.2 → 7.3 → 7.9 → 8.0

**Parallelizable:**
- PR 7.5 (fonts/DPI/theme) can run in parallel with 7.4–7.7.
- PR 7.6 (drawing tools) can run in parallel with 7.3–7.4.
- PR 7.7 (culling) can start after 7.4 while 7.8 is in progress.

---

## Current Baseline (2026-02-21)

| Metric | Value |
|--------|-------|
| Total Odin files | 45 |
| Total LOC | 4,053 |
| Core LOC | ~2,100 (52%) |
| Native LOC | ~1,300 (32%) |
| Web LOC | ~200 (5%) |
| Widgets | 6 (hello, trade_counter, trades, orderbook, heatmap, vpvr) |
| Services | 5 (trades, orderbook, heatmap, vpvr, settings) |
| Ports | 5 (input, text, font, marketdata, settings) |
| RCL Commands | 6 (clear, rect, line, text, clip_push, clip_pop) |
| Code debt markers | 0 (zero TODO/FIXME/HACK) |
| Native completeness | 95% |
| Web completeness | 40% |
| Architecture grade | A+ |

---

## PR 6.8 — RCL Golden Render Spec + Hash Gate

- **Track:** PAR
- **Status:** PENDING
- **Dependencies:** None (self-contained, touches only core/)

### Objective

Establish a **machine-verifiable contract** that guarantees bit-identical RCL output across web and native platforms. Every widget, given identical input, must produce the exact same `Command_Buffer` — differences in rendering are isolated to platform renderers, never to core logic.

This is the *foundation* of all subsequent parity work. Without it, every future PR would need manual visual comparison across platforms.

### Architecture

```
core/ui/commands.odin          ──┐
core/ui/rcl_hash.odin (NEW)    ──┤  Golden Spec
core/ui/rcl_golden_test.odin   ──┘
         │
         ▼
   FNV-1a hash of Command_Buffer
         │
    ┌────┴────┐
    │         │
  native    web
  renderer  renderer
    │         │
    ▼         ▼
  pixels    pixels   (may differ: font rasterization, AA)
```

### Deliverables

| # | File | Description |
|---|------|-------------|
| 1 | `src/core/ui/rcl_hash.odin` | `hash_buffer(buf: ^Command_Buffer) -> u64` — FNV-1a over all commands + text arena. Deterministic, zero-alloc. |
| 2 | `src/core/ui/rcl_golden_test.odin` | Golden-hash tests for each widget: feed known store data → emit RCL → assert hash matches. One test per widget. |
| 3 | `src/core/ui/rcl_snapshot.odin` | `snapshot_buffer(buf: ^Command_Buffer) -> string` — human-readable dump (for debugging mismatches). Allocated on temp_allocator. |
| 4 | `scripts/check-rcl-golden.sh` | CI script: builds core as `js_wasm32` target, runs golden tests, compares hashes against native. Fails if divergence. |
| 5 | `Makefile` | New target: `check-golden` — runs `check-rcl-golden.sh`. Added to `make ci`. |
| 6 | `src/core/ui/commands.odin` | Minor: add `Eq` semantics for `Command` union (needed for hash verification). |

### Hash Function Specification

```
hash_buffer(buf):
  h = FNV_OFFSET_64
  for cmd in buf.commands:
    h = fnv1a(h, cmd_tag_byte)      // 1 byte: discriminator
    h = fnv1a(h, cmd_fields_bytes)  // field-by-field, IEEE754 f32 as raw bytes
    if cmd is Cmd_Text:
      h = fnv1a(h, text_arena[off:off+len])  // text content, NOT pointer
  return h
```

**Why FNV-1a:** Already proven in shared/hash (zero-alloc, fast, good distribution). Consistent with backend's `HashFieldsFast`.

### Gates de Validação

| Gate | Criterion | Automation |
|------|-----------|------------|
| G1 — Compilation | `check-core` passes (core compiles standalone for both `native` and `js_wasm32`) | `make check-core` |
| G2 — Import Purity | `check-core-imports.sh` passes (rcl_hash imports nothing from platform) | `make check-core` |
| G3 — Hash Stability | All 6 widget golden tests pass with pinned demo data | `make check-golden` |
| G4 — Cross-Target Parity | Native hash == WASM hash for all golden tests | `scripts/check-rcl-golden.sh` |
| G5 — Zero Regression | Existing `make build` (native + wasm) still passes | CI |

### Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| f32 non-determinism across targets | Medium | Hash raw IEEE754 bytes; Odin guarantees same f32 layout. Avoid platform-dependent rounding in core. |
| Text interning order dependency | Low | Hash text by content (arena bytes), not by offset. |
| Demo data drift | Low | Pin LCG seeds in golden tests (already deterministic). |

### Success Criteria

- `make check-golden` passes in CI.
- Any future core change that accidentally alters widget output is caught by hash regression.
- Developers can add new widgets by adding one golden test — the hash gate covers parity automatically.

---

## PR 6.9 — Web Canvas2D Feature Complete

- **Track:** PAR
- **Status:** PENDING
- **Dependencies:** PR 6.8

### Scope

Complete the 6 missing Canvas2D foreign procs so **all 6 RCL commands** render on web.

### Deliverables

| File | Change |
|------|--------|
| `web/odin.js` | Add: `canvas_stroke_line(x1,y1,x2,y2,r,g,b,a,thickness)`, `canvas_clip_push(x,y,w,h)`, `canvas_clip_pop()` |
| `src/platform/web/renderer_canvas2d.odin` | Wire `Cmd_Line` → `canvas_stroke_line`, `Cmd_Clip_Push` → `canvas_clip_push`, `Cmd_Clip_Pop` → `canvas_clip_pop` |

### Impact

- VPVR POC/VA lines render on web.
- Scroll-clipped widgets (trades, orderbook) clip correctly.
- Heatmap grid lines render.

### Gates

- All 6 command types render on web (visual inspection + screenshot diff).
- `make build-wasm && make check-wasm` passes.
- Golden hashes unchanged (core untouched).

### Risks

- Canvas2D `save()/restore()` stack depth could overflow if clip nesting is deep (mitigate: max 8 nesting levels, assert in debug).

---

## PR 7.0 — Input Unification + rAF Loop

- **Track:** INP, PAR
- **Status:** PENDING
- **Dependencies:** PR 6.9

### Scope

1. **Web animation loop:** Replace one-shot `main()` with `requestAnimationFrame` loop calling exported `update_frame()`.
2. **Web input wiring:** DOM events → `Input_State` struct via foreign callbacks.
3. **Input port normalization:** Ensure `Input_State` semantics are identical across platforms.

### Deliverables

| File | Change |
|------|--------|
| `web/odin.js` | `requestAnimationFrame` loop; DOM listeners for `mousemove`, `mousedown`, `mouseup`, `wheel`, `keydown`, `keyup` |
| `src/platform/web/main.odin` | Export `@(export) update_frame :: proc()` instead of running in `main()` |
| `src/platform/web/input_web.odin` | Foreign procs: `get_mouse_x/y/buttons`, `get_scroll_delta`, `get_key_state` → build `Input_State` |
| `src/core/ports/input.odin` | Add `delta_time: f32` to `Input_State` (needed for smooth scroll/zoom) |

### Impact — Architectural

- Web becomes interactive (currently renders one frame and stops).
- Opens door for all subsequent input-dependent features (scroll, drag, zoom).
- `delta_time` enables frame-rate-independent animations.

### Gates

- Web renders at 60fps (rAF loop).
- Mouse hover highlights work in web (same as native).
- Scroll works in trades_widget on web.
- Golden hashes unchanged (Input_State is external to RCL hashing).

### Risks

- WASM memory model: DOM event data must be marshaled to linear memory (no JS objects crossing boundary). Mitigate: use scalar foreign procs (f32/i32 only).
- Frame timing: `performance.now()` vs Odin `time.tick_now()` — use JS-side timing only.

---

## PR 7.1 — WebSocket Bridge + Subscription Manager

- **Track:** DAT, PAR
- **Status:** PENDING
- **Dependencies:** PR 7.0

### Scope

1. **Web WebSocket client:** JS `WebSocket` → event queue → Odin `Marketdata_Port`.
2. **Subscription manager (core):** Typed channels with subscribe/unsubscribe, timeframe-aware.
3. **Reconnection logic:** Exponential backoff (mirror native: 500ms → 30s, 2x).

### Deliverables

| File | Change |
|------|--------|
| `web/odin.js` | `WebSocket` wrapper: connect, send, recv queue (ring buffer in JS, polled by Odin) |
| `src/platform/web/marketdata_web.odin` (NEW) | `make_web_marketdata_port()` — polls JS event queue via foreign procs |
| `src/core/services/subscription_manager.odin` (NEW) | `Subscription_Manager` — tracks active channels, timeframes, deduplicates |
| `src/core/ports/marketdata.odin` | Add `subscribe(channel, pair, tf)`, `unsubscribe(channel, pair, tf)` to port |

### Impact

- Web receives live market data (trades, orderbook, heatmaps, VPVR).
- Subscription manager enables future timeframe switching without full reconnect.
- Native marketdata_native.odin refactored to use same subscription interface.

### Gates

- Web connects to MR server, receives trades, updates trades_widget live.
- Reconnection works (kill server → web shows RECONNECTING → auto-recovers).
- Native still works (regression test).
- Subscription deduplication: 2 widgets subscribing to same channel = 1 WS subscription.

### Risks

- JS WebSocket binary mode vs text mode (MR server uses JSON envelopes — text mode). Verify protocol compatibility.
- Ring buffer overflow if Odin polls too slowly (mitigate: drop oldest, counter metric).

---

## PR 7.2 — Candlestick Chart Core

- **Track:** INP, DAT
- **Status:** PENDING
- **Dependencies:** PR 7.0 (input), PR 7.1 (live candle data). Can start core widget without live data using demo candles.

### Scope

The single most impactful feature gap. Implement a pure-RCL candlestick chart with OHLC bars, volume underlay, and mouse-driven pan/zoom.

### Deliverables

| File | Change |
|------|--------|
| `src/core/services/candle_store.odin` (NEW) | Ring buffer (2048 candles), append/update, timeframe field |
| `src/core/widgets/candle_chart.odin` (NEW) | Pure RCL: OHLC bars, wicks, volume bars, price axis, time axis |
| `src/core/widgets/chart_viewport.odin` (NEW) | `Chart_Viewport` — manages visible range, pan offset, zoom level, pixel-to-price mapping |
| `src/core/model/model.odin` | Add `Candle` to model types if not present |
| `src/core/app/app.odin` | Wire candle_store, add candle_chart to update() |

### Key Design Decisions

- **Chart_Viewport** is a pure value type (no pointers, no state mutation beyond what update() receives). Enables deterministic golden-hash testing.
- **Pan/zoom** computed from Input_State deltas: `drag_delta.x → pan`, `scroll_y → zoom` (only when mouse is over chart rect).
- **Visible range:** Binary search on sorted candle timestamps (O(log n)).
- **Volume bars:** Rendered below candles in same viewport (bottom 20% of height), stacked buy/sell with separate colors.

### Impact — Architectural

- First "complex widget" — validates that RCL + pure core can handle real chart rendering.
- Chart_Viewport becomes the reusable foundation for all overlay widgets (7.4).
- Candle_Store becomes the data backbone for funding/liq/stats overlays.

### Gates

- Candlestick chart renders 500+ candles with correct OHLC proportions.
- Pan: drag left/right shifts visible range smoothly.
- Zoom: scroll in/out changes candle width, recomputes visible range.
- Golden hash test for candle_chart with 100 pinned candles.
- Web + native render identically (golden hash parity).

### Risks

- f32 precision for price axis at high zoom (BTC = $100k+ with $0.01 ticks). Mitigate: use f64 for price math in chart_viewport, convert to f32 only for RCL emission.
- Candle width < 1px at low zoom. Mitigate: min 1px width, skip bodies when bar_width < 3px (line-only mode, same as MM).

---

## PR 7.3 — Layout Engine + Widget Orchestrator

- **Track:** PAR, INP
- **Status:** PENDING
- **Dependencies:** PR 7.2 (candlestick chart must exist to be the center panel)

### Scope

Replace hardcoded widget positions with a flexible layout engine. Support docking-style panels (chart center, orderbook right, trades bottom-right, etc.).

### Deliverables

| File | Change |
|------|--------|
| `src/core/ui/layout_engine.odin` (NEW) | `Layout_Node` tree: split(h/v, ratio), leaf(widget_id). Recursive rect subdivision. |
| `src/core/ui/layout_presets.odin` (NEW) | Default layout: chart=center, orderbook=right, trades=bottom-right. Serializable to JSON. |
| `src/core/app/app.odin` | Replace hardcoded positions with `layout_engine.compute_rects(root, window_size)` |
| `src/core/services/settings_store.odin` | Add layout persistence (key = "layout", value = JSON) |

### Impact — Architectural

- Window resize works correctly (currently hardcoded to 800x600).
- Users can rearrange panels (future: drag borders to resize).
- Layout preset system enables "trading view" vs "orderbook focus" vs "analysis" modes.

### Gates

- Window resize redistributes widget rects proportionally.
- Default layout matches MM's typical arrangement.
- Layout persists across sessions (native JSON file, web localStorage).
- Golden hashes: layout engine is tested with fixed window size → deterministic rects.

### Risks

- Minimum size constraints: widgets need min_width/min_height to avoid degenerate rects. Mitigate: enforce 100px minimums.
- Layout tree depth: keep max 4 levels (chart layouts rarely need more).

---

## PR 7.4 — Chart Overlays (Heatmap/VPVR/Volume)

- **Track:** DAT
- **Status:** PENDING
- **Dependencies:** PR 7.2 (candlestick chart + Chart_Viewport), PR 7.1 (live heatmap/VPVR data)

### Scope

Render heatmap, VPVR, and volume as **overlays** on the candlestick chart (sharing Chart_Viewport), not as separate widgets.

### Deliverables

| File | Change |
|------|--------|
| `src/core/widgets/chart_layers.odin` (NEW) | `Chart_Layer` interface: `render(buf, viewport, store)`. Overlay/underlay flag. |
| `src/core/widgets/heatmap_overlay.odin` (NEW) | Heatmap rendered on chart coordinates (price x time grid), alpha-blended. |
| `src/core/widgets/vpvr_overlay.odin` (NEW) | VPVR rendered on visible price range (right side of chart), 20% max width. |
| `src/core/widgets/volume_underlay.odin` (NEW) | Volume bars below candles (reuse from candle_chart, extract as layer). |
| `src/core/widgets/candle_chart.odin` | Refactor to accept `[]Chart_Layer`, iterate and render in order. |

### Impact

- This is how MM renders analytics — as chart layers, not separate panels.
- Enables toggling overlays on/off via future menu system (PR 7.9).
- Heatmap on chart is the #1 feature traders use in MM.

### Gates

- Heatmap alpha-blends correctly over candles (not obscuring OHLC).
- VPVR profile aligns with visible price range (recalculates on pan/zoom).
- Volume bars scale independently from candle Y-axis.
- Overlays togglable via programmatic flag (UI toggle in PR 7.9).

### Risks

- Draw order: overlays must render after candles but before crosshair. Mitigate: explicit layer ordering (underlay → candles → overlay → UI).
- Performance: heatmap on chart = potentially 1000s of rects. Mitigate: cull to visible range (leverages PR 7.7 culling).

---

## PR 7.5 — Asset Pipeline (Fonts/DPI/Theme)

- **Track:** AST, PAR
- **Status:** PENDING
- **Dependencies:** PR 7.3 (layout engine must scale with DPI)

### Scope

1. **Font system:** Consistent font rendering across platforms (native: TTF atlas, web: CSS @font-face).
2. **DPI awareness:** Content scale detection → font size adjustment → layout scaling.
3. **Theme system:** Color palette as named tokens, swappable presets (dark, light).

### Deliverables

| File | Change |
|------|--------|
| `src/core/ui/theme.odin` (NEW) | `Theme` struct with named color tokens (bg, fg, bull, bear, grid, etc.). `default_dark_theme()`, `default_light_theme()`. |
| `src/core/ui/styles.odin` | Refactor constants to reference `Theme` tokens. |
| `src/core/ports/font.odin` | Ensure `push_font/pop_font` works for both platforms. |
| `src/platform/native/font_native.odin` | Call `GetWindowContentScale()` on init, wire to DPI-aware font sizes. |
| `src/platform/web/font_web.odin` | Detect `window.devicePixelRatio`, scale canvas, wire to font port. |
| `web/odin.js` | DPI-aware canvas sizing: `canvas.width = w * dpr; ctx.scale(dpr, dpr)`. |

### Gates

- Retina/HiDPI: text is crisp on 2x displays (native + web).
- Theme switch: calling `set_theme(light)` changes all widget colors in one frame.
- Font consistency: mono font renders identically on both platforms (within rasterization limits).

---

## PR 7.6 — Drawing Tools + State Persistence

- **Track:** INP, DAT
- **Status:** PENDING
- **Dependencies:** PR 7.2 (Chart_Viewport for coordinate mapping), PR 7.0 (input)

### Scope

Horizontal line and rectangle drawing tools (MM parity).

### Deliverables

| File | Change |
|------|--------|
| `src/core/model/drawing.odin` (NEW) | `Drawing` union: `HLine{price, color, thickness}`, `DrawRect{price_range, time_range, color}` |
| `src/core/services/drawing_store.odin` (NEW) | Fixed-capacity store (64 drawings), CRUD, serializable |
| `src/core/widgets/drawing_overlay.odin` (NEW) | Renders drawings on chart viewport. Hit-test for selection/drag. |
| `src/core/ports/input.odin` | Add `drag_state: Drag_Info` (start_pos, current_pos, active) |

### Gates

- Click on chart to place H-line at price.
- Drag H-line to move. Double-click to delete.
- Rectangle: click-drag to define corners.
- Drawings persist via settings store (save/load JSON).

---

## PR 7.7 — Visible-Range Culling + Frame Budget

- **Track:** OBS
- **Status:** PENDING
- **Dependencies:** PR 7.2 + 7.4 (chart + overlays must exist to cull)

### Scope

Performance optimization: only emit RCL commands for visible elements. Target: 10k candles + heatmap + VPVR at 60fps.

### Deliverables

| File | Change |
|------|--------|
| `src/core/widgets/chart_viewport.odin` | `find_visible_range(candles, viewport) -> (start, end)` — binary search, O(log n) |
| `src/core/ui/rcl_stats.odin` (NEW) | `Stats` struct: cmd_count, text_bytes, clip_depth. Populated per frame. |
| `src/core/widgets/heatmap_overlay.odin` | Cull heatmap cells outside visible price/time range |
| `src/core/widgets/vpvr_overlay.odin` | Cull VPVR buckets outside visible price range |
| `Makefile` | New target: `bench-render` — renders 10k candles, reports frame time + cmd count |

### Gates

- 10k candles: < 5ms core update time (RCL emission only, excluding renderer).
- Command count: < 2000 commands for typical chart view (vs unbounded before).
- No visual artifacts from culling (off-by-one at edges).

---

## PR 7.8 — Stats/Funding/Liquidations Layers

- **Track:** DAT
- **Status:** PENDING
- **Dependencies:** PR 7.4 (chart layer system), PR 7.1 (live data)

### Scope

Add remaining MM chart layers: funding rate underlay, liquidation bars, stats display.

### Deliverables

| File | Change |
|------|--------|
| `src/core/services/stats_store.odin` (NEW) | Ring buffer (512): funding rate, mark price, trade counts per TF |
| `src/core/widgets/funding_underlay.odin` (NEW) | Line chart of funding rate below candles (separate Y-axis) |
| `src/core/widgets/liquidation_underlay.odin` (NEW) | Stacked bars (buy/sell liq volume) |
| `src/core/widgets/stats_panel.odin` (NEW) | Mark price, funding, 24h volume, trade counts — text panel |

### Gates

- Funding rate renders as line with zero-line.
- Liquidation bars color-coded (green=buy, red=sell).
- Stats panel updates live from marketdata port.
- All layers togglable.

---

## PR 7.9 — Menu System + Config UI

- **Track:** INP, AST
- **Status:** PENDING
- **Dependencies:** PR 7.6 (drawing tools), PR 7.8 (layers to toggle), PR 7.1 (subscription manager for TF switch)

### Scope

Timeframe selector, chart type menu, indicator toggles, tool selection.

### Deliverables

| File | Change |
|------|--------|
| `src/core/ui/menu.odin` (NEW) | `Menu`, `Menu_Item`, `Dropdown` — pure RCL menu primitives |
| `src/core/widgets/toolbar.odin` (NEW) | Top bar: timeframe buttons, chart type dropdown, indicator toggles, tool selector |
| `src/core/app/app.odin` | Wire toolbar to app state (selected_tf, enabled_layers, active_tool) |

### Gates

- Timeframe switch: click "5m" → unsubscribes 1m, subscribes 5m, clears candle store.
- Indicator toggle: click "Heatmap" → heatmap overlay on/off.
- Tool select: click "H-Line" → next chart click places line.
- Menu renders on both web and native (RCL-based, no ImGui menus).

---

## PR 8.0 — MM Competitive Parity Gate

- **Track:** ALL
- **Status:** PENDING
- **Dependencies:** ALL previous PRs

### Scope

Final validation PR. No new features — only integration testing, polish, and the parity checklist.

### Deliverables

| File | Change |
|------|--------|
| `scripts/parity-check.sh` (NEW) | Automated checklist: feature matrix MR vs MM, pass/fail per item |
| `docs/client-parity-report.md` (NEW) | Human-readable parity report with screenshots |
| All widgets | Bug fixes, visual polish, edge case handling |

### Parity Checklist

| Feature | MM | MR Target | Delivered By |
|---------|----|----|-------------|
| Candlestick chart | yes | yes | PR 7.2 |
| Volume bars | yes | yes | PR 7.4 |
| Heatmap overlay | yes | yes | PR 7.4 |
| VPVR overlay | yes | yes | PR 7.4 |
| Orderbook widget | yes | yes | Existing |
| Trades widget | yes | yes | Existing |
| Pan/Zoom | yes | yes | PR 7.0 + 7.2 |
| Drawing tools | yes | yes | PR 7.6 |
| Timeframe switching | yes | yes | PR 7.9 |
| Funding rate | yes | yes | PR 7.8 |
| Liquidations | yes | yes | PR 7.8 |
| Stats panel | yes | yes | PR 7.8 |
| Layout/docking | yes | yes | PR 7.3 |
| Settings persistence | yes | yes | PR 7.3 |
| **Web platform** | NO | yes | **MR advantage** |
| **6-exchange backfill** | NO (1 only) | yes | **MR advantage** |
| **Deterministic render** | NO | yes | **MR advantage** |

### Gates

- All golden hash tests pass.
- Web/native visual parity (screenshot diff < 5% pixel delta).
- 10k candles + all overlays at 60fps (native), 30fps (web/WASM).
- Zero TODO/FIXME/HACK in client/ (maintain existing zero-debt standard).

---

## Risk Matrix (Project-Level)

| Risk | Impact | Likelihood | Mitigation |
|------|--------|-----------|------------|
| WASM performance ceiling | High | Medium | Profile early (PR 7.7). WASM SIMD not needed for RCL. |
| f32 parity drift between targets | High | Low | Hash gate (PR 6.8) catches immediately. |
| ImGui dependency lock-in (native) | Medium | Low | RCL abstraction already isolates. Could swap to raylib. |
| WebSocket binary protocol change | Medium | Low | Subscription manager (PR 7.1) abstracts protocol. |
| Font rendering inconsistency | Low | High | Accept platform differences in rasterization. Hash gate covers RCL, not pixels. |
| Scope creep beyond MM parity | Medium | Medium | PR 8.0 gate enforces "stop here, ship it." |

---

## MR Structural Advantages Over MM (Post-8.0)

These are architectural advantages that compound over time:

1. **Web platform** — MM has zero web presence. MR runs in browser from day 1.
2. **Deterministic rendering** — Hash gate ensures refactoring never breaks output.
3. **Zero-alloc steady-state** — No GC pauses, no allocation storms during trading.
4. **6-exchange backfill** — MM supports 1 exchange for historical data.
5. **RCL portability** — Could target Metal, Vulkan, or terminal with a new renderer.
6. **Subscription dedup** — Multiple widgets sharing data = 1 WS stream (MM duplicates).
