# Stage 145 — Dashboard Recovery UX & Operator Trust

## Objective

Make the UI communicate failures, degradation, and recovery professionally, increasing operational confidence. Operators should understand terminal state without ambiguity, distinguish degraded from broken, and have clear action affordances.

## Changes

### 1. Reliability-Aware Overlay Messages (shell_common.odin)

`draw_pane_state_overlay` now accepts `reliability` and `recovery_attempts` parameters. The `Degraded` and `Error` overlays differentiate sub-causes:

| Reliability | Title | Sub-label | Hint |
|---|---|---|---|
| Stale_Recovering | "Recovering" | "Auto-recovery attempt N/3" | "Resubscribing, please wait..." |
| Stale_Unrecoverable | "Stale" | "Data stale, auto-recovery exhausted" | "Ctrl+R to resync manually" |
| Degraded_Aging | "Aging" | "Data aging, may become stale" | "Monitoring..." |
| Desync | "Desync" | "Stream integrity lost" | "Ctrl+R to resync" |
| Offline | "Offline" | "Cached data shown, connection lost" | "Reconnecting automatically..." |
| Manual_Resync | "Manual Resync" | "Recovery exhausted, manual action needed" | "Ctrl+R to resync" |

Error overlay similarly differentiates Desync, Manual_Resync, Stale_Unrecoverable, and Offline causes.

### 2. Recovery Status Badge on Cell Headers (shell_common.odin)

New `draw_recovery_badge` proc renders a compact tinted pill next to the health dot:

- **Stale_Recovering**: "REC N/3" (warning color) — shows attempt progress
- **Stale_Unrecoverable / Manual_Resync**: "STALE" (red) — manual action needed
- **Desync**: "DSYNC" (red) — integrity loss
- **Degraded_Aging**: "AGING" (warning) — early warning
- **Reliable / Offline**: hidden (no badge noise)

Wired into both `render_cell_widget` (legacy path) and `render_pane_via_contract` (S109 path).

### 3. Compare Pane Badge Upgrade (build_compare.odin)

Replaced the S42 RCVR/XHST inline badge with the new `draw_recovery_badge`, providing consistent reliability-aware badges across all cell types.

### 4. Cell_Surface_View Enrichment (stream_slots.odin)

Added `recovery_attempts: u8` to `Cell_Surface_View`. Populated from `apply_state.recovery_attempts` in both `resolve_cell_surface_view_with_stores` and `resolve_compare_surface_view`.

### 5. Operator-Friendly Error Messages (health.odin)

- Auto-recovery: "Recovering: attempt N/3" (was "STALE: auto-recovery resubscribe")
- Exhaustion: "Recovery exhausted. Ctrl+R to resync" (was "DESYNC: stale recovery exhausted")

### 6. Offline Overlay Progress Bar (shell_common.odin)

Added `show_progress = true` to the Offline overlay state, showing an animated reconnection indicator instead of static text.

## Files Modified

| File | Change |
|---|---|
| `app/shell_common.odin` | +`draw_recovery_badge`, enriched `draw_pane_state_overlay` with reliability/recovery params, Offline progress bar |
| `app/build_cell.odin` | Pass reliability + recovery_attempts to overlay, wire recovery badge into both render paths |
| `app/build_compare.odin` | Replace S42 RCVR/XHST with `draw_recovery_badge` |
| `app/stream_slots.odin` | Add `recovery_attempts` to `Cell_Surface_View`, populate in both resolvers |
| `app/health.odin` | Operator-friendly recovery error messages |
| `app/interaction_test.odin` | +8 S145 tests |

## Tests

- 418 app tests (410 prior + 8 new S145), all passing
- 438 md_common tests, all passing
- Zero regressions

## Design Decisions

1. **No new overlays or modals** — enriched existing overlays with context-specific messaging. Reduces cognitive load by keeping the same visual patterns.
2. **Badge over toast** — recovery progress shown as a persistent header badge rather than ephemeral toast, since recovery can take 15-60s and the operator needs continuous visibility.
3. **Reliability enum as UX driver** — the `Stream_Reliability` enum (S143) now directly drives UI messaging, creating a single truth path from health model to operator communication.
4. **Default parameters** — `reliability` and `recovery_attempts` default to safe values (`Reliable`, `0`) so all existing callers compile unchanged.
