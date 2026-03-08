package app

import "mr:services"

// Trigger a GetRange request for older candles when the user scrolls near the left edge.
check_lazy_candle_loading :: proc(state: ^App_State) {
	// Global active stream lazy loading — use focused candle cell's view state.
	if !state.getrange.pending && state.getrange.seeded && state.stores.candle.count > 0 &&
	   state.stores.candle.count < services.CANDLE_CAP && state.getrange.oldest_ts > 0 {
		fci := max(state.world.focused, 0)
		if fci >= state.world.count do fci = 0
		fvw := &state.world.views[fci]
		visible := fvw.candle_zoom > 0 ? max(int(fvw.candle_zoom), 1) : max(state.stores.candle.count, 1)
		scroll := int(fvw.candle_scroll_x)
		end_idx := state.stores.candle.count - scroll
		start_idx := max(end_idx - visible, 0)
		LAZY_LOAD_THRESHOLD :: 10
		if start_idx < LAZY_LOAD_THRESHOLD {
			request_older_candles(state)
		}
	}

	// Per-cell lazy loading: throttle to max 2 concurrent getrange globally.
	pending_count := 0
	if state.getrange.pending do pending_count += 1
	for ci in 0 ..< state.world.count {
		if state.world.getranges[ci].pending do pending_count += 1
	}
	// S41: Include compare pane pending count in concurrency budget.
	if state.compare.active {
		for cpi in 0 ..< state.compare.count {
			if state.compare.getranges[cpi].pending do pending_count += 1
		}
	}
	MAX_CONCURRENT_GETRANGE :: 2
	if pending_count >= MAX_CONCURRENT_GETRANGE do return

	for ci in 0 ..< state.world.count {
		wid := &state.world.widgets[ci]
		gr := &state.world.getranges[ci]
		vw := &state.world.views[ci]
		if wid.kind != .Candle do continue
		if gr.pending do continue
		if !gr.seeded do continue
		if gr.oldest_ts <= 0 do continue
		// S20: Stop if timeline boundary reached.
		if state.timeline.loaded && state.timeline.first_ts > 0 && gr.oldest_ts <= state.timeline.first_ts do continue

		// Resolve the candle store for this cell.
		stores := resolve_stores_for_cell(state, ci)
		if stores.candle == nil do continue
		if stores.candle.count <= 0 do continue
		if stores.candle.count >= services.CANDLE_CAP do continue

		visible := vw.candle_zoom > 0 ? max(int(vw.candle_zoom), 1) : max(stores.candle.count, 1)
		scroll := int(vw.candle_scroll_x)
		end_idx := stores.candle.count - scroll
		start_idx := max(end_idx - visible, 0)
		if start_idx < 10 {
			request_cell_older_candles(state, ci)
			pending_count += 1
			if pending_count >= MAX_CONCURRENT_GETRANGE do return
		}
	}

	// S41: Lazy loading for compare panes — scroll-near-edge triggers older candle fetch.
	if state.compare.active && state.compare.widget_idx == 2 { // widget_idx 2 = Candle
		for cpi in 0 ..< state.compare.count {
			if pending_count >= MAX_CONCURRENT_GETRANGE do return
			cgr := &state.compare.getranges[cpi]
			if cgr.pending do continue
			if !cgr.seeded do continue
			if cgr.oldest_ts <= 0 do continue

			eff_sid := compare_pane_resolve_subject_id(state, cpi)
			if eff_sid == 0 do continue
			reg := state.stream_views
			if reg == nil do continue
			slot_idx := stream_view_find_slot(reg, eff_sid)
			if slot_idx < 0 do continue
			slot := &reg.slots[slot_idx]
			if slot.candle_store.count <= 0 do continue
			if slot.candle_store.count >= services.CANDLE_CAP do continue

			visible := state.compare.zoom[cpi] > 0 ? max(int(state.compare.zoom[cpi]), 1) : max(slot.candle_store.count, 1)
			scroll := int(state.compare.scroll_x[cpi])
			end_idx := slot.candle_store.count - scroll
			start_idx := max(end_idx - visible, 0)
			if start_idx < 10 {
				request_compare_pane_older_candles(state, cpi)
				pending_count += 1
			}
		}
	}
}
