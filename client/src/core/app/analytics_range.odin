package app

// S81: Analytics historical range fetch.
// Fetches CVD/DV/BS data from cold reader APIs and populates the analytics store.
// Called on analytics widget creation and on TF change.

import "mr:layers"
import "mr:services"

ANALYTICS_RANGE_BUF_CAP :: i32(16384)

// S138: Bootstrap subplot analytics for all candle cells with active subplots.
// Called on reconnect, TF change, and stream switch to pre-populate
// CVD/DeltaVol/OI data from cold reader APIs so subplots render immediately.
request_active_subplot_analytics :: proc(state: ^App_State) {
	if state == nil do return
	for ci in 0 ..< state.world.count {
		if state.world.widgets[ci].kind != .Candle do continue
		ind := &state.world.indicators[ci]
		if ind.show_cvd {
			_fetch_subplot_analytics_for_cell(state, ci, .CVD)
		}
		if ind.show_delta_vol {
			_fetch_subplot_analytics_for_cell(state, ci, .Delta_Volume)
		}
		if ind.show_oi {
			_fetch_subplot_analytics_for_cell(state, ci, .Open_Interest)
		}
	}
}

// S138: Fetch a single analytics kind for a candle cell's subplot.
// Reuses the same venue/symbol/TF/store resolution as request_analytics_range
// but accepts an explicit kind rather than reading from analytics ECS component.
@(private = "file")
_fetch_subplot_analytics_for_cell :: proc(state: ^App_State, ci: int, kind: services.Analytics_Kind) {
	if state == nil do return
	if ci < 0 || ci >= state.world.count do return

	// Resolve venue/symbol from binding.
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

	symbol = normalized_symbol(symbol)

	// Resolve TF.
	tf_opts := TF_OPTIONS
	eff_tf := cell_effective_tf_idx(state, ci)
	tf := tf_opts[0]
	if eff_tf >= 0 && eff_tf < len(tf_opts) {
		tf = tf_opts[eff_tf]
	}

	// Resolve target store.
	store: ^services.Analytics_Store
	if reg != nil {
		stream_idx := state.world.bindings[ci].stream_idx
		if stream_idx >= 0 && stream_idx < STREAM_VIEW_CAP && reg.slots[stream_idx].used {
			sid := reg.slots[stream_idx].subject_id
			if ms := layers.market_store_stream_get_or_alloc(&state.layer_store, sid); ms != nil {
				store = &ms.analytics
			}
		}
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

// Request historical analytics data for a cell's analytics kind.
// Resolves venue/symbol/TF from the cell's stream binding, then
// dispatches to the appropriate cold reader API.
request_analytics_range :: proc(state: ^App_State, ci: int) {
	if state == nil do return
	if ci < 0 || ci >= state.world.count do return
	if state.world.widgets[ci].kind != .Analytics do return

	kind := state.world.analytics[ci].analytics_kind
	// OI is now served via cold reader API with cadence/confidence metadata.

	// Resolve venue/symbol from binding.
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

	// S92: Normalize symbol — strip market type suffix (e.g. "BTCUSDT:PERP" → "BTCUSDT")
	// to match backend API contract. Without this, analytics endpoints return 400.
	symbol = normalized_symbol(symbol)

	// Resolve TF.
	tf_opts := TF_OPTIONS
	eff_tf := cell_effective_tf_idx(state, ci)
	tf := tf_opts[0]
	if eff_tf >= 0 && eff_tf < len(tf_opts) {
		tf = tf_opts[eff_tf]
	}

	// S99: Resolve target store — canonical source is layer_store Market_Stream.
	store: ^services.Analytics_Store
	if reg != nil {
		stream_idx := state.world.bindings[ci].stream_idx
		if stream_idx >= 0 && stream_idx < STREAM_VIEW_CAP && reg.slots[stream_idx].used {
			sid := reg.slots[stream_idx].subject_id
			if ms := layers.market_store_stream_get_or_alloc(&state.layer_store, sid); ms != nil {
				store = &ms.analytics
			}
		}
	}
	if store == nil {
		store = active_analytics_store(state)
	}
	if store == nil do return

	// Dispatch fetch by analytics kind.
	buf: [ANALYTICS_RANGE_BUF_CAP]u8
	switch kind {
	case .CVD:
		if state.marketdata.fetch_analytics_cvd != nil {
			n := state.marketdata.fetch_analytics_cvd(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 {
				services.parse_analytics_cvd_range(store, buf[:n])
			}
		}
	case .Delta_Volume:
		if state.marketdata.fetch_analytics_delta_volume != nil {
			n := state.marketdata.fetch_analytics_delta_volume(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 {
				services.parse_analytics_delta_volume_range(store, buf[:n])
			}
		}
	case .Bar_Stats:
		if state.marketdata.fetch_analytics_bar_stats != nil {
			n := state.marketdata.fetch_analytics_bar_stats(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 {
				services.parse_analytics_bar_stats_range(store, buf[:n])
			}
		}
	case .Open_Interest:
		if state.marketdata.fetch_analytics_oi != nil {
			n := state.marketdata.fetch_analytics_oi(&buf[0], ANALYTICS_RANGE_BUF_CAP, venue, symbol, tf, 64)
			if n > 0 {
				services.parse_analytics_oi_range(store, buf[:n])
			}
		}
	}
}
