# Stage 52 — UI Shell Architecture

**Date:** 2026-03-07
**Branch:** `codex/s9-legacy-removal-cutover`
**Commits:** 4

## Objective

Decompose the 880-line `build_ui.odin` god function into a clean shell
architecture with separated concerns: shell orchestration, page rendering,
status bar, overlay dispatch, and shared primitives.

## Problems Addressed

| # | Problem | Resolution |
|---|---------|------------|
| P1 | `build_ui.odin` 880-line god function | Reduced to 190 lines (78% reduction) |
| P2 | Dashboard workspace grid inline in shell | Extracted to `build_dashboard_grid()` |
| P5 | Connection status duplicated 5x | Canonical `resolve_conn_status_display()` |
| P6 | Status bar 300+ lines inline | Extracted to `draw_status_bar()` |
| P7 | Toast + OSD inline in shell | Extracted to `draw_toast_osd()` |
| P8 | No overlay dispatch abstraction | `draw_shell_overlays()` with z-order docs |

## Architecture After S52

```
build_ui.odin (190 lines) — Shell orchestrator
├── Zen mode fade logic
├── Top bar: draw_top_bar()
├── Sidebar: nav rail + detail panel dispatch
├── Page dispatch:
│   ├── Dashboard: build_focus_mode / build_compare_mode / build_dashboard_grid
│   ├── Markets: build_markets_page
│   └── Settings: build_settings_page
├── Status bar: draw_status_bar()
└── Overlays: draw_shell_overlays()
    ├── Health panel
    ├── Help overlay
    ├── Exchange manager
    ├── Cell stream picker
    ├── Widget catalog
    ├── Stream picker
    └── Toast + TF OSD
```

## Files Changed

| File | Change |
|------|--------|
| `shell_common.odin` | **NEW** — `Conn_Status_Display`, `resolve_conn_status_display`, `modal_backdrop`, `draw_shell_overlays` |
| `build_ui.odin` | 880 → 190 lines: pure shell orchestrator |
| `build_status.odin` | +360 lines: `SHELL_STATUS_BAR_H`, `draw_status_bar`, `draw_toast_osd`, `cache_string` |
| `build_dashboard.odin` | +330 lines: `build_dashboard_grid`, `col_weight_sum`, `row_weight_sum` |
| `top_bar.odin` | Replaced inline conn status with shared call |
| `build_markets.odin` | Replaced inline conn status with shared call |
| `overlays.odin` | Replaced inline conn status with shared call |
| `settings.odin` | Replaced inline conn status with shared call |

## Invariants Preserved

- Zero wire protocol changes
- Zero new mutable state
- Zero render output changes (pixel-identical)
- All existing tests pass (md_common: 402, services: 80)
- Retained Command List (RCL) pipeline unchanged
- Z-layer compositing order unchanged
- Action queue dispatch unchanged

## Commit History

1. `refactor(client): S52 extract shell primitives and connection status`
2. `refactor(client): S52 extract status bar, toast, and OSD from shell`
3. `refactor(client): S52 extract dashboard grid into build_dashboard_grid`
4. `refactor(client): S52 extract overlay dispatch into shell_common`

## Metrics

- **build_ui.odin:** 880 → 190 lines (78% reduction)
- **Conn status duplication:** 5x → 1x canonical
- **Shell concerns separated:** 5 (orchestration, pages, status, overlays, primitives)
- **New shared procs:** 6 (`resolve_conn_status_display`, `current_conn_status_display`, `modal_backdrop`, `draw_status_bar`, `draw_toast_osd`, `draw_shell_overlays`)
