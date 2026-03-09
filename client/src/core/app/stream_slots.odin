package app

import "core:strings"
import "mr:layers"
import "mr:md_common"
import "mr:ports"
import "mr:services"
import "mr:util"

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

// S61/S80: Cell_View_Model — unified per-cell resolved state.
// Bundles surface view, data stores, effective TF, widget config, chart display,
// and indicator state into a single query result. Resolved once per cell per frame,
// consumed by all render procs. Pure derived view — no mutation, no allocations.
Cell_View_Model :: struct {
	// Read model: composition, health, staleness, identity.
	surface:        Cell_Surface_View,
	// Data stores: pointers to the appropriate stores for this cell.
	stores:         Cell_Stores,
	// Effective configuration (resolved from ECS components).
	widget_kind:    Widget_Kind,
	tf_idx:         int,            // effective TF index (resolved per-cell or global)
	tf_string:      string,         // effective TF label (e.g. "1m", "5m")
	tf_ms:          i64,            // effective TF in milliseconds
	// Analytics config (only meaningful for .Analytics widget kind).
	analytics_kind: services.Analytics_Kind,
	show_history:   bool,
	// S80: Chart display + indicator state (closes abstraction gap — widgets
	// no longer need to reach into state.world.charts[ci] or state.world.indicators[ci]).
	chart:          Chart_Component,
	indicators:     Indicator_Component,
	ind_params:     Indicator_Params,
	// Cell identity.
	cell_idx:       int,
	focused:        bool,
}

// S61: Resolve a complete Cell_View_Model for a cell. Single call replaces
// separate resolve_cell_surface_view + resolve_stores_for_cell + manual ECS reads.
// Must be called once per cell per frame in the render loop.
resolve_cell_view_model :: proc(state: ^App_State, ci: int) -> Cell_View_Model {
	vm: Cell_View_Model
	if state == nil || ci < 0 || ci >= state.world.count do return vm

	vm.cell_idx = ci
	vm.widget_kind = state.world.widgets[ci].kind
	vm.focused = (ci == state.world.focused)

	// Effective TF (resolved once, reused by surface view and stores).
	vm.tf_idx = cell_effective_tf_idx(state, ci)
	tf_opts := TF_OPTIONS
	vm.tf_string = tf_opts[vm.tf_idx] if vm.tf_idx >= 0 && vm.tf_idx < len(tf_opts) else tf_opts[0]
	tf_ms_opts := TF_OPTION_MS
	vm.tf_ms = tf_ms_opts[vm.tf_idx] if vm.tf_idx >= 0 && vm.tf_idx < len(tf_ms_opts) else tf_ms_opts[0]

	// Analytics config (from ECS component).
	vm.analytics_kind = state.world.analytics[ci].analytics_kind
	vm.show_history = state.world.analytics[ci].show_history

	// S80: Chart display + indicator state (value copy — immutable for this frame).
	vm.chart = state.world.charts[ci]
	vm.indicators = state.world.indicators[ci]
	vm.ind_params = state.world.ind_params[ci]

	// Stores (resolved once — avoid double resolution).
	vm.stores = resolve_stores_for_cell(state, ci)

	// Surface view (uses pre-resolved stores and TF to avoid redundant computation).
	vm.surface = resolve_cell_surface_view_with_stores(state, ci, vm.stores, vm.tf_ms)

	return vm
}

// S61: Internal surface view resolution that accepts pre-resolved stores and TF.
// Avoids the double resolve_stores_for_cell call in the original resolve_cell_surface_view.
// S108: Widened to package-private for Widget_Data_Context resolution.
@(private = "package")
resolve_cell_surface_view_with_stores :: proc(
	state: ^App_State,
	ci: int,
	stores: Cell_Stores,
	tf_ms: i64,
) -> Cell_Surface_View {
	view: Cell_Surface_View
	if state == nil || ci < 0 || ci >= state.world.count do return view

	view.composition = resolve_cell_composition(state, ci)
	apply := resolve_cell_apply_state(state, ci)

	now_ms := current_now_ms(state)

	view.candle_health = compute_candle_health_for_store(
		stores.candle,
		md_common.apply_state_candle_recv_ms(apply),
		tf_ms,
		now_ms,
	)

	// S125: Per-artifact live flags for widget-specific readiness.
	view.artifact_has_live = apply.has_live
	for kind in md_common.Artifact_Kind {
		if apply.has_live[kind] {
			view.has_live_data = true
			break
		}
	}

	view.stale_count, view.aging_count = md_common.apply_state_stale_artifact_count(apply, now_ms, tf_ms)
	view.health_level = md_common.stream_health_level(apply, now_ms, tf_ms)
	view.recovery_status = md_common.apply_state_recovery_status(apply)

	bind := &state.world.bindings[ci]
	view.stream_bound = bind.stream_idx >= 0 || binding_has(bind)

	reg := state.stream_views
	if reg != nil {
		slot_idx := -1
		if bind.stream_idx >= 0 && bind.stream_idx < STREAM_VIEW_CAP && reg.slots[bind.stream_idx].used {
			slot_idx = bind.stream_idx
		} else if reg.has_active {
			slot_idx = stream_view_find_slot(reg, reg.active_subject_id)
		}
		if slot_idx >= 0 {
			slot := &reg.slots[slot_idx]
			if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
			if slot.has_stream_info {
				view.venue = slot.stream_info.venue
				view.symbol = slot.stream_info.symbol
			}
		}
	}

	return view
}

// Resolve data stores for a cell. Returns pointers to the appropriate stores.
Cell_Stores :: struct {
	candle:       ^services.Candle_Store,
	heatmap:      ^services.Heatmap_Store,
	vpvr:         ^services.VPVR_Store,
	trades:       ^services.Trades_Store,
	orderbook:    ^services.Orderbook_Store,
	stats:        ^services.Stats_Store,
	analytics:    ^services.Analytics_Store,        // S47: per-cell analytics ring
	session_vpvr: ^services.Session_VPVR_Store,     // S49: per-cell session VP
	tpo:          ^services.TPO_Store,               // S49: per-cell TPO profile
	heatmap_live: bool,
	vpvr_live:    bool,
}

// S36: Cell_Surface_View — unified per-cell read model for surfaces.
// Bundles composition, health, staleness, and identity into a single query result.
// Surfaces consume this instead of reaching into protocol/apply_state internals.
Cell_Surface_View :: struct {
	composition:     md_common.Composition_Stage,
	candle_health:   Candle_Health,
	has_live_data:   bool, // any artifact with live data
	// S125: Per-artifact live flags — enables widget-specific readiness checks.
	// Stats/OB/DOM use these to distinguish Snapshot_Pending (artifact live, store empty)
	// from Seeding (other artifacts live, this one not yet).
	artifact_has_live: [md_common.Artifact_Kind]bool,
	stale_count:     int,
	aging_count:     int,
	venue:           string, // resolved label (may be empty)
	symbol:          string, // resolved label (may be empty)
	stream_bound:    bool,   // has explicit stream binding
	health_level:    md_common.System_Health_Level,
	recovery_status: md_common.Recovery_Status, // S42: per-pane recovery diagnostics
}

resolve_stores_for_cell :: proc(state: ^App_State, ci: int) -> Cell_Stores {
	stores: Cell_Stores

	// S100: Default fallback resolves from layer_store active stream (no mirror).
	if active := layers.market_store_active_stream(&state.layer_store); active != nil {
		stores.candle       = &active.candles
		stores.heatmap      = &active.heatmap
		stores.vpvr         = &active.vpvr
		stores.trades       = &active.trades
		stores.orderbook    = &active.orderbook
		stores.stats        = &active.stats
		stores.analytics    = &active.analytics
	}
	// S100: session_vpvr + tpo from active slot (per-slot data, not on Market_Stream).
	reg := state.stream_views
	if reg != nil {
		if active_slot := stream_view_active_slot(reg); active_slot != nil {
			stores.session_vpvr = &active_slot.session_vpvr_store
			stores.tpo          = &active_slot.tpo_store
		}
	}
	// S36: Read from canonical apply_state (was active_metrics — a derived copy).
	stores.heatmap_live = state.active_apply_state.has_live[.Heatmap]
	stores.vpvr_live    = state.active_apply_state.has_live[.VPVR]

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
		// S99: Analytics from canonical layer_store (needs only subject_id, not venue/symbol).
		// S49: Session VP + TPO stores from bound slot.
		sid := slot.subject_id
		if ms := layers.market_store_stream_for_subject(&state.layer_store, sid); ms != nil {
			stores.analytics = &ms.analytics
		}
		stores.session_vpvr = &slot.session_vpvr_store
		stores.tpo = &slot.tpo_store
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
		stores.heatmap_live = slot.apply_state.has_live[.Heatmap]
	}
	if slot := find_market_channel_slot(state, reg, venue, symbol, .VPVR, cell_tf); slot != nil {
		stores.vpvr = &slot.vpvr_store
		stores.vpvr_live = slot.apply_state.has_live[.VPVR]
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

// S112: Resolve data stores for a pane using pane-local binding + TF.
// Reads from the pane's Stream_Binding and effective TF index directly —
// no Entity_World lookup. Used by resolve_widget_data_context to eliminate
// implicit coupling to state.world.bindings[ci].
resolve_stores_for_pane :: proc(state: ^App_State, binding: ^Stream_Binding, eff_tf_idx: int) -> Cell_Stores {
	stores: Cell_Stores

	// Default fallback: layer_store active stream (same as resolve_stores_for_cell).
	if active := layers.market_store_active_stream(&state.layer_store); active != nil {
		stores.candle    = &active.candles
		stores.heatmap   = &active.heatmap
		stores.vpvr      = &active.vpvr
		stores.trades    = &active.trades
		stores.orderbook = &active.orderbook
		stores.stats     = &active.stats
		stores.analytics = &active.analytics
	}
	// Session VP + TPO from active slot.
	reg := state.stream_views
	if reg != nil {
		if active_slot := stream_view_active_slot(reg); active_slot != nil {
			stores.session_vpvr = &active_slot.session_vpvr_store
			stores.tpo          = &active_slot.tpo_store
		}
	}
	stores.heatmap_live = state.active_apply_state.has_live[.Heatmap]
	stores.vpvr_live    = state.active_apply_state.has_live[.VPVR]

	if reg == nil || binding == nil do return stores

	// Resolve binding to a stream slot (same logic as resolve_stores_for_cell).
	if binding.stream_idx < 0 {
		if binding_has(binding) {
			bv := binding_venue(binding)
			bs := binding_symbol(binding)
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
				binding.stream_idx = best_idx
			}
		}
		if binding.stream_idx < 0 {
			return stores
		}
	}

	venue, symbol: string
	stream_idx := binding.stream_idx
	if stream_idx >= 0 && stream_idx < STREAM_VIEW_CAP && reg.slots[stream_idx].used {
		slot := &reg.slots[stream_idx]
		if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
		if slot.has_stream_info {
			venue = slot.stream_info.venue
			symbol = slot.stream_info.symbol
		}
		sid := slot.subject_id
		if ms := layers.market_store_stream_for_subject(&state.layer_store, sid); ms != nil {
			stores.analytics = &ms.analytics
		}
		stores.session_vpvr = &slot.session_vpvr_store
		stores.tpo = &slot.tpo_store
	} else {
		binding.stream_idx = -1
		return stores
	}

	if len(venue) == 0 || len(symbol) == 0 do return stores

	// Active market with matching TF → use global stores.
	if active := stream_view_active_slot(reg); active != nil {
		if !active.has_stream_info { refresh_stream_info_for_slot(state, active) }
		if active.has_stream_info &&
			normalized_venue(active.stream_info.venue) == normalized_venue(venue) &&
			normalized_symbol(active.stream_info.symbol) == normalized_symbol(symbol) &&
			eff_tf_idx == state.active_tf_idx {
			return stores
		}
	}

	tf_opts := TF_OPTIONS
	cell_tf := tf_opts[0]
	if eff_tf_idx >= 0 && eff_tf_idx < len(tf_opts) {
		cell_tf = tf_opts[eff_tf_idx]
	}

	// Resolve each channel from the best slot for this market.
	if slot := find_market_channel_slot(state, reg, venue, symbol, .Candles, cell_tf); slot != nil {
		stores.candle = &slot.candle_store
	}
	if slot := find_market_channel_slot(state, reg, venue, symbol, .Heatmaps, cell_tf); slot != nil {
		stores.heatmap = &slot.heatmap_store
		stores.heatmap_live = slot.apply_state.has_live[.Heatmap]
	}
	if slot := find_market_channel_slot(state, reg, venue, symbol, .VPVR, cell_tf); slot != nil {
		stores.vpvr = &slot.vpvr_store
		stores.vpvr_live = slot.apply_state.has_live[.VPVR]
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

// S26: Resolve the composition stage for a cell. Pure query — no mutation.
// Bound cells use their GetRange_Component + slot live candle state.
// Follow-active cells use the global active composition stage.
resolve_cell_composition :: proc(state: ^App_State, ci: int) -> md_common.Composition_Stage {
	if state == nil || ci < 0 || ci >= state.world.count do return .Empty
	gr := state.world.getranges[ci]
	bind := &state.world.bindings[ci]

	// Follow-active: derive from global active apply state.
	if bind.stream_idx < 0 && !binding_has(bind) {
		return md_common.apply_state_composition_stage(state.active_apply_state)
	}

	// Bound cell: derive from cell getrange + slot live candle.
	has_live_candle := false
	reg := state.stream_views
	if reg != nil && bind.stream_idx >= 0 && bind.stream_idx < STREAM_VIEW_CAP && reg.slots[bind.stream_idx].used {
		has_live_candle = reg.slots[bind.stream_idx].apply_state.has_live[.Candle]
	}
	return md_common.cell_composition_stage(gr.pending, gr.seeded, has_live_candle)
}

// S36: Resolve the apply_state for a cell — either from the bound slot or global active.
// Returns a snapshot (value copy) suitable for pure queries.
resolve_cell_apply_state :: proc(state: ^App_State, ci: int) -> md_common.Stream_Apply_State {
	if state == nil || ci < 0 || ci >= state.world.count do return {}
	bind := &state.world.bindings[ci]

	// Follow-active cells use the global active apply state.
	if bind.stream_idx < 0 && !binding_has(bind) {
		return state.active_apply_state
	}

	// Bound cell: read from the slot's per-stream apply state.
	reg := state.stream_views
	if reg != nil && bind.stream_idx >= 0 && bind.stream_idx < STREAM_VIEW_CAP && reg.slots[bind.stream_idx].used {
		return reg.slots[bind.stream_idx].apply_state
	}

	return state.active_apply_state
}

// S38: Resolve the effective subject_id for a compare pane at its per-pane TF.
// Uses the seed subject_id (compare.slots[ci]) to identify the market, then
// finds the best slot at the pane's effective TF. Falls back to seed if no
// TF-matched slot exists yet (subscription may be in flight).
compare_pane_resolve_subject_id :: proc(state: ^App_State, pane_idx: int) -> u64 {
	if state == nil do return 0
	if pane_idx < 0 || pane_idx >= state.compare.count do return 0
	seed_sid := state.compare.slots[pane_idx]
	if seed_sid == 0 do return 0

	reg := state.stream_views
	if reg == nil do return seed_sid

	slot_idx := stream_view_find_slot(reg, seed_sid)
	if slot_idx < 0 do return seed_sid
	slot := &reg.slots[slot_idx]
	if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
	if !slot.has_stream_info do return seed_sid

	// If per-pane TF matches global, seed subject_id is already correct.
	pane_tf := compare_pane_effective_tf_string(state, pane_idx)

	// Find best slot for this market at the pane's effective TF.
	if found := find_market_channel_slot(state, reg, slot.stream_info.venue, slot.stream_info.symbol, .Candles, pane_tf); found != nil {
		return found.subject_id
	}

	return seed_sid
}

// S38: Surface view for compare mode panes. Takes a pane index (not subject_id)
// to support per-pane TF resolution. Resolves the correct slot for the pane's
// effective TF, then derives composition, health, staleness, and identity.
// Pure derived view — no mutation, no allocations.
resolve_compare_surface_view :: proc(state: ^App_State, pane_idx: int) -> Cell_Surface_View {
	view: Cell_Surface_View
	if state == nil || pane_idx < 0 || pane_idx >= state.compare.count do return view

	// S38: Resolve effective subject_id (per-pane TF aware).
	eff_sid := compare_pane_resolve_subject_id(state, pane_idx)
	if eff_sid == 0 do return view

	reg := state.stream_views
	if reg == nil do return view

	slot_idx := stream_view_find_slot(reg, eff_sid)
	if slot_idx < 0 do return view

	slot := &reg.slots[slot_idx]
	apply := slot.apply_state

	now_ms := current_now_ms(state)
	// S38: Per-pane TF for health/staleness (was global_tf_ms in S37).
	tf_ms := compare_pane_effective_tf_ms(state, pane_idx)

	// S63: Use pre-resolved eff_sid to avoid double compare_pane_resolve_subject_id call.
	view.composition = resolve_compare_pane_composition_for_sid(state, pane_idx, eff_sid)

	view.candle_health = compute_candle_health_for_store(
		&slot.candle_store,
		md_common.apply_state_candle_recv_ms(apply),
		tf_ms,
		now_ms,
	)

	// S125: Per-artifact live flags for widget-specific readiness.
	view.artifact_has_live = apply.has_live
	for kind in md_common.Artifact_Kind {
		if apply.has_live[kind] {
			view.has_live_data = true
			break
		}
	}

	view.stale_count, view.aging_count = md_common.apply_state_stale_artifact_count(apply, now_ms, tf_ms)
	view.health_level = md_common.stream_health_level(apply, now_ms, tf_ms)
	view.recovery_status = md_common.apply_state_recovery_status(apply) // S42

	if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
	if slot.has_stream_info {
		view.venue = slot.stream_info.venue
		view.symbol = slot.stream_info.symbol
	}
	view.stream_bound = true

	return view
}

// S39: Resolve the composition stage for a compare pane. Pure query — no mutation.
// Uses per-pane getrange state + slot live candle to derive composition.
resolve_compare_pane_composition :: proc(state: ^App_State, pane_idx: int) -> md_common.Composition_Stage {
	if state == nil || pane_idx < 0 || pane_idx >= state.compare.count do return .Empty
	eff_sid := compare_pane_resolve_subject_id(state, pane_idx)
	return resolve_compare_pane_composition_for_sid(state, pane_idx, eff_sid)
}

// S63: Composition resolver accepting pre-resolved subject_id to avoid double resolution.
@(private = "file")
resolve_compare_pane_composition_for_sid :: proc(state: ^App_State, pane_idx: int, eff_sid: u64) -> md_common.Composition_Stage {
	if state == nil || pane_idx < 0 || pane_idx >= state.compare.count do return .Empty
	if eff_sid == 0 do return .Empty

	gr := state.compare.getranges[pane_idx]
	reg := state.stream_views
	if reg == nil do return .Empty

	slot_idx := stream_view_find_slot(reg, eff_sid)
	if slot_idx < 0 do return .Empty

	has_live_candle := reg.slots[slot_idx].apply_state.has_live[.Candle]
	return md_common.cell_composition_stage(gr.pending, gr.seeded, has_live_candle)
}

// S39: Request historical candle data for a compare pane.
request_compare_pane_candle_range :: proc(state: ^App_State, pane_idx: int) {
	if state == nil do return
	if state.marketdata.send_getrange == nil do return
	if pane_idx < 0 || pane_idx >= state.compare.count do return
	if state.compare.getranges[pane_idx].pending do return

	seed_sid := state.compare.slots[pane_idx]
	if seed_sid == 0 do return

	reg := state.stream_views
	if reg == nil do return

	slot_idx := stream_view_find_slot(reg, seed_sid)
	if slot_idx < 0 do return
	slot := &reg.slots[slot_idx]
	if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
	if !slot.has_stream_info do return

	venue := slot.stream_info.venue
	symbol := slot.stream_info.symbol
	if len(venue) == 0 || len(symbol) == 0 do return

	limit := adaptive_getrange_limit(state)
	tf := compare_pane_effective_tf_string(state, pane_idx)

	candle_subject := util.build_subject_with_timeframe(venue, symbol, .Candles, tf)
	state.marketdata.send_getrange(candle_subject, limit, state.compare.getranges[pane_idx].oldest_ts)
	delete(candle_subject)

	state.compare.getranges[pane_idx].pending = true
	state.compare.getranges[pane_idx].seeded = true
	state.compare.getranges[pane_idx].sent_frame = state.frame
}

// S39: Request older candles for a compare pane (lazy loading on scroll-left).
request_compare_pane_older_candles :: proc(state: ^App_State, pane_idx: int) {
	if state == nil do return
	if state.marketdata.send_getrange == nil do return
	if pane_idx < 0 || pane_idx >= state.compare.count do return
	if state.compare.getranges[pane_idx].pending do return
	if state.compare.getranges[pane_idx].oldest_ts <= 0 do return

	seed_sid := state.compare.slots[pane_idx]
	if seed_sid == 0 do return

	reg := state.stream_views
	if reg == nil do return

	slot_idx := stream_view_find_slot(reg, seed_sid)
	if slot_idx < 0 do return
	slot := &reg.slots[slot_idx]
	if !slot.has_stream_info do return

	venue := slot.stream_info.venue
	symbol := slot.stream_info.symbol
	if len(venue) == 0 || len(symbol) == 0 do return

	limit := adaptive_getrange_limit(state)
	tf := compare_pane_effective_tf_string(state, pane_idx)

	candle_subject := util.build_subject_with_timeframe(venue, symbol, .Candles, tf)
	state.marketdata.send_getrange(candle_subject, limit, state.compare.getranges[pane_idx].oldest_ts)
	delete(candle_subject)

	state.compare.getranges[pane_idx].pending = true
	state.compare.getranges[pane_idx].sent_frame = state.frame
}

// S39: Set a per-pane timeframe for a compare pane. Returns true if changed.
// Invalidates only the target pane — other panes are unaffected.
apply_set_compare_pane_timeframe :: proc(state: ^App_State, pane_idx: int, tf_idx: int) -> bool {
	if state == nil do return false
	if pane_idx < 0 || pane_idx >= state.compare.count do return false
	if tf_idx < -1 || tf_idx >= len(TF_OPTIONS) do return false
	if state.compare.tf_idx[pane_idx] == tf_idx do return false

	state.compare.tf_idx[pane_idx] = tf_idx

	// S39: Reset per-pane getrange state — stale data from old TF.
	state.compare.getranges[pane_idx] = {}
	state.compare.scroll_x[pane_idx] = 0
	state.compare.zoom[pane_idx] = 0

	// S39: Clear TF-sensitive stores in the pane's slot.
	eff_sid := compare_pane_resolve_subject_id(state, pane_idx)
	if eff_sid != 0 {
		reg := state.stream_views
		if reg != nil {
			if slot_idx := stream_view_find_slot(reg, eff_sid); slot_idx >= 0 {
				slot := &reg.slots[slot_idx]
				slot.candle_store.head = 0
				slot.candle_store.count = 0
				slot.heatmap_store = {}
				slot.heatmap_snapshot = {}
				slot.vpvr_store = {}
				// S99: Clear analytics on canonical layer_store stream (was slot.analytics_store).
				if ms := layers.market_store_stream_for_subject(&state.layer_store, eff_sid); ms != nil {
					services.analytics_store_clear(&ms.analytics)
				}
				md_common.apply_state_on_tf_change(&slot.apply_state)
			}
		}
	}

	// Reconcile subscriptions since TF change affects candle/heatmap/vpvr subjects.
	reconcile_subscriptions(state)

	// Request historical data for the new TF.
	request_compare_pane_candle_range(state, pane_idx)

	// S95: Refetch subplot analytics for the new TF.
	request_compare_pane_subplot_analytics(state, pane_idx)

	return true
}

// S84: Request analytics range data for a compare pane.
// Resolves venue/symbol/TF from the pane's seed slot, then dispatches
// to the appropriate cold reader API for the pane's analytics kind.
request_compare_pane_analytics_range :: proc(state: ^App_State, pane_idx: int) {
	if state == nil do return
	if pane_idx < 0 || pane_idx >= state.compare.count do return

	seed_sid := state.compare.slots[pane_idx]
	if seed_sid == 0 do return

	reg := state.stream_views
	if reg == nil do return

	slot_idx := stream_view_find_slot(reg, seed_sid)
	if slot_idx < 0 do return
	slot := &reg.slots[slot_idx]
	if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
	if !slot.has_stream_info do return

	venue := slot.stream_info.venue
	symbol := normalized_symbol(slot.stream_info.symbol)  // S92: Normalize to strip market type suffix for API.
	if len(venue) == 0 || len(symbol) == 0 do return

	tf := compare_pane_effective_tf_string(state, pane_idx)
	kind := state.compare.analytics_kind[pane_idx]

	// S99: Resolve target store — canonical source is layer_store Market_Stream.
	eff_sid := compare_pane_resolve_subject_id(state, pane_idx)
	store := resolve_analytics_store_for_subject(state, eff_sid)
	if store == nil {
		// Fallback: try seed slot's subject_id.
		store = resolve_analytics_store_for_subject(state, reg.slots[slot_idx].subject_id)
	}
	if store == nil {
		store = active_analytics_store(state)
	}
	if store == nil do return

	buf: [ANALYTICS_RANGE_BUF_CAP]u8
	switch kind {
	case .CVD:
		if state.marketdata.fetch_analytics_cvd != nil {
			n := state.marketdata.fetch_analytics_cvd(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 { services.parse_analytics_cvd_range(store, buf[:n]) }
		}
	case .Delta_Volume:
		if state.marketdata.fetch_analytics_delta_volume != nil {
			n := state.marketdata.fetch_analytics_delta_volume(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 { services.parse_analytics_delta_volume_range(store, buf[:n]) }
		}
	case .Bar_Stats:
		if state.marketdata.fetch_analytics_bar_stats != nil {
			n := state.marketdata.fetch_analytics_bar_stats(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 { services.parse_analytics_bar_stats_range(store, buf[:n]) }
		}
	case .Open_Interest:
		if state.marketdata.fetch_analytics_oi != nil {
			n := state.marketdata.fetch_analytics_oi(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 { services.parse_analytics_oi_range(store, buf[:n]) }
		}
	}
}

// S95: Fetch analytics range data for all active subplots on a compare pane.
// Called on compare entry and when subplots are toggled on.
request_compare_pane_subplot_analytics :: proc(state: ^App_State, pane_idx: int) {
	if state == nil do return
	if pane_idx < 0 || pane_idx >= state.compare.count do return

	if state.compare.show_cvd[pane_idx] {
		request_compare_pane_subplot_analytics_kind(state, pane_idx, .CVD)
	}
	if state.compare.show_delta_vol[pane_idx] {
		request_compare_pane_subplot_analytics_kind(state, pane_idx, .Delta_Volume)
	}
	if state.compare.show_oi[pane_idx] {
		request_compare_pane_subplot_analytics_kind(state, pane_idx, .Open_Interest)
	}
}

// S95: Fetch analytics range for a specific kind on a compare pane.
// Reuses the same resolution logic as request_compare_pane_analytics_range
// but with an explicit kind parameter instead of the pane's analytics_kind.
request_compare_pane_subplot_analytics_kind :: proc(
	state: ^App_State,
	pane_idx: int,
	kind: services.Analytics_Kind,
) {
	if state == nil do return
	if pane_idx < 0 || pane_idx >= state.compare.count do return

	seed_sid := state.compare.slots[pane_idx]
	if seed_sid == 0 do return

	reg := state.stream_views
	if reg == nil do return

	slot_idx := stream_view_find_slot(reg, seed_sid)
	if slot_idx < 0 do return
	slot := &reg.slots[slot_idx]
	if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
	if !slot.has_stream_info do return

	venue := slot.stream_info.venue
	symbol := normalized_symbol(slot.stream_info.symbol)
	if len(venue) == 0 || len(symbol) == 0 do return

	tf := compare_pane_effective_tf_string(state, pane_idx)

	// S99: Resolve target store — canonical source is layer_store Market_Stream.
	eff_sid := compare_pane_resolve_subject_id(state, pane_idx)
	store := resolve_analytics_store_for_subject(state, eff_sid)
	if store == nil {
		store = resolve_analytics_store_for_subject(state, reg.slots[slot_idx].subject_id)
	}
	if store == nil {
		store = active_analytics_store(state)
	}
	if store == nil do return

	buf: [ANALYTICS_RANGE_BUF_CAP]u8
	switch kind {
	case .CVD:
		if state.marketdata.fetch_analytics_cvd != nil {
			n := state.marketdata.fetch_analytics_cvd(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 { services.parse_analytics_cvd_range(store, buf[:n]) }
		}
	case .Delta_Volume:
		if state.marketdata.fetch_analytics_delta_volume != nil {
			n := state.marketdata.fetch_analytics_delta_volume(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 { services.parse_analytics_delta_volume_range(store, buf[:n]) }
		}
	case .Bar_Stats:
		if state.marketdata.fetch_analytics_bar_stats != nil {
			n := state.marketdata.fetch_analytics_bar_stats(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 { services.parse_analytics_bar_stats_range(store, buf[:n]) }
		}
	case .Open_Interest:
		if state.marketdata.fetch_analytics_oi != nil {
			n := state.marketdata.fetch_analytics_oi(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 { services.parse_analytics_oi_range(store, buf[:n]) }
		}
	}
}

// S99: Resolve the canonical analytics store for a subject_id.
// Returns a pointer to the Market_Stream.analytics in layer_store, or nil if not found.
resolve_analytics_store_for_subject :: proc(state: ^App_State, subject_id: u64) -> ^services.Analytics_Store {
	if state == nil || subject_id == 0 do return nil
	if ms := layers.market_store_stream_get_or_alloc(&state.layer_store, subject_id); ms != nil {
		return &ms.analytics
	}
	return nil
}

// S37: Resolve global TF in milliseconds (utility, retained for non-pane contexts).
global_tf_ms :: proc(state: ^App_State) -> i64 {
	options := TF_OPTION_MS
	if state.active_tf_idx >= 0 && state.active_tf_idx < len(options) {
		return options[state.active_tf_idx]
	}
	return options[0]
}
