# Stage 46 — Deterministic Runtime Snapshot & Incident Capture

## Executive Summary

S46 introduces a deterministic runtime snapshot system that captures the minimum canonical state needed to reproduce client behavior at any point in time. Snapshots are serialized to pipe-delimited ASCII (matching existing persistence conventions) and exported to clipboard via Ctrl+D or the "Copy Snapshot" button in the health panel. Zero wire changes, zero new mutable state, zero functional regressions.

## Runtime Audit

### Canonical State (captured in snapshot)
| Structure | Source | Why captured |
|-----------|--------|-------------|
| Stream_Apply_State (32 slots) | stream_views.slots[].apply_state | Protocol truth: snapshot gates, live flags, getrange, recovery |
| Slot identity | stream_views.slots[].stream_info | Market identification for reproduction |
| Active apply state | App_State.active_apply_state | Active stream's canonical state |
| Active TF/subject | App_State.active_tf_idx, active_subject_id | Stream selection context |
| Cell config | Entity_World.widgets/bindings/timeframes | Cell layout and binding state |
| Compare_State | App_State.compare | Compare mode configuration + per-pane TF/getrange |
| Recovery_Event_Log | App_State.recovery_log | Recent recovery history (ring buffer) |

### Derived State (included for convenience, recomputable)
| Structure | Source | Why included |
|-----------|--------|-------------|
| Aggregate_Health_Summary | aggregate_health_from_slots() | Dashboard-level health at capture time |

### Excluded State (non-deterministic or too large)
| State | Reason for exclusion |
|-------|---------------------|
| Frame counter | Monotonically increasing, not reproducible |
| Scroll/zoom positions | UI-specific, not behavior-affecting |
| Data store contents | Too large (candles, orderbook, trades), protocol-dependent |
| Transport metrics | Server-side timing, non-reproducible |
| UI overlay state | Transient modals/toasts |
| Mouse/keyboard state | Input-specific |

## Snapshot Architecture

```
Runtime_Snapshot (md_common)     <-- Pure struct, no app dependency
  |-- version (1)
  |-- capture_ts_ms
  |-- active_subject_id / active_tf_idx
  |-- active_apply_state: Stream_Apply_State
  |-- slots[32]: Snapshot_Slot
  |     |-- identity (venue, symbol, channel, tf_ms)
  |     |-- apply_state: Stream_Apply_State
  |-- cells[12]: Snapshot_Cell
  |     |-- widget_kind, stream_idx, binding, tf_idx
  |-- compare: Snapshot_Compare
  |     |-- active, count, widget_idx, focused_pane
  |     |-- per-pane: slots, tf_idx, getranges
  |-- recovery_log: Recovery_Event_Log
  |-- aggregate_health: Aggregate_Health_Summary

capture_runtime_snapshot(state)  <-- Pure read, app package
  |-- Reads App_State -> Runtime_Snapshot
  |-- No mutation, no allocations

runtime_snapshot_serialize(snap, buf)  <-- Pure function, md_common
  |-- Pipe-delimited ASCII to fixed buffer
  |-- Zero allocations, deterministic output
```

## Deterministic Capture Plan

1. **Same input -> same output**: `runtime_snapshot_serialize` is a pure function operating on value types. No pointers, no heap reads, no time-dependent logic in serialization.
2. **Bitmask packing**: Boolean arrays (snapshot_seen, has_live, using_synthetic) are packed into u16 bitmasks for compact, deterministic representation.
3. **Fixed-buffer serialization**: 16KB stack buffer, zero heap allocations. Integer formatting uses manual digit extraction (no fmt.tprintf dependency).
4. **Version field**: `RUNTIME_SNAPSHOT_VERSION = 1` enables future format evolution without breaking existing snapshots.

## Minimal Correct Implementation

### New Files
- `client/src/core/md_common/runtime_snapshot.odin` — Runtime_Snapshot struct + serialization + equality
- `client/src/core/app/runtime_snapshot_capture.odin` — Capture logic (App_State -> Runtime_Snapshot)

### Modified Files
- `client/src/core/app/app.odin` — Added `Capture_Runtime_Snapshot` to UI_Action_Kind
- `client/src/core/app/actions.odin` — Wired Ctrl+D keybinding + dispatch
- `client/src/core/app/build_status.odin` — Added "Copy Snapshot" button in health panel
- `client/src/core/ui/hotkeys.odin` — Added `Capture_Runtime_Snapshot` to Global_Command + key_d_pressed
- `client/src/core/ports/input.odin` — Added `D` to Key enum
- `client/src/platform/native/backend/sdl2_backend.odin` — D key mapping (SDL)
- `client/src/platform/native/backend/glfw_backend.odin` — D key mapping (GLFW)
- `client/src/platform/web/main.odin` — Comment noting web D key deferred (u32 bit limit)
- `client/src/core/md_common/store_boundary_test.odin` — 13 new S46 tests

## Code Changes

### Serialization Format
```
SNAP1|capture_ts|active_sid|active_tf|slot_count|cell_count
AS|<apply_state_fields>
SL|idx|sid|venue|symbol|ch|tf_ms|<apply_state_fields>
CL|idx|widget|stream_idx|has_bind|venue|symbol|tf_idx
CM|active|count|widget_idx|focused|s0|s1|s2|s3|tf0|tf1|tf2|tf3|gr_fields...
RL|count|head
RE|kind|ts|att|slot
AH|health|slots|composed|live|pending|empty|recovering|exhausted|stale|aging|events
```

### Apply State Serialization
```
snap_mask|live_mask|synth_mask|recv_ms[10]|evt_count[10]|gr_seeded|gr_pending|gr_oldest|gr_frame|range_sid|gr_req_id|rec_last|rec_att|evt_count|heatmap_dedup
```

## Tests (13 new, 342 total)

| Test | Validates |
|------|-----------|
| test_s46_snapshot_version_constant | Version and capacity constants |
| test_s46_empty_snapshot_serializes | Zero-value snapshot produces valid output |
| test_s46_serialize_deterministic | Same input produces identical output twice |
| test_s46_apply_state_equality_basic | Zero states equal, different events differ |
| test_s46_apply_state_equality_getrange | GetRange field comparison |
| test_s46_apply_state_equality_recovery | Recovery field comparison |
| test_s46_snapshot_with_slots_serializes | Multi-slot snapshot produces SL| lines |
| test_s46_snapshot_with_cells_serializes | Multi-cell snapshot produces CL| lines |
| test_s46_snapshot_compare_serializes | Compare mode snapshot produces CM| line |
| test_s46_snapshot_recovery_log_serializes | Recovery log produces RL|/RE| lines |
| test_s46_nil_snapshot_serializes_zero | Nil safety returns 0 |
| test_s46_aggregate_health_in_snapshot | Health summary produces AH| line |
| test_s46_apply_state_bitmask_roundtrip | All artifact kinds captured in bitmask |

## Risks

| Risk | Mitigation |
|------|-----------|
| Snapshot too large for clipboard | 16KB cap; real-world ~4-8KB for 10 slots |
| Web platform missing Ctrl+D | "Copy Snapshot" button in health panel provides GUI alternative |
| Format evolution | Version field enables backward-compatible parsing |
| Non-deterministic capture_ts_ms | Timestamp is for identification, not reproduction logic |

## Recommended S47

**Snapshot Deserialization & Offline Replay** — Parse serialized snapshots back into Runtime_Snapshot structs. Enable offline state injection for reproduction: load a snapshot, reconstruct Stream_Apply_State per slot, derive surfaces, and verify visual output matches the captured state. This completes the capture-replay loop for deterministic bug reproduction.
