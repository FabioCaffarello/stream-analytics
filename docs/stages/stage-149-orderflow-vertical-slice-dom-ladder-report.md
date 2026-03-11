# Stage 149 — Orderflow Vertical Slice I: DOM Ladder

**Date:** 2026-03-09
**Status:** COMPLETE
**Scope:** First end-to-end orderflow slice — DOM Ladder compositing Book Depth + Trade Fills

---

## Objective

Implement a first complete, robust orderflow vertical slice validated end-to-end:
backend trade data → delivery → client WS → Market_Store → DOM_Store → DOM Ladder widget → UX states.

**Slice chosen:** DOM Ladder — composites Orderbook levels (book depth) with DOM fill tracking (trade volume at price). High value, low risk: both data sources already flow, DOM_Store already wired in S148.

---

## What Changed

### 1. DOM Store Enhancements (`services/dom_store.odin`)

New helper functions for the rendering and readiness layers:

| Function | Purpose |
|----------|---------|
| `dom_store_has_data()` | Check if any fills accumulated |
| `dom_store_imbalance()` | Buy/sell imbalance ratio [-1, +1] |
| `dom_store_fill_at_price()` | Lookup fill volume at an exact orderbook price (with price grouping) |
| `dom_store_max_fill()` | Maximum fill volume across all levels (for bar normalization) |

### 2. DOM Ladder Rendering (`layers/layer_strategies.odin`)

Enhanced `orderbook_dom_render` to detect DOM bundle mode (`active_bundle == Bundle_DOM`) and overlay fill data:

**Header (expanded from 24px → 36px in DOM mode):**
- Mid-price hero badge (existing)
- Spread in absolute + bps (existing)
- **NEW:** VWAP badge (cyan accent) from DOM_Store accumulator
- **NEW:** Imbalance badge (green/red/muted based on ±5% threshold)
- **NEW:** Trade count badge showing total fills

**Per-level fill overlay:**
- Thin 3px bar at bottom of each price level row
- Ask levels: orange accent (`COL_WARNING`) showing sell-side fills
- Bid levels: cyan accent (`COL_ACCENT_CYAN`) showing buy-side fills
- Width proportional to fill volume vs max fill (normalized)

**Backward-compatible:** When `active_bundle` is `Bundle_Orderbook` (standard orderbook widget), rendering is unchanged.

### 3. DOM Widget Readiness (`app/widget_readiness.odin`)

- **`widget_store_has_data(.DOM)`** now checks BOTH orderbook AND dom stores — DOM is usable when either source has data
- **`widget_store_label(.DOM)`** returns `"dom"` (was grouped with `"orderbook"`)
- Orderbook widget readiness unchanged

### 4. Tests

**16 new DOM Store tests** (`services/dom_store_test.odin`):
- Empty store behavior, push trade validation (zero qty/price rejected)
- Fill accumulation at same bucket, price grouping, fill lookup miss
- VWAP, TWAP, imbalance (all buys, all sells, balanced)
- Max fill, recent fills ring wrap, reset, multiple price levels, nil safety

**7 new DOM readiness tests** (`app/widget_contract_test.odin`):
- DOM usable with orderbook only, fills only, both, neither
- Nil stores safety, store label, readiness policy validation

---

## Data Flow (End-to-End)

```
Exchange WS → Adapter (parser) → TradeTickV1
  → Delivery (CHANNEL_TRADE) → Client WS
  → market_store_reduce_trade()
    → Trades_Store (ring buffer)
    → DOM_Store.dom_store_push_trade() (fill accumulation by price bucket)
  → market_store_reduce_orderbook()
    → Orderbook_Store (L2 book snapshot)

Widget render (DOM bundle):
  → orderbook_dom_render()
    reads Orderbook_Store (bid/ask levels)
    reads DOM_Store (fill volumes at each price)
    → Header: mid-price, spread, VWAP, imbalance, fills count
    → Per-level: book depth bars + cumulative shadow + fill overlay
```

---

## Architecture Decisions

1. **No new Layer_ID** — DOM rendering enhanced within existing `OrderBook_DOM` layer via `active_bundle` detection. Avoids layer registry expansion.

2. **Fill overlay, not replacement** — DOM fills shown as thin accent bars (3px) overlaid on existing book depth bars. Book depth remains the primary visual, fills augment it.

3. **Bidirectional readiness** — DOM widget usable with either orderbook OR fills. Supports partial utility: fills accumulate immediately from trades, book snapshot may arrive later.

4. **Price grouping respected** — `dom_store_fill_at_price` applies the same price_group bucketing as push_trade, so overlay aligns correctly with book levels.

---

## Test Results

| Package | Tests | Status |
|---------|-------|--------|
| services | 204 | ALL PASS |
| app | 437 | ALL PASS |
| layers | 54 | ALL PASS |
| md_common | 485 | ALL PASS |
| **Total** | **1,180** | **ALL PASS** |

New tests: 23 (16 DOM store + 7 DOM readiness)

---

## What's NOT in This Slice

- **Footprint chart** — Store exists but renderer not built (separate slice)
- **DOM scroll/zoom** — Price levels are static book depth; interactive navigation deferred
- **DOM price grouping UI** — `dom_group_idx` in Chart_Component exists but not wired to overlay
- **Cross-venue DOM** — Single-venue only; cross-venue book composition deferred
- **Fill age decay** — All fills weighted equally; time-decay for recency bias deferred

---

## Next Steps

1. **S150 (Footprint Chart)** — Wire Footprint_Store in reducer + build candle-aligned footprint renderer
2. **DOM grouping UI** — Wire `dom_group_idx` to control DOM_Store price_group dynamically
3. **Fill decay** — Exponential time-weighting for recent fills (visual freshness)
4. **Cumulative Delta Profile** — Standalone CVD widget (not subplot)
