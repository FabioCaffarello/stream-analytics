# Stage 62 — Legacy Cutover: Layer Compat Removal

**Date:** 2026-03-08
**Status:** COMPLETE

## Objective

Remove the legacy `layer_compat` indirection from the main application hot path,
replacing it with direct `Widget_Kind`-based mappings for both channel subscription
and layer rendering. Reduce the structural compat surface to only what is genuinely
needed for offline migration/forensics.

## Inventory — Legacy/Compat Dependencies

| Item | File | Hot Path? | Action | Justification |
|------|------|-----------|--------|---------------|
| `layer_compat.odin` | app/ | **YES** — every frame + reconcile | **DELETED** | Indirection layer packing two concerns into one u32 |
| `LEGACY_ROUTE_*` (8 constants) | layer_compat.odin | YES — subscription reconcile | **DELETED** | Route tag encoding no longer needed |
| `legacy_widget_bundle()` | layer_compat.odin | YES — render + reconcile | **REPLACED** by `channels_for_widget()` + `layer_bundle_for_widget()` |
| `channels_for_bundle()` | reconcile.odin | YES — subscription reconcile | **REPLACED** by `channels_for_widget()` |
| `compare_bundle_for_idx()` | reconcile.odin | YES — compare reconcile | **REPLACED** by `compare_widget_kind_for_idx()` |
| Duplicate switch in `build_compare.odin` | build_compare.odin | YES — compare render | **CONSOLIDATED** to use `compare_widget_kind_for_idx()` |
| `message_parser_compat.odin` | services/ | No — offline only | **PRESERVED** | Forensics/migration parsers, explicitly marked non-hot-path |
| `transport_legacy.odin` | md_common/ | No — config-gated, off by default | **PRESERVED** | Transitional fallback, disabled by default |

## What Changed

### Removed (from hot path)
- **`layer_compat.odin`** — entire file deleted
  - 8 `LEGACY_ROUTE_*` bitmask constants (bits 24-31)
  - `legacy_widget_bundle()` proc — packed Layer_Bundle + route tags into one u32
- **`channels_for_bundle()`** — decoded route tags back to channel bitmask (round-trip through encoding)
- **`compare_bundle_for_idx()`** — returned packed u32 for compare mode

### Added
- **`widget_channels.odin`** — three direct mappings:
  - `channels_for_widget(kind: Widget_Kind) -> u16` — direct Widget_Kind → channel bitmask
  - `layer_bundle_for_widget(kind: Widget_Kind) -> u32` — direct Widget_Kind → Layer_Bundle
  - `compare_widget_kind_for_idx(idx: int) -> Widget_Kind` — compare pane index → Widget_Kind

### Updated
- **`reconcile.odin`** — subscription reconciliation uses `channels_for_widget()` directly (was: `legacy_widget_bundle() → channels_for_bundle()`)
- **`layer_canvas.odin`** — rendering uses `layer_bundle_for_widget()` directly (was: `legacy_widget_bundle()`)
- **`build_compare.odin`** — consolidated duplicate Widget_Kind switch to use `compare_widget_kind_for_idx()`
- **`marketdata_test.odin`** — tests rewritten for new direct API, 4 new test procs

### Preserved (with justification)
- **`message_parser_compat.odin`** — 3 parsers for offline migration/forensics. Comment explicitly states "Runtime hot path must not call these helpers." Not on any render or update path.
- **`transport_legacy.odin`** — Config-gated legacy WS fallback (`ALLOW_LEGACY_WS` = false by default). Legitimate transitional compat for environments that may still need older WS protocol.

## Impact

### Before
```
Widget_Kind → legacy_widget_bundle() → packed u32 (Layer_Bundle | LEGACY_ROUTE_*)
                                           ↓
                                    channels_for_bundle() → extracts high bits → u16 channel mask
                                    layer_registry_render_bundle() → uses low bits → renders
```

### After
```
Widget_Kind → channels_for_widget() → u16 channel mask     (subscription)
Widget_Kind → layer_bundle_for_widget() → u32 Layer_Bundle  (rendering)
```

- **Eliminated:** 8 route tag constants, 1 encoding proc, 1 decoding proc, 1 packed-return proc
- **Hot path reduction:** 2 proc calls → 1 proc call per widget per frame (render path)
- **Hot path reduction:** 3 proc calls → 1 proc call per cell per reconcile (subscription path)
- **Code duplication removed:** compare mode Widget_Kind switch consolidated into single canonical proc

## Test Results

| Suite | Count | Status |
|-------|-------|--------|
| app | 17 | ALL PASS |
| services | 112 | ALL PASS |
| md_common | 402 | ALL PASS |
| **Total** | **531** | **ALL PASS** |

- 4 new tests: `test_channels_for_widget_direct_mapping`, `test_channels_for_widget_empty_returns_zero`, `test_compare_widget_kind_for_idx`, `test_layer_bundle_for_widget_non_zero`
- 1 legacy test removed: `test_channels_for_bundle_uses_layer_mapping`
- Zero regressions, zero wire changes

## Remaining Compat Surface

| Item | Reason to Keep | Next Cutover Candidate? |
|------|---------------|------------------------|
| `message_parser_compat.odin` | Offline forensics — no runtime cost | Only if migration tooling is retired |
| `transport_legacy.odin` | Config-gated fallback — disabled by default | When all deployments confirmed on Terminal_V1 |

## Files Changed

- `app/widget_channels.odin` — **NEW** (canonical direct mappings)
- `app/layer_compat.odin` — **DELETED** (legacy indirection)
- `app/reconcile.odin` — updated subscription reconciliation
- `app/layer_canvas.odin` — updated rendering bundle resolution
- `app/build_compare.odin` — consolidated compare mode switch
- `app/marketdata_test.odin` — rewritten tests for new API
