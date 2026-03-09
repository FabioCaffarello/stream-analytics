# ADR-0027 — Widget Host Contract

**Status:** Accepted
**Date:** 2026-03-08
**Owners:** Core maintainers
**Supersedes:** —
**Related:** ADR-0024, ADR-0026

## Context

Widgets are currently identified by `Widget_Kind` enum (12 variants) and rendered through a dispatch table in `render_cell_widget()` / `render_cell_layer_canvas()`. The relationship between widget kind, required data channels, layer bundles, and render entry points is scattered across multiple files:

- `widget_channels.odin` — `channels_for_widget()`: widget → MD channel bitmask
- `widget_channels.odin` — `layer_bundle_for_widget()`: widget → layer bundle mask
- `build_cell.odin` — `render_cell_widget()`: header + content dispatch
- `layer_api.odin` — `Layer_Strategy`: init/event/render/reset/diagnostics
- `components.odin` — various per-cell components (indicators, chart config, analytics)

There is no unified contract that describes what a widget needs, what it produces, and how it transitions through its lifecycle. Adding a new widget kind requires touching 4+ files with no compile-time guarantee of completeness.

## Decision

### 1. Standard Widget Lifecycle

Every widget hosted in a pane follows this lifecycle:

```
Create  →  Bind  →  Update  →  Render  →  Dispose
  │          │        │          │          │
  │          │        │          │          └─ Release resources, clear state
  │          │        │          └─ Emit draw primitives (pure, no mutation)
  │          │        └─ Process frame data (apply events, resolve view model)
  │          └─ Attach data context (stream binding, TF, analytics kind)
  └─ Allocate widget-specific state, register required channels
```

### 2. Widget Host Structure

```
Widget_Host :: struct {
    kind:       Widget_Kind,
    state:      Widget_Lifecycle_State,

    // Requirements (set at Create, immutable after)
    channels:   u16,                    // MD_Channel bitmask
    bundle:     u32,                    // Layer_Bundle mask
    min_w:      f32,                    // minimum width
    min_h:      f32,                    // minimum height

    // Widget-specific config (mutable)
    config:     Widget_Config,          // union of per-kind configs
}

Widget_Lifecycle_State :: enum u8 {
    Created,     // allocated, not yet bound
    Bound,       // data context attached
    Active,      // receiving updates, rendering
    Suspended,   // workspace inactive — no updates, no render
    Disposing,   // teardown in progress
}
```

### 3. Widget Descriptor Table

A compile-time table replaces scattered `switch` statements:

```
Widget_Descriptor :: struct {
    kind:           Widget_Kind,
    label:          string,              // display name for UI
    channels:       u16,                 // required MD channels
    bundle:         u32,                 // layer bundle mask
    min_w:          f32,
    min_h:          f32,
    supports_tf:    bool,                // can override timeframe
    supports_indicators: bool,           // has indicator toggles
    supports_subplots:   bool,           // has CVD/DV/OI subplots
    supports_analytics:  bool,           // has analytics kind selector
}

WIDGET_DESCRIPTORS :: [Widget_Kind]Widget_Descriptor { ... }
```

Single source of truth. Adding a new widget kind requires one entry here — the compiler enforces exhaustive enum coverage.

### 4. Widget Config Union

```
Widget_Config :: struct #raw_union {
    candle:    Candle_Widget_Config,
    orderbook: Orderbook_Widget_Config,
    dom:       DOM_Widget_Config,
    trades:    Trades_Widget_Config,
    analytics: Analytics_Widget_Config,
    stats:     Stats_Widget_Config,
    // ... one variant per kind that needs config
}
```

Only widgets with meaningful configuration carry state. Simple widgets (Empty, Counter) use zero-size config.

### 5. Render Contract

Each widget kind provides a render procedure with a uniform signature:

```
Widget_Render_Proc :: proc(
    host:    ^Widget_Host,
    pane:    ^Pane,
    ctx:     ^Layer_Context,
    out:     ^Layer_Outputs,
)
```

This replaces the current dispatch chain (`render_cell_widget → render_cell_layer_canvas → layer strategy`). The widget render proc:

1. Reads data from `ctx.stream` (via `Layer_Context`).
2. Reads pane-local state from `pane.view`, `pane.indicators`, etc.
3. Emits primitives to `out` (draw commands, text, lines, rects).
4. **Must not mutate** `ctx` or `pane.binding`. View state mutations (scroll, zoom) are applied separately by input handlers.

### 6. Widget Catalog Integration

The existing widget catalog overlay (`Overlay_Widget_Catalog`) queries `WIDGET_DESCRIPTORS` for display name, icon, and compatibility. Adding a pane and selecting a widget kind:

1. User opens catalog → sees available widgets.
2. User selects kind → `widget_host_create(kind)` populates `Widget_Host` from descriptor.
3. Pane lifecycle (ADR-0026) calls `Bind` with data context.
4. Frame loop calls `Update` then `Render`.
5. On pane removal, `Dispose` is called.

### 7. Relationship to Layer Strategies

`Layer_Strategy` (in `layer_api.odin`) remains the low-level render abstraction for individual data layers (candles, trades, orderbook, etc.). The `Widget_Render_Proc` is higher-level — it selects which layer strategies to invoke based on the widget's `bundle` mask and current capabilities.

```
Widget_Render_Proc (ADR-0027)
  └─ activates Layer_Strategy[] based on bundle mask
       └─ Layer_Strategy.render() emits primitives
```

This preserves the existing layer architecture while adding widget-level orchestration.

### 8. Invariants

- **One Widget_Host per Pane.** Widget swap = Dispose old + Create new (not in-place mutation).
- **Channels and bundle are immutable after Create.** Changing widget kind requires a new host.
- **Render is pure.** No data mutation, no side effects. Side effects (stream subscription changes) are queued as workspace actions.
- **Descriptor table is exhaustive.** Compiler error if a `Widget_Kind` variant is missing.

## Consequences

- New widget kinds require exactly one descriptor entry + one render proc. No multi-file scatter.
- Widget catalog becomes data-driven from `WIDGET_DESCRIPTORS`.
- `channels_for_widget()` and `layer_bundle_for_widget()` in `widget_channels.odin` are replaced by descriptor lookup.
- Layer strategies are preserved — widget host is a composition layer above them.
- Lifecycle states enable workspace switching (Active ↔ Suspended) without data loss.

## Alternatives

1. **Interface/vtable per widget.** Rejected: Odin has no interfaces. Procedure pointers in a struct achieve the same with less ceremony.
2. **Merge widget and layer into one abstraction.** Rejected: a widget (e.g., Candle chart) composes multiple layers (Price_Candles + VPVR_Heatmap + Evidence + Signal). They are different granularities.
3. **Keep switch-based dispatch.** Rejected: already 12 widget kinds across 4 files. Does not scale and provides no compile-time completeness guarantee.

## Evidence

- `widget_channels.odin`: `channels_for_widget()` (41 lines) + `layer_bundle_for_widget()` (39 lines) — both replaced by descriptor table.
- `build_cell.odin`: `render_cell_widget()` dispatches on widget kind with a large switch.
- `components.odin`: `Widget_Kind` enum has 12 variants (Candle, Stats, Counter, Heatmap, VPVR, Trades, Orderbook, DOM, Empty, Analytics, Session_VPVR, TPO).
- `layer_api.odin`: `Layer_Strategy` lifecycle (init/event/render/reset/diagnostics) is preserved.

## Changelog

- 2026-03-08: Initial acceptance.
