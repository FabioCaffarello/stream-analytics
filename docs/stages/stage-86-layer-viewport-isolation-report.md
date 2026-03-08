# Stage S86 — Layer Viewport Isolation & Cell Safety

**Date**: 2026-03-08
**Branch**: `codex/s9-legacy-removal-cutover`
**Validates**: S85 BUG-1 (P0), BUG-2 (P1), partial BUG-3 (P1)

---

## Objective

Close the P0/P1 structural renderer bugs identified in S85: each cell must render only within its viewport and only its authorized content. This is the architectural floor without which any UI stage becomes rework.

## Changes

### 1. Viewport Clipping (`layer_canvas.odin`)

Added `Cmd_Clip_Push{rect = cell_vp}` before layer rendering and `Cmd_Clip_Pop{}` after — including on early-return paths (empty bundle, missing stream). All layer output is now confined to the cell rectangle via the Canvas2D `save()/clip()/restore()` path in the web renderer.

**Files**: `client/src/core/app/layer_canvas.odin`

### 2. Strategy Bundle Mask Tightening (`layer_strategies.odin`)

**Root cause**: Each `Layer_Strategy` had its `bundle_mask` set to include composite bundles (e.g., `Bundle_Candles | Bundle_Stats`). Since composite bundles share bits (Evidence=bit4, Signal=bit5 appear in nearly all composites), a strategy designed for Candle cells would also match Trades, OB, Stats, and Counter cells.

**Fix**: Each strategy now sets its `bundle_mask` to its own single bit only:
- `Price_Candles` → bit 0
- `Trades_Tape` → bit 1
- `OrderBook_DOM` → bit 2
- `VPVR_Heatmap` → bit 3
- `Evidence` → bit 4
- `Signal` → bit 5
- `Analytics` → bit 6

This works because the composite `Layer_Bundle` definitions already include the appropriate single bits:
- `Bundle_Candles` = bits 0,3,4,5,6 → matches Price_Candles, VPVR_Heatmap, Evidence, Signal, Analytics
- `Bundle_Stats` = bits 0,5 → matches Price_Candles, Signal
- `Bundle_Counter` = bits 1,5 → matches Trades_Tape, Signal
- `Bundle_Trades` = bits 1,4,5 → matches Trades_Tape, Evidence, Signal
- `Bundle_Orderbook` = bits 2,4,5 → matches OrderBook_DOM, Evidence, Signal

The previous redundant composite bits on each strategy caused cross-contamination.

**Files**: `client/src/core/layers/layer_strategies.odin`

### 3. Active Bundle Context (`layer_api.odin` + `layer_canvas.odin`)

Added `active_bundle: u32` to `Layer_Context`. Set at the render call site from the resolved `bundle_mask`. Used in `price_candles_render` as defense-in-depth: candle bars only render when `active_bundle == Bundle_Candles`.

**Files**: `client/src/core/layers/layer_api.odin`, `client/src/core/app/layer_canvas.odin`

### 4. Zero-Data Trade Guard (`layer_strategies.odin`)

Added `t.price == 0 && t.qty == 0` skip in `trades_tape_render` to prevent rendering "0.00 x 0.0000" labels for empty/uninitialized trade entries.

### 5. Test Fix (`layers_test.odin`)

Updated `test_price_candles_layer_renders_expected_primitive_count` to set `active_bundle = Bundle_Candles` so the test correctly exercises candle bar rendering.

---

## Bug Resolution

| Bug | Severity | Status | Fix |
|-----|----------|--------|-----|
| BUG-1: Depth overlay across all cells | P0 | **FIXED** | Clip_Push/Pop + strategy mask tightening |
| BUG-2: Stats text bleeds across cells | P1 | **FIXED** | Clip_Push/Pop + strategy mask tightening |
| BUG-3: Wrong content in bottom widgets | P1 | **FIXED** | Strategy mask tightening (single-bit) |

## Validation

### Playwright Screenshots

| Screenshot | Description |
|-----------|-------------|
| `s86-02-1m-clip-verify.png` | After clip fix — cross-cell bleed eliminated |
| `s86-03-bundle-fix-verify.png` | After first bundle fix attempt — zero-data labels gone |
| `s86-04-strategy-mask-fix.png` | After strategy mask tightening — correct per-cell content |
| `s86-05-data-accumulated.png` | Steady-state verification with accumulated data |

### Test Results

- **9** layer tests — all pass
- **28** app tests — all pass
- **181** services tests — all pass
- **218 total tests, 0 failures**
- WASM compile check: OK

### Visual Verification

- Main candle chart: candle bars + heatmap overlay render correctly
- Stats cell: shows "Last {price}" text badge only, no candle bars
- Counter cell: empty when no trade data (correct)
- Trades cell: empty when no trade data (correct)
- OB cell: empty when no orderbook data (correct)
- Zero cross-cell visual bleed in any cell combination

---

## Architectural Note

The bundle mask design uses a two-level scheme:

1. **Single bits** (bits 0-6): one per layer type (Price_Candles, Trades_Tape, etc.)
2. **Composite bundles**: OR-combinations that define which layers render in each widget kind

Each `Layer_Strategy` now registers only its single bit. The composite bundles in `Layer_Bundle` enum define the routing. The `layer_registry_render_bundle` function matches `strategy.bundle_mask & requested_bundle != 0`. This cleanly separates "what I am" (strategy) from "where I should render" (bundle composite).

Previously, strategies also included composite bundle values in their mask, which leaked through shared bits (Evidence/Signal appear in most composites). This caused every layer to render in nearly every cell type.

---

## Acceptance Criteria

- [x] BUG-1 and BUG-2 resolved
- [x] Zero visual bleed between cells
- [x] No regression in candle/main chart
- [x] Each cell renders only its authorized content
- [x] Text, bars, overlays, and labels validated
- [x] Canvas renderer invariants hardened (Clip_Push/Pop bracket)
