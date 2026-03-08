package app

import "mr:md_common"
import "mr:services"

// S46: Runtime snapshot capture — reads canonical App_State into a
// deterministic Runtime_Snapshot for incident reproduction.
// Pure read — no mutation, no allocations beyond the snapshot struct.

// capture_runtime_snapshot builds a full Runtime_Snapshot from current state.
capture_runtime_snapshot :: proc(state: ^App_State) -> md_common.Runtime_Snapshot {
	snap: md_common.Runtime_Snapshot
	if state == nil do return snap

	snap.version = md_common.RUNTIME_SNAPSHOT_VERSION
	snap.capture_ts_ms = current_now_ms(state)
	snap.active_tf_idx = state.active_tf_idx
	snap.active_apply_state = state.active_apply_state
	// S80: Active route for scene state reproduction.
	snap.active_route = u8(state.chrome.active_route)

	// Active subject_id
	reg := state.stream_views
	if reg != nil && reg.has_active {
		snap.active_subject_id = reg.active_subject_id
	}

	// Per-slot state
	if reg != nil {
		slot_idx := 0
		for si in 0 ..< STREAM_VIEW_CAP {
			if slot_idx >= md_common.SNAPSHOT_MAX_SLOTS do break
			slot := &reg.slots[si]
			if !slot.used do continue

			ss := &snap.slots[slot_idx]
			ss.used = true
			ss.subject_id = slot.subject_id
			ss.apply_state = slot.apply_state

			// Identity
			if slot.has_stream_info {
				copy_fixed_string(ss.venue[:], &ss.venue_len, slot.stream_info.venue)
				copy_fixed_string(ss.symbol[:], &ss.symbol_len, slot.stream_info.symbol)
				ss.channel = u8(slot.stream_info.channel)
			}
			if slot.has_timeframe_ms {
				ss.timeframe_ms = slot.timeframe_ms
			}

			slot_idx += 1
		}
		snap.slot_count = slot_idx
	}

	// Per-cell state
	cell_count := min(state.world.count, md_common.SNAPSHOT_MAX_CELLS)
	snap.cell_count = cell_count
	for ci in 0 ..< cell_count {
		cc := &snap.cells[ci]
		cc.widget_kind = u8(state.world.widgets[ci].kind)
		cc.stream_idx = state.world.bindings[ci].stream_idx
		cc.tf_idx = state.world.timeframes[ci].tf_idx
		// S80: Chart display + indicator flags for deterministic reproduction.
		cc.chart_display = pack_chart_display_with_analytics(&state.world.charts[ci], &state.world.analytics[ci])
		cc.indicator_flags = pack_indicator_flags(&state.world.indicators[ci])

		bind := &state.world.bindings[ci]
		cc.has_binding = binding_has(bind)
		if cc.has_binding {
			bv := binding_venue(bind)
			copy_fixed_string(cc.bind_venue[:], &cc.bind_venue_len, bv)
			bs := binding_symbol(bind)
			copy_fixed_string(cc.bind_symbol[:], &cc.bind_symbol_len, bs)
		}
	}

	// Compare mode
	snap.compare.active = state.compare.active
	snap.compare.count = state.compare.count
	snap.compare.widget_idx = state.compare.widget_idx
	snap.compare.focused_pane = state.compare.focused_pane
	for pi in 0 ..< md_common.SNAPSHOT_MAX_COMPARE_PANES {
		snap.compare.slots[pi] = state.compare.slots[pi]
		snap.compare.tf_idx[pi] = state.compare.tf_idx[pi]
		gr := &state.compare.getranges[pi]
		snap.compare.getranges[pi] = md_common.Snapshot_Compare_Getrange{
			pending    = gr.pending,
			seeded     = gr.seeded,
			oldest_ts  = gr.oldest_ts,
			sent_frame = gr.sent_frame,
		}
	}

	// Recovery event log (copy the ring buffer)
	snap.recovery_log = state.recovery_log

	// Derived: aggregate health
	snap.aggregate_health = compute_aggregate_health(state)

	return snap
}

// capture_runtime_snapshot_to_clipboard captures and serializes the snapshot
// to the system clipboard. Returns true on success.
capture_runtime_snapshot_to_clipboard :: proc(state: ^App_State) -> bool {
	if state == nil do return false

	snap := capture_runtime_snapshot(state)
	buf: [md_common.SNAPSHOT_SERIALIZE_CAP]u8
	n := md_common.runtime_snapshot_serialize(&snap, buf[:])
	if n <= 0 do return false

	return services.settings_clipboard_write(&state.settings, string(buf[:n]))
}

// Helper: copy a string into a fixed-size buffer with length tracking.
@(private = "file")
copy_fixed_string :: proc(dst: []u8, dst_len: ^u8, src: string) {
	n := min(len(src), len(dst))
	for i in 0 ..< n {
		dst[i] = src[i]
	}
	dst_len^ = u8(n)
}
