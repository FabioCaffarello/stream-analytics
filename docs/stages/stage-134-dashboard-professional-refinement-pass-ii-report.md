# Stage 134 — Dashboard Professional Refinement Pass II

**Date:** 2026-03-09
**Status:** COMPLETE
**Tests:** 962 (408 md_common + 346 app + 186 services + 22 layers) — all passing, zero regressions

## Objective

Refine the dashboard UI to feel more like a professional trading workstation, reducing "system in development" feel while preserving chart-dominant layout and operational correctness.

## Changes

### 1. Chrome Elevation Unification
- **Top bar background:** `COL_SURFACE_1` → `COL_SURFACE_0H` — unified chrome band with workspace toolbar
- **Top bar accent line:** blue alpha 0.20 → 0.12 — subtler professional edge, less visual competition with content

### 2. Cell Header Refinement
- **Header height:** 22px → 24px — more breathing room, professional terminal density
- **Stream badge text:** `COL_TEXT_SECONDARY` → `COL_TEXT_PRIMARY` — instrument name is primary information, deserves full readability
- **Widget type label:** `COL_TEXT_MUTED` → `COL_TEXT_DIM` — reduced noise for secondary metadata
- **Focused cell border:** white `COL_BORDER_STRONG` → blue-tinted `COL_BLUE` at 0.25 alpha — matches compare mode, gives clearer spatial focus

### 3. Composition Badge Noise Reduction
- **COMP badge hidden in steady state** — when data is fully composed (normal operation), the "COMP" badge added visual noise. Now only transitional states (PEND, BFILL, LIVE) show badges, drawing attention only when action is relevant.

### 4. State Overlay Improvements
- **Backdrop opacity:** 0.65 → 0.72 — better text contrast on loading/seeding/error overlays
- **Progress bar height:** 3px → 2px — subtler, more professional loading indicator

### 5. Workspace Toolbar Enhancement
- **Hero instrument font:** `FONT_SIZE_XS` (11px) → `FONT_SIZE_SM` (13px) — the active instrument name now has proper visual hierarchy as the toolbar's primary element
- **Indicator pills width:** 14px → 16px — better readability and larger click/hover targets

### 6. Compare Mode Alignment
- **Compare header height:** 20px → 22px — aligned with main cell headers for visual consistency

## Files Modified

| File | Changes |
|------|---------|
| `client/src/core/app/top_bar.odin` | Chrome bg, accent line alpha |
| `client/src/core/app/build_cell.odin` | Header height, badge text, widget label, focused border |
| `client/src/core/app/shell_common.odin` | COMP badge hide, overlay backdrop, progress bar |
| `client/src/core/app/workspace_toolbar.odin` | Hero font size, pill width |
| `client/src/core/app/build_compare.odin` | Compare header height |

## Design Principles Applied

- **Information hierarchy:** primary data (instrument name, price) is most readable; secondary metadata (widget type) is subdued
- **Signal-to-noise:** badges only appear when they carry actionable information (transitional states)
- **Spatial focus:** blue-tinted focused border gives clear visual anchor without harshness
- **Consistency:** compare headers now match main cell header proportions
- **Professional density:** 24px headers balance compactness with readability (Bloomberg/CQG reference)
