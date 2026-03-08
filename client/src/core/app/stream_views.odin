package app

import "core:fmt"
import "mr:md_common"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:util"

// ---------------------------------------------------------------------------
// Stream view helpers that remain after splitting out:
//   stream_slots.odin   — slot CRUD and store resolution
//   layout_persist.odin — layout persistence V1-V4
//   reconcile.odin      — subscription reconciliation
// ---------------------------------------------------------------------------

current_conn_status :: proc(state: ^App_State) -> ports.MD_Conn_Status {
	if state.marketdata.conn_status != nil {
		return state.marketdata.conn_status()
	}
	return .Offline
}

current_now_ms :: proc(state: ^App_State) -> i64 {
	if state.marketdata.now_ms != nil {
		return state.marketdata.now_ms()
	}
	return 0
}

@(private = "package")
adaptive_getrange_limit :: proc(state: ^App_State) -> int {
	limit := min(FETCH_CANDLES_RANGE_LEN, services.RANGE_CANDLE_PARSE_MAX)
	if limit <= 0 do limit = services.RANGE_CANDLE_PARSE_MAX
	if limit <= 0 do limit = 1
	if state == nil do return limit
	divisor := max(state.bp_assist.getrange_divisor, 1)
	adapted := limit / divisor
	if adapted <= 0 do adapted = 1
	return adapted
}

parse_subject_id_hex :: proc(s: string) -> (u64, bool) {
	if len(s) == 0 do return 0, false
	v := u64(0)
	for c in s {
		digit := u64(0)
		if c >= '0' && c <= '9' {
			digit = u64(c - '0')
		} else if c >= 'a' && c <= 'f' {
			digit = 10 + u64(c - 'a')
		} else if c >= 'A' && c <= 'F' {
			digit = 10 + u64(c - 'A')
		} else {
			return 0, false
		}
		v = (v << 4) | digit
	}
	return v, true
}

persist_active_stream_subject :: proc(state: ^App_State) {
	if state == nil do return
	reg := state.stream_views
	if reg == nil || !reg.has_active do return
	if reg.active_subject_id == 0 do return
	buf: [32]u8
	value := fmt.bprintf(buf[:], "%x", reg.active_subject_id)
	services.settings_set(&state.settings, services.SETTING_ACTIVE_STREAM_SUBJECT_ID, value)
	slot := stream_view_active_slot(reg)
	if slot != nil && !slot.has_stream_info {
		refresh_stream_info_for_slot(state, slot)
	}
	if slot != nil && slot.has_stream_info {
		info := slot.stream_info
		if len(info.venue) > 0 {
			services.settings_set(&state.settings, services.SETTING_ACTIVE_STREAM_VENUE, info.venue)
		}
		if len(info.symbol) > 0 {
			services.settings_set(&state.settings, services.SETTING_ACTIVE_STREAM_SYMBOL, info.symbol)
		}
		services.settings_set(&state.settings, services.SETTING_ACTIVE_STREAM_CHANNEL, channel_short_label(info.channel))
	}
}

stream_view_cycle_active :: proc(reg: ^Stream_View_Registry, forward: bool) -> bool {
	if reg == nil || !reg.has_active do return false
	if reg.count <= 1 do return false

	curr_idx := stream_view_find_slot(reg, reg.active_subject_id)
	start := curr_idx
	if start < 0 do start = 0

	for step in 1 ..< len(reg.slots) + 1 {
		idx := 0
		if forward {
			idx = (start + step) % len(reg.slots)
		} else {
			idx = (start - step) % len(reg.slots)
			if idx < 0 do idx += len(reg.slots)
		}
		if !reg.slots[idx].used do continue
		if reg.slots[idx].subject_id == reg.active_subject_id do continue
		reg.active_subject_id = reg.slots[idx].subject_id
		return true
	}
	return false
}

// Dual store contract:
// - Per-slot stores (Stream_View_Slot.{candle,trades,...}_store) hold data per subject_id.
//   Data is always written here first in layer_marketdata.
// - Global stores (App_State.{candle,trades,...}_store) mirror the ACTIVE stream's slot stores.
//   They are synced via this proc when the active stream changes (Tab, Pick_Stream, etc.).
// - Cells following active use global stores; bound cells resolve via resolve_stores_for_cell.
sync_active_stream_view_to_global_stores :: proc(state: ^App_State) {
	reg := state.stream_views
	if reg == nil do return
	if !reg.has_active do return
	if idx := stream_view_find_slot(reg, reg.active_subject_id); idx >= 0 {
		slot := &reg.slots[idx]
		if slot.has_stream_info {
			stream_id_buf: [streams.STREAM_ID_CAP]u8
			stream_id := build_stream_id_from_market_into(stream_id_buf[:], slot.stream_info.venue, slot.stream_info.symbol)
			streams.registry_set_active(&state.stream_registry, stream_id)
		}
		state.stores.trades = slot.trades_store
		state.stores.orderbook = slot.orderbook_store
		state.stores.heatmap = slot.heatmap_store
		if state.stores.heatmap.count <= 0 && slot.apply_state.has_live[.Heatmap] {
			services.push_heatmap_snapshot(&state.stores.heatmap, slot.heatmap_snapshot)
		}
		state.stores.vpvr = slot.vpvr_store
		state.stores.stats = slot.stats_store
		state.stores.candle = slot.candle_store
		// Reset DOM fill tracking and footprint accumulation on stream switch.
		services.dom_store_reset(&state.stores.dom)
		services.footprint_store_reset(&state.stores.footprint)
	}
}

// Eagerly compute and set active_candle_subject_id from the current active stream + global TF.
// Must be called on every stream switch and TF change so the candle TF guard in layer_marketdata
// is never stale. This prevents candles from wrong TFs polluting the global store.
ensure_active_candle_subject_id :: proc(state: ^App_State) {
	sv := state.stream_views
	if sv == nil || !sv.has_active do return
	slot := stream_view_active_slot(sv)
	if slot == nil do return
	if !slot.has_stream_info {
		refresh_stream_info_for_slot(state, slot)
	}
	if !slot.has_stream_info do return
	info := slot.stream_info
	if len(info.venue) == 0 || len(info.symbol) == 0 do return
	tf_opts := TF_OPTIONS
	tf := tf_opts[0]
	if state.active_tf_idx >= 0 && state.active_tf_idx < len(tf_opts) {
		tf = tf_opts[state.active_tf_idx]
	}
	cs := util.build_subject_with_timeframe(info.venue, info.symbol, .Candles, tf)
	sid := util.subject_id64(cs)
	delete(cs)
	// S25: Write to apply state (source of truth), then sync to getrange global.
	state.active_apply_state.range_candle_subject_id = sid
	apply_state_sync_to_getrange(state)
}

apply_cycle_stream_action :: proc(state: ^App_State, forward: bool) -> bool {
	_ = stream_view_repair_invariants(state.stream_views)

	is_offline := current_conn_status(state) == .Offline
	if is_offline {
		// In offline mode with no stream views, re-populate demo data.
		state.stores.trades = {}
		state.stores.orderbook = {}
		state.stores.heatmap = {}
		state.stores.vpvr = {}
		state.stores.stats = {}
		state.stores.candle = {}
		services.fill_demo_trades(&state.stores.trades)
		services.fill_demo_orderbook(&state.stores.orderbook)
		services.fill_demo_heatmaps(&state.stores.heatmap)
		services.fill_demo_vpvr(&state.stores.vpvr)
		services.fill_demo_stats(&state.stores.stats)
		services.fill_demo_candles(&state.stores.candle)
			// S23: Use canonical reset for offline demo path.
			reset_active_apply_state(state)
			return true
		}

	if !stream_view_cycle_active(state.stream_views, forward) do return false

	sync_active_stream_view_to_global_stores(state)
	persist_active_stream_subject(state)

	// BUG-23: Reset scroll/zoom on follow-active candle cells so they don't show stale position.
	for ci in 0 ..< state.world.count {
		if state.world.bindings[ci].stream_idx >= 0 do continue // bound cell, not affected
		if state.world.widgets[ci].kind != .Candle do continue
		state.world.views[ci].candle_scroll_x = 0
		state.world.views[ci].candle_zoom = 0
	}
	// S25: Canonical apply state sync from new active slot drives getrange state.
	// S34: getrange_request_id cleared by sync_active_apply_state_from_slot (slot's apply_state).
	sync_active_apply_state_from_slot(state)
	ensure_active_candle_subject_id(state)
	state.candle_health = .No_Data
	if state.stores.candle.count <= 0 {
		request_active_stream_candle_range(state)
	}
	return true
}

request_active_stream_candle_range :: proc(state: ^App_State) {
	if state == nil do return
	if state.marketdata.send_getrange == nil do return
	if state.getrange.pending do return

	slot := stream_view_active_slot(state.stream_views)
	if slot == nil do return
	if !slot.has_stream_info {
		refresh_stream_info_for_slot(state, slot)
	}
	if !slot.has_stream_info do return

	info := slot.stream_info
	if len(info.venue) == 0 || len(info.symbol) == 0 do return

	limit := adaptive_getrange_limit(state)

	tf_opts := TF_OPTIONS
	tf := tf_opts[0]
	if state.active_tf_idx >= 0 && state.active_tf_idx < len(tf_opts) {
		tf = tf_opts[state.active_tf_idx]
	}
	candle_subject := util.build_subject_with_timeframe(info.venue, info.symbol, .Candles, tf)
	sid := util.subject_id64(candle_subject)
	// S20: Use timeline last_ts as end bound if available (smarter than sending 0).
	end_ts := i64(0)
	if state.timeline.loaded && state.timeline.last_ts > 0 {
		end_ts = state.timeline.last_ts
	}
	state.marketdata.send_getrange(candle_subject, limit, end_ts)
	delete(candle_subject)
	// S34: apply_state is sole owner of getrange_request_id + pending state.
	md_common.apply_state_mark_range_sent(&state.active_apply_state, state.frame, sid)
	apply_state_sync_to_getrange(state)
}

// Request older candles (before the oldest we have) for lazy loading on scroll-left.
request_older_candles :: proc(state: ^App_State) {
	if state == nil do return
	if state.marketdata.send_getrange == nil do return
	if state.getrange.pending do return
	if state.getrange.oldest_ts <= 0 do return
	if state.stores.candle.count >= services.CANDLE_CAP do return // store full, no point fetching more
	// S20: Stop requesting if we've reached the timeline's first_ts boundary.
	if state.timeline.loaded && state.timeline.first_ts > 0 && state.getrange.oldest_ts <= state.timeline.first_ts {
		return
	}

	slot := stream_view_active_slot(state.stream_views)
	if slot == nil do return
	if !slot.has_stream_info do return

	info := slot.stream_info
	if len(info.venue) == 0 || len(info.symbol) == 0 do return

	limit := adaptive_getrange_limit(state)

	tf_opts := TF_OPTIONS
	tf := tf_opts[0]
	if state.active_tf_idx >= 0 && state.active_tf_idx < len(tf_opts) {
		tf = tf_opts[state.active_tf_idx]
	}
	candle_subject := util.build_subject_with_timeframe(info.venue, info.symbol, .Candles, tf)
	sid := util.subject_id64(candle_subject)
	state.marketdata.send_getrange(candle_subject, limit, state.getrange.oldest_ts)
	delete(candle_subject)
	// S34: apply_state is sole owner of getrange_request_id + pending state.
	md_common.apply_state_mark_range_sent(&state.active_apply_state, state.frame, sid)
	apply_state_sync_to_getrange(state)
}

// Resolve the effective TF index for a cell: per-cell if >= 0, else global.
cell_effective_tf_idx :: proc(state: ^App_State, ci: int) -> int {
	tf := state.world.timeframes[ci].tf_idx
	if tf >= 0 && tf < len(TF_OPTIONS) {
		return tf
	}
	return state.active_tf_idx
}

// Resolve the effective TF string label for a cell (e.g. "1m", "5m").
cell_effective_tf_string :: proc(state: ^App_State, ci: int) -> string {
	tf_opts := TF_OPTIONS
	idx := cell_effective_tf_idx(state, ci)
	if idx >= 0 && idx < len(tf_opts) {
		return tf_opts[idx]
	}
	return tf_opts[0]
}

// Resolve the effective TF duration in milliseconds for a cell.
cell_effective_tf_ms :: proc(state: ^App_State, ci: int) -> i64 {
	options := TF_OPTION_MS
	idx := cell_effective_tf_idx(state, ci)
	if idx >= 0 && idx < len(options) {
		return options[idx]
	}
	return options[0]
}

// S38: Resolve the effective TF index for a compare pane: per-pane if >= 0, else global.
compare_pane_effective_tf_idx :: proc(state: ^App_State, ci: int) -> int {
	if ci >= 0 && ci < 4 {
		tf := state.compare.tf_idx[ci]
		if tf >= 0 && tf < len(TF_OPTIONS) {
			return tf
		}
	}
	return state.active_tf_idx
}

// S38: Resolve the effective TF string label for a compare pane.
compare_pane_effective_tf_string :: proc(state: ^App_State, ci: int) -> string {
	tf_opts := TF_OPTIONS
	idx := compare_pane_effective_tf_idx(state, ci)
	if idx >= 0 && idx < len(tf_opts) {
		return tf_opts[idx]
	}
	return tf_opts[0]
}

// S38: Resolve the effective TF duration in milliseconds for a compare pane.
compare_pane_effective_tf_ms :: proc(state: ^App_State, ci: int) -> i64 {
	options := TF_OPTION_MS
	idx := compare_pane_effective_tf_idx(state, ci)
	if idx >= 0 && idx < len(options) {
		return options[idx]
	}
	return options[0]
}

apply_set_timeframe_action :: proc(state: ^App_State, idx: int) -> bool {
	if idx < 0 || idx >= len(TF_OPTIONS) do return false
	if idx == state.active_tf_idx do return false

	state.active_tf_idx = idx
	opts := TF_OPTIONS
	tf := opts[idx]

	// Update TF filter in the adapter.
	if state.marketdata.set_candle_tf != nil {
		state.marketdata.set_candle_tf(tf)
	}

	// Clear candle store and reset zoom/scroll for new TF data.
	state.stores.candle.head = 0
	state.stores.candle.count = 0
	state.stores.heatmap = {}
	state.stores.vpvr = {}
	// S25: Canonical apply state TF change (policy-driven) — also syncs getrange.
	// S34: getrange_request_id cleared by apply_state_on_tf_change.
	tf_change_active_apply_state(state)
	state.candle_health = .No_Data

	// S25: Update active candle subject_id via apply state for stale getrange guard.
	if as := stream_view_active_slot(state.stream_views); as != nil && as.has_stream_info {
		cs := util.build_subject_with_timeframe(as.stream_info.venue, as.stream_info.symbol, .Candles, tf)
		state.active_apply_state.range_candle_subject_id = util.subject_id64(cs)
		delete(cs)
		apply_state_sync_to_getrange(state)
	}

	// Also clear timeframe-sensitive overlays in the active stream view slot.
	if slot := stream_view_active_slot(state.stream_views); slot != nil {
		slot.candle_store.head = 0
		slot.candle_store.count = 0
		slot.heatmap_store = {}
		slot.heatmap_snapshot = {}
		slot.vpvr_store = {}
		services.analytics_store_clear(&slot.analytics_store) // S47
		// Clear orderbook store — stale L2 data from the prior TF doesn't persist.
		// A fresh snapshot will arrive after resubscribe.
		slot.orderbook_store = {}
		// S24: apply_state_on_tf_change resets has_live[.Heatmap/.VPVR],
		// snapshot_seen[.Orderbook], and synth_heatmap_last_window per policy.
		md_common.apply_state_on_tf_change(&slot.apply_state)
	}

	// Clear TF-sensitive data for cells following global TF.
	for ci in 0 ..< state.world.count {
		if state.world.timeframes[ci].tf_idx >= 0 do continue  // per-cell TF, not affected
		state.world.views[ci].candle_scroll_x = 0
		state.world.views[ci].candle_zoom = 0
		// Clear per-cell getrange state so stale batches from old TF are rejected.
		state.world.getranges[ci].pending = false
		state.world.getranges[ci].seeded = false
		state.world.getranges[ci].oldest_ts = 0
	}

	// S44: Invalidate compare panes following global TF (tf_idx == -1).
	// Per-pane TF overrides are unaffected — only global-followers need reset.
	if state.compare.active {
		for cpi in 0 ..< state.compare.count {
			if state.compare.tf_idx[cpi] >= 0 do continue // Per-pane TF, not affected.
			state.compare.getranges[cpi] = {}
			state.compare.scroll_x[cpi] = 0
			state.compare.zoom[cpi] = 0
		}
	}

	// S20: Refetch timeline for new TF (bounds may differ per timeframe).
	state.timeline = {}
	fetch_timeline_for_active(state)

	// Request historical data for the new TF.
	request_active_stream_candle_range(state)

	// Reconcile subscriptions since TF change affects candle/heatmap/vpvr subjects.
	reconcile_subscriptions(state)

	// S44: Trigger fresh backfill for global-following compare panes after reconcile.
	if state.compare.active {
		for cpi in 0 ..< state.compare.count {
			if state.compare.tf_idx[cpi] >= 0 do continue
			request_compare_pane_candle_range(state, cpi)
		}
	}

	// Persist active timeframe selection.
	tf_store_buf: [8]u8
	services.settings_set(&state.settings, services.SETTING_ACTIVE_TF_IDX,
		fmt.bprintf(tf_store_buf[:], "%d", state.active_tf_idx))
	services.settings_flush(&state.settings)

	return true
}

// Request historical candle data for a specific cell.
request_cell_candle_range :: proc(state: ^App_State, cell_idx: int) {
	if state == nil do return
	if state.marketdata.send_getrange == nil do return
	if cell_idx < 0 || cell_idx >= state.world.count do return

	if state.world.getranges[cell_idx].pending do return

	// Only candle cells need backfill.
	if state.world.widgets[cell_idx].kind != .Candle do return

	// Resolve venue/symbol from cell's stream binding.
	reg := state.stream_views
	if reg == nil do return

	venue, symbol: string
	// PRD-0009: prefer bound fields for venue/symbol resolution.
	if binding_has(&state.world.bindings[cell_idx]) {
		venue = binding_venue(&state.world.bindings[cell_idx])
		symbol = binding_symbol(&state.world.bindings[cell_idx])
	} else if state.world.bindings[cell_idx].stream_idx >= 0 && state.world.bindings[cell_idx].stream_idx < STREAM_VIEW_CAP && reg.slots[state.world.bindings[cell_idx].stream_idx].used {
		slot := &reg.slots[state.world.bindings[cell_idx].stream_idx]
		if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
		if !slot.has_stream_info do return
		venue = slot.stream_info.venue
		symbol = slot.stream_info.symbol
	} else {
		// No binding — follow active stream.
		slot := stream_view_active_slot(reg)
		if slot == nil do return
		if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
		if !slot.has_stream_info do return
		venue = slot.stream_info.venue
		symbol = slot.stream_info.symbol
	}
	if len(venue) == 0 || len(symbol) == 0 do return

	limit := adaptive_getrange_limit(state)

	tf := cell_effective_tf_string(state, cell_idx)

	candle_subject := util.build_subject_with_timeframe(venue, symbol, .Candles, tf)
	state.marketdata.send_getrange(candle_subject, limit, state.world.getranges[cell_idx].oldest_ts)
	delete(candle_subject)
	state.world.getranges[cell_idx].pending = true
	state.world.getranges[cell_idx].seeded = true
	state.world.getranges[cell_idx].sent_frame = state.frame
}

// Request older candles for a specific cell (lazy loading on scroll-left).
request_cell_older_candles :: proc(state: ^App_State, cell_idx: int) {
	if state == nil do return
	if state.marketdata.send_getrange == nil do return
	if cell_idx < 0 || cell_idx >= state.world.count do return

	if state.world.getranges[cell_idx].pending do return
	if state.world.getranges[cell_idx].oldest_ts <= 0 do return
	if state.world.widgets[cell_idx].kind != .Candle do return

	reg := state.stream_views
	if reg == nil do return

	venue, symbol: string
	if binding_has(&state.world.bindings[cell_idx]) {
		venue = binding_venue(&state.world.bindings[cell_idx])
		symbol = binding_symbol(&state.world.bindings[cell_idx])
	} else if state.world.bindings[cell_idx].stream_idx >= 0 && state.world.bindings[cell_idx].stream_idx < STREAM_VIEW_CAP && reg.slots[state.world.bindings[cell_idx].stream_idx].used {
		slot := &reg.slots[state.world.bindings[cell_idx].stream_idx]
		if !slot.has_stream_info do return
		venue = slot.stream_info.venue
		symbol = slot.stream_info.symbol
	} else {
		slot := stream_view_active_slot(reg)
		if slot == nil do return
		if !slot.has_stream_info do return
		venue = slot.stream_info.venue
		symbol = slot.stream_info.symbol
	}
	if len(venue) == 0 || len(symbol) == 0 do return

	limit := adaptive_getrange_limit(state)

	tf := cell_effective_tf_string(state, cell_idx)

	candle_subject := util.build_subject_with_timeframe(venue, symbol, .Candles, tf)
	state.marketdata.send_getrange(candle_subject, limit, state.world.getranges[cell_idx].oldest_ts)
	delete(candle_subject)
	state.world.getranges[cell_idx].pending = true
	state.world.getranges[cell_idx].sent_frame = state.frame
}

// Set a per-cell timeframe. -1 means revert to following global.
apply_set_cell_timeframe_action :: proc(state: ^App_State, cell_idx: int, tf_idx: int) -> bool {
	if cell_idx < 0 || cell_idx >= state.world.count do return false
	if tf_idx < -1 || tf_idx >= len(TF_OPTIONS) do return false

	if state.world.timeframes[cell_idx].tf_idx == tf_idx do return false

	state.world.timeframes[cell_idx].tf_idx = tf_idx
	state.world.views[cell_idx].candle_scroll_x = 0
	state.world.views[cell_idx].candle_zoom = 0
	// BUG-24: Clear stale getrange state so the new TF request isn't suppressed.
	state.world.getranges[cell_idx].pending = false
	state.world.getranges[cell_idx].seeded = false
	state.world.getranges[cell_idx].oldest_ts = 0

	// Clear the cell's stream slot candle/heatmap/vpvr data for fresh TF data.
	stream_idx := state.world.bindings[cell_idx].stream_idx
	if stream_idx >= 0 && stream_idx < STREAM_VIEW_CAP {
		reg := state.stream_views
		if reg != nil && reg.slots[stream_idx].used {
			slot := &reg.slots[stream_idx]
			slot.candle_store.head = 0
			slot.candle_store.count = 0
			slot.heatmap_store = {}
			slot.heatmap_snapshot = {}
			slot.vpvr_store = {}
			services.analytics_store_clear(&slot.analytics_store) // S47
			// S24: apply_state_on_tf_change handles has_live/snapshot_seen resets per policy.
			md_common.apply_state_on_tf_change(&slot.apply_state)
		}
	} else {
		// BUG-17: Follow-active cell — only clear candle store (TF-sensitive).
		// Do NOT clear global heatmap/vpvr stores; they serve other cells.
			state.stores.candle.head = 0
			state.stores.candle.count = 0
			// S25: Partial TF reset for follow-active — only candle.
			state.active_apply_state.has_live[.Candle] = false
			state.active_apply_state.using_synthetic[.Candle] = false
			state.active_apply_state.last_recv_ms[.Candle] = 0
			state.active_apply_state.artifact_event_count[.Candle] = 0
			apply_state_sync_all(state)
		}

	// BUG-16: Reset candle health so stale badge doesn't persist across TF changes.
	state.candle_health = .No_Data

	// Persist and reconcile subscriptions for the new TF.
	persist_layout_v6(state)
	reconcile_subscriptions(state)

	// Request historical candle data for the new TF.
	request_cell_candle_range(state, cell_idx)

	return true
}

// ===============================================================
// PRD-0009: Intent-driven cell binding helpers (zero heap alloc).
// ===============================================================

// ECS binding helpers — operate on Stream_Binding component.

binding_venue :: proc(b: ^Stream_Binding) -> string {
	if b == nil || b.bound_venue_len == 0 do return ""
	n := min(int(b.bound_venue_len), len(b.bound_venue))
	for i in 0 ..< n {
		if b.bound_venue[i] == 0 {
			n = i
			break
		}
	}
	if n <= 0 do return ""
	return string(b.bound_venue[:n])
}

binding_symbol :: proc(b: ^Stream_Binding) -> string {
	if b == nil || b.bound_symbol_len == 0 do return ""
	n := min(int(b.bound_symbol_len), len(b.bound_symbol))
	for i in 0 ..< n {
		if b.bound_symbol[i] == 0 {
			n = i
			break
		}
	}
	if n <= 0 do return ""
	return string(b.bound_symbol[:n])
}

binding_set :: proc(b: ^Stream_Binding, venue: string, symbol: string) {
	if b == nil do return
	b.stream_idx = -1 // follow active by default
	for i in 0 ..< len(b.bound_venue) { b.bound_venue[i] = 0 }
	for i in 0 ..< len(b.bound_symbol) { b.bound_symbol[i] = 0 }
	vn := min(len(venue), len(b.bound_venue))
	for i in 0 ..< vn { b.bound_venue[i] = venue[i] }
	b.bound_venue_len = u8(vn)
	sn := min(len(symbol), len(b.bound_symbol))
	for i in 0 ..< sn { b.bound_symbol[i] = symbol[i] }
	b.bound_symbol_len = u8(sn)
}

binding_has :: proc(b: ^Stream_Binding) -> bool {
	if b == nil do return false
	return b.bound_venue_len > 0 &&
		b.bound_symbol_len > 0 &&
		b.bound_venue[0] != 0 &&
		b.bound_symbol[0] != 0
}

binding_clear :: proc(b: ^Stream_Binding) {
	if b == nil do return
	for i in 0 ..< len(b.bound_venue) { b.bound_venue[i] = 0 }
	for i in 0 ..< len(b.bound_symbol) { b.bound_symbol[i] = 0 }
	b.bound_venue_len = 0
	b.bound_symbol_len = 0
}
