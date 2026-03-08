# Stage 60 — Market Explorer 2.0 + Discovery Architecture

**Date:** 2026-03-08
**Status:** COMPLETE
**Tests:** 13 new (112 total services), check-core OK, check-wasm-compile OK

---

## Diagnosis of Previous Markets Page (S54)

| Issue | Impact |
|-------|--------|
| Flat list, no venue grouping | Poor discoverability at scale |
| No market type filter | SPOT/PERP mixed, no quick filtering |
| No collapsible sections | Viewport overflow with large catalogs |
| No lifecycle hooks | No periodic refresh, stale data |
| No session dashboard integration | Missing global health context |
| Available section undifferentiated | Flat `+ venue:symbol` rows with no venue context |
| No backend catalog usage | Only `/api/v1/markets`, not dashboard/catalog |
| Detail panel minimal | Just compact stream list, no venue summary |

## Architecture: Market Explorer 2.0

### Data Flow

```
/api/v1/markets → Markets_Store (64 entries)
/api/v1/session/dashboard → Explorer_State.dashboard_* (health summary)
Stream_View_Registry → Market_Row_View (price, health, subscription)
                    ↓
             build_markets_page()
                    ↓
    ┌───────────────────────────────────┐
    │ Header: "Market Explorer" + badge │
    │ Dashboard: 6v 19i ready flow:...  │
    │ Tabs: [All] [Spot] [Perp]         │
    │ ─────────────────────────────     │
    │ ACTIVE STREAMS (3)                │
    │   binance-spot:BTCUSDT  67,234... │
    │   bybit:ETHUSDT         3,412...  │
    │ ─────────────────────────────     │
    │ CATALOG (6 venues, 19 instruments)│
    │ [-] binance-spot        2/3 active│
    │      ● BTCUSDT SPOT    67,234 +2% │
    │      + SOLUSDT SPOT               │
    │ [-] binance-futures     1/3 active│
    │      ● BTCUSDT PERP    67,230     │
    │      + ETHUSDT PERP               │
    │ [+] bybit              (collapsed)│
    │ [+] coinbase            0/3 active│
    └───────────────────────────────────┘
```

### Components

| Component | File | Purpose |
|-----------|------|---------|
| `Explorer_State` | `app.odin` | Type filter, scroll, collapse, dashboard cache |
| `explorer_resolve_venues()` | `services/market_explorer.odin` | Venue grouping service |
| `explorer_entry_matches()` | `services/market_explorer.odin` | Filter matching (type + search) |
| `contains_ci()` | `services/market_explorer.odin` | Case-insensitive substring (zero alloc) |
| `build_markets_page()` | `build_markets.odin` | Main page render |
| `draw_markets_detail()` | `build_markets.odin` | Detail panel with venue summary |
| `page_explorer_enter/leave()` | `build_markets.odin` | Lifecycle hooks |
| `poll_explorer()` | `build_markets.odin` | Periodic dashboard refresh (~10s) |
| `fetch_explorer_dashboard()` | `build_markets.odin` | HTTP fetch + parse session dashboard |

### Page Module Registration

```
.Markets = {
    render_page   = page_markets_render,
    render_detail = page_markets_render_detail,
    on_enter      = page_explorer_enter,    // NEW: fetch dashboard
    on_leave      = page_explorer_leave,    // NEW: preserve filters
}
```

### Explorer_State (zero-alloc, fixed-size)

```odin
Explorer_State :: struct {
    type_filter:             Explorer_Market_Type_Filter,  // All/Spot/Perp
    scroll_y:                f32,
    collapsed:               [16]bool,                     // per-venue collapse
    has_dashboard:           bool,
    dashboard_status:        [16]u8,                       // "ready"/"degraded"
    dashboard_venues:        int,
    dashboard_instruments:   int,
    dashboard_active:        int,
    dashboard_stale:         int,
    dashboard_freshness:     [16]u8,                       // "flowing"/"stale"
    fetch_frame:             u64,
    fetch_status:            Overview_Fetch_Status,
}
```

## Changes

### New Files
- `client/src/core/services/market_explorer.odin` — Explorer view model + venue grouping + filters
- `client/src/core/services/market_explorer_test.odin` — 13 tests

### Modified Files
- `client/src/core/app/build_markets.odin` — Complete rewrite: venue-grouped layout, filter tabs, dashboard header, lifecycle hooks
- `client/src/core/app/page_module.odin` — Added lifecycle hooks for Markets route
- `client/src/core/app/app.odin` — Added `Explorer_State`, `poll_explorer()` in both update loops
- `client/src/core/app/actions.odin` — Added Escape key handler for Markets→Dashboard navigation

## Features

### 1. Venue-Grouped Catalog
- Markets organized under collapsible venue headers (`binance-spot`, `bybit`, etc.)
- Per-venue counts: `N/M active` (subscribed / visible)
- Click venue header to collapse/expand — manages vertical space

### 2. Market Type Filter Tabs
- **All** — show all instruments
- **Spot** — show only SPOT markets
- **Perp** — show only perpetual futures (USD_M_FUTURES)
- Active tab highlighted with cyan accent

### 3. Session Dashboard Integration
- Global health summary line: venues, instruments, status, freshness, active/stale counts
- Fetched on page enter + periodic poll (~10s)
- Uses `/api/v1/session/dashboard` (same endpoint as Session Health page)

### 4. Lifecycle Hooks
- `on_enter`: Reset scroll, fetch session dashboard
- `on_leave`: Preserve filter and collapse state across navigations
- `poll_explorer()`: Periodic refresh in both native and web update loops

### 5. Active Streams Section
- Only shown when subscribed streams exist
- Full detail: venue:symbol, price, change%, health dot, composition badge, overview/unsubscribe actions
- Click to switch active stream

### 6. Catalog Instrument Rows
- **Subscribed**: health dot + symbol + type + price + change% + overview(>) + unsubscribe(-)
- **Available**: `+ symbol type` with green hover, click to subscribe

### 7. Keyboard Navigation
- Escape from Markets → returns to Dashboard

### 8. Detail Panel
- "EXPLORER" header with connection badge
- Dashboard summary (venues, instruments, active)
- Stream count
- Compact stream list with health dots
- "Open Explorer" link

## Test Coverage

| Test | Validates |
|------|-----------|
| `test_explorer_resolve_venues_empty` | Empty store → zero venues |
| `test_explorer_resolve_venues_groups_by_venue` | Correct venue grouping + counts |
| `test_explorer_resolve_venues_counts_active` | Active count via callback |
| `test_explorer_resolve_venues_type_totals` | SPOT vs PERP totals |
| `test_entry_matches_all_filter` | All filter passes everything |
| `test_entry_matches_spot_filter` | Spot filter blocks PERP |
| `test_entry_matches_perp_filter` | Perp filter blocks SPOT |
| `test_entry_matches_search_ticker` | Case-insensitive ticker search |
| `test_entry_matches_search_venue` | Venue name search |
| `test_entry_matches_combined_filter` | Type + search combined |
| `test_contains_ci_basic` | Case-insensitive substring matching |
| `test_explorer_resolve_venues_nil_store` | Nil safety |
| `test_explorer_first_idx` | First index tracking per venue |

## Quality Gates

- `make check-core`: All 10 packages OK
- `make check-wasm-compile`: OK
- `odin test services`: 112 tests, all successful
- Zero backend changes required
- Zero wire protocol changes
- Zero regressions

## Structural Gains

| Before (S54) | After (S60) |
|--------------|-------------|
| Flat list, no grouping | Venue-grouped with collapsible sections |
| No filtering | Market type tabs (All/Spot/Perp) |
| No health context | Session dashboard summary header |
| No lifecycle hooks | on_enter/on_leave + periodic poll |
| Undifferentiated available section | Venue-scoped, filtered catalog |
| No escape navigation | Escape → Dashboard |
| Detail says "MARKETS" | Detail says "EXPLORER" with dashboard summary |
| Single-pass flat render | Two-section: Active Streams + Venue Catalog |

## Product Gains

1. **Scalable discovery** — venue grouping + collapse handles 100+ instruments without viewport overflow
2. **Operational clarity** — market type filters let operators focus on SPOT or PERP independently
3. **Health awareness** — dashboard summary provides at-a-glance system status on the discovery page
4. **Consistent navigation** — Escape key works from Markets (like Session Health)
5. **Preserved state** — filters and collapse survive page transitions
6. **Zero-alloc filters** — case-insensitive search infrastructure ready for future text input

## Future Extensions

- Full text search when text input is added to the input port
- Favorite/pinned instruments
- Sort by venue/price/change
- Virtual scrolling for very large catalogs (1000+ instruments)
- Backend catalog endpoint (`/api/v1/catalog`) integration for artifact availability badges
