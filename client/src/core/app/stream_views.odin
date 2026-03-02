package app

import "core:fmt"
import "core:strings"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:ui"
import "mr:util"

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

refresh_stream_info_for_slot :: proc(state: ^App_State, slot: ^Stream_View_Slot) {
	if state == nil || slot == nil do return
	if slot.subject_id == 0 do return
	if state.marketdata.describe_stream == nil do return

	info: ports.MD_Stream_Info
	if !state.marketdata.describe_stream(slot.subject_id, &info) do return

	slot.stream_info = info
	slot.has_stream_info = true
	if !slot.has_channel {
		slot.has_channel = true
		slot.channel = info.channel
	}
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

stream_view_find_slot :: proc(reg: ^Stream_View_Registry, subject_id: u64) -> int {
	if reg == nil do return -1
	if subject_id == 0 do return -1
	for i in 0 ..< len(reg.slots) {
		if reg.slots[i].used && reg.slots[i].subject_id == subject_id do return i
	}
	return -1
}

stream_view_repair_invariants :: proc(reg: ^Stream_View_Registry) -> bool {
	if reg == nil do return false
	repaired := false

	used_count := 0
	first_used_subject := u64(0)
	has_first_used := false

	for i in 0 ..< len(reg.slots) {
		if !reg.slots[i].used do continue
		used_count += 1
		if !has_first_used {
			has_first_used = true
			first_used_subject = reg.slots[i].subject_id
		}
	}

	if reg.count != used_count {
		reg.count = used_count
		repaired = true
	}

	if reg.count <= 0 {
		if reg.has_active || reg.active_subject_id != 0 {
			reg.has_active = false
			reg.active_subject_id = 0
			repaired = true
		}
		if repaired { reg.repair_count += 1 }
		return repaired
	}

	if reg.has_active {
		if stream_view_find_slot(reg, reg.active_subject_id) < 0 {
			reg.active_subject_id = first_used_subject
			repaired = true
		}
	} else {
		reg.has_active = true
		reg.active_subject_id = first_used_subject
		repaired = true
	}

	if repaired {
		reg.repair_count += 1
	}
	return repaired
}

stream_view_get_or_alloc_slot :: proc(reg: ^Stream_View_Registry, subject_id: u64, frame: u64, state: ^App_State = nil) -> ^Stream_View_Slot {
	if reg == nil do return nil
	if subject_id == 0 do return nil

	if idx := stream_view_find_slot(reg, subject_id); idx >= 0 {
		reg.slots[idx].last_seen_frame = frame
		return &reg.slots[idx]
	}

	slot_idx := -1
	for i in 0 ..< len(reg.slots) {
		if !reg.slots[i].used {
			slot_idx = i
			break
		}
	}

	if slot_idx < 0 {
		oldest_idx := -1
		oldest_frame := u64(0)
		for i in 0 ..< len(reg.slots) {
			if reg.has_active && reg.slots[i].subject_id == reg.active_subject_id do continue
			// G1 fix: skip slots referenced by any cell assignment.
			if state != nil && slot_referenced_by_cell(state, i) do continue
			if oldest_idx < 0 || reg.slots[i].last_seen_frame < oldest_frame {
				oldest_idx = i
				oldest_frame = reg.slots[i].last_seen_frame
			}
		}
		if oldest_idx < 0 {
			// All slots are referenced — fall back to absolute oldest (last resort).
			oldest_idx = 0
			oldest_frame = reg.slots[0].last_seen_frame
			for i in 1 ..< len(reg.slots) {
				if reg.slots[i].last_seen_frame < oldest_frame {
					oldest_idx = i
					oldest_frame = reg.slots[i].last_seen_frame
				}
			}
		}
		slot_idx = oldest_idx
		if slot_idx >= 0 && reg.slots[slot_idx].used {
			reg.eviction_count += 1
			// G1 fix: clear dangling cell references to the evicted slot.
			if state != nil {
				clear_cell_refs_to_slot(state, slot_idx)
			}
		}
	} else {
		reg.count += 1
	}

	reg.slots[slot_idx] = Stream_View_Slot{
		used            = true,
		subject_id      = subject_id,
		last_seen_frame = frame,
	}
	if !reg.has_active {
		reg.has_active = true
		reg.active_subject_id = subject_id
	}
	return &reg.slots[slot_idx]
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

sync_active_stream_view_to_global_stores :: proc(state: ^App_State) {
	reg := state.stream_views
	if reg == nil do return
	if !reg.has_active do return
	if idx := stream_view_find_slot(reg, reg.active_subject_id); idx >= 0 {
		slot := reg.slots[idx]
		if slot.has_stream_info {
			stream_id_buf: [streams.STREAM_ID_CAP]u8
			stream_id := build_stream_id_from_market_into(stream_id_buf[:], slot.stream_info.venue, slot.stream_info.symbol)
			streams.registry_set_active(&state.stream_registry, stream_id)
		}
		state.trades_store = slot.trades_store
		state.orderbook_store = slot.orderbook_store
		state.heatmap_store = slot.heatmap_store
		if state.heatmap_store.count <= 0 && slot.has_heatmap_snapshot {
			services.push_heatmap_snapshot(&state.heatmap_store, slot.heatmap_snapshot)
		}
		state.vpvr_store = slot.vpvr_store
		state.stats_store = slot.stats_store
		state.candle_store = slot.candle_store
		// Reset DOM fill tracking and footprint accumulation on stream switch.
		services.dom_store_reset(&state.dom_store)
		services.footprint_store_reset(&state.footprint_store)
	}
}

apply_cycle_stream_action :: proc(state: ^App_State, forward: bool) -> bool {
	_ = stream_view_repair_invariants(state.stream_views)

	is_offline := current_conn_status(state) == .Offline
	if is_offline {
		// In offline mode with no stream views, re-populate demo data.
		state.trades_store = {}
		state.orderbook_store = {}
		state.heatmap_store = {}
		state.vpvr_store = {}
		state.stats_store = {}
		state.candle_store = {}
		services.fill_demo_trades(&state.trades_store)
		services.fill_demo_orderbook(&state.orderbook_store)
		services.fill_demo_heatmaps(&state.heatmap_store)
		services.fill_demo_vpvr(&state.vpvr_store)
		services.fill_demo_stats(&state.stats_store)
		services.fill_demo_candles(&state.candle_store)
		state.active_has_live_stats = false
		state.active_has_live_heatmap = false
		state.active_has_live_vpvr = false
		state.active_has_live_candle = false
		return true
	}

	if !stream_view_cycle_active(state.stream_views, forward) do return false

	sync_active_stream_view_to_global_stores(state)
	persist_active_stream_subject(state)
	state.active_has_live_stats = false
	state.active_has_live_heatmap = false
	state.active_has_live_vpvr = false
	state.active_has_live_candle = false
	state.active_stream_last_stats_ts_ms = 0
	state.active_stream_last_orderbook_ts_ms = 0
	state.synth_heatmap_last_window = 0
	state.getrange_pending = false
	state.getrange_seeded = false
	state.getrange_subject_id = 0
	state.getrange_oldest_ts = 0
	state.active_candle_subject_id = 0 // Clear so stale batches from old stream are rejected.
	state.candle_health = .No_Data
	if now_ms := current_now_ms(state); now_ms > 0 {
		state.candle_last_recv_local_ms = now_ms
	}
	if state.candle_store.count <= 0 {
		request_active_stream_candle_range(state)
	}
	return true
}

request_active_stream_candle_range :: proc(state: ^App_State) {
	if state == nil do return
	if state.marketdata.send_getrange == nil do return
	if state.getrange_pending do return

	slot := stream_view_active_slot(state.stream_views)
	if slot == nil do return
	if !slot.has_stream_info {
		refresh_stream_info_for_slot(state, slot)
	}
	if !slot.has_stream_info do return

	info := slot.stream_info
	if len(info.venue) == 0 || len(info.symbol) == 0 do return

	limit := min(FETCH_CANDLES_RANGE_LEN, services.RANGE_CANDLE_PARSE_MAX)
	if limit <= 0 do limit = services.RANGE_CANDLE_PARSE_MAX
	if limit <= 0 do limit = 1

	tf_opts := TF_OPTIONS
	tf := tf_opts[0]
	if state.active_tf_idx >= 0 && state.active_tf_idx < len(tf_opts) {
		tf = tf_opts[state.active_tf_idx]
	}
	candle_subject := util.build_subject_with_timeframe(info.venue, info.symbol, .Candles, tf)
	sid := util.subject_id64(candle_subject)
	state.getrange_subject_id = sid
	state.active_candle_subject_id = sid
	state.marketdata.send_getrange(candle_subject, limit, 0)
	delete(candle_subject)
	state.getrange_pending = true
	state.getrange_seeded = true
	state.getrange_sent_frame = state.frame
}

// Request older candles (before the oldest we have) for lazy loading on scroll-left.
request_older_candles :: proc(state: ^App_State) {
	if state == nil do return
	if state.marketdata.send_getrange == nil do return
	if state.getrange_pending do return
	if state.getrange_oldest_ts <= 0 do return

	slot := stream_view_active_slot(state.stream_views)
	if slot == nil do return
	if !slot.has_stream_info do return

	info := slot.stream_info
	if len(info.venue) == 0 || len(info.symbol) == 0 do return

	limit := min(FETCH_CANDLES_RANGE_LEN, services.RANGE_CANDLE_PARSE_MAX)
	if limit <= 0 do limit = services.RANGE_CANDLE_PARSE_MAX
	if limit <= 0 do limit = 1

	tf_opts := TF_OPTIONS
	tf := tf_opts[0]
	if state.active_tf_idx >= 0 && state.active_tf_idx < len(tf_opts) {
		tf = tf_opts[state.active_tf_idx]
	}
	candle_subject := util.build_subject_with_timeframe(info.venue, info.symbol, .Candles, tf)
	state.getrange_subject_id = util.subject_id64(candle_subject)
	state.marketdata.send_getrange(candle_subject, limit, state.getrange_oldest_ts)
	delete(candle_subject)
	state.getrange_pending = true
	state.getrange_sent_frame = state.frame
}

// Resolve the effective TF index for a cell: per-cell if >= 0, else global.
cell_effective_tf_idx :: proc(state: ^App_State, cell: ^Cell_Assignment) -> int {
	if cell.tf_idx >= 0 && cell.tf_idx < len(TF_OPTIONS) {
		return cell.tf_idx
	}
	return state.active_tf_idx
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
	state.candle_store.head = 0
	state.candle_store.count = 0
	state.heatmap_store = {}
	state.vpvr_store = {}
	state.candle_zoom = 0
	state.candle_scroll_x = 0
	state.active_has_live_heatmap = false
	state.active_has_live_vpvr = false
	state.active_has_live_candle = false
	state.active_stream_last_stats_ts_ms = 0
	state.active_stream_last_orderbook_ts_ms = 0
	state.synth_heatmap_last_window = 0
	state.getrange_pending = false
	state.getrange_seeded = false
	state.getrange_subject_id = 0
	state.getrange_oldest_ts = 0
	state.candle_health = .No_Data
	if now_ms := current_now_ms(state); now_ms > 0 {
		state.candle_last_recv_local_ms = now_ms
	}

	// Update active candle subject_id for stale getrange guard.
	if as := stream_view_active_slot(state.stream_views); as != nil && as.has_stream_info {
		cs := util.build_subject_with_timeframe(as.stream_info.venue, as.stream_info.symbol, .Candles, tf)
		state.active_candle_subject_id = util.subject_id64(cs)
		delete(cs)
	}

	// Also clear timeframe-sensitive overlays in the active stream view slot.
	if slot := stream_view_active_slot(state.stream_views); slot != nil {
		slot.candle_store.head = 0
		slot.candle_store.count = 0
		slot.heatmap_store = {}
		slot.has_heatmap_snapshot = false
		slot.heatmap_snapshot = {}
		slot.vpvr_store = {}
	}

	// Clear TF-sensitive data for cells following global TF.
	for ci in 0 ..< state.cell_count {
		cell := &state.cell_assignments[ci]
		if cell.tf_idx >= 0 do continue  // per-cell TF, not affected
		cell.candle_scroll_x = 0
		cell.candle_zoom = 0
	}

	// Request historical data for the new TF.
	request_active_stream_candle_range(state)

	// Reconcile subscriptions since TF change affects candle/heatmap/vpvr subjects.
	reconcile_subscriptions(state)

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
	if cell_idx < 0 || cell_idx >= state.cell_count do return

	cell := &state.cell_assignments[cell_idx]
	if cell.getrange_pending do return

	// Only candle cells need backfill.
	if cell.widget != .Candle do return

	// Resolve venue/symbol from cell's stream binding.
	reg := state.stream_views
	if reg == nil do return

	venue, symbol: string
	// PRD-0009: prefer bound fields for venue/symbol resolution.
	if cell_has_binding(cell) {
		venue = cell_bound_venue(cell)
		symbol = cell_bound_symbol(cell)
	} else if cell.stream_idx >= 0 && cell.stream_idx < STREAM_VIEW_CAP && reg.slots[cell.stream_idx].used {
		slot := &reg.slots[cell.stream_idx]
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

	limit := min(FETCH_CANDLES_RANGE_LEN, services.RANGE_CANDLE_PARSE_MAX)
	if limit <= 0 do limit = services.RANGE_CANDLE_PARSE_MAX
	if limit <= 0 do limit = 1

	tf_opts := TF_OPTIONS
	eff_tf := cell_effective_tf_idx(state, cell)
	tf := tf_opts[eff_tf] if eff_tf >= 0 && eff_tf < len(tf_opts) else tf_opts[0]

	candle_subject := util.build_subject_with_timeframe(venue, symbol, .Candles, tf)
	state.marketdata.send_getrange(candle_subject, limit, cell.getrange_oldest_ts)
	delete(candle_subject)
	cell.getrange_pending = true
	cell.getrange_seeded = true
	cell.getrange_sent_frame = state.frame
}

// Request older candles for a specific cell (lazy loading on scroll-left).
request_cell_older_candles :: proc(state: ^App_State, cell_idx: int) {
	if state == nil do return
	if state.marketdata.send_getrange == nil do return
	if cell_idx < 0 || cell_idx >= state.cell_count do return

	cell := &state.cell_assignments[cell_idx]
	if cell.getrange_pending do return
	if cell.getrange_oldest_ts <= 0 do return
	if cell.widget != .Candle do return

	reg := state.stream_views
	if reg == nil do return

	venue, symbol: string
	if cell_has_binding(cell) {
		venue = cell_bound_venue(cell)
		symbol = cell_bound_symbol(cell)
	} else if cell.stream_idx >= 0 && cell.stream_idx < STREAM_VIEW_CAP && reg.slots[cell.stream_idx].used {
		slot := &reg.slots[cell.stream_idx]
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

	limit := min(FETCH_CANDLES_RANGE_LEN, services.RANGE_CANDLE_PARSE_MAX)
	if limit <= 0 do limit = services.RANGE_CANDLE_PARSE_MAX
	if limit <= 0 do limit = 1

	tf_opts := TF_OPTIONS
	eff_tf := cell_effective_tf_idx(state, cell)
	tf := tf_opts[eff_tf] if eff_tf >= 0 && eff_tf < len(tf_opts) else tf_opts[0]

	candle_subject := util.build_subject_with_timeframe(venue, symbol, .Candles, tf)
	state.marketdata.send_getrange(candle_subject, limit, cell.getrange_oldest_ts)
	delete(candle_subject)
	cell.getrange_pending = true
	cell.getrange_sent_frame = state.frame
}

// Set a per-cell timeframe. -1 means revert to following global.
apply_set_cell_timeframe_action :: proc(state: ^App_State, cell_idx: int, tf_idx: int) -> bool {
	if cell_idx < 0 || cell_idx >= state.cell_count do return false
	if tf_idx < -1 || tf_idx >= len(TF_OPTIONS) do return false

	cell := &state.cell_assignments[cell_idx]
	if cell.tf_idx == tf_idx do return false

	cell.tf_idx = tf_idx
	cell.candle_scroll_x = 0
	cell.candle_zoom = 0
	cell.getrange_pending = false
	cell.getrange_seeded = false
	cell.getrange_oldest_ts = 0

	// Clear the cell's stream slot candle/heatmap/vpvr data for fresh TF data.
	if cell.stream_idx >= 0 && cell.stream_idx < STREAM_VIEW_CAP {
		reg := state.stream_views
		if reg != nil && reg.slots[cell.stream_idx].used {
			slot := &reg.slots[cell.stream_idx]
			slot.candle_store.head = 0
			slot.candle_store.count = 0
			slot.heatmap_store = {}
			slot.has_heatmap_snapshot = false
			slot.heatmap_snapshot = {}
			slot.vpvr_store = {}
		}
	}

	// Persist and reconcile subscriptions for the new TF.
	persist_layout_v4(state)
	reconcile_subscriptions(state)

	// Request historical candle data for the new TF.
	request_cell_candle_range(state, cell_idx)

	return true
}

stream_view_active_slot :: proc(reg: ^Stream_View_Registry) -> ^Stream_View_Slot {
	if reg == nil do return nil
	if !reg.has_active do return nil
	if idx := stream_view_find_slot(reg, reg.active_subject_id); idx >= 0 {
		return &reg.slots[idx]
	}
	return nil
}

// Returns 0-based index of the active stream among used slots (for "2/5" display).
stream_view_active_index :: proc(reg: ^Stream_View_Registry) -> int {
	if reg == nil || !reg.has_active do return 0
	n := 0
	for i in 0 ..< STREAM_VIEW_CAP {
		if !reg.slots[i].used do continue
		if reg.slots[i].subject_id == reg.active_subject_id do return n
		n += 1
	}
	return 0
}

slot_market_key_known :: proc(slot: ^Stream_View_Slot) -> bool {
	if slot == nil do return false
	if !slot.has_stream_info do return false
	if len(slot.stream_info.venue) == 0 do return false
	if len(slot.stream_info.symbol) == 0 do return false
	return true
}

// ═══════════════════════════════════════════════════════════════
// PRD-0009: Intent-driven cell binding helpers (zero heap alloc).
// ═══════════════════════════════════════════════════════════════

cell_bound_venue :: proc(cell: ^Cell_Assignment) -> string {
	if cell == nil || cell.bound_venue_len == 0 do return ""
	n := int(cell.bound_venue_len)
	if n > len(cell.bound_venue) do n = len(cell.bound_venue)
	return string(cell.bound_venue[:n])
}

cell_bound_symbol :: proc(cell: ^Cell_Assignment) -> string {
	if cell == nil || cell.bound_symbol_len == 0 do return ""
	n := int(cell.bound_symbol_len)
	if n > len(cell.bound_symbol) do n = len(cell.bound_symbol)
	return string(cell.bound_symbol[:n])
}

cell_set_binding :: proc(cell: ^Cell_Assignment, venue: string, symbol: string) {
	if cell == nil do return
	vn := min(len(venue), len(cell.bound_venue))
	for i in 0 ..< vn { cell.bound_venue[i] = venue[i] }
	cell.bound_venue_len = u8(vn)

	sn := min(len(symbol), len(cell.bound_symbol))
	for i in 0 ..< sn { cell.bound_symbol[i] = symbol[i] }
	cell.bound_symbol_len = u8(sn)
}

cell_has_binding :: proc(cell: ^Cell_Assignment) -> bool {
	if cell == nil do return false
	return cell.bound_venue_len > 0
}

cell_clear_binding :: proc(cell: ^Cell_Assignment) {
	if cell == nil do return
	cell.bound_venue_len = 0
	cell.bound_symbol_len = 0
}

// Initialize cell_assignments from legacy 7-panel layout.
layout_from_legacy :: proc(state: ^App_State) {
	PANEL_WIDGET_MAP :: [ui.PANEL_COUNT]Widget_Kind{
		.Candle, .Stats, .Counter, .Heatmap, .VPVR, .Trades, .Orderbook,
	}
	state.cell_count = 0
	panel_map := PANEL_WIDGET_MAP
	for i in 0 ..< ui.PANEL_COUNT {
		if !state.panel_visible[i] do continue
		if state.cell_count >= CELL_MAX do break
		ci := state.cell_count
		cell := make_default_cell(state, panel_map[i])
		cell.candle_scroll_x     = state.candle_scroll_x
		cell.candle_zoom         = state.candle_zoom
		cell.crosshair           = state.candle_crosshair
		cell.ob_scroll_y         = state.ob_scroll_y
		cell.ob_group_idx        = state.ob_group_idx
		cell.trades_scroll_y     = state.scroll_y
		cell.trade_filter_idx    = state.trade_filter_idx
		cell.chart_type          = state.candle_chart_type
		state.cell_assignments[ci] = cell
		state.cell_count += 1
	}
}

// G1 helper: check if any cell references a given slot index (by index or binding match).
@(private = "file")
slot_referenced_by_cell :: proc(state: ^App_State, slot_idx: int) -> bool {
	reg := state.stream_views
	for ci in 0 ..< state.cell_count {
		cell := &state.cell_assignments[ci]
		if cell.stream_idx == slot_idx do return true
		// PRD-0009: also protect slots matching a cell's venue/symbol binding.
		if cell_has_binding(cell) && reg != nil && slot_idx >= 0 && slot_idx < STREAM_VIEW_CAP && reg.slots[slot_idx].used {
			slot := &reg.slots[slot_idx]
			if slot.has_stream_info &&
				normalized_venue(slot.stream_info.venue) == normalized_venue(cell_bound_venue(cell)) &&
				normalized_symbol(slot.stream_info.symbol) == normalized_symbol(cell_bound_symbol(cell)) {
				return true
			}
		}
	}
	return false
}

// G1 helper: clear cell references to an evicted slot (reset stream_idx for lazy re-resolution).
// PRD-0009: bound_venue/bound_symbol are preserved so the cell can re-resolve when the slot returns.
@(private = "file")
clear_cell_refs_to_slot :: proc(state: ^App_State, slot_idx: int) {
	for ci in 0 ..< state.cell_count {
		if state.cell_assignments[ci].stream_idx == slot_idx {
			state.cell_assignments[ci].stream_idx = -1 // binding preserved for re-resolution
		}
	}
}

// Resolve data stores for a cell assignment. Returns pointers to the appropriate stores.
Cell_Stores :: struct {
	candle:    ^services.Candle_Store,
	heatmap:   ^services.Heatmap_Store,
	vpvr:      ^services.VPVR_Store,
	trades:    ^services.Trades_Store,
	orderbook: ^services.Orderbook_Store,
	stats:     ^services.Stats_Store,
}

@(private = "file")
find_market_channel_slot :: proc(
	state: ^App_State,
	reg: ^Stream_View_Registry,
	venue, symbol: string,
	channel: ports.MD_Channel,
	timeframe: string = "",
) -> ^Stream_View_Slot {
	if state == nil || reg == nil do return nil
	if len(venue) == 0 || len(symbol) == 0 do return nil

	best_idx := -1
	best_seen := u64(0)
	want_venue := normalized_venue(venue)
	want_symbol := normalized_symbol(symbol)
	for si in 0 ..< STREAM_VIEW_CAP {
		if !reg.slots[si].used do continue
		slot := &reg.slots[si]
		if !slot_market_key_known(slot) {
			refresh_stream_info_for_slot(state, slot)
		}
		if !slot_market_key_known(slot) do continue
		if normalized_venue(slot.stream_info.venue) != want_venue || normalized_symbol(slot.stream_info.symbol) != want_symbol do continue

		slot_ch := slot.channel
		if !slot.has_channel {
			slot_ch = slot.stream_info.channel
		}
		if slot_ch != channel do continue

		if len(timeframe) > 0 {
			slot_tf := slot.stream_info.timeframe
			if len(slot_tf) > 0 && slot_tf != timeframe do continue
		}

		if best_idx < 0 || slot.last_seen_frame > best_seen {
			best_idx = si
			best_seen = slot.last_seen_frame
		}
	}

	if best_idx < 0 do return nil
	return &reg.slots[best_idx]
}

resolve_stores_for_cell :: proc(state: ^App_State, cell: ^Cell_Assignment, cell_idx: int = -1) -> Cell_Stores {
	stores: Cell_Stores

	// Default fallback: active/global stores.
	stores.candle    = &state.candle_store
	stores.heatmap   = &state.heatmap_store
	stores.vpvr      = &state.vpvr_store
	stores.trades    = &state.trades_store
	stores.orderbook = &state.orderbook_store
	stores.stats     = &state.stats_store

	reg := state.stream_views
	if reg == nil do return stores

	// Follow-active cells (stream_idx=-1) use global stores by default.
	// If a binding exists, try to resolve it to a concrete slot first.
	if cell.stream_idx < 0 {
		if cell_has_binding(cell) {
			bv := cell_bound_venue(cell)
			bs := cell_bound_symbol(cell)
			best_idx := -1
			best_seen := u64(0)
			for si in 0 ..< STREAM_VIEW_CAP {
				if !reg.slots[si].used do continue
				slot := &reg.slots[si]
				if !slot_market_key_known(slot) { refresh_stream_info_for_slot(state, slot) }
				if !slot_market_key_known(slot) do continue
				if normalized_venue(slot.stream_info.venue) != normalized_venue(bv) || normalized_symbol(slot.stream_info.symbol) != normalized_symbol(bs) do continue
				priority := 0
				slot_ch := slot.channel
				if !slot.has_channel { slot_ch = slot.stream_info.channel }
				if slot_ch == .Candles do priority = 1
				if best_idx < 0 || priority > 0 || slot.last_seen_frame > best_seen {
					best_idx = si
					best_seen = slot.last_seen_frame
					if priority > 0 do break
				}
			}
			if best_idx >= 0 {
				cell.stream_idx = best_idx
			}
		}
		if cell.stream_idx < 0 {
			return stores
		}
	}

	venue, symbol: string
	if cell.stream_idx >= 0 && cell.stream_idx < STREAM_VIEW_CAP && reg.slots[cell.stream_idx].used {
		slot := &reg.slots[cell.stream_idx]
		if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
		if slot.has_stream_info {
			venue = slot.stream_info.venue
			symbol = slot.stream_info.symbol
		}
	} else {
		cell.stream_idx = -1
		return stores
	}

	if len(venue) == 0 || len(symbol) == 0 do return stores

	// Active market should always read from global stores, which are already
	// synchronized across per-channel subjects for the selected active stream.
	if active := stream_view_active_slot(reg); active != nil {
		if !active.has_stream_info { refresh_stream_info_for_slot(state, active) }
		if active.has_stream_info &&
			normalized_venue(active.stream_info.venue) == normalized_venue(venue) &&
			normalized_symbol(active.stream_info.symbol) == normalized_symbol(symbol) {
			return stores
		}
	}

	tf_opts := TF_OPTIONS
	eff_tf := cell_effective_tf_idx(state, cell)
	cell_tf := tf_opts[0]
	if eff_tf >= 0 && eff_tf < len(tf_opts) {
		cell_tf = tf_opts[eff_tf]
	}

	// Resolve each channel from the best slot for this market.
	if slot := find_market_channel_slot(state, reg, venue, symbol, .Candles, cell_tf); slot != nil {
		stores.candle = &slot.candle_store
	}
	if slot := find_market_channel_slot(state, reg, venue, symbol, .Heatmaps, cell_tf); slot != nil {
		stores.heatmap = &slot.heatmap_store
	}
	if slot := find_market_channel_slot(state, reg, venue, symbol, .VPVR, cell_tf); slot != nil {
		stores.vpvr = &slot.vpvr_store
	}
	if slot := find_market_channel_slot(state, reg, venue, symbol, .Trades); slot != nil {
		stores.trades = &slot.trades_store
	}
	if slot := find_market_channel_slot(state, reg, venue, symbol, .Orderbook); slot != nil {
		stores.orderbook = &slot.orderbook_store
	}
	if slot := find_market_channel_slot(state, reg, venue, symbol, .Stats); slot != nil {
		stores.stats = &slot.stats_store
	}

	return stores
}

// Persist cell layout to settings. Format: "N:K0,K1,...,KN-1" where Ki = Widget_Kind enum value.
persist_layout :: proc(state: ^App_State) {
	buf: [128]u8
	off := 0
	// Write cell count.
	n := state.cell_count
	if n > 9 {
		buf[off] = '0' + u8(n / 10); off += 1
	}
	buf[off] = '0' + u8(n % 10); off += 1
	buf[off] = ':'; off += 1
	// Write widget kinds.
	for i in 0 ..< n {
		if i > 0 { buf[off] = ','; off += 1 }
		k := int(state.cell_assignments[i].widget)
		buf[off] = '0' + u8(k); off += 1
	}
	services.settings_set(&state.settings, services.SETTING_LAYOUT, string(buf[:off]))
	// Persist preset index.
	preset_buf: [4]u8
	services.settings_set(&state.settings, services.SETTING_LAYOUT_PRESET,
		fmt.bprintf(preset_buf[:], "%d", state.layout_preset))
	services.settings_flush(&state.settings)
}

// Restore cell layout from settings. Returns true if layout was restored.
restore_layout :: proc(state: ^App_State) -> bool {
	v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT)
	if !ok || len(v) < 3 do return false
	return restore_layout_from_string(state, v)
}

// Parse and apply a layout string. Format: "N:K0,K1,...,KN-1".
restore_layout_from_string :: proc(state: ^App_State, v: string) -> bool {
	if len(v) < 3 do return false

	// Parse cell count (1-2 digits before ':').
	colon_idx := -1
	for i in 0 ..< len(v) {
		if v[i] == ':' { colon_idx = i; break }
	}
	if colon_idx <= 0 || colon_idx > 2 do return false

	n := 0
	for i in 0 ..< colon_idx {
		d := int(v[i]) - '0'
		if d < 0 || d > 9 do return false
		n = n * 10 + d
	}
	if n <= 0 || n > CELL_MAX do return false

	// Parse widget kinds after colon.
	rest := v[colon_idx + 1:]
	kinds: [CELL_MAX]Widget_Kind
	ki := 0
	for c in rest {
		if c == ',' do continue
		d := int(c) - '0'
		if d < 0 || d > 7 do return false
		if ki >= n do break
		kinds[ki] = Widget_Kind(d)
		ki += 1
	}
	if ki != n do return false

	// Apply.
	state.cell_count = n
	for i in 0 ..< n {
		state.cell_assignments[i] = make_default_cell(state, kinds[i])
	}
	return true
}

// ═══════════════════════════════════════════════════════════════
// V2 persistence — captures stream binding + indicator flags.
// Format: "V2|K:S:F|K:S:F|..." (pipe-delimited cells)
//   K = widget kind digit (0-8)
//   S = stream binding: "-1" for follow-active, or "venue/symbol"
//   F = indicator flags bitfield (8 bools packed into decimal int)
// ═══════════════════════════════════════════════════════════════

// Pack 8 indicator booleans into a single integer.
pack_indicator_flags :: proc(cell: ^Cell_Assignment) -> int {
	f := 0
	if cell.show_ma             do f |= 1 << 0
	if cell.show_bbands         do f |= 1 << 1
	if cell.show_vwap           do f |= 1 << 2
	if cell.show_rsi            do f |= 1 << 3
	if cell.show_macd           do f |= 1 << 4
	if cell.show_funding        do f |= 1 << 5
	if cell.show_liq            do f |= 1 << 6
	if cell.show_trade_counter  do f |= 1 << 7
	return f
}

// Unpack indicator flags into a cell.
unpack_indicator_flags :: proc(cell: ^Cell_Assignment, f: int) {
	cell.show_ma            = (f & (1 << 0)) != 0
	cell.show_bbands        = (f & (1 << 1)) != 0
	cell.show_vwap          = (f & (1 << 2)) != 0
	cell.show_rsi           = (f & (1 << 3)) != 0
	cell.show_macd          = (f & (1 << 4)) != 0
	cell.show_funding       = (f & (1 << 5)) != 0
	cell.show_liq           = (f & (1 << 6)) != 0
	cell.show_trade_counter = (f & (1 << 7)) != 0
}

persist_layout_v2 :: proc(state: ^App_State) {
	buf: [1024]u8
	off := 0

	// Header: "V2"
	buf[off] = 'V'; off += 1
	buf[off] = '2'; off += 1

	n := state.cell_count
	for i in 0 ..< n {
		cell := &state.cell_assignments[i]
		buf[off] = '|'; off += 1

		// K: widget kind digit.
		buf[off] = '0' + u8(cell.widget); off += 1
		buf[off] = ':'; off += 1

		// S: stream binding.
		if cell.stream_idx < 0 {
			buf[off] = '-'; off += 1
			buf[off] = '1'; off += 1
		} else {
			// Look up venue/symbol from stream_views.
			reg := state.stream_views
			wrote_stream := false
			if reg != nil && cell.stream_idx >= 0 && cell.stream_idx < STREAM_VIEW_CAP && reg.slots[cell.stream_idx].used {
				slot := &reg.slots[cell.stream_idx]
				if !slot.has_stream_info {
					refresh_stream_info_for_slot(state, slot)
				}
				if slot.has_stream_info && len(slot.stream_info.venue) > 0 && len(slot.stream_info.symbol) > 0 {
					// Write venue/symbol.
					v := slot.stream_info.venue
					s := slot.stream_info.symbol
					for vi in 0 ..< len(v) {
						if off < len(buf) { buf[off] = v[vi]; off += 1 }
					}
					buf[off] = '/'; off += 1
					for si in 0 ..< len(s) {
						if off < len(buf) { buf[off] = s[si]; off += 1 }
					}
					wrote_stream = true
				}
			}
			if !wrote_stream {
				buf[off] = '-'; off += 1
				buf[off] = '1'; off += 1
			}
		}
		buf[off] = ':'; off += 1

		// F: indicator flags.
		flags := pack_indicator_flags(cell)
		if flags >= 100 {
			buf[off] = '0' + u8(flags / 100); off += 1
		}
		if flags >= 10 {
			buf[off] = '0' + u8((flags / 10) % 10); off += 1
		}
		buf[off] = '0' + u8(flags % 10); off += 1
	}

	services.settings_set(&state.settings, services.SETTING_LAYOUT_V2, string(buf[:off]))
	// Also persist V1 for backwards compatibility.
	persist_layout(state)
}

// ═══════════════════════════════════════════════════════════════
// V4 persistence — extends V3 with per-cell timeframe.
// Format: "V4|MODE|CW:w0,w1,...|RW:w0,w1,...|K:S:F:CS:RS:SM:SR:TF|..."
//   TF = tf_idx+1 (0 = follow global, 1-9 = per-cell TF 0-8)
// ═══════════════════════════════════════════════════════════════

persist_layout_v4 :: proc(state: ^App_State) {
	buf: [2048]u8
	off := 0

	// Header: "V4"
	buf[off] = 'V'; off += 1
	buf[off] = '4'; off += 1

	// MODE.
	buf[off] = '|'; off += 1
	buf[off] = state.layout_mode == .Custom ? 'C' : 'P'; off += 1

	// CW: col weights.
	buf[off] = '|'; off += 1
	buf[off] = 'C'; off += 1
	buf[off] = 'W'; off += 1
	buf[off] = ':'; off += 1
	for c in 0 ..< state.custom_grid_def.col_count {
		if c > 0 { buf[off] = ','; off += 1 }
		w := int(state.custom_grid_def.col_weights[c] * 100 + 0.5)
		off = write_int_to_buf(buf[:], off, w)
	}

	// RW: row weights.
	buf[off] = '|'; off += 1
	buf[off] = 'R'; off += 1
	buf[off] = 'W'; off += 1
	buf[off] = ':'; off += 1
	for r in 0 ..< state.custom_grid_def.row_count {
		if r > 0 { buf[off] = ','; off += 1 }
		w := int(state.custom_grid_def.row_weights[r] * 100 + 0.5)
		off = write_int_to_buf(buf[:], off, w)
	}

	// Cells: K:S:F:CS:RS:SM:SR:TF per cell.
	n := state.cell_count
	for i in 0 ..< n {
		cell := &state.cell_assignments[i]
		buf[off] = '|'; off += 1

		// K: widget kind.
		buf[off] = '0' + u8(cell.widget); off += 1
		buf[off] = ':'; off += 1

		// S: stream binding — read from cell's bound fields (PRD-0009).
		bv := cell_bound_venue(cell)
		bs := cell_bound_symbol(cell)
		if len(bv) > 0 && len(bs) > 0 {
			for vi in 0 ..< len(bv) { if off < len(buf) { buf[off] = bv[vi]; off += 1 } }
			buf[off] = '/'; off += 1
			for si in 0 ..< len(bs) { if off < len(buf) { buf[off] = bs[si]; off += 1 } }
		} else {
			buf[off] = '-'; off += 1
			buf[off] = '1'; off += 1
		}
		buf[off] = ':'; off += 1

		// F: indicator flags.
		flags := pack_indicator_flags(cell)
		off = write_int_to_buf(buf[:], off, flags)
		buf[off] = ':'; off += 1

		// CS: col_span.
		cs := cell.col_span > 1 ? cell.col_span : 1
		off = write_int_to_buf(buf[:], off, cs)
		buf[off] = ':'; off += 1

		// RS: row_span.
		rs := cell.row_span > 1 ? cell.row_span : 1
		off = write_int_to_buf(buf[:], off, rs)
		buf[off] = ':'; off += 1

		// SM: sub_main_split (×1000).
		sm := int(cell.sub_main_split * 1000 + 0.5)
		off = write_int_to_buf(buf[:], off, sm)
		buf[off] = ':'; off += 1

		// SR: sub_ratios (×1000, comma-separated).
		for sri in 0 ..< 5 {
			if sri > 0 { buf[off] = ','; off += 1 }
			sr := int(cell.sub_ratios[sri] * 1000 + 0.5)
			off = write_int_to_buf(buf[:], off, sr)
		}
		buf[off] = ':'; off += 1

		// TF: tf_idx+1 (0 = global, 1-9 = per-cell).
		tf_val := cell.tf_idx + 1
		if tf_val < 0 { tf_val = 0 }
		off = write_int_to_buf(buf[:], off, tf_val)
	}

	services.settings_set(&state.settings, services.SETTING_LAYOUT_V4, string(buf[:off]))
	services.settings_set(&state.settings, services.SETTING_LAYOUT_MODE,
		state.layout_mode == .Custom ? "C" : "P")
	// Also persist V3, V2, V1 for backwards compatibility.
	persist_layout_v3(state)
}

// ═══════════════════════════════════════════════════════════════
// V3 persistence — extends V2 with layout mode, weights, and spans.
// Format: "V3|MODE|CW:w0,w1,...|RW:w0,w1,...|K:S:F:CS:RS|..."
//   MODE = P (Preset) or C (Custom)
//   CW: = col weights (comma-sep integers, weight*100)
//   RW: = row weights (comma-sep integers, weight*100)
//   K:S:F:CS:RS = widget kind, stream, flags, col_span, row_span
// ═══════════════════════════════════════════════════════════════

persist_layout_v3 :: proc(state: ^App_State) {
	buf: [2048]u8
	off := 0

	// Header: "V3"
	buf[off] = 'V'; off += 1
	buf[off] = '3'; off += 1

	// MODE.
	buf[off] = '|'; off += 1
	buf[off] = state.layout_mode == .Custom ? 'C' : 'P'; off += 1

	// CW: col weights.
	buf[off] = '|'; off += 1
	buf[off] = 'C'; off += 1
	buf[off] = 'W'; off += 1
	buf[off] = ':'; off += 1
	for c in 0 ..< state.custom_grid_def.col_count {
		if c > 0 { buf[off] = ','; off += 1 }
		w := int(state.custom_grid_def.col_weights[c] * 100 + 0.5)
		off = write_int_to_buf(buf[:], off, w)
	}

	// RW: row weights.
	buf[off] = '|'; off += 1
	buf[off] = 'R'; off += 1
	buf[off] = 'W'; off += 1
	buf[off] = ':'; off += 1
	for r in 0 ..< state.custom_grid_def.row_count {
		if r > 0 { buf[off] = ','; off += 1 }
		w := int(state.custom_grid_def.row_weights[r] * 100 + 0.5)
		off = write_int_to_buf(buf[:], off, w)
	}

	// Cells: K:S:F:CS:RS per cell.
	n := state.cell_count
	for i in 0 ..< n {
		cell := &state.cell_assignments[i]
		buf[off] = '|'; off += 1

		// K: widget kind.
		buf[off] = '0' + u8(cell.widget); off += 1
		buf[off] = ':'; off += 1

		// S: stream binding — read from cell's bound fields (PRD-0009).
		bv := cell_bound_venue(cell)
		bs := cell_bound_symbol(cell)
		if len(bv) > 0 && len(bs) > 0 {
			for vi in 0 ..< len(bv) { if off < len(buf) { buf[off] = bv[vi]; off += 1 } }
			buf[off] = '/'; off += 1
			for si in 0 ..< len(bs) { if off < len(buf) { buf[off] = bs[si]; off += 1 } }
		} else {
			buf[off] = '-'; off += 1
			buf[off] = '1'; off += 1
		}
		buf[off] = ':'; off += 1

		// F: indicator flags.
		flags := pack_indicator_flags(cell)
		off = write_int_to_buf(buf[:], off, flags)
		buf[off] = ':'; off += 1

		// CS: col_span.
		cs := cell.col_span > 1 ? cell.col_span : 1
		off = write_int_to_buf(buf[:], off, cs)
		buf[off] = ':'; off += 1

		// RS: row_span.
		rs := cell.row_span > 1 ? cell.row_span : 1
		off = write_int_to_buf(buf[:], off, rs)
		buf[off] = ':'; off += 1

		// SM: sub_main_split (×1000).
		sm := int(cell.sub_main_split * 1000 + 0.5)
		off = write_int_to_buf(buf[:], off, sm)
		buf[off] = ':'; off += 1

		// SR: sub_ratios (×1000, comma-separated).
		for sri in 0 ..< 5 {
			if sri > 0 { buf[off] = ','; off += 1 }
			sr := int(cell.sub_ratios[sri] * 1000 + 0.5)
			off = write_int_to_buf(buf[:], off, sr)
		}
	}

	services.settings_set(&state.settings, services.SETTING_LAYOUT_V3, string(buf[:off]))
	services.settings_set(&state.settings, services.SETTING_LAYOUT_MODE,
		state.layout_mode == .Custom ? "C" : "P")
	// Also persist V2 and V1 for backwards compatibility.
	persist_layout_v2(state)
}

// Helper: write a non-negative integer to buf at off, returns new off.
@(private = "file")
write_int_to_buf :: proc(buf: []u8, start: int, val: int) -> int {
	off := start
	v := val
	if v <= 0 {
		if off < len(buf) { buf[off] = '0'; off += 1 }
		return off
	}
	// Write digits in reverse, then flip.
	d_start := off
	for v > 0 && off < len(buf) {
		buf[off] = '0' + u8(v % 10)
		off += 1
		v /= 10
	}
	// Reverse the digits.
	lo := d_start
	hi := off - 1
	for lo < hi {
		buf[lo], buf[hi] = buf[hi], buf[lo]
		lo += 1
		hi -= 1
	}
	return off
}

// Parse a non-negative integer from a string.
@(private = "file")
parse_int_from :: proc(s: string) -> int {
	v := 0
	for c in s {
		if c < '0' || c > '9' do break
		v = v * 10 + int(c - '0')
	}
	return v
}

restore_layout_v3 :: proc(state: ^App_State) -> bool {
	v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V3)
	if !ok || len(v) < 4 do return false
	if v[0] != 'V' || v[1] != '3' do return false

	rest := v[2:]
	pos := 0

	// Parse MODE.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos >= len(rest) do return false
	mode_ch := rest[pos]
	pos += 1
	if mode_ch == 'C' {
		state.layout_mode = .Custom
	} else {
		state.layout_mode = .Preset
	}

	// Parse CW: col weights.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos + 2 >= len(rest) || rest[pos] != 'C' || rest[pos + 1] != 'W' || rest[pos + 2] != ':' do return false
	pos += 3
	cw_start := pos
	for pos < len(rest) && rest[pos] != '|' { pos += 1 }
	cw_field := rest[cw_start:pos]
	// Parse comma-separated col weights.
	col_idx := 0
	seg_start := 0
	for ci in 0 ..= len(cw_field) {
		if ci == len(cw_field) || cw_field[ci] == ',' {
			if ci > seg_start && col_idx < ui.GRID_MAX_COLS {
				w := parse_int_from(cw_field[seg_start:ci])
				state.custom_grid_def.col_weights[col_idx] = f32(w) / 100.0
				col_idx += 1
			}
			seg_start = ci + 1
		}
	}
	if col_idx > 0 { state.custom_grid_def.col_count = col_idx }

	// Parse RW: row weights.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos + 2 >= len(rest) || rest[pos] != 'R' || rest[pos + 1] != 'W' || rest[pos + 2] != ':' do return false
	pos += 3
	rw_start := pos
	for pos < len(rest) && rest[pos] != '|' { pos += 1 }
	rw_field := rest[rw_start:pos]
	row_idx := 0
	seg_start = 0
	for ri in 0 ..= len(rw_field) {
		if ri == len(rw_field) || rw_field[ri] == ',' {
			if ri > seg_start && row_idx < ui.GRID_MAX_ROWS {
				w := parse_int_from(rw_field[seg_start:ri])
				state.custom_grid_def.row_weights[row_idx] = f32(w) / 100.0
				row_idx += 1
			}
			seg_start = ri + 1
		}
	}
	if row_idx > 0 { state.custom_grid_def.row_count = row_idx }

	// Parse cells.
	cell_count := 0
	cells: [CELL_MAX]Cell_Assignment

	for cell_count < CELL_MAX && pos < len(rest) {
		if rest[pos] != '|' do break
		pos += 1
		if pos >= len(rest) do break

		// K: widget kind.
		k_digit := int(rest[pos]) - '0'
		if k_digit < 0 || k_digit > 8 do break
		pos += 1
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// S: stream binding (until next ':').
		s_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		s_field := rest[s_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// F: flags (until next ':').
		f_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		f_field := rest[f_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// CS: col_span (until next ':').
		cs_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		cs_field := rest[cs_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// RS: row_span (until next ':' or '|' or end).
		rs_start := pos
		for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
		rs_field := rest[rs_start:pos]

		// SM: sub_main_split (optional, ×1000).
		sm_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sm_start := pos
			for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
			sm_field = rest[sm_start:pos]
		}

		// SR: sub_ratios (optional, comma-separated ×1000).
		sr_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sr_start := pos
			for pos < len(rest) && rest[pos] != '|' { pos += 1 }
			sr_field = rest[sr_start:pos]
		}

		// Build cell.
		cell := make_default_cell(state, Widget_Kind(k_digit))

		if s_field == "-1" {
			cell.stream_idx = -1
			cell_clear_binding(&cell)
		} else {
			slash := -1
			for si in 0 ..< len(s_field) {
				if s_field[si] == '/' { slash = si; break }
			}
			if slash > 0 && slash < len(s_field) - 1 {
				cell_set_binding(&cell, s_field[:slash], s_field[slash + 1:])
			}
			cell.stream_idx = -1
		}

		flags := parse_int_from(f_field)
		unpack_indicator_flags(&cell, flags)

		cs := parse_int_from(cs_field)
		rs := parse_int_from(rs_field)
		cell.col_span = cs > 1 ? cs : 1
		cell.row_span = rs > 1 ? rs : 1

		// Subplot ratios.
		if len(sm_field) > 0 {
			cell.sub_main_split = f32(parse_int_from(sm_field)) / 1000.0
		}
		if len(sr_field) > 0 {
			sr_idx := 0
			sr_seg_start := 0
			for si in 0 ..= len(sr_field) {
				if si == len(sr_field) || sr_field[si] == ',' {
					if si > sr_seg_start && sr_idx < 5 {
						cell.sub_ratios[sr_idx] = f32(parse_int_from(sr_field[sr_seg_start:si])) / 1000.0
						sr_idx += 1
					}
					sr_seg_start = si + 1
				}
			}
		}

		cells[cell_count] = cell
		cell_count += 1
	}

	if cell_count <= 0 do return false

	state.cell_count = cell_count
	for i in 0 ..< cell_count {
		state.cell_assignments[i] = cells[i]
	}

	return true
}

restore_layout_v4 :: proc(state: ^App_State) -> bool {
	v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V4)
	if !ok || len(v) < 4 do return false
	if v[0] != 'V' || v[1] != '4' do return false

	rest := v[2:]
	pos := 0

	// Parse MODE.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos >= len(rest) do return false
	mode_ch := rest[pos]
	pos += 1
	if mode_ch == 'C' {
		state.layout_mode = .Custom
	} else {
		state.layout_mode = .Preset
	}

	// Parse CW: col weights.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos + 2 >= len(rest) || rest[pos] != 'C' || rest[pos + 1] != 'W' || rest[pos + 2] != ':' do return false
	pos += 3
	cw_start := pos
	for pos < len(rest) && rest[pos] != '|' { pos += 1 }
	cw_field := rest[cw_start:pos]
	col_idx := 0
	seg_start := 0
	for ci in 0 ..= len(cw_field) {
		if ci == len(cw_field) || cw_field[ci] == ',' {
			if ci > seg_start && col_idx < ui.GRID_MAX_COLS {
				w := parse_int_from(cw_field[seg_start:ci])
				state.custom_grid_def.col_weights[col_idx] = f32(w) / 100.0
				col_idx += 1
			}
			seg_start = ci + 1
		}
	}
	if col_idx > 0 { state.custom_grid_def.col_count = col_idx }

	// Parse RW: row weights.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos + 2 >= len(rest) || rest[pos] != 'R' || rest[pos + 1] != 'W' || rest[pos + 2] != ':' do return false
	pos += 3
	rw_start := pos
	for pos < len(rest) && rest[pos] != '|' { pos += 1 }
	rw_field := rest[rw_start:pos]
	row_idx := 0
	seg_start = 0
	for ri in 0 ..= len(rw_field) {
		if ri == len(rw_field) || rw_field[ri] == ',' {
			if ri > seg_start && row_idx < ui.GRID_MAX_ROWS {
				w := parse_int_from(rw_field[seg_start:ri])
				state.custom_grid_def.row_weights[row_idx] = f32(w) / 100.0
				row_idx += 1
			}
			seg_start = ri + 1
		}
	}
	if row_idx > 0 { state.custom_grid_def.row_count = row_idx }

	// Parse cells.
	cell_count := 0
	cells: [CELL_MAX]Cell_Assignment

	for cell_count < CELL_MAX && pos < len(rest) {
		if rest[pos] != '|' do break
		pos += 1
		if pos >= len(rest) do break

		// K: widget kind.
		k_digit := int(rest[pos]) - '0'
		if k_digit < 0 || k_digit > 8 do break
		pos += 1
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// S: stream binding (until next ':').
		s_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		s_field := rest[s_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// F: flags (until next ':').
		f_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		f_field := rest[f_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// CS: col_span (until next ':').
		cs_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		cs_field := rest[cs_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// RS: row_span (until next ':' or '|' or end).
		rs_start := pos
		for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
		rs_field := rest[rs_start:pos]

		// SM: sub_main_split (optional, ×1000).
		sm_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sm_start := pos
			for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
			sm_field = rest[sm_start:pos]
		}

		// SR: sub_ratios (optional, comma-separated ×1000).
		sr_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sr_start := pos
			for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
			sr_field = rest[sr_start:pos]
		}

		// TF: tf_idx+1 (0 = global, 1-9 = per-cell TF).
		tf_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			tf_start := pos
			for pos < len(rest) && rest[pos] != '|' { pos += 1 }
			tf_field = rest[tf_start:pos]
		}

		// Build cell.
		cell := make_default_cell(state, Widget_Kind(k_digit))

		if s_field == "-1" {
			cell.stream_idx = -1
			cell_clear_binding(&cell)
		} else {
			slash := -1
			for si in 0 ..< len(s_field) {
				if s_field[si] == '/' { slash = si; break }
			}
			if slash > 0 && slash < len(s_field) - 1 {
				// PRD-0009: store venue/symbol intent directly on cell.
				cell_set_binding(&cell, s_field[:slash], s_field[slash + 1:])
			}
			cell.stream_idx = -1 // will be resolved lazily (M2)
		}

		flags := parse_int_from(f_field)
		unpack_indicator_flags(&cell, flags)

		cs := parse_int_from(cs_field)
		rs := parse_int_from(rs_field)
		cell.col_span = cs > 1 ? cs : 1
		cell.row_span = rs > 1 ? rs : 1

		// Subplot ratios.
		if len(sm_field) > 0 {
			cell.sub_main_split = f32(parse_int_from(sm_field)) / 1000.0
		}
		if len(sr_field) > 0 {
			sr_idx := 0
			sr_seg_start := 0
			for si in 0 ..= len(sr_field) {
				if si == len(sr_field) || sr_field[si] == ',' {
					if si > sr_seg_start && sr_idx < 5 {
						cell.sub_ratios[sr_idx] = f32(parse_int_from(sr_field[sr_seg_start:si])) / 1000.0
						sr_idx += 1
					}
					sr_seg_start = si + 1
				}
			}
		}

		// Per-cell TF: stored as tf_idx+1; 0 → -1 (global), 1-9 → 0-8.
		if len(tf_field) > 0 {
			tf_val := parse_int_from(tf_field)
			cell.tf_idx = tf_val > 0 ? tf_val - 1 : -1
		}

		cells[cell_count] = cell
		cell_count += 1
	}

	if cell_count <= 0 do return false

	state.cell_count = cell_count
	for i in 0 ..< cell_count {
		state.cell_assignments[i] = cells[i]
	}

	return true
}

restore_layout_v2 :: proc(state: ^App_State) -> bool {
	v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V2)
	if !ok || len(v) < 4 do return false
	if v[0] != 'V' || v[1] != '2' do return false

	rest := v[2:]  // skip "V2"

	// Parse pipe-delimited cells.
	cell_count := 0
	cells: [CELL_MAX]Cell_Assignment
	stream_bindings: [CELL_MAX][2]string  // [venue, symbol] for re-association

	pos := 0
	for cell_count < CELL_MAX && pos < len(rest) {
		// Expect '|' before each cell.
		if rest[pos] != '|' do break
		pos += 1
		if pos >= len(rest) do break

		// Parse K (widget kind digit).
		k_digit := int(rest[pos]) - '0'
		if k_digit < 0 || k_digit > 8 do break
		pos += 1
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// Parse S (stream binding — up to next ':').
		s_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' {
			pos += 1
		}
		s_field := rest[s_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// Parse F (flags — digits until '|' or end).
		f_start := pos
		for pos < len(rest) && rest[pos] != '|' {
			pos += 1
		}
		f_field := rest[f_start:pos]

		// Build cell.
		cell := make_default_cell(state, Widget_Kind(k_digit))

		// Decode stream binding.
		if s_field == "-1" {
			cell.stream_idx = -1
		} else {
			// Parse "venue/symbol".
			slash := -1
			for si in 0 ..< len(s_field) {
				if s_field[si] == '/' { slash = si; break }
			}
			if slash > 0 && slash < len(s_field) - 1 {
				stream_bindings[cell_count][0] = s_field[:slash]
				stream_bindings[cell_count][1] = s_field[slash + 1:]
			}
			cell.stream_idx = -1  // will be resolved after all cells parsed
		}

		// Decode flags.
		flags := 0
		for fi in 0 ..< len(f_field) {
			d := int(f_field[fi]) - '0'
			if d < 0 || d > 9 do break
			flags = flags * 10 + d
		}
		unpack_indicator_flags(&cell, flags)

		cells[cell_count] = cell
		cell_count += 1
	}

	if cell_count <= 0 do return false

	// Apply cells.
	state.cell_count = cell_count
	for i in 0 ..< cell_count {
		state.cell_assignments[i] = cells[i]
	}

	// Re-associate stream bindings: find matching stream_view slot by venue/symbol.
	reg := state.stream_views
	if reg != nil {
		for i in 0 ..< cell_count {
			venue := stream_bindings[i][0]
			symbol := stream_bindings[i][1]
			if len(venue) == 0 || len(symbol) == 0 do continue
			// Find slot with matching venue/symbol.
			for si in 0 ..< STREAM_VIEW_CAP {
				if !reg.slots[si].used do continue
				slot := &reg.slots[si]
				if !slot.has_stream_info {
					refresh_stream_info_for_slot(state, slot)
				}
				if slot.has_stream_info &&
					normalized_venue(slot.stream_info.venue) == normalized_venue(venue) &&
					normalized_symbol(slot.stream_info.symbol) == normalized_symbol(symbol) {
					state.cell_assignments[i].stream_idx = si
					break
				}
			}
		}
	}

	return true
}

// Custom preset settings keys indexed 0-3.
CUSTOM_LAYOUT_KEYS :: [4]string{
	services.SETTING_CUSTOM_LAYOUT_0,
	services.SETTING_CUSTOM_LAYOUT_1,
	services.SETTING_CUSTOM_LAYOUT_2,
	services.SETTING_CUSTOM_LAYOUT_3,
}

// Save current layout to a custom preset slot (0-3).
save_custom_preset :: proc(state: ^App_State, slot: int) {
	if slot < 0 || slot >= 4 do return
	// Build layout string.
	buf: [128]u8
	off := 0
	n := state.cell_count
	if n > 9 { buf[off] = '0' + u8(n / 10); off += 1 }
	buf[off] = '0' + u8(n % 10); off += 1
	buf[off] = ':'; off += 1
	for i in 0 ..< n {
		if i > 0 { buf[off] = ','; off += 1 }
		buf[off] = '0' + u8(state.cell_assignments[i].widget); off += 1
	}
	keys := CUSTOM_LAYOUT_KEYS
	services.settings_set(&state.settings, keys[slot], string(buf[:off]))
	services.settings_flush(&state.settings)
}

// Load a custom preset slot. Returns true if valid and applied.
load_custom_preset :: proc(state: ^App_State, slot: int) -> bool {
	if slot < 0 || slot >= 4 do return false
	keys := CUSTOM_LAYOUT_KEYS
	v, ok := services.settings_get(&state.settings, keys[slot])
	if !ok || len(v) < 3 do return false
	return restore_layout_from_string(state, v)
}

// Check if a custom preset slot has a valid saved layout.
custom_preset_valid :: proc(state: ^App_State, slot: int) -> bool {
	if slot < 0 || slot >= 4 do return false
	keys := CUSTOM_LAYOUT_KEYS
	v, ok := services.settings_get(&state.settings, keys[slot])
	return ok && len(v) >= 3
}

stream_ids_same_market :: proc(state: ^App_State, a_subject_id, b_subject_id: u64) -> bool {
	if a_subject_id == 0 || b_subject_id == 0 do return false
	if a_subject_id == b_subject_id do return true
	reg := state.stream_views
	if reg == nil do return false

	a_idx := stream_view_find_slot(reg, a_subject_id)
	b_idx := stream_view_find_slot(reg, b_subject_id)
	if a_idx < 0 || b_idx < 0 do return false

	a_slot := &reg.slots[a_idx]
	b_slot := &reg.slots[b_idx]
	if !slot_market_key_known(a_slot) do refresh_stream_info_for_slot(state, a_slot)
	if !slot_market_key_known(b_slot) do refresh_stream_info_for_slot(state, b_slot)
	if !slot_market_key_known(a_slot) || !slot_market_key_known(b_slot) do return false

	return normalized_venue(a_slot.stream_info.venue) == normalized_venue(b_slot.stream_info.venue) &&
		normalized_symbol(a_slot.stream_info.symbol) == normalized_symbol(b_slot.stream_info.symbol)
}

normalized_venue :: proc(v: string) -> string {
	if len(v) == 0 do return ""
	if has_prefix_ci(v, "binance") do return "binance"
	if has_prefix_ci(v, "kraken") do return "kraken"
	if has_prefix_ci(v, "coinbase") do return "coinbase"
	if has_prefix_ci(v, "bybit") do return "bybit"
	if has_prefix_ci(v, "hyperliquid") do return "hyperliquid"
	if dash := strings.index(v, "-"); dash > 0 {
		return v[:dash]
	}
	return v
}

@(private = "file")
has_prefix_ci :: proc(s: string, prefix: string) -> bool {
	if len(prefix) > len(s) do return false
	for i in 0 ..< len(prefix) {
		a := s[i]
		b := prefix[i]
		if a >= 'A' && a <= 'Z' do a += 32
		if b >= 'A' && b <= 'Z' do b += 32
		if a != b do return false
	}
	return true
}

normalized_symbol :: proc(s: string) -> string {
	if len(s) == 0 do return ""
	if sep := strings.index(s, ":"); sep > 0 {
		return s[:sep]
	}
	return s
}

build_stream_id_from_market_into :: proc(buf: []u8, venue: string, symbol: string) -> string {
	return streams.format_stream_id_into(buf, normalized_venue(venue), symbol, "")
}

// ═══════════════════════════════════════════════════════════════
// Smart Subscription Management (PRD-0006-B M5)
// Subscribe only to channels cells actually need.
// ═══════════════════════════════════════════════════════════════

CHANNEL_COUNT :: 6

// Returns a bitmask of channels a widget type needs.
channels_for_widget :: proc(kind: Widget_Kind) -> u8 {
	CH_TRADES    :: u8(1 << u8(ports.MD_Channel.Trades))
	CH_ORDERBOOK :: u8(1 << u8(ports.MD_Channel.Orderbook))
	CH_STATS     :: u8(1 << u8(ports.MD_Channel.Stats))
	CH_HEATMAPS  :: u8(1 << u8(ports.MD_Channel.Heatmaps))
	CH_VPVR      :: u8(1 << u8(ports.MD_Channel.VPVR))
	CH_CANDLES   :: u8(1 << u8(ports.MD_Channel.Candles))

	switch kind {
	case .Candle:    return CH_CANDLES | CH_STATS | CH_HEATMAPS | CH_VPVR
	case .Orderbook: return CH_ORDERBOOK
	case .DOM:       return CH_ORDERBOOK | CH_TRADES
	case .Trades:    return CH_TRADES
	case .Stats:     return CH_STATS
	case .Counter:   return CH_STATS
	case .Heatmap:   return CH_HEATMAPS
	case .VPVR:      return CH_VPVR
	case .Empty:     return 0
	}
	return 0
}

// Wanted subscription: venue + symbol + channel bitmask + TF for TF-sensitive channels.
Sub_Want :: struct {
	venue:           string,
	symbol:          string,
	channels:        u8,
	tf:              string,  // for TF-sensitive channels (Candles, Heatmaps, VPVR)
	has_explicit_tf: bool,    // true = at least one cell has per-cell TF override
}

// Snapshot of a previous subscription entry — stored as inline fixed buffers to avoid lifetime issues.
Prev_Sub_Entry :: struct {
	venue:      [24]u8,
	venue_len:  u8,
	symbol:     [32]u8,
	symbol_len: u8,
	channels:   u8,
	tf:         [8]u8,
	tf_len:     u8,
}

SUB_WANT_CAP :: 128

// TF-sensitive channel bitmask.
@(private = "file")
CH_TF_SENSITIVE :: u8(1 << u8(ports.MD_Channel.Candles)) |
                   u8(1 << u8(ports.MD_Channel.Heatmaps)) |
                   u8(1 << u8(ports.MD_Channel.VPVR))

@(private = "file")
seed_stream_slot_for_subject :: proc(
	state: ^App_State,
	venue, symbol: string,
	channel: ports.MD_Channel,
	tf: string = "",
) {
	if state == nil || state.stream_views == nil do return
	subject := util.build_subject_with_timeframe(venue, symbol, channel, tf)
	subject_id := util.subject_id64(subject)
	delete(subject)
	if subject_id == 0 do return

	slot := stream_view_get_or_alloc_slot(state.stream_views, subject_id, state.frame, state)
	if slot == nil do return
	slot.has_stream_info = true
	slot.has_channel = true
	slot.channel = channel
	slot.stream_info = ports.MD_Stream_Info{
		subject_id = subject_id,
		channel    = channel,
		venue      = venue,
		symbol     = symbol,
		timeframe  = tf,
	}
}

// Reconcile subscriptions: subscribe to channels cells need, unsubscribe excess.
reconcile_subscriptions :: proc(state: ^App_State) {
	if state.marketdata.subscribe == nil do return
	if state.marketdata.unsubscribe == nil do return
	reg := state.stream_views
	if reg == nil do return

	// Build wanted set by scanning all cells.
	wanted: [SUB_WANT_CAP]Sub_Want
	wanted_count := 0

	tf_opts := TF_OPTIONS

	for ci in 0 ..< state.cell_count {
		cell := &state.cell_assignments[ci]
		ch_mask := channels_for_widget(cell.widget)
		if ch_mask == 0 do continue

		// Resolve venue/symbol for this cell — PRD-0009: prefer bound fields.
		venue, symbol: string
		if cell_has_binding(cell) {
			// Intent-driven: use bound venue/symbol directly (can subscribe venues with NO slot yet).
			venue = cell_bound_venue(cell)
			symbol = cell_bound_symbol(cell)
		} else if cell.stream_idx >= 0 && cell.stream_idx < STREAM_VIEW_CAP && reg.slots[cell.stream_idx].used {
			slot := &reg.slots[cell.stream_idx]
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			if !slot.has_stream_info do continue
			venue = slot.stream_info.venue
			symbol = slot.stream_info.symbol
		} else {
			// No binding, no valid slot — follow active stream.
			slot := stream_view_active_slot(reg)
			if slot == nil do continue
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			if !slot.has_stream_info do continue
			venue = slot.stream_info.venue
			symbol = slot.stream_info.symbol
		}

		if len(venue) == 0 || len(symbol) == 0 do continue

		// Resolve TF for this cell (per-cell or global).
		eff_tf_idx := cell_effective_tf_idx(state, cell)
		cell_tf := tf_opts[eff_tf_idx] if eff_tf_idx >= 0 && eff_tf_idx < len(tf_opts) else tf_opts[0]
		cell_has_per_cell_tf := cell.tf_idx >= 0 && cell.tf_idx < len(TF_OPTIONS)

		// Merge into wanted set (keyed by venue + symbol + tf).
		found := false
		for wi in 0 ..< wanted_count {
			if wanted[wi].venue == venue && wanted[wi].symbol == symbol && wanted[wi].tf == cell_tf {
				wanted[wi].channels |= ch_mask
				if cell_has_per_cell_tf { wanted[wi].has_explicit_tf = true }
				found = true
				break
			}
		}
		if !found && wanted_count < SUB_WANT_CAP {
			wanted[wanted_count] = Sub_Want{
				venue = venue, symbol = symbol, channels = ch_mask,
				tf = cell_tf, has_explicit_tf = cell_has_per_cell_tf,
			}
			wanted_count += 1
		}
	}

	// Compare mode: ensure compare slot streams are subscribed for the selected widget type.
	if state.compare_mode && state.compare_count > 0 {
		cmp_widget: Widget_Kind
		switch state.compare_widget_idx {
		case 0:  cmp_widget = .Orderbook
		case 1:  cmp_widget = .Trades
		case 2:  cmp_widget = .Candle
		case:    cmp_widget = .Candle
		}
		cmp_ch_mask := channels_for_widget(cmp_widget)
		for csi in 0 ..< state.compare_count {
			sid := state.compare_slots[csi]
			slot_idx := stream_view_find_slot(reg, sid)
			if slot_idx < 0 do continue
			slot := &reg.slots[slot_idx]
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			if !slot.has_stream_info do continue
			cmp_venue := slot.stream_info.venue
			cmp_symbol := slot.stream_info.symbol
			if len(cmp_venue) == 0 || len(cmp_symbol) == 0 do continue
			eff_tf_idx := state.active_tf_idx
			cmp_tf := tf_opts[eff_tf_idx] if eff_tf_idx >= 0 && eff_tf_idx < len(tf_opts) else tf_opts[0]
			// Merge into wanted set.
			found_cmp := false
			for wi in 0 ..< wanted_count {
				if wanted[wi].venue == cmp_venue && wanted[wi].symbol == cmp_symbol && wanted[wi].tf == cmp_tf {
					wanted[wi].channels |= cmp_ch_mask
					found_cmp = true
					break
				}
			}
			if !found_cmp && wanted_count < SUB_WANT_CAP {
				wanted[wanted_count] = Sub_Want{
					venue = cmp_venue, symbol = cmp_symbol, channels = cmp_ch_mask,
					tf = cmp_tf,
				}
				wanted_count += 1
			}
		}
	}

	// Sync stream registry wiring + refcount ownership from the wanted set.
	streams.registry_reset_ref_counts(&state.stream_registry)
	conn := current_conn_status(state)
	for wi in 0 ..< wanted_count {
		w := wanted[wi]
		stream_id_buf: [streams.STREAM_ID_CAP]u8
		stream_id := build_stream_id_from_market_into(stream_id_buf[:], w.venue, w.symbol)
		_, market_type := streams.split_symbol_market_type(w.symbol)
		if handle := streams.registry_acquire(&state.stream_registry, stream_id, normalized_venue(w.venue), w.symbol, market_type); handle != nil {
			streams.controller_mark_connected(&handle.status, conn == .Connected)
		}
	}
	if active_slot := stream_view_active_slot(reg); active_slot != nil {
		if !active_slot.has_stream_info {
			refresh_stream_info_for_slot(state, active_slot)
		}
		if active_slot.has_stream_info {
			stream_id_buf: [streams.STREAM_ID_CAP]u8
			stream_id := build_stream_id_from_market_into(stream_id_buf[:], active_slot.stream_info.venue, active_slot.stream_info.symbol)
			streams.registry_set_active(&state.stream_registry, stream_id)
		}
	}
	streams.registry_prune_unused(&state.stream_registry)

	// Subscribe to wanted channels (skip channels already in prev_subs to avoid duplicates).
	for wi in 0 ..< wanted_count {
		w := wanted[wi]
		for chi in 0 ..< CHANNEL_COUNT {
			if (w.channels & (1 << u8(chi))) == 0 do continue
			ch := ports.MD_Channel(chi)
			is_tf_ch := ((1 << u8(chi)) & CH_TF_SENSITIVE) != 0
			eff_tf := is_tf_ch ? w.tf : ""
			seed_stream_slot_for_subject(state, w.venue, w.symbol, ch, eff_tf)
			// Skip subscribe if this exact channel+venue+symbol+tf is already in prev_subs.
			already_subscribed := false
			for pi in 0 ..< state.prev_subs_count {
				prev := state.prev_subs[pi]
				pv := string(prev.venue[:int(prev.venue_len)])
				ps := string(prev.symbol[:int(prev.symbol_len)])
				if pv == w.venue && ps == w.symbol && (prev.channels & (1 << u8(chi))) != 0 {
					// For TF-sensitive channels, also verify timeframe matches.
					if is_tf_ch {
						pt := string(prev.tf[:int(prev.tf_len)])
						if pt != w.tf do continue // TF mismatch — not already subscribed
					}
					already_subscribed = true
					break
				}
			}
			if already_subscribed do continue
			if is_tf_ch && w.has_explicit_tf && state.marketdata.subscribe_tf != nil {
				state.marketdata.subscribe_tf(w.venue, w.symbol, ch, w.tf)
			} else {
				state.marketdata.subscribe(w.venue, w.symbol, ch)
			}
		}
	}

	// Unsubscribe stale channels: anything in prev_subs but NOT in wanted.
	for pi in 0 ..< state.prev_subs_count {
		prev := state.prev_subs[pi]
		pv := string(prev.venue[:int(prev.venue_len)])
		ps := string(prev.symbol[:int(prev.symbol_len)])
		// For each channel bit in prev, check if still in wanted.
		for chi in 0 ..< CHANNEL_COUNT {
			if (prev.channels & (1 << u8(chi))) == 0 do continue
			still_wanted := false
			for wi in 0 ..< wanted_count {
				if wanted[wi].venue == pv && wanted[wi].symbol == ps && (wanted[wi].channels & (1 << u8(chi))) != 0 {
					still_wanted = true
					break
				}
			}
			if !still_wanted {
				ch := ports.MD_Channel(chi)
				state.marketdata.unsubscribe(pv, ps, ch)
			}
		}
	}

	// Save current wanted set into prev_subs for next reconcile diff.
	state.prev_subs_count = wanted_count
	for wi in 0 ..< wanted_count {
		w := wanted[wi]
		entry: Prev_Sub_Entry
		entry.channels = w.channels
		vn := min(len(w.venue), len(entry.venue))
		for i in 0 ..< vn { entry.venue[i] = w.venue[i] }
		entry.venue_len = u8(vn)
		sn := min(len(w.symbol), len(entry.symbol))
		for i in 0 ..< sn { entry.symbol[i] = w.symbol[i] }
		entry.symbol_len = u8(sn)
		tn := min(len(w.tf), len(entry.tf))
		for i in 0 ..< tn { entry.tf[i] = w.tf[i] }
		entry.tf_len = u8(tn)
		state.prev_subs[wi] = entry
	}
}
