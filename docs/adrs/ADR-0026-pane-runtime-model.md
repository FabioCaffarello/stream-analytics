# ADR-0026 — Pane Runtime Model

**Status:** Accepted
**Date:** 2026-03-08
**Owners:** Core maintainers
**Supersedes:** —
**Related:** ADR-0024, ADR-0025, ADR-0027

## Context

Cells in the current `Entity_World` are identified by array index (0–11) with parallel component arrays (`widgets[]`, `bindings[]`, `views[]`, `indicators[]`, `ind_params[]`, `charts[]`, `subplots[]`, `spans[]`, `timeframes[]`, `analytics[]`, `getranges[]`). This has problems:

1. **Index instability.** Removing a cell shifts indices, breaking references held by compare mode, focus tracking, and keybindings.
2. **No lifecycle hooks.** Adding/removing a cell is a manual multi-array operation with no centralized validation.
3. **Viewport not owned.** Scroll, zoom, and crosshair state (`View_Component`) lacks connection to the resolved layout rect, causing stale viewport calculations on resize.
4. **Focus is a bare index.** `world.focused` is an `int` with no validation that the focused cell still exists or is visible.

A first-class **Pane** model with stable IDs and explicit lifecycle replaces the implicit parallel-array entity.

## Decision

### 1. Pane Identity

```
Pane_ID :: distinct u16   // workspace-local, monotonic (never reused within a workspace session)
PANE_ID_NONE :: Pane_ID(0)

Pane :: struct {
    id:          Pane_ID,
    alive:       bool,

    // Widget host (ADR-0027)
    widget:      Widget_Host,

    // Data binding (ADR-0028)
    binding:     Stream_Binding,
    tf_override: i8,             // -1 = inherit from workspace data context

    // View state
    view:        View_State,

    // Indicators & chart config (widget-specific)
    indicators:  Indicator_Flags,
    ind_params:  Indicator_Params,
    chart:       Chart_Config,
    subplots:    Subplot_Config,
    analytics:   Analytics_Config,
}
```

### 2. Pane Lifecycle

```
                ┌─────────┐
                │ Allocate │  assign ID, zero-init
                └────┬────┘
                     │
                ┌────▼────┐
                │  Bind    │  attach widget, resolve stream binding
                └────┬────┘
                     │
          ┌──────────▼──────────┐
          │      Active         │  receives data, renders, responds to input
          │  (update + render)  │
          └──────────┬──────────┘
                     │
                ┌────▼────┐
                │ Dispose  │  release widget, unsubscribe, clear view state
                └────┬────┘
                     │
                ┌────▼────┐
                │  Free    │  return slot to pool (ID never reused this session)
                └─────────┘
```

State transitions are driven by workspace actions (ADR-0024), not direct field mutation.

### 3. Focus Model

```
Focus_State :: struct {
    active:   Pane_ID,       // currently focused pane
    previous: Pane_ID,       // for toggle-back (Alt+Tab style)
    locked:   bool,          // if true, focus does not follow mouse hover
}
```

Focus rules:
- Click on pane header → set focus.
- Keyboard navigation (Ctrl+Arrow) → move focus in tree spatial order.
- Pane removal → focus moves to `previous`, or nearest sibling if `previous` is also gone.
- Focus lock (e.g., during drag-resize) prevents accidental focus changes.

### 4. View State (Viewport)

```
View_State :: struct {
    // Scroll & zoom (candle-relative)
    scroll_x:     f32,    // horizontal scroll offset (candles)
    zoom_level:   f32,    // candle width multiplier (1.0 = default)

    // Widget-specific scroll
    ob_scroll_y:  f32,    // orderbook vertical scroll
    trades_scroll_y: f32, // trades vertical scroll

    // Crosshair
    crosshair_x:  f32,    // cursor x within pane (local coords)
    crosshair_y:  f32,    // cursor y within pane (local coords)
    crosshair_active: bool,

    // Resolved rect (set by tree layout each frame)
    rect:         Rect,   // current frame's resolved position
    rect_valid:   bool,   // false if pane not visible this frame
}
```

- `rect` is written by `resolve_tree_layout()` every frame — never stale.
- Input handlers use `rect` for hit testing instead of re-resolving from grid.
- Zoom/scroll are pane-local. Cross-pane sync (linked scrolling) is an opt-in behavior applied after independent resolution.

### 5. Resize

Pane resize is handled by the split tree (ADR-0025), not by the pane itself. The pane receives its `rect` passively. However, panes can declare minimum size constraints:

```
pane_min_size :: proc(widget_kind: Widget_Kind) -> (min_w, min_h: f32) {
    switch widget_kind {
    case .Candle:    return 200, 150
    case .Orderbook: return 120, 100
    case .Trades:    return 120, 80
    case .DOM:       return 140, 120
    case .Stats:     return 100, 60
    // ...
    }
}
```

Tree layout enforces these minimums when resolving split ratios.

### 6. Pane Pool

```
Pane_Pool :: struct {
    panes:       [PANE_MAX]Pane,      // 16 max
    count:       u8,
    next_id:     u16,                 // monotonic counter for ID generation
}

pane_pool_alloc :: proc(pool: ^Pane_Pool) -> (^Pane, Pane_ID)
pane_pool_free  :: proc(pool: ^Pane_Pool, id: Pane_ID)
pane_pool_get   :: proc(pool: ^Pane_Pool, id: Pane_ID) -> ^Pane  // nil if not alive
```

- Fixed-capacity, no heap allocation.
- Linear scan for lookup (16 max — negligible cost at 60fps).
- `next_id` wraps at `u16_max` (65535 allocations per session — sufficient).

### 7. Invariants

- **Pane_ID is stable** for the lifetime of the pane. No index shifting on removal.
- **One widget per pane.** Stacking multiple widgets uses a Stack tree node (ADR-0025) with one pane per tab.
- **Pane does not know its position in the tree.** Layout is resolved top-down; pane receives its rect.
- **View_State.rect is frame-transient.** Never persisted — always recomputed from tree + viewport.

## Consequences

- Compare mode panes become regular panes — no parallel `Compare_State` arrays.
- Focus tracking is robust (stable IDs, toggle-back, locked state).
- Viewport rect is always fresh — eliminates stale-rect bugs on resize.
- Per-pane TF override, analytics kind, and indicator config are co-located — no parallel array sync.
- Panel indices (`PANEL_CANDLE=0..6`) are eliminated. Panes are identified by `Pane_ID`, not by position.

## Alternatives

1. **Keep parallel arrays with stable IDs via indirection table.** Rejected: adds complexity without solving lifecycle or viewport ownership.
2. **Dynamic allocation with pointers.** Rejected: fragmentation risk, cache-unfriendly, incompatible with Odin's value-oriented style.
3. **Generational indices (like ECS).** Rejected: over-engineered for 16-max entities. Simple monotonic ID suffices.

## Evidence

- Current `Entity_World` in `components.odin`: 11 parallel arrays, `CELL_MAX = 12`, `focused: int`.
- `View_Component` fields (scroll_x, zoom, ob_scroll_y, trades_scroll_y, crosshair) map 1:1 to `View_State`.
- `Compare_State` in `components.odin`: 23+ fields that duplicate per-cell concepts — eliminated by this model.

## Changelog

- 2026-03-08: Initial acceptance.
