# Stage 48 — Orderflow Analytics Pack v1

**Date:** 2026-03-07
**Status:** COMPLETE
**Tests:** 443 (358 md_common + 85 services) — 13 new S48 tests
**Wire changes:** Zero
**Breaking changes:** Zero

## Summary

Delivers the first four analytics widgets using the canonical analytics substrate
introduced in S47. Open Interest, Delta Volume, CVD, and Bar Statistics are now
rendered as first-class widgets in the cell grid, with full workspace persistence,
timeframe switching, and deterministic store updates.

## What Was Delivered

### Widget Kind Extension
- `Widget_Kind.Analytics` (ordinal 9) added to enum
- `Analytics_Component { analytics_kind, show_history }` per-cell ECS component
- `UI_Action.analytics_kind` field for catalog → action transport

### Analytics Render Procs (`render_analytics.odin`)
- **Open Interest:** OI value, delta, delta_pct, sparkline history
- **Delta Volume:** Buy/sell bars, delta label, ±bar chart history
- **CVD:** Cumulative value, window delta, line history
- **Bar Statistics:** Trade count, buy/sell ratio, volume, VWAP, imbalance, burst flag

All procs are zero-allocation (fixed-size buffers, `bprintf`), deterministic
(pure reads from `Analytics_Store` ring buffer), and render directly to `cmd_buf`
bypassing the layer canvas (analytics stores live on slots, not `Market_Stream`).

### Widget Catalog
- 4 new catalog entries: Open Interest, Delta Volume, CVD, Bar Stats
- Each maps to `Widget_Kind.Analytics` with preset `analytics_kind`
- Stream picker integration (Follow Active + market binding)

### Workspace Persistence
- `chart_display` packed int extended: bits 17-18 = `analytics_kind` (0-3)
- `pack_chart_display_with_analytics` / `unpack_chart_display_with_analytics`
- V6 format backward compatible (bits 17-18 default to 0 = Open_Interest)
- Widget kind parser accepts ordinal 9 (was capped at 8)

### Cell Header
- Analytics widget shows kind-specific short label: OI, DV, CVD, BS
- Composition badge and health dot render normally via `Cell_Surface_View`

### ECS Integration
- `Entity_World.analytics[CELL_MAX]` parallel array
- Compacted in `apply_remove_cell_action`
- Initialized in `init_world_cell_defaults` and `write_default_cell_to_world`
- Context menu includes Analytics option (10 entries, was 9)

## Architecture Decisions

1. **No layer canvas for analytics** — Analytics stores live on `Stream_View_Slot`,
   not `Market_Stream`. Routing through the layer canvas would require duplicating
   stores. Instead, analytics widgets render directly from `Cell_Stores.analytics`
   pointer resolved by `resolve_stores_for_cell`.

2. **Single Widget_Kind, sub-kind via component** — Rather than 4 separate widget
   kinds (OI/DV/CVD/BS), a single `.Analytics` kind with `Analytics_Component.analytics_kind`
   avoids enum bloat and reduces per-kind wiring to the catalog + renderer.

3. **Bits 17-18 of chart_display** — Reuses the existing V6 packed field without
   bumping schema version. Old clients reading the field ignore high bits. New clients
   reading old data get analytics_kind=0 (Open_Interest), which is safe.

## Backend Status

**No backend changes.** The Go aggregation pipeline already produces all four
analytics streams (OI, Delta Volume, CVD, Bar Stats) with:
- Deterministic replay (fixed-point arithmetic)
- Monotonic sequences per venue/instrument/channel
- Idempotent persistence (DB ON CONFLICT DO NOTHING)
- Canonical envelope wrapping via ArtifactPublisher
- JetStream delivery via session protocol

## Tests Added (13 new)

### md_common (8 new → 358 total)
- `test_s48_artifact_kind_count_is_14` — enum cardinality
- `test_s48_analytics_kind_values_stable` — ordinal stability
- `test_s48_analytics_policy_ring_append_for_tf_sensitive` — DV/CVD/BS policy
- `test_s48_oi_policy_sparse_not_tf_sensitive` — OI policy
- `test_s48_analytics_apply_state_mark_and_query` — mark + live + count
- `test_s48_tf_change_preserves_oi_clears_others` — TF change behavior
- `test_s48_analytics_staleness_at_boundaries` — threshold validation
- `test_s48_reconnect_resets_analytics` — reconnect + re-seed

### services (5 new → 85 total)
- `test_s48_analytics_store_push_and_get_latest` — push/get contract
- `test_s48_analytics_store_count_by_kind` — per-kind counting
- `test_s48_analytics_store_ring_overflow` — ring wrapping at cap 64
- `test_s48_analytics_store_clear` — reset to empty
- `test_s48_analytics_value_slot_mapping` — documented slot contracts

## Files Modified

| File | Change |
|------|--------|
| `app.odin` | `Widget_Kind.Analytics`, `UI_Action.analytics_kind` |
| `components.odin` | `Analytics_Component`, `Entity_World.analytics`, `Overlay_State.catalog_analytics_kind` |
| `build_cell.odin` | Analytics dispatch, kind-specific header label |
| `render_analytics.odin` | **NEW** — 4 render procs + sparkline/delta bar helpers |
| `layer_compat.odin` | `.Analytics` → `Bundle_Empty` (bypasses layer canvas) |
| `overlays.odin` | 4 analytics catalog entries, analytics_kind transport |
| `actions.odin` | `init_world_cell_defaults` analytics init |
| `actions_cell_mutations.odin` | Analytics component compact + add-cell init |
| `build_ui.odin` | Context menu 9→10 entries |
| `layout_persist.odin` | Widget kind parser accepts 0-9, analytics-aware pack/unpack |
| `workspace_schema.odin` | Docs + `pack/unpack_chart_display_with_analytics` |
| `store_boundary_test.odin` | 8 new S48 tests |
| `message_parser_test.odin` | 5 new S48 analytics store tests |

## Runtime Behavior

### Data Flow
1. Backend tape window closes → DeltaVolumeClosed/CVDClosed/BarStatsClosed published
2. WS delivery frames → message_parser routes to `.Delta_Volume`/`.CVD`/`.Bar_Stats`
3. Drain handler pushes `Analytics_Entry` to slot ring + global ring (S47)
4. Cell render: `resolve_stores_for_cell` → `Cell_Stores.analytics` pointer
5. `render_analytics_*` reads latest entry + history from ring buffer
6. Emits text badges + bar/line primitives to `cmd_buf`

### Timeframe Switching
- OI: Preserved across TF changes (Sparse_Adaptive, not TF-sensitive)
- DV/CVD/BS: Cleared on TF change, re-seeded from new window stream

### Recovery
- Existing S29-S35 health/recovery logic covers analytics automatically
- `apply_state_stale_artifact_count` includes all 14 artifact kinds
- `health_tick_evaluate` triggers reconnect if analytics stale beyond thresholds
