# Stage 155 — Orderflow Contract Architecture

**Date:** 2026-03-09
**Status:** COMPLETE
**ADR:** ADR-0035 (Orderflow Contract Architecture)
**Extends:** ADR-0033 (Orderflow Domain Blueprint, S147)

## Objective

Formalize the orderflow data contract architecture and close the remaining structural gaps from S147-S149, creating a solid foundation for future vertical slices (footprint chart, enhanced DOM, flow-weighted VPVR).

## What Changed

### 1. Footprint Reducer Wiring (Gap Closure)

`Footprint_Store` was allocated per-stream on `Market_Stream` since S148 but never populated — `footprint_store_push_trade()` was never called from any reducer.

**Fix:** `market_store_reduce_trade()` now calls `footprint_store_push_trade()` when `active_tf_ms > 0`, accumulating trades into candle-aligned price bins.

**TF Resolution:** Added `active_tf_ms` field to `Market_Store`. The app layer sets this from `state.active_tf_idx` before each poll cycle in `drain_layer_marketdata()`. When `active_tf_ms == 0` (no TF context), footprint accumulation is skipped — DOM and Trades stores still accumulate normally.

**Files:**
- `layers/market_store_reducers.odin` — added footprint accumulation to `market_store_reduce_trade()`
- `layers/market_store.odin` — added `active_tf_ms` to `Market_Store`, passed to reducer
- `app/layer_marketdata.odin` — set `active_tf_ms` from workspace TF before polling

### 2. Widget_Kind.Footprint

Added `Footprint` variant to `Widget_Kind` enum with full contract coverage:

- **Widget descriptor** (`workspace.odin`): label="Footprint", min 140×120
- **Widget contract** (`widget_contract.odin`): no-op renderer (render_empty_contract) — placeholder until footprint chart renderer is built
- **Readiness policy** (`widget_readiness.odin`): primary_artifact=Trade, partial_usable=true, backfill_absent_usable=true
- **Channel subscription** (`widget_channels.odin`): subscribes to Trades + Candles channels
- **Layer bundle**: Bundle_Empty (no layer rendering until renderer exists)
- **Pane role**: Auxiliary
- **Shell labels** (`shell_common.odin`): glyph="F", loading/seeding/snapshot/empty labels

### 3. ADR-0035 — Orderflow Contract Architecture

Published formal contract matrix documenting the end-to-end data flow for all 15 orderflow capabilities:
- Raw Trades → Trades_Store (T0)
- Trade Tape → Trades_Store fallback (T0)
- Orderbook L2 → Orderbook_Store (T1)
- DOM Fills → DOM_Store (client-local, T0)
- Footprint Grid → Footprint_Store (client-local, T0)
- Delta Volume, CVD, Bar Stats → Analytics_Store (T1)
- VPVR, Heatmap, Session VP, TPO → respective stores (T2)
- Evidence, Signals → Evidence ring, Signal_Store (T3)

Key decisions documented:
- **Single reducer entry point** — all trade-derived stores fed from `market_store_reduce_trade()`
- **Client-local footprint is MVP** — backend `FootprintCandleV1` deferred until replay-safe footprint or multi-client consistency needed
- **No new backend BC** — reaffirms ADR-0033 decision
- **TF dependency managed at store level** — `active_tf_ms` flows from app → store → reducer

## Files Modified

| File | Change |
|------|--------|
| `app/app.odin` | Added `Footprint` to `Widget_Kind` enum |
| `app/widget_contract.odin` | Added `Footprint` to `WIDGET_CONTRACTS` |
| `app/widget_readiness.odin` | Added policy, store check, label for `Footprint` |
| `app/widget_channels.odin` | Added channel mask and bundle for `Footprint` |
| `app/workspace.odin` | Added descriptor and pane role for `Footprint` |
| `app/shell_common.odin` | Added glyph and state labels for `Footprint` (5 switch cases) |
| `app/layer_marketdata.odin` | Set `active_tf_ms` before poll cycle |
| `layers/market_store.odin` | Added `active_tf_ms` to `Market_Store`, pass to reducer |
| `layers/market_store_reducers.odin` | Wire footprint accumulation in trade reducer |

## Files Created

| File | Purpose |
|------|---------|
| `docs/adrs/ADR-0035-orderflow-contract-architecture.md` | Formal orderflow contract architecture |
| `docs/stages/stage-155-orderflow-contract-architecture-report.md` | This report |

## Tests

### New Tests (13)

**Layers (4):**
- `test_s155_footprint_accumulates_with_tf` — trades bin into footprint when TF set
- `test_s155_footprint_skipped_without_tf` — no accumulation when TF is 0
- `test_s155_trade_feeds_dom_and_footprint` — single trade feeds DOM + Footprint + Trades stores
- Updated `test_footprint_store_in_market_stream` — adjusted for new wiring semantics

**App (9):**
- `test_s155_footprint_contract_exists` — contract has all required procs
- `test_s155_footprint_descriptor` — descriptor has correct label and dimensions
- `test_s155_footprint_readiness_policy` — policy correct for Trade artifact
- `test_s155_footprint_store_has_data` — readiness gate works for footprint store
- `test_s155_footprint_store_label` — diagnostics label correct
- `test_s155_footprint_channels` — channel subscription non-zero
- `test_s155_footprint_pane_role` — inferred as Auxiliary
- `test_s155_footprint_lifecycle` — create → bind → dispose without panic
- Existing exhaustive tests (contract table, readiness policies) automatically cover Footprint

### Test Totals

| Package | Count | Status |
|---------|-------|--------|
| md_common | 512 | All green |
| app | 455 | All green |
| services | 204 | All green |
| layers | 57 | All green |
| **Total** | **1,228** | **All green** |

## Data Flow (End-to-End, Post-S155)

```
Exchange WS → marketdata → aggregation → delivery → WS → Client
                                                          ↓
                                                    MD_Event polling
                                                          ↓
                                              market_store_apply_event()
                                                          ↓
                                              market_store_reduce_trade()
                                              ├→ Trades_Store.push_trade()
                                              ├→ DOM_Store.dom_store_push_trade()      [S148]
                                              └→ Footprint_Store.push_trade()          [S155] ← NEW
                                                  (when active_tf_ms > 0)
```

## Deferred

- **Footprint chart renderer** — `footprint_chart_render()` proc + Layer_Bundle entry (future slice)
- **Context menu entry** — Footprint not yet in cell context menu (add when renderer exists)
- **Backend FootprintCandleV1** — insights BC producer (add when replay-safe footprint needed)
- **TF change reset** — footprint store orphaned entries on TF switch (mitigate with store reset)
- **Footprint price grouping UI** — connect `dom_group_idx` to footprint price_group

## Invariants Preserved

- All trade-derived stores fed from `market_store_reduce_trade()` — single entry point
- Widget_Kind enum is exhaustive across all arrays and switches — compiler enforces
- No new bounded context created — orderflow remains cross-cutting (ADR-0033 affirmed)
- Zero regressions, zero wire-breaking changes
