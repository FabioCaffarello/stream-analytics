package app

import "core:strings"
import "mr:ports"
import "mr:services"

// ---------------------------------------------------------------------------
// Slot CRUD and stream resolution
// Extracted from stream_views.odin for cohesion.
// ---------------------------------------------------------------------------

stream_view_find_slot :: proc(reg: ^Stream_View_Registry, subject_id: u64) -> int {
	if reg == nil do return -1
	if subject_id == 0 do return -1
	for i in 0 ..< len(reg.slots) {
		if reg.slots[i].used && reg.slots[i].subject_id == subject_id do return i
	}
	return -1
}

stream_view_clear_stream_info :: proc(slot: ^Stream_View_Slot) {
	if slot == nil do return
	if len(slot.stream_info.venue) > 0 do delete(slot.stream_info.venue)
	if len(slot.stream_info.symbol) > 0 do delete(slot.stream_info.symbol)
	if len(slot.stream_info.timeframe) > 0 do delete(slot.stream_info.timeframe)
	if len(slot.stream_info.subject) > 0 do delete(slot.stream_info.subject)
	slot.stream_info = {}
	slot.has_stream_info = false
}

stream_view_set_stream_info :: proc(slot: ^Stream_View_Slot, info: ports.MD_Stream_Info) {
	if slot == nil do return
	stream_view_clear_stream_info(slot)
	slot.stream_info = ports.MD_Stream_Info{
		subject_id = info.subject_id,
		channel    = info.channel,
		venue      = strings.clone(info.venue),
		symbol     = strings.clone(info.symbol),
		timeframe  = strings.clone(info.timeframe),
		subject    = strings.clone(info.subject),
	}
	slot.has_stream_info = len(slot.stream_info.venue) > 0 && len(slot.stream_info.symbol) > 0
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

	stream_view_clear_stream_info(&reg.slots[slot_idx])
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

refresh_stream_info_for_slot :: proc(state: ^App_State, slot: ^Stream_View_Slot) {
	if state == nil || slot == nil do return
	if slot.subject_id == 0 do return
	if state.marketdata.describe_stream == nil {
		stream_view_clear_stream_info(slot)
		return
	}

	info: ports.MD_Stream_Info
	if !state.marketdata.describe_stream(slot.subject_id, &info) {
		stream_view_clear_stream_info(slot)
		return
	}

	stream_view_set_stream_info(slot, info)
	if !slot.has_channel {
		slot.has_channel = true
		slot.channel = info.channel
	}
}

// G1 helper: check if any cell references a given slot index (by index or binding match).
@(private = "file")
slot_referenced_by_cell :: proc(state: ^App_State, slot_idx: int) -> bool {
	reg := state.stream_views
	for ci in 0 ..< state.world.count {
		if state.world.bindings[ci].stream_idx == slot_idx do return true
		// PRD-0009: also protect slots matching a cell's venue/symbol binding.
		if binding_has(&state.world.bindings[ci]) && reg != nil && slot_idx >= 0 && slot_idx < STREAM_VIEW_CAP && reg.slots[slot_idx].used {
			slot := &reg.slots[slot_idx]
			if slot.has_stream_info &&
				normalized_venue(slot.stream_info.venue) == normalized_venue(binding_venue(&state.world.bindings[ci])) &&
				normalized_symbol(slot.stream_info.symbol) == normalized_symbol(binding_symbol(&state.world.bindings[ci])) {
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
	for ci in 0 ..< state.world.count {
		if state.world.bindings[ci].stream_idx == slot_idx {
			state.world.bindings[ci].stream_idx = -1 // binding preserved for re-resolution
		}
	}
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

// Resolve data stores for a cell. Returns pointers to the appropriate stores.
Cell_Stores :: struct {
	candle:    ^services.Candle_Store,
	heatmap:   ^services.Heatmap_Store,
	vpvr:      ^services.VPVR_Store,
	trades:    ^services.Trades_Store,
	orderbook: ^services.Orderbook_Store,
	stats:     ^services.Stats_Store,
}

resolve_stores_for_cell :: proc(state: ^App_State, ci: int) -> Cell_Stores {
	stores: Cell_Stores

	// Default fallback: active/global stores.
	stores.candle    = &state.stores.candle
	stores.heatmap   = &state.stores.heatmap
	stores.vpvr      = &state.stores.vpvr
	stores.trades    = &state.stores.trades
	stores.orderbook = &state.stores.orderbook
	stores.stats     = &state.stores.stats

	reg := state.stream_views
	if reg == nil do return stores

	// Follow-active cells (stream_idx=-1) use global stores by default.
	// If a binding exists, try to resolve it to a concrete slot first.
	if state.world.bindings[ci].stream_idx < 0 {
		if binding_has(&state.world.bindings[ci]) {
			bv := binding_venue(&state.world.bindings[ci])
			bs := binding_symbol(&state.world.bindings[ci])
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
				state.world.bindings[ci].stream_idx = best_idx
			}
		}
		if state.world.bindings[ci].stream_idx < 0 {
			return stores
		}
	}

	venue, symbol: string
	stream_idx := state.world.bindings[ci].stream_idx
	if stream_idx >= 0 && stream_idx < STREAM_VIEW_CAP && reg.slots[stream_idx].used {
		slot := &reg.slots[stream_idx]
		if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
		if slot.has_stream_info {
			venue = slot.stream_info.venue
			symbol = slot.stream_info.symbol
		}
	} else {
		state.world.bindings[ci].stream_idx = -1
		return stores
	}

	if len(venue) == 0 || len(symbol) == 0 do return stores

	// Active market with matching TF should read from global stores, which are
	// already synchronized across per-channel subjects for the selected active stream.
	// A cell with a per-cell TF override on the same market must NOT get global stores
	// (they contain data for the global TF, not the cell's TF).
	if active := stream_view_active_slot(reg); active != nil {
		if !active.has_stream_info { refresh_stream_info_for_slot(state, active) }
		if active.has_stream_info &&
			normalized_venue(active.stream_info.venue) == normalized_venue(venue) &&
			normalized_symbol(active.stream_info.symbol) == normalized_symbol(symbol) &&
			cell_effective_tf_idx(state, ci) == state.active_tf_idx {
			return stores
		}
	}

	tf_opts := TF_OPTIONS
	eff_tf := cell_effective_tf_idx(state, ci)
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
