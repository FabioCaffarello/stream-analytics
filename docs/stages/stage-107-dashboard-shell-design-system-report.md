# Stage 107 — Dashboard Shell & Design System

**Date:** 2026-03-09
**Status:** COMPLETE
**Objective:** Consolidate the design system and visual structure of the dashboard for consistent, high-density, low-noise UX with explicit system state feedback.

---

## Summary

S107 delivers a unified design system layer across the dashboard shell. The changes are strictly visual — no data flow, protocol, or state management changes. All modifications are additive and zero-allocation in the hot path.

## Changes

### 1. Design Token Additions (`styles.odin`)

**Semantic state colors** — 5 new tokens for pane visual states:
- `COL_STATE_LOADING` — blue-ish (0.4, 0.7, 0.95, 0.7)
- `COL_STATE_EMPTY` — dim white (1.0, 1.0, 1.0, 0.2)
- `COL_STATE_SEEDING` — yellow (0.98, 1.0, 0.412, 0.6)
- `COL_STATE_OFFLINE` — very dim (1.0, 1.0, 1.0, 0.25)
- `COL_STATE_ERROR` — red (0.965, 0.278, 0.365, 0.8)

**Spacing:** `SPACING_2XL :: f32(24)`

**Cell header accent:** `CELL_HDR_ACCENT_H :: f32(2)` — accent line height for pane headers.

### 2. Pane Visual State System (`shell_common.odin`)

**`Pane_Visual_State` enum** — 6 canonical states:
- `Active` — normal rendering, no overlay
- `Loading` — connected, composition Range_Pending
- `Seeding` — connected, composition Live_Only or Backfilled
- `Empty` — no stream bound or composition Empty
- `Offline` — connection offline
- `Error` — desync or critical health

**`resolve_pane_visual_state`** — pure resolver from `Cell_Surface_View` + connection + stream state.

**`draw_pane_state_overlay`** — renders centered state label over semi-transparent backdrop for non-Active panes. Uses semantic state colors with `FONT_SIZE_SM` bold text.

### 3. Enhanced Pane Headers (`build_cell.odin`)

- **Composition-colored accent line** — 2px strip at bottom of each cell header, colored by composition stage (green=composed, yellow=live_only, amber=pending/backfilled, dim=empty). Replaces the previous 1px white divider.
- **Widget switcher button** — "W" icon button in header opens cell context menu for quick widget type switching. Hidden when cell width < 100px.
- **State overlay integration** — `draw_pane_state_overlay` called after widget render for non-Active panes (Loading.../Seeding.../No Data/Offline/Error centered in pane body).
- **Layout accounting** — all header element positions now account for `switcher_inset` to prevent overlap.

### 4. Connection Badge Enhancement (`top_bar.odin`)

- **Stronger pill background** — alpha increased from 0.12/0.22 to 0.18/0.30 for better visibility.
- **Pill border** — `draw_rect_stroke` with connection-colored border at 0.35 alpha.
- **TF change underline** — 2px cyan underline on active TF segment that lingers 10 frames after timeframe change (alpha fades from 0.4 to 0).

### 5. Status Bar Visual Improvements (`build_status.odin`)

- **Health status pill** — LIVE/LAG/DESYNC/OFFLINE wrapped in pill with semantic background (0.15 alpha) + border (0.3 alpha). Previously plain text.
- **CTX composition pill** — CTX:COMPOSED/PENDING/etc. wrapped in pill with composition-colored background (0.12 alpha). Previously plain text.

## Tests

8 new tests in `marketdata_test.odin`:
- `test_pane_visual_state_offline` — Offline connection → Offline state
- `test_pane_visual_state_desync` — Desync stream → Error state
- `test_pane_visual_state_critical` — Critical health → Error state
- `test_pane_visual_state_empty_unbound` — Empty+unbound → Empty state
- `test_pane_visual_state_range_pending` — Range_Pending → Loading state
- `test_pane_visual_state_live_only` — Live_Only → Seeding state
- `test_pane_visual_state_backfilled` — Backfilled → Seeding state
- `test_pane_visual_state_composed` — Composed → Active state

**Total:** 128 tests (120 existing + 8 new), all passing.

## Files Modified

| File | Change |
|------|--------|
| `client/src/core/ui/styles.odin` | +5 state colors, +SPACING_2XL, +CELL_HDR_ACCENT_H |
| `client/src/core/app/shell_common.odin` | +Pane_Visual_State enum, resolver, overlay drawer |
| `client/src/core/app/build_cell.odin` | Accent line, widget switcher, state overlay, layout fixes |
| `client/src/core/app/top_bar.odin` | Connection pill border, TF underline accent |
| `client/src/core/app/build_status.odin` | Health pill, CTX pill |
| `client/src/core/app/marketdata_test.odin` | +8 pane visual state tests |

## Design System Token Summary

| Category | Tokens |
|----------|--------|
| **Surfaces** | COL_SURFACE_0..3 (elevation) |
| **Text** | COL_TEXT_PRIMARY/SECONDARY/MUTED (alpha-based) |
| **Borders** | COL_BORDER_SUBTLE/STRONG, COL_DIVIDER |
| **Status** | COL_GREEN/RED/WARNING/YELLOW_ACCENT |
| **Pane States** | COL_STATE_LOADING/EMPTY/SEEDING/OFFLINE/ERROR |
| **Accents** | COL_ACCENT_ORANGE/CYAN, COL_BLUE, COL_PURPLE |
| **Spacing** | XS(2), SM(4), MD(8), LG(12), XL(16), 2XL(24) |
| **Typography** | XS(11), SM(13), MD(16), LG(20), XL(24), 2XL(28) |
| **Fonts** | Default, Mono, Bold |

## Visual State Flow

```
Connection Offline ──────────────────────────────────> Offline
Stream Desync ───────────────────────────────────────> Error
Health Critical ─────────────────────────────────────> Error
Empty + Unbound ─────────────────────────────────────> Empty
Composition Range_Pending ───────────────────────────> Loading
Composition Live_Only/Backfilled ────────────────────> Seeding
Otherwise ───────────────────────────────────────────> Active (no overlay)
```

## Acceptance Criteria

- [x] Dashboard with consistent visual language
- [x] Standardized pane headers with composition accent + widget switcher
- [x] Design tokens defined: spacing, typography, color system
- [x] Visual states standardized: loading, empty, offline, seeding, error
- [x] Visual feedback improved: active TF, streams, WS connection, health status
- [x] Zero regressions, zero wire-breaking changes
- [x] All 128 tests passing
