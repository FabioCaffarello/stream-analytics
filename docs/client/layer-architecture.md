# Client Layer Architecture

## Overview

The Odin client now runs a controlled layer pipeline:

1. `DataSource` reads Terminal V1 market frames through `Marketdata_Port.poll`.
2. `MarketStore` applies frames into bounded per-stream stores (`StreamKey -> ring/capped stores`).
3. `LayerRegistry` dispatches enabled layers in deterministic order.
4. `Canvas` renders only layer primitives (`line`, `heatmap cell`, `bar`, `text badge`).

## Package Layout

- `client/src/core/layers/layer_api.odin`
  - `Layer_Strategy` lifecycle (`init`, `on_event`, `on_snapshot`, `render`, `reset`, `diagnostics`)
  - `Layer_Context` (read-only store view + capabilities + time/seq)
- `client/src/core/layers/render_primitives.odin`
  - fixed-size `Layer_Outputs` (no heap in hot path)
  - primitive stable sort (`z`, insertion order)
- `client/src/core/layers/market_store.odin`
  - bounded `Market_Store`
  - per-stream bounded stores and eviction counters
- `client/src/core/layers/data_source.odin`
  - canonical ingest path into `Market_Store`
- `client/src/core/layers/layer_registry.odin`
  - plugin registry + enabled flags from settings
- `client/src/core/layers/layer_strategies.odin`
  - migrated layers:
    - Price/Candles
    - Trades Tape
    - OrderBook/DOM
    - VPVR/Heatmap
    - Evidence
    - Signal

## UI Integration

Legacy widget slots are mapped to layer bundles by `client/src/core/app/layer_compat.odin`.
`render_cell_layer_canvas` is the unified renderer used by normal/focus/compare flows.

## Performance and Safety Invariants

- No heap allocation in parse/apply/render hot paths.
- Bounded buffers for store state and render outputs.
- Deterministic rendering order via stable sort.
- Diagnostics avoid secrets (only counters and bounded text fields).

## Hard Cutover Note

Hot path computation is cut over to `DataSource -> MarketStore -> LayerRegistry -> Canvas`.
Legacy widget-specific compute/render paths remain in source only as fallback code, but are not exercised in the runtime hot path.
