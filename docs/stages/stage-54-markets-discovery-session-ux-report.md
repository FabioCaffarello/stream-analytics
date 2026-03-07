# Stage 54 — Markets / Discovery / Session UX

**Date:** 2026-03-07
**Branch:** `codex/s9-legacy-removal-cutover`
**Status:** COMPLETE

## Summary

Rewrote the Markets page from a passive connected-stream list into a full operations center with session status, health/composition indicators, and market discovery with click-to-subscribe.

## Changes

### New: `build_markets.odin` (478 lines, was 153)

- **`Market_Row_View`** — per-market read model struct. Pure derived view assembled once per frame per market. Fields: venue, symbol, market_type, subscription status, price data, composition stage, health level, staleness counts, back-reference indices.
- **`resolve_market_rows`** — pure function that iterates `Markets_Store`, matches each entry to stream view slots, resolves subscription status, price, composition, health, and staleness. Returns populated row count.
- **`build_markets_page`** — full Markets page with:
  - Session status header (connection badge, freshness badge, bootstrap state)
  - Active Streams section: subscribed markets with price, change%, composition badge (PEND/BFILL/LIVE/COMP), health dot, staleness count, market type, active indicator, click-to-switch, unsubscribe button
  - Available Markets section: unsubscribed markets with click-to-subscribe, market type badge
- **`draw_active_market_row`** — renders a two-line subscribed market row (line 1: identity + price; line 2: composition + health + staleness + type + unsubscribe)
- **`draw_available_market_row`** — renders a single-line discoverable market with green highlight on hover
- **`draw_markets_detail`** — compact sidebar panel with connection badge, stream count, per-stream health dots, click-to-switch, and "Open Markets" navigation link

### Removed: `settings.odin` old `build_markets_page` (108 lines)

The old proc was a flat stream-view iteration with venue:symbol, price, change%, and click-to-switch. It had no discovery, no health indicators, no session status, and no unsubscribe capability.

## Architecture

```
Markets_Store (64 entries)
    |
    v
resolve_market_rows() -- pure, no alloc
    |
    v
[MARKET_ROW_CAP]Market_Row_View
    |
    +---> build_markets_page()     (main content area)
    |         +-- draw_active_market_row()    (subscribed, 2-line)
    |         +-- draw_available_market_row() (discoverable, 1-line)
    |
    +---> draw_markets_detail()    (sidebar, compact)
```

### Read model pattern

`Market_Row_View` follows the same pattern as `Cell_Surface_View` (S36): a pure derived struct computed once per frame, consumed by multiple render paths, with no mutation or allocation.

### Shared components reused

- `draw_composition_badge` (S53) — PEND/BFILL/LIVE/COMP labels
- `draw_health_dot` (S53) — green/yellow/red square indicator
- `resolve_conn_status_display` (S52) — connection status mapping
- `status_badge` / `status_badge_width` — UI badge primitives

### Action integration

All mutations go through the existing action queue:
- `.Subscribe_Market` — click available market row
- `.Unsubscribe_Market` — click unsubscribe button on active row
- `.Pick_Stream` — click active market row to switch
- `.Navigate_Route` — "Open Markets" link in sidebar detail

## Metrics

| Metric | Value |
|--------|-------|
| Lines added | 410 |
| Lines removed | 193 |
| Net new | +217 |
| Files changed | 2 |
| New structs | 1 (`Market_Row_View`) |
| New procs | 5 |
| Old procs removed | 1 (`build_markets_page` from settings.odin) |
| Wire changes | 0 |
| New mutable state | 0 |
| Compilation | Clean (`odin check`) |
| Commits | 1 |

## Design decisions

1. **Slot matching over `markets_is_subscribed`**: `resolve_market_rows` matches Markets_Store entries to stream view slots directly using `normalized_venue`/`normalized_symbol`, which also captures price and health data in a single pass rather than requiring separate lookups.

2. **Two-line active rows vs one-line available rows**: Active markets have more information density (price, change%, health, composition, staleness, unsubscribe) warranting 44px rows. Available markets are simple identity + subscribe action at 24px.

3. **Sidebar detail kept minimal**: The detail panel shows only stream count + health dots + navigation link, avoiding duplication of the full page's discovery/session features.

4. **No scroll state**: Both sections clip at viewport bounds. Scroll support is deferred until market count exceeds typical viewport capacity (>20 active streams).
