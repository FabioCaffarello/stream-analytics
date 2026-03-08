package app

// S82: Session volume profile data layer.
// Fetches the current session VP snapshot from the backend cold reader API
// and populates the per-cell Session_VPVR_Store.
// Called on Session_VPVR widget creation, stream binding change, and periodic poll.

import "mr:services"

SESSION_VPVR_BUF_CAP :: i32(32768) // 32KB buffer for session VP responses

// request_session_vpvr_snapshot fetches the current session VP from the backend
// and populates the target store for the given cell.
request_session_vpvr_snapshot :: proc(state: ^App_State, ci: int) {
	if state == nil do return
	if ci < 0 || ci >= state.world.count do return
	if state.world.widgets[ci].kind != .Session_VPVR do return

	// Check port method exists.
	if state.marketdata.fetch_session_volume_profile == nil do return

	// Resolve venue/symbol from binding or active stream.
	venue, symbol: string
	reg := state.stream_views
	if binding_has(&state.world.bindings[ci]) {
		venue = binding_venue(&state.world.bindings[ci])
		symbol = binding_symbol(&state.world.bindings[ci])
	} else if reg != nil && reg.has_active {
		if slot := stream_view_active_slot(reg); slot != nil {
			if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
			if slot.has_stream_info {
				venue = slot.stream_info.venue
				symbol = slot.stream_info.symbol
			}
		}
	}
	if len(venue) == 0 || len(symbol) == 0 do return

	// S92: Normalize symbol for backend API contract.
	symbol = normalized_symbol(symbol)

	// Fetch from backend.
	buf: [SESSION_VPVR_BUF_CAP]u8
	n := state.marketdata.fetch_session_volume_profile(&buf[0], SESSION_VPVR_BUF_CAP, venue, symbol, "current")
	if n <= 0 do return

	// Resolve target store (per-slot if bound, else global).
	store: ^services.Session_VPVR_Store
	if reg != nil {
		stream_idx := state.world.bindings[ci].stream_idx
		if stream_idx >= 0 && stream_idx < STREAM_VIEW_CAP && reg.slots[stream_idx].used {
			store = &reg.slots[stream_idx].session_vpvr_store
		}
	}
	if store == nil {
		if reg != nil {
			if active_slot := stream_view_active_slot(reg); active_slot != nil {
				store = &active_slot.session_vpvr_store
			}
		}
	}
	if store == nil do return

	// Parse response into store.
	services.parse_session_vpvr_snapshot(store, buf[:n])
}
