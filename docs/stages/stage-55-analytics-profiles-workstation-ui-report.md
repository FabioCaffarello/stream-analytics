# Stage 55 — Analytics & Profiles Workstation UI

**Date:** 2026-03-07
**Branch:** `codex/s9-legacy-removal-cutover`
**Status:** COMPLETE

## Summary

Evolved the analytics and profile widget UX from basic cell rendering to a coherent, inspectable workstation surface with full context menu support, grouped widget catalog, interactive cell header controls, and a dashboard inspector panel.

## Changes

### 1. `cell_context_menu.odin` — Complete widget menu + analytics sub-kinds

**Before:** 10 of 12 widget types shown (Session_VPVR, TPO missing). Analytics shown as single generic entry without sub-kind picker.

**After:** All 12 widget types with grouped dividers:
- Chart widgets (Candle, Stats, Counter, Heatmap, VPVR, Trades, Orderbook, DOM, Empty)
- Analytics sub-kinds with divider: OI: Open Interest, DV: Delta Volume, CVD, BS: Bar Stats
- Profiles with divider: Session VPVR, TPO Profile
- Actions: Add Cell, Remove, Expand/Span controls

Clicking an analytics sub-kind sets both `widget_kind=.Analytics` and `analytics_kind` in the action.

### 2. `overlays.odin` — Grouped widget catalog

**Before:** Flat 3-col grid of 14 entries with no visual grouping.

**After:** Three category sections with headers:
- **CHART** (8 widgets, 4-col grid)
- **ANALYTICS** (4 widgets, 4-col grid)
- **PROFILES** (2 widgets, 4-col grid)

`catalog_render_group` helper eliminates duplication across sections.

### 3. `build_cell.odin` — Interactive analytics header controls

**Before:** Static muted "OI"/"DV"/"CVD"/"BS" label in cell header, no interaction.

**After:**
- **Kind badge** (cyan accent): clickable, cycles OI→DV→CVD→BS→OI
- **History toggle** ("H" button): toggles `show_history` per cell
- Session profile cells get dedicated SVPVR/TPO labels
- Non-analytics cells unchanged

### 4. `build_dashboard.odin` — Analytics inspector in detail panel

New collapsible **ANALYTICS (N)** section between STREAMS and LAYERS:
- Lists all analytics and profile cells by index
- Analytics cells: shows kind name + history toggle state
- Session VPVR cells: shows POC price
- TPO cells: shows POC price + period count
- Focused cell highlighted with blue background
- Section only rendered when analytics/profile cells exist

### 5. `actions_cell_mutations.odin` — Extended Set_Cell_Widget

`apply_set_cell_widget_action` now accepts full `UI_Action` (was 3 params). When widget is `.Analytics`, sets `analytics_kind` and `show_history=true`.

### 6. `components.odin` — New section state

`section_analytics: ui.Section_State` added to `UI_Chrome_State`.

## Architecture

```
Context Menu (right-click)
    ├── Chart widgets (0-7 + Empty)
    ├── Analytics sub-kinds (OI/DV/CVD/BS) ──► Set_Cell_Widget + analytics_kind
    ├── Profiles (Session VPVR, TPO)
    └── Actions (Add/Remove/Span)

Widget Catalog (modal, 2-step)
    ├── CHART section (8 entries, 4-col)
    ├── ANALYTICS section (4 entries, 4-col)
    └── PROFILES section (2 entries, 4-col)

Cell Header (analytics cells)
    ├── Venue:Symbol badge (clickable → stream picker)
    ├── Composition badge + Health dot
    ├── History toggle [H] (click → toggle show_history)
    └── Kind badge [OI] (click → cycle sub-kind)

Dashboard Detail Panel
    ├── MARKET INFO
    ├── STREAMS (collapsible)
    ├── ANALYTICS (N) (collapsible, S55)  ◄── NEW
    ├── LAYERS (collapsible)
    └── PANELS (collapsible)
```

## Metrics

| Metric | Value |
|--------|-------|
| Lines added | 308 |
| Lines removed | 89 |
| Net new | +219 |
| Files changed | 7 |
| New procs | 1 (`catalog_render_group`) |
| New state fields | 1 (`section_analytics`) |
| Wire changes | 0 |
| New mutable state | 0 (section state is UI chrome only) |
| Compilation | Clean (`odin check`) |
| Commits | 5 |

## Problems Fixed

| # | Problem | Fix |
|---|---------|-----|
| P1 | Context menu missing Session_VPVR, TPO | All 12 types now shown |
| P2 | Context menu no analytics sub-kind picker | 4 sub-kinds listed directly |
| P3 | Widget catalog flat, ungrouped | 3 category headers |
| P4 | No analytics section in dashboard detail | Collapsible ANALYTICS section |
| P5 | Analytics kind label not interactive | Clickable cycle badge |
| P6 | No history toggle in UI | H button in cell header |
| P7 | No profile inspector | POC/period info in detail panel |
