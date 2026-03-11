# Stage 157 — Orderflow Vertical Slice I: DOM + Trades + Footprint

## Objective

Deliver a robust, end-to-end validated orderflow slice covering DOM, Trades, and Footprint widgets. Fix data wiring gaps, integrate health/readiness models, and build the Footprint chart renderer.

## Changes

### 1. Bug Fix: Follow-Active Cell Store Resolution (stream_slots.odin)

**Problem:** `resolve_stores_for_cell` default fallback path (follow-active cells with `stream_idx=-1`) did not wire DOM or Footprint store pointers. Only candle/heatmap/vpvr/trades/orderbook/stats/analytics were set.

**Impact:** Follow-active DOM and Footprint widgets received nil store pointers and could never render data.

**Fix:** Added `stores.dom = &active.dom` and `stores.footprint = &active.footprint` to the default fallback block (line 478). The pane path (`resolve_stores_for_pane`) already had this wiring since S148.

### 2. Bug Fix: Footprint Store Not Cleared on TF Change (stream_views.odin)

**Problem:** `apply_set_timeframe_action` and `apply_set_cell_timeframe_action` cleared candle/heatmap/vpvr/analytics stores on TF change, but did NOT clear the Footprint store. Since Footprint data is TF-dependent (trades bucketed into candle windows by `active_tf_ms`), stale entries from the old TF persisted.

**Fix:** Added `services.footprint_store_reset(&ms.footprint)` in 3 locations:
- Active slot TF change (line 383)
- Per-cell bound slot TF change loop (line 417)
- Per-cell TF override change (line 611)

### 3. Feature: Footprint Chart Renderer (render_footprint.odin)

New file implementing `render_footprint_widget`:
- Multi-column grid: each column = candle window, newest on right
- Price levels as horizontal bars: buy (green, right) + sell (red, left) of center
- Intensity proportional to volume (alpha: 0.3 + 0.6 * intensity)
- Column separators for visual clarity
- Header: candle count badge
- Delta badge: net buy-sell for newest candle (bottom-right)
- Graceful empty states: "waiting for trades", "Accumulating..."

### 4. Widget Contract Wiring (widget_contract.odin, build_cell.odin)

- Changed Footprint from `render_empty_contract` to `render_footprint_contract`
- Added `render_footprint_contract` proc dispatching to `render_footprint_widget`
- Legacy path (`render_cell_widget` / `build_cell.odin`) routes Footprint to dedicated renderer (same pattern as Session_VPVR/TPO)

### 5. Tests (s157_orderflow_slice_test.odin)

17 new tests covering:

| Category | Tests | Description |
|----------|-------|-------------|
| Store resolution | 4 | DOM/Footprint wired for follow-active cells (cell + pane paths) |
| Footprint readiness | 3 | nil store, empty store, populated store |
| DOM readiness | 4 | OB-only, fills-only, both, empty |
| Contract routing | 2 | Footprint has renderer, not empty contract |
| Readiness policy | 1 | Footprint policy matches Trade artifact |
| TF-change clearing | 1 | footprint_store_reset clears correctly |
| Trades readiness | 2 | with data, empty |

## Files Changed

| File | Change |
|------|--------|
| `app/stream_slots.odin` | Added DOM/Footprint to follow-active default fallback |
| `app/stream_views.odin` | Clear Footprint store on TF change (3 locations) |
| `app/render_footprint.odin` | **NEW** — Footprint chart renderer |
| `app/widget_contract.odin` | Footprint → render_footprint_contract |
| `app/build_cell.odin` | Legacy path routes Footprint to dedicated renderer |
| `app/s157_orderflow_slice_test.odin` | **NEW** — 17 integration tests |

## Test Results

- **app**: 472 tests (455 existing + 17 new) — all green
- **services**: 246 tests — all green
- **md_common**: 512 tests — all green
- **layers**: 57 tests — all green
- **Total: 1,287 tests** — zero regressions

## Architecture Validation

### End-to-End Data Flow Verified

```
Wire (TradeTickV1) → MD_Trade_Event → market_store_reduce_trade()
  ├─ Trades_Store (256-ring)      → Trades widget ✅
  ├─ DOM_Store (512 price buckets) → DOM Ladder widget ✅
  └─ Footprint_Store (200×50)     → Footprint chart widget ✅ (NEW)

Wire (OrderBookSnapshotV2) → MD_Orderbook_Event → market_store_reduce_orderbook()
  └─ Orderbook_Store (50/side)    → Orderbook widget ✅
                                  → DOM Ladder (shared) ✅
```

### Health Model Integration

- DOM: `widget_readiness_policies[.DOM]` = primary artifact `.Orderbook`, partial_usable=false
- Trades: `widget_readiness_policies[.Trades]` = primary artifact `.Trade`, partial_usable=true
- Footprint: `widget_readiness_policies[.Footprint]` = primary artifact `.Trade`, partial_usable=true
- All three use `uses_artifact_live_flag=true` for per-artifact Snapshot_Pending detection

### TF-Aware Behavior

- **DOM**: TF-independent (accumulates all trades regardless of TF) — not cleared on TF change
- **Trades**: TF-independent (ring buffer of raw ticks) — not cleared on TF change
- **Footprint**: TF-dependent (trades bucketed by `active_tf_ms`) — **now cleared on TF change**
- **Orderbook**: Snapshot-replaced on each event — cleared on TF change (fresh snapshot expected)

### Partial Usability on High TFs

| Widget | 1h TF | 4h TF | 1d TF |
|--------|-------|-------|-------|
| Trades | Full (tick-level, no TF dependency) | Full | Full |
| DOM | Full (fills accumulate instantly) | Full | Full |
| Orderbook | Full (snapshot on connect) | Full | Full |
| Footprint | Partial (builds as trades arrive) | Partial | Minimal |

## Performance Impact

- Zero new allocations (all stores fixed-capacity)
- Footprint renderer: O(visible_candles * levels_per_candle) per frame
- Worst case: 40 candles × 50 levels = 2,000 rect commands per frame
- No hot-path fmt.Sprintf (bprintf to stack buffers only)

## Deferred

- **P3**: DOM scroll/zoom interaction (price navigation)
- **P3**: DOM price grouping UI (wire `dom_group_idx`)
- **P3**: Footprint candle-viewport alignment (sync with candle chart scroll/zoom)
- **P4**: Cross-venue orderflow composition
- **P4**: Fill age decay animation
