# Stage 87 — Cell-Type Rendering Specialization Recovery

**Date:** 2026-03-08
**Status:** COMPLETE

## Problem (BUG-3)

After the S9/S62 legacy widget removal, specialized cell types fell through to
wrong renderers via their bundle mappings:

| Cell Kind | Bundle (before) | Layer Triggered | Actual Rendering |
|-----------|----------------|-----------------|------------------|
| Stats | `Price_Candles \| Signal` | `price_candles_render` | Candle range scan + "Last X.XX" badge — wrong semantics for a stats panel |
| Counter | `Trades_Tape \| Signal` | `trades_tape_render` | Individual trade rows — wrong semantics for a trade counter |

The S86 guard (`render_bars := ctx.active_bundle == Bundle_Candles`) prevented
candle bars from appearing in Stats cells, but the renderer still performed a
full candle range scan and emitted price/regime badges that don't belong in a
stats-only view.

Counter cells had no guard at all — they rendered the full trades tape.

## Solution

Added two new dedicated layer strategies within the layer runtime (no legacy
widget resurrection):

### New Layer IDs

| Layer_ID | Bit | Z-Order | Budget (µs) |
|----------|-----|---------|-------------|
| `Stats_Panel` | 1 << 7 | 22 | 600 |
| `Trade_Counter` | 1 << 8 | 23 | 600 |

### Rewired Bundles

| Bundle | Before | After |
|--------|--------|-------|
| `Bundle_Stats` | `Price_Candles \| Signal` | `Stats_Panel \| Signal` |
| `Bundle_Counter` | `Trades_Tape \| Signal` | `Trade_Counter \| Signal` |

### Stats Panel Renderer (`stats_panel_render`)

Dedicated stats rendering:
- Mark price (primary)
- Funding rate (green/red signed)
- Liquidation levels (buy/sell)
- Window duration
- Quality flags (warning highlight when non-zero)

### Trade Counter Renderer (`trade_counter_render`)

Dedicated counter rendering:
- Trade count from latest candle aggregate
- Volume summary (total, buy, sell)
- Buy/sell ratio bar (visual)
- B/S percentage label
- Last close price (reference)
- Funding rate supplement (if stats available)

## Files Changed

| File | Change |
|------|--------|
| `layers/layer_api.odin` | +2 Layer_IDs, +2 bundle bits, rewired Bundle_Stats/Counter, z-order/mask updates |
| `layers/layer_strategies.odin` | +stats_panel_render, +stats_panel_diagnostics, +trade_counter_render, +trade_counter_diagnostics, +2 strategy constructors |
| `layers/layer_registry.odin` | LAYER_REGISTRY_CAP 8→10, +2 setting keys |
| `layers/layer_registry_runtime.odin` | +2 strategy registrations, +2 budget entries, +2 setting key entries |
| `layers/layers_test.odin` | +7 tests (stats panel render, stats no-data, counter render, counter no-data, bundle isolation ×2) |
| `app/build_status.odin` | +2 switch cases for diagnostics display |

## Tests

- 15 layer tests (7 new): all pass
- 28 app tests: all pass
- 181 services tests: all pass
- 402 md_common tests: all pass
- **Total: 626 tests, zero regressions**

## Verification

- `Bundle_Stats` (bits 7+5) no longer matches `Price_Candles` layer (bit 0) — confirmed by `test_bundle_stats_does_not_trigger_price_candles_layer`
- `Bundle_Counter` (bits 8+5) no longer matches `Trades_Tape` layer (bit 1) — confirmed by `test_bundle_counter_does_not_trigger_trades_tape_layer`
- Stats panel emits exactly 5 text badges, zero bars/lines
- Trade counter emits aggregate metrics, not individual trade rows
- `make check-core` compiles all 10 core packages clean
