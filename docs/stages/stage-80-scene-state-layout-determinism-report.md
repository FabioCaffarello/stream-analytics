# Stage 80 — Scene State, Layout Persistence & Deterministic Behavior

**Date:** 2026-03-08
**Branch:** `codex/s9-legacy-removal-cutover`
**Predecessor:** S79 (Contract Hardening, Determinism & UI Validation)

## Objective

Consolidate scene state, layout persistence, and deterministic behavior across the client/UI. Make the UI reproducible, versioned, and consistent across restores, snapshots, toggles, tabs, and widgets.

## Slices Delivered

### S80.1 — Cell_View_Model Enrichment

**Problem:** `Cell_View_Model` (S61) was missing chart display and indicator state. Widgets still needed to reach into `state.world.charts[ci]` and `state.world.indicators[ci]` directly, defeating the abstraction.

**Solution:** Added `chart: Chart_Component`, `indicators: Indicator_Component`, and `ind_params: Indicator_Params` to `Cell_View_Model`. The resolver copies these as value types from ECS in `resolve_cell_view_model()`, making them immutable snapshots for the render frame.

**Impact:** Closes the abstraction gap — all per-cell resolved state is now available in a single read model. Widgets can consume `vm.chart.show_vol`, `vm.indicators.show_ma`, `vm.ind_params.rsi_period` without accessing App_State.

**Files:**
- `client/src/core/app/stream_slots.odin` — Cell_View_Model struct + resolver

### S80.2 — Route + Portfolio Tab Persistence

**Problem:** Active route and portfolio tab were ephemeral — restarting the app always landed on Dashboard/Positions. Users who primarily use Portfolio or Markets had to re-navigate every time.

**Solution:**
- New settings keys: `SETTING_ACTIVE_ROUTE`, `SETTING_PORTFOLIO_TAB`
- Route persisted on every `Navigate_Route` action
- Portfolio tab persisted on every tab click
- Restored during `init()` after settings load
- Instrument_Overview is excluded from route restore (it's contextual, requires target instrument)

**Impact:** Route and tab selection survive app restarts. Zero wire changes, backward compatible (missing keys default to Dashboard/Positions).

**Files:**
- `client/src/core/services/settings_store.odin` — new keys + known_keys registration
- `client/src/core/app/actions.odin` — route persist on navigate
- `client/src/core/app/build_portfolio.odin` — tab persist on click
- `client/src/core/app/app.odin` — restore during init

### S80.3 — Compare Mode Deterministic Init

**Problem:** When entering compare mode, `ob_grp` was hardcoded to `1` and `trade_filter` to `0`, ignoring the focused cell's actual chart display settings. Adding a new compare stream had the same issue — filters reset instead of syncing from existing panes.

**Solution:**
- `apply_enter_compare` now copies chart display (vol, heatmap, vpvr, heatmap_idx, ob_grp, trade_filter) from the focused cell's `Chart_Component`. Falls back to global defaults if no cell is focused.
- `apply_add_compare_stream` copies filter settings from pane 0 (consistent with existing pane state) instead of resetting.

**Impact:** Compare mode inherits the user's active display preferences. No more jarring filter resets when entering/adding compare panes.

**Files:**
- `client/src/core/app/actions.odin` — `apply_enter_compare`, `apply_add_compare_stream`

### S80.4 — Runtime Snapshot V2

**Problem:** Runtime snapshot (S46) captured per-cell widget_kind, binding, and TF but not chart display or indicator state. Active route was also missing, making scene state incomplete for incident reproduction.

**Solution:**
- Bumped `RUNTIME_SNAPSHOT_VERSION` to 2
- Added `chart_display` (packed int, same encoding as V6 layout) and `indicator_flags` (packed bitmask) to `Snapshot_Cell`
- Added `active_route` (Route ordinal) to `Runtime_Snapshot`
- Snapshot serializer extended: header includes route, CL lines include chart_display + indicator_flags
- Capture proc populates new fields via existing `pack_chart_display_with_analytics` and `pack_indicator_flags`

**Impact:** Full scene state now captured in snapshots. Deterministic reproduction includes which route was active, what chart display each cell had, and which indicators were toggled.

**Files:**
- `client/src/core/md_common/runtime_snapshot.odin` — struct + serializer
- `client/src/core/app/runtime_snapshot_capture.odin` — capture proc
- `client/src/core/md_common/store_boundary_test.odin` — updated V1→V2 assertions

### S80.5 — Workspace Schema V8 + Deterministic Tests

**Schema bump:** `WORKSPACE_SCHEMA_VERSION` 7 → 8 (documents new persisted fields: active_route, portfolio_tab, snapshot V2).

**New tests (12 added):**

| Test | Validates |
|------|-----------|
| `test_view_model_resolves_chart_display` | Chart_Component fields in view model |
| `test_view_model_resolves_indicators` | Indicator_Component fields in view model |
| `test_view_model_resolves_indicator_params` | Indicator_Params fields in view model |
| `test_view_model_chart_is_value_copy` | Mutation isolation (value copy, not pointer) |
| `test_snapshot_captures_chart_display` | Packed chart_display in snapshot cells |
| `test_snapshot_captures_indicator_flags` | Packed indicator_flags in snapshot cells |
| `test_snapshot_captures_active_route` | Route ordinal in snapshot |
| `test_layout_v6_roundtrip_deterministic` | V6 serialize→restore→re-serialize byte identity |

**Files:**
- `client/src/core/app/workspace_schema.odin` — version + doc update
- `client/src/core/app/cell_view_model_test.odin` — 12 new tests

## Test Results

| Suite | Count | Status |
|-------|-------|--------|
| md_common | 402 | ALL PASS |
| app | 25 | ALL PASS (was 13, +12 new) |
| services | 148 | ALL PASS |
| **Total** | **575** | **ZERO regressions** |

## Architecture Invariants

1. **Cell_View_Model is a value snapshot** — mutations to ECS components after resolution don't leak into the rendered frame
2. **Route persistence skips contextual routes** — Instrument_Overview requires a target and is not restored
3. **Compare mode init is deterministic** — filter state derives from focused cell, not hardcoded constants
4. **Snapshot V2 is forward-compatible** — parsers ignore unknown trailing fields in CL/header lines
5. **V6 layout format unchanged** — no new per-cell fields in the layout string; route/tab use settings keys
6. **Portfolio/readiness flows untouched** — zero changes to S74-S78 data layer or polling

## What's NOT Persisted (By Design)

| State | Reason |
|-------|--------|
| Compare_State | Ephemeral session — meaningless after restart |
| View_Component (scroll/zoom) | Reseeded from data on connect |
| GetRange_Component | Transient backfill state |
| Overlay_State | Modal dialogs are transient |
| Connection/Telemetry/Error | Runtime-only |

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Route restore to page with stale data | Page `on_enter` lifecycle triggers fresh fetch |
| Portfolio tab restore before backend connect | Poll gate checks connection status before fetch |
| Snapshot V2 breaks external parsers | Version field in header allows parser branching |
| Cell_View_Model size increase (3 structs) | Value types, stack-allocated, ~200 bytes total |

## Acceptance Criteria

- [x] Cell_View_Model includes chart + indicator + params
- [x] Active route survives restart (except Instrument_Overview)
- [x] Portfolio tab survives restart
- [x] Compare mode inherits focused cell display settings
- [x] Runtime snapshot captures chart_display, indicator_flags, route
- [x] V6 layout round-trip is byte-identical (deterministic test)
- [x] 575 tests pass, zero regressions
- [x] Zero wire protocol changes
- [x] Zero backend changes
- [x] S74-S78 portfolio/readiness flows unaffected
