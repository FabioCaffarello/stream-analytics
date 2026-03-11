# Stage 142 — Dashboard Interaction & Utility Pass

**Date:** 2026-03-09
**Status:** COMPLETE

## Objective

Refine dashboard ergonomics and reduce operational friction before the orderflow block. Preserve chart principal + context stack + pane roles.

## Changes

### 1. New Keyboard Shortcuts

| Shortcut | Action | Rationale |
|----------|--------|-----------|
| **D** | Cycle Context Tab | Previously unbound S119 action — now one-key context switching |
| **Shift+C** | Toggle CVD subplot | Analytics subplots had no keyboard access |
| **Shift+V** | Toggle Delta Volume subplot | Same |
| **Shift+D** | Toggle Open Interest subplot | Same |
| **Left/Right** (in Compare) | Cycle compare pane focus | Was mouse-only; arrow keys natural for pane navigation |

### 2. Shift Modifier Guards

- `C` key: plain = Compare toggle, Shift = CVD toggle
- `V` key: plain = VWAP toggle, Shift = Delta Volume toggle
- `D` key: plain = Context Tab cycle, Shift = OI toggle, Ctrl = Runtime Snapshot (unchanged)

### 3. Help Overlay Overhaul

Expanded from 22 to 33 entries, organized by category:
- **Navigation**: Tab, 1-9, Shift+1-9, D, S, G, Escape
- **Modes**: C, F, Z, Left/Right (compare)
- **Indicators**: M, B, V/Shift+V, R, I, H, J, K, Shift+C, Shift+D
- **Chart**: Scroll, Ctrl+Scroll, Home/End, Dbl-click, Shift+Drag, Del
- **System**: Ctrl+K, Ctrl+H/J/R, Ctrl+D, ?

Panel resized from 280x500 to 300x720 (clamped to viewport).

### 4. Actionable Status Overlay Hints

| State | Old Hint | New Hint |
|-------|----------|----------|
| No_History | "Historical backfill not available" | "Ctrl+R to retry backfill" |
| Error | "Recovery in progress" | "Ctrl+R to resync" |

Users now see an actionable shortcut instead of a passive description.

## Files Changed

| File | Change |
|------|--------|
| `app.odin` | +5 UI_Action_Kind entries |
| `actions.odin` | Shift guards on C/V, D key bindings, compare pane cycling, new action handlers |
| `overlays.odin` | Help overlay expanded to 33 entries, panel resized |
| `shell_common.odin` | Actionable hints for No_History and Error states |
| `interaction_test.odin` | **NEW** — 7 tests for compare pane cycling + analytics toggles |

## Tests

- 402 tests pass (395 existing + 7 new)
- New tests: compare pane wrap-around (next/prev), normal cycling, inactive noop, CVD/DeltaVol/OI global toggle
- Zero regressions

## Acceptance Criteria

- [x] Dashboard more ergonomic — analytics subplots keyboard-accessible
- [x] Less operational friction — context tab cycling, compare pane arrows
- [x] Better continuous usability — actionable hints, comprehensive help overlay
- [x] No cosmetic-only changes — every change reduces operational friction
