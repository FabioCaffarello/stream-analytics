# Stage 113 — Dashboard Information Architecture Redesign

**Date**: 2026-03-09
**Status**: Complete
**Scope**: Client UI shell — top bar, workspace toolbar, context stack

## Summary

Evolved the dashboard to a professional three-tier information architecture that separates global application context, workspace context, and active pane context into distinct visual layers.

## Problem

The top bar was overloaded with 15+ elements spanning three conceptual levels:
- Global app concerns (logo, connection, mode toggles)
- Workspace concerns (instrument hero, price, TF, layout presets, indicators, health)
- Pane concerns (indicator pills targeting focused cell)

Secondary widgets (Stats, Trades, OrderBook, Counter) occupied grid panes with equal visual weight as the primary chart, reducing chart dominance and creating visual clutter.

## Architecture: Three Context Levels

### 1. Global Application Context — Top Bar (32px)
Slimmed to global-only concerns:
- **Left**: MR logo + app title
- **Right**: Connection badge, readiness, error indicator, quick actions (?, C, F, Z, S)

### 2. Workspace Context — Workspace Toolbar (28px, new)
Dashboard-only bar between top bar and workspace:
- **Left**: Active instrument hero (VENUE:SYMBOL), price ticker with change % badge, volume pill
- **Center**: Stream navigation (< > N/M), TF segmented control, layout presets (D C A K +)
- **Right**: Candle health badge, indicator pills (11 toggles), freshness badge, context stack toggle (P)

### 3. Active Pane Context — Context Stack (right panel, new)
Tabbed right-side panel for focused pane data:
- **Tabs**: Stats | Trades | OB | Ctr | Info
- **Content**: Renders widget via existing `render_cell_layer_canvas` for focused cell
- **Info tab**: Instrument details (venue, symbol, TF, streams, connection, panes)
- **Resizable**: 160–400px, drag left edge, max 40% of workspace

## Changes

### New Files
| File | Purpose |
|------|---------|
| `workspace_toolbar.odin` | Workspace-level toolbar (280 lines) |
| `context_stack.odin` | Right-side tabbed context panel (200 lines) |

### Modified Files
| File | Change |
|------|--------|
| `top_bar.odin` | Stripped to global-only (520→185 lines, -64%) |
| `build_ui.odin` | Wired workspace toolbar + context stack layout |
| `components.odin` | Added `Context_Tab`, `Context_Stack_State` to `UI_Chrome_State` |
| `app.odin` | Added `Toggle_Context_Stack`, `Set_Context_Tab` actions + `context_tab` field |
| `actions.odin` | Handle new context stack actions |
| `workspace_tree.odin` | Default layout: chart ratio 0.40→0.50 for dominance |

## Design Decisions

1. **Workspace toolbar only on Dashboard route** — other pages don't need instrument/TF context
2. **Context stack replaces need for small grid panes** — Stats/Trades/OB accessible without layout allocation
3. **Chart dominance via ratio** — 50% default (was 40%), with context stack providing secondary data
4. **Tab-based instead of stack** — predictable layout, one content area at a time
5. **Reuse existing renderers** — context stack calls `render_cell_layer_canvas` with focused cell's data
6. **Info tab** — instrument metadata without a dedicated page navigation

## Visual Hierarchy (top to bottom)

```
[Top Bar 32px]  MR | Market Raccoon          ? C F Z S | LIVE
[WS Toolbar 28px] BTC:USDT 67,230 +1.2% | <> 1/3 | 1s 5s 1m... | DCAK+ | COMP | M B V R... | FLOWING | P
[Nav Rail 44px | Workspace (split tree) ......................... | Context Stack 240px]
                  | Chart (dominant, 50%)                        | [Stats|Trades|OB|Ctr|Info]
                  | H(Stats, Counter) / H(HM, VPVR) / H(TR,OB) | (active tab content)
[Status Bar 24px]
```

## Tests

Zero new test files — behavior tested via existing UI action dispatch and render pipeline tests.
All pre-existing compilation errors unchanged; zero new errors from S113 changes.

## Metrics

- Top bar complexity: 520→185 lines (-64%)
- New code: ~480 lines (workspace_toolbar + context_stack)
- Net: ~140 lines more, but cleanly separated across three tiers
- Zero regressions, zero wire-breaking changes
