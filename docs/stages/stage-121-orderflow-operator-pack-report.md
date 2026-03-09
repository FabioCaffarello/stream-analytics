# Stage 121 — Orderflow Operator Pack

**Date:** 2026-03-09
**Status:** COMPLETE
**Scope:** Trades Tape, Orderbook/DOM, Stats Panel, Trade Counter — rendering upgrades

## Summary

Upgraded four orderflow widget renderers from basic diagnostic displays to professional-grade
operational views. All changes are confined to render procs in `layer_strategies.odin` — no new
widget kinds, no new layer IDs, no store schema changes, zero allocation changes.

## Changes

### 1. Trades Tape (`trades_tape_render`)

- **Size classification**: Pre-scan computes avg_qty; trades mapped to 4 tiers (small→0.20α, medium→0.35α, large→0.55α, whale→0.70α)
- **Dust filter**: Trades below 5% of avg_qty are suppressed (adapts per instrument)
- **Split columns**: Price (COL_TEXT_PRIMARY, left) + Qty (COL_TEXT_SECONDARY, center) as separate text badges for visual alignment
- **Age column**: Right-edge badge showing time since trade (Xs/Xm/Xh), using `ctx.now_ms`
- **Whale highlight**: 1px COL_YELLOW_ACCENT top border on trades ≥ 10x average

### 2. Orderbook/DOM (`orderbook_dom_render`)

- **Mid-price hero badge**: Centered at top, FONT_SIZE_SM, COL_TEXT_PRIMARY
- **Spread in basis points**: "Spd X.XX (Y.Y bps)" badge, COL_TEXT_MUTED
- **24px header reservation**: Body starts below header for cleaner separation
- **Cumulative depth shadow**: Low-alpha (0.10) bars showing running cumulative size per side
- **Price labels**: Per-level price at outer edges (FONT_SIZE_XS, COL_TEXT_MUTED)
- **Whale wall highlight**: Levels ≥ 5x average get brighter alpha (0.55) + COL_YELLOW_ACCENT accent line

### 3. Stats Panel (`stats_panel_render`)

- **Hero mark price**: FONT_SIZE_LG (20px) with "Mark Price" label below
- **Funding rate upgrade**: FONT_SIZE_SM with directional green/red coloring
- **Spread cross-store**: When orderbook data available, shows spread in absolute + bps (COL_ACCENT_CYAN)
- **Liquidation bar**: Visual buy/sell ratio bar (green/red, 8px) above text summary
- **Quality flags suppressed**: Only displayed when non-zero (reduces visual noise)

### 4. Trade Counter (`trade_counter_render`)

- **Trade count**: Upgraded to FONT_SIZE_SM for primary metric emphasis
- **Net delta**: Dedicated "Delta ±X.XXXX" line with directional color (FONT_SIZE_SM)
- **Ratio bar taller**: 12px instead of 8px for better visibility
- **Volume rate**: "Rate X.XX/s" computed from candle window duration
- **Rolling 5-bar summary**: Aggregates buy+sell volume and trade count across last N candles

## Architecture

- All changes in `layer_strategies.odin` — 4 render procs modified
- No new files, no store changes, no new layer IDs
- Zero-allocation maintained: all formatting via `fmt.bprintf` into stack buffers
- Cross-store reads use existing `ctx.stream` pointer (same Market_Stream)
- Primitive budget: most complex widget (DOM) ~110 items, well under 2048 cap

## Tests

- 22 layer tests — all pass
- Updated expected counts: trades (≥6), orderbook (≥10), stats (≥6), counter (≥7)
- All core packages compile clean (`check-core: all packages OK`)

## Files Modified

| File | Change |
|------|--------|
| `client/src/core/layers/layer_strategies.odin` | 4 render procs rewritten (trades, orderbook, stats, counter) |
| `client/src/core/layers/layers_test.odin` | 4 test assertions updated for new primitive counts |

## Performance

- No new allocations (stack buffers only)
- Pre-scan loops bounded by `vis_cap` (18 for trades/orderbook)
- Rolling summary bounded to 5 candles max
- All within existing render budget constraints

## Zero Regressions

- No wire changes, no schema changes, no store API changes
- Existing layer bundle routing, diagnostic procs, and capability gates unchanged
- All 22 existing tests pass without modification to test logic (only assertion thresholds updated)
