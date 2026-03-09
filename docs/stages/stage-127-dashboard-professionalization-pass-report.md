# Stage 127 — Dashboard Professionalization Pass

**Date:** 2026-03-09
**Status:** COMPLETE
**Tests:** 908 (321 app + 186 services + 401 md_common) — all green
**Build:** WASM clean, zero regressions

## Objective

Elevate the dashboard from "good technical system" to professional trading terminal. Reinforce visual hierarchy, legibility, and operational ergonomics without cosmetic redesign.

## Changes

### 1. Design Token Additions (`styles.odin`)
- **`COL_SURFACE_0H`** — half-step surface between app background (SURFACE_0) and panel backgrounds (SURFACE_1), used for toolbar and chrome bars to create subtle but clear visual hierarchy
- **`COL_TEXT_DIM`** — very dim text token (0.22 alpha) for widget type labels in headers
- **`CHROME_SEPARATOR_ALPHA`** — shared constant (0.10) for vertical separator lines between toolbar groups

### 2. Top Bar Refinement (`top_bar.odin`)
- **Full-width bottom accent line** — replaced split 50/50 accent (0.30/0.08 alpha) with unified full-width line at 0.20 alpha; cleaner professional edge
- **Logo separator** — vertical divider after "MR" logo box separates branding from functional controls
- Both normal and compact (zen) modes updated consistently

### 3. Workspace Toolbar Elevation (`workspace_toolbar.odin`)
- **Background upgraded** from `SURFACE_0` (same as app bg) to `SURFACE_0H` — toolbar now visually distinguishable from workspace area
- **Three vertical separators** between functional groups:
  - Price section | Stream navigation
  - Stream navigation | TF selector
  - Layout presets | Indicator pills
- **Bottom accent** changed from partial-width blue line to full-width subtle border — cleaner termination
- **Volume badge** opacity increased (0.06→0.08) for readability

### 4. Cell Header Professionalization (`build_cell.odin`)
- **Header height 20→22px** — more breathing room for badges and controls
- **Stream badge padding** refined (inset 2,1→3,2) for better visual balance
- **Accent line alpha** increased (0.5→0.6) for stronger composition status visibility
- Both `render_cell_widget` and `render_pane_via_contract` paths updated identically

### 5. Composition Badge Upgrade (`shell_common.odin`)
- **Background pill** added behind PEND/BFILL/LIVE/COMP labels — tinted with composition color at 0.12 alpha
- Previously floating text labels are now enclosed in a subtle colored pill, dramatically improving readability against dark header backgrounds
- Used by both dashboard cells and compare mode panes

### 6. Health Dot Enhancement (`shell_common.odin`)
- **Background ring** added (2px padding) behind health indicator squares
- Ring uses health color at 0.10 alpha, creating a subtle halo effect
- Improves visibility of small (6px) health squares against dark surfaces

### 7. Compare Mode Consistency (`build_compare.odin`)
- **Pane headers** elevated from 18→20px with SURFACE_2 background (matching main dashboard)
- **Accent lines** added to compare pane headers (composition-colored, matching main cells)
- **Control bar** upgraded with SURFACE_0H background and bottom border

### 8. Nav Rail Lightening (`sidebar.odin`)
- **Full box stroke removed** — 4-line border replaced with single subtle bottom edge
- **Active indicator** refined: 4→3px width, vertically inset for cleaner look
- **Border alpha** reduced (0.12→0.06 default) for lighter visual weight
- Result: navigation feels more professional, less like a debug tool

### 9. Segmented Control Refinement (`controls.odin`)
- **Border weight reduced** on TF selector segments
- **Borders now conditional** — only drawn on selected/hovered segments, unselected segments are borderless
- **Active segment** slightly desaturated (0.60→0.55 blue alpha) for less aggressive look
- Result: TF selector feels integrated rather than heavy

### 10. Context Stack Polish (`context_stack.odin`)
- **Role badge height** increased (16→18px) for breathing room
- **Role badge background** uses SURFACE_0H instead of SURFACE_0
- **Tinted label pill** added behind role text (CHART/AUX/CTX) for visual weight

## Files Modified

| File | Change |
|------|--------|
| `ui/styles.odin` | +2 color tokens, +1 alpha constant |
| `app/top_bar.odin` | Full-width accent, logo separator |
| `app/workspace_toolbar.odin` | Elevated bg, 3 separators, bottom border |
| `app/build_cell.odin` | Header 20→22px, badge refinement |
| `app/shell_common.odin` | Composition pill bg, health dot ring |
| `app/build_compare.odin` | Header 18→20px, accent lines, ctrl bar |
| `app/context_stack.odin` | Role badge pill, taller strip |
| `ui/sidebar.odin` | Lighter nav rail borders |
| `ui/controls.odin` | Conditional segment borders |

## Design Philosophy

Every change follows three principles:
1. **Hierarchy through elevation** — chrome bars use intermediate surface colors to create clear spatial zones
2. **Grouping through separation** — vertical separators organize toolbar controls into logical groups
3. **Legibility through contrast** — composition badges, health dots, and accent lines all gained subtle backgrounds that improve readability without adding visual noise

No pixel was changed for cosmetic reasons alone — every modification serves operational clarity.
