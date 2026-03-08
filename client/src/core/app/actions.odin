package app

import "mr:ports"
import "mr:services"
import "mr:ui"
import "mr:widgets"

// S53: Close all modal overlays. Called before opening a new modal to enforce exclusivity.
close_all_overlays :: proc(state: ^App_State) {
	state.overlays.show_help = false
	state.overlays.show_exchange_manager = false
	state.overlays.show_widget_catalog = false
	state.overlays.show_stream_picker = false
	state.overlays.cell_stream_picker_open = -1
	state.overlays.catalog_step = 0
}

queue_ui_action :: proc(state: ^App_State, action: UI_Action) {
	if state.ui_action_count >= len(state.ui_actions) {
		state.ui_action_drops += 1
		return
	}
	state.ui_actions_enqueued_total += 1
	state.ui_actions[state.ui_action_count] = action
	state.ui_action_count += 1
}

queue_ui_actions_from_input :: proc(state: ^App_State, input: ports.Input_State) {
	pressed := input.keys.just_pressed
	switch ui.global_command_from_keys(
		input.modifiers.ctrl,
		.K in pressed,
		.G in pressed,
		.R in pressed,
		.H in pressed,
		.D in pressed,
	) {
	case .Open_Connection_Manager:
		queue_ui_action(state, UI_Action{kind = .Toggle_Connection_Modal})
	case .Toggle_Stream_Picker:
		queue_ui_action(state, UI_Action{kind = .Toggle_Stream_Picker})
	case .Resync_Active_Stream:
		queue_ui_action(state, UI_Action{kind = .Resync_Active_Stream})
	case .Toggle_Telemetry_HUD:
		queue_ui_action(state, UI_Action{kind = .Toggle_Telemetry_HUD})
	case .Capture_Runtime_Snapshot:
		queue_ui_action(state, UI_Action{kind = .Capture_Runtime_Snapshot})
	case .None:
	}

	// Escape: close picker, exit focus mode, compare mode, close modals, or close help overlay.
	if .Escape in pressed {
		if state.overlays.show_stream_picker {
			queue_ui_action(state, UI_Action{kind = .Toggle_Stream_Picker})
		} else if state.overlays.show_widget_catalog {
			state.overlays.show_widget_catalog = false
		} else if state.overlays.cell_stream_picker_open >= 0 {
			state.overlays.cell_stream_picker_open = -1
		} else if state.overlays.show_exchange_manager {
			queue_ui_action(state, UI_Action{kind = .Toggle_Connection_Modal})
		} else if state.zen.active {
			queue_ui_action(state, UI_Action{kind = .Toggle_Zen_Mode})
		} else if state.focus_mode {
			queue_ui_action(state, UI_Action{kind = .Toggle_Focus_Mode})
		} else if state.compare.active {
			queue_ui_action(state, UI_Action{kind = .Exit_Compare})
		} else if state.overlays.show_help {
			queue_ui_action(state, UI_Action{kind = .Toggle_Help})
		} else if state.chrome.active_route == .Instrument_Overview {
			queue_ui_action(state, UI_Action{kind = .Navigate_Route, route = .Markets})
		} else if state.chrome.active_route == .Session_Health {
			queue_ui_action(state, UI_Action{kind = .Navigate_Route, route = .Dashboard})
		}
	}

	if .Tab in pressed {
		if state.compare.active {
			// In compare mode, Tab adds next stream instead of switching.
			queue_ui_action(state, UI_Action{kind = .Add_Compare_Stream})
		} else if input.modifiers.shift {
			queue_ui_action(state, UI_Action{kind = .Cycle_Stream_Prev})
		} else {
			queue_ui_action(state, UI_Action{kind = .Cycle_Stream_Next})
		}
	}
	// Number keys: Shift+N = set focused cell/pane TF, plain N = set global TF.
	if input.modifiers.shift {
		// S39: In compare mode, Shift+N targets the focused pane.
		if state.compare.active && state.compare.focused_pane >= 0 && state.compare.focused_pane < state.compare.count {
			fpi := state.compare.focused_pane
			if .Num_1 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Compare_Pane_Timeframe, pane_idx = fpi, timeframe_idx = 0}) }
			if .Num_2 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Compare_Pane_Timeframe, pane_idx = fpi, timeframe_idx = 1}) }
			if .Num_3 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Compare_Pane_Timeframe, pane_idx = fpi, timeframe_idx = 2}) }
			if .Num_4 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Compare_Pane_Timeframe, pane_idx = fpi, timeframe_idx = 3}) }
			if .Num_5 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Compare_Pane_Timeframe, pane_idx = fpi, timeframe_idx = 4}) }
			if .Num_6 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Compare_Pane_Timeframe, pane_idx = fpi, timeframe_idx = 5}) }
			if .Num_7 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Compare_Pane_Timeframe, pane_idx = fpi, timeframe_idx = 6}) }
			if .Num_8 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Compare_Pane_Timeframe, pane_idx = fpi, timeframe_idx = 7}) }
			if .Num_9 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Compare_Pane_Timeframe, pane_idx = fpi, timeframe_idx = 8}) }
		} else {
			fci := state.world.focused
			if fci >= 0 && fci < state.world.count && state.world.widgets[fci].kind == .Candle {
				if .Num_1 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = fci, timeframe_idx = 0}) }
				if .Num_2 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = fci, timeframe_idx = 1}) }
				if .Num_3 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = fci, timeframe_idx = 2}) }
				if .Num_4 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = fci, timeframe_idx = 3}) }
				if .Num_5 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = fci, timeframe_idx = 4}) }
				if .Num_6 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = fci, timeframe_idx = 5}) }
				if .Num_7 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = fci, timeframe_idx = 6}) }
				if .Num_8 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = fci, timeframe_idx = 7}) }
				if .Num_9 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = fci, timeframe_idx = 8}) }
			}
		}
	} else {
		if .Num_1 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = 0}) } // 1s
		if .Num_2 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = 1}) } // 5s
		if .Num_3 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = 2}) } // 1m
		if .Num_4 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = 3}) } // 5m
		if .Num_5 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = 4}) } // 15m
		if .Num_6 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = 5}) } // 30m
		if .Num_7 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = 6}) } // 1h
		if .Num_8 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = 7}) } // 4h
		if .Num_9 in pressed { queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = 8}) } // 1d
	}
	if .S in pressed {
		queue_ui_action(state, UI_Action{kind = .Toggle_Detail_Panel})
	}
	if .Slash in pressed && input.modifiers.shift {
		queue_ui_action(state, UI_Action{kind = .Toggle_Help})
	}
	if .C in pressed {
		queue_ui_action(state, UI_Action{kind = .Toggle_Compare})
	}
	if .F in pressed {
		queue_ui_action(state, UI_Action{kind = .Toggle_Focus_Mode})
	}
	// G handled by ui.command_from_input.
	if .M in pressed {
		queue_ui_action(state, UI_Action{kind = .Toggle_MA})
	}
	if .B in pressed {
		queue_ui_action(state, UI_Action{kind = .Toggle_BBands})
	}
	if .V in pressed {
		queue_ui_action(state, UI_Action{kind = .Toggle_VWAP})
	}
	if .R in pressed {
		if !input.modifiers.ctrl do queue_ui_action(state, UI_Action{kind = .Toggle_RSI})
	}
	if .I in pressed {
		queue_ui_action(state, UI_Action{kind = .Toggle_MACD})
	}
	if .H in pressed {
		if !input.modifiers.ctrl do queue_ui_action(state, UI_Action{kind = .Toggle_Funding})
	}
	if .J in pressed {
		queue_ui_action(state, UI_Action{kind = .Toggle_Liq})
	}
	if .K in pressed {
		if !input.modifiers.ctrl do queue_ui_action(state, UI_Action{kind = .Toggle_Trade_Counter})
	}
	if .Z in pressed {
		queue_ui_action(state, UI_Action{kind = .Toggle_Zen_Mode})
	}
	if .Delete in pressed {
		queue_ui_action(state, UI_Action{kind = .Delete_Draw_Tool})
	}

	state.last_keys_pressed = input.keys.pressed
}

apply_ui_actions :: proc(state: ^App_State) -> (stream_switched: bool, tf_switched: bool) {
	set_timeframe_consumed := false
	for i in 0 ..< state.ui_action_count {
		action := state.ui_actions[i]
		switch action.kind {
		case .Cycle_Stream_Next:
			if apply_cycle_stream_action(state, true) {
				stream_switched = true
				state.stream_switches_total += 1
			}
		case .Cycle_Stream_Prev:
			if apply_cycle_stream_action(state, false) {
				stream_switched = true
				state.stream_switches_total += 1
			}
		case .Set_Timeframe:
			if set_timeframe_consumed do break
			set_timeframe_consumed = true
			if apply_set_timeframe_action(state, action.timeframe_idx) {
				tf_switched = true
				state.timeframe_switches_total += 1
				state.tf_flash_frame = state.frame
				tf_opts := TF_OPTIONS
				show_toast(state, tf_opts[state.active_tf_idx])
				// Trigger TF OSD in zen mode.
				if state.zen.active {
					state.zen.tf_osd_frame = state.frame
				}
			}
		case .Toggle_Sidebar:
			state.chrome.sidebar.expanded = !state.chrome.sidebar.expanded
			services.settings_set(&state.settings, services.SETTING_SIDEBAR_EXPANDED,
				state.chrome.sidebar.expanded ? "1" : "0")
			services.settings_flush(&state.settings)
		case .Toggle_Panel:
			idx := action.panel_idx
			if idx >= 0 && idx < ui.PANEL_COUNT {
				if state.chrome.panel_visible[idx] {
					visible_count := 0
					for p in 0 ..< ui.PANEL_COUNT {
						if state.chrome.panel_visible[p] do visible_count += 1
					}
					// Keep at least one panel visible to avoid an unusable empty workspace.
					if visible_count <= 1 {
						break
					}
				}
				state.chrome.panel_visible[idx] = !state.chrome.panel_visible[idx]
				ui.sync_sidebar_visibility(&state.chrome.sidebar, state.chrome.panel_visible)
				layout_from_panels(state)
				mask_buf: [ui.PANEL_COUNT]u8
				services.settings_set(&state.settings, services.SETTING_PANEL_VISIBLE_MASK,
					panel_visibility_mask_encode_into(mask_buf[:], state.chrome.panel_visible))
				services.settings_flush(&state.settings)
			}
		case .Toggle_Help:
			if !state.overlays.show_help { close_all_overlays(state) }
			state.overlays.show_help = !state.overlays.show_help
		case .Toggle_Telemetry_HUD:
			state.telemetry.hud_enabled = !state.telemetry.hud_enabled
			show_toast(state, state.telemetry.hud_enabled ? "Telemetry HUD: ON" : "Telemetry HUD: OFF")
		case .Toggle_Compare:
			if state.compare.active {
				state.compare.active = false
				show_toast(state, "Compare: OFF")
			} else {
				// S53: Entering compare exits focus mode.
				state.focus_mode = false
				apply_enter_compare(state)
				if state.compare.active {
					show_toast(state, "Compare: ON")
				}
			}
		case .Add_Compare_Stream:
			apply_add_compare_stream(state)
		case .Exit_Compare:
			state.compare.active = false
			show_toast(state, "Compare: OFF")
		case .Navigate_Route:
			page_navigate(state, state.chrome.active_route, action.route)
		case .Toggle_Detail_Panel:
			state.chrome.detail_expanded = !state.chrome.detail_expanded
			services.settings_set(&state.settings, services.SETTING_SIDEBAR_EXPANDED,
				state.chrome.detail_expanded ? "1" : "0")
			services.settings_flush(&state.settings)
		case .Set_Layout_Preset:
			preset_idx := clamp(action.layout_preset, 0, ui.LAYOUT_PRESET_COUNT - 1)
			state.layout_preset = preset_idx
			grid_def, vis := ui.get_layout_preset(preset_idx, 6)
			state.custom_grid_def = grid_def
			state.chrome.panel_visible = vis
			ui.sync_sidebar_visibility(&state.chrome.sidebar, state.chrome.panel_visible)
			layout_from_panels(state)
			// Reset per-cell TF overrides and spans so all cells follow global TF in new layout.
			for ci in 0 ..< state.world.count {
				state.world.timeframes[ci].tf_idx = -1
				state.world.getranges[ci].pending = false
				state.world.getranges[ci].seeded = false
				state.world.getranges[ci].oldest_ts = 0
				state.world.spans[ci] = {} // BUG-18: Clear spans on layout preset change.
			}
			persist_layout_v6(state)
		case .Toggle_Connection_Modal:
			if !state.overlays.show_exchange_manager { close_all_overlays(state) }
			state.overlays.show_exchange_manager = !state.overlays.show_exchange_manager
		case .Select_Profile:
			apply_select_profile_action(state, action.profile_idx)
		case .Add_Profile:
			apply_add_profile_action(state)
		case .Remove_Profile:
			apply_remove_profile_action(state, action.profile_idx)
		case .Apply_Profile:
			apply_apply_profile_action(state, action.profile_idx)
		case .Connect_Profile:
			apply_connect_profile_action(state, action.profile_idx)
		case .Disconnect_Profile:
			apply_disconnect_profile_action(state)
		case .Set_Cell_Widget:
			apply_set_cell_widget_action(state, action)
		case .Set_Cell_Stream:
			apply_set_cell_stream_action(state, action)
		case .Add_Cell:
			apply_add_cell_action(state, action)
		case .Remove_Cell:
			apply_remove_cell_action(state, action.cell_idx)
		case .Toggle_Focus_Mode:
			state.focus_mode = !state.focus_mode
			if state.focus_mode {
				// S53: Entering focus exits compare mode.
				state.compare.active = false
			}
			show_toast(state, state.focus_mode ? "Focus Mode" : "Normal Mode")
		case .Toggle_Stream_Picker:
			if !state.overlays.show_stream_picker { close_all_overlays(state) }
			state.overlays.show_stream_picker = !state.overlays.show_stream_picker
		case .Pick_Stream:
			apply_pick_stream_action(state, action.subject_id)
			state.overlays.show_stream_picker = false
		case .Toggle_MA:
			toggle_focused_indicator(state, 0)
		case .Toggle_BBands:
			toggle_focused_indicator(state, 1)
		case .Toggle_VWAP:
			toggle_focused_indicator(state, 2)
		case .Toggle_RSI:
			toggle_focused_indicator(state, 3)
		case .Toggle_MACD:
			toggle_focused_indicator(state, 4)
		case .Toggle_Funding:
			toggle_focused_indicator(state, 5)
		case .Toggle_Liq:
			toggle_focused_indicator(state, 6)
		case .Toggle_Trade_Counter:
			toggle_focused_indicator(state, 7)
		case .Delete_Draw_Tool:
			widgets.remove_selected_tool(&state.draw_tools)
		case .Subscribe_Market:
			apply_subscribe_market_action(state, action.market_entry_idx)
			state.overlays.show_stream_picker = false
		case .Unsubscribe_Market:
			apply_unsubscribe_market_action(state, action.market_entry_idx)
		case .Toggle_Widget_Catalog:
			if !state.overlays.show_widget_catalog { close_all_overlays(state) }
			state.overlays.show_widget_catalog = !state.overlays.show_widget_catalog
			state.overlays.catalog_step = 0
		case .Open_Cell_Stream_Picker:
			close_all_overlays(state)
			state.overlays.cell_stream_picker_open = action.cell_idx
		case .Close_Cell_Stream_Picker:
			state.overlays.cell_stream_picker_open = -1
		case .Toggle_Zen_Mode:
			state.zen.active = !state.zen.active
			// BUG-25: Reset all alpha values on both enter and exit.
			state.zen.top_alpha = 0
			state.zen.bottom_alpha = 0
			state.zen.left_alpha = 0
			show_toast(state, state.zen.active ? "Zen Mode" : "Normal")
		case .Set_Cell_Timeframe:
			if apply_set_cell_timeframe_action(state, action.cell_idx, action.timeframe_idx) {
				tf_switched = true
				state.timeframe_switches_total += 1
				tf_opts := TF_OPTIONS
				cell_tf := action.timeframe_idx
				if cell_tf >= 0 && cell_tf < len(tf_opts) {
					show_toast(state, tf_opts[cell_tf])
				}
			}
		case .Set_Compare_Pane_Timeframe:
			if apply_set_compare_pane_timeframe(state, action.pane_idx, action.timeframe_idx) {
				tf_switched = true
				state.timeframe_switches_total += 1
				tf_opts := TF_OPTIONS
				pane_tf := action.timeframe_idx
				if pane_tf >= 0 && pane_tf < len(tf_opts) {
					show_toast(state, tf_opts[pane_tf])
				}
			}
		case .Focus_Compare_Pane:
			if state.compare.active && action.pane_idx >= 0 && action.pane_idx < state.compare.count {
				state.compare.focused_pane = action.pane_idx
			}
		case .Resync_Active_Stream:
			apply_resync_active_stream_action(state)
		case .Capture_Runtime_Snapshot:
			if capture_runtime_snapshot_to_clipboard(state) {
				show_toast(state, "Snapshot copied")
			}
		case .Set_Cell_Span:
			apply_set_cell_span_action(state, action.cell_idx, action.col_span, action.row_span)
		case .Clear_All_Cells:
			apply_clear_all_cells_action(state)
		case .Navigate_Instrument_Overview:
			apply_navigate_instrument_overview(state, action.market_entry_idx)
		}
	}
	state.ui_action_count = 0
	return
}

apply_enter_compare :: proc(state: ^App_State) {
	reg := state.stream_views
	if reg == nil || !reg.has_active do return
	if reg.count < 2 do return // need at least 2 streams to compare

	state.compare.active = true
	state.compare.count = 1
	state.compare.widget_idx = 2 // Default to Candles (most reliable data via GetRange)
	state.compare.focused_pane = 0 // S39: focus first pane by default
	state.compare.slots[0] = reg.active_subject_id
	for i in 0 ..< len(state.compare.show_vol) {
		state.compare.tf_idx[i] = -1 // S38: follow global TF by default
		state.compare.getranges[i] = {} // S39: reset per-pane getrange
		state.compare.show_vol[i] = state.chart_display.show_vol
		state.compare.show_heatmap[i] = state.chart_display.show_heatmap
		state.compare.show_vpvr[i] = state.chart_display.show_vpvr
		state.compare.heatmap_idx[i] = state.chart_display.heatmap_intensity_idx
		state.compare.scroll_x[i] = state.world.views[0].candle_scroll_x
		state.compare.zoom[i] = state.world.views[0].candle_zoom
		state.compare.ob_scroll[i] = 0
		state.compare.ob_grp[i] = 1
		state.compare.trade_scroll[i] = 0
		state.compare.trade_filter[i] = 0
	}

	// Auto-add the next stream.
	for i in 0 ..< STREAM_VIEW_CAP {
		if !reg.slots[i].used do continue
		if reg.slots[i].subject_id == state.compare.slots[0] do continue
		state.compare.slots[1] = reg.slots[i].subject_id
		state.compare.count = 2
		break
	}

	// S41: Trigger initial backfill for all compare panes.
	for cpi in 0 ..< state.compare.count {
		request_compare_pane_candle_range(state, cpi)
	}
}

// Set a specific indicator on a cell's Indicator_Component by index (0-7).
set_indicator_on_cell :: proc(ind: ^Indicator_Component, idx: int, value: bool) {
	if ind == nil || idx < 0 || idx >= 8 do return
	cell_ptrs := [8]^bool{
		&ind.show_ma, &ind.show_bbands, &ind.show_vwap, &ind.show_rsi,
		&ind.show_macd, &ind.show_funding, &ind.show_liq, &ind.show_trade_counter,
	}
	cell_ptrs[idx]^ = value
}

// Toggle an indicator on the focused candle cell, syncing to global default + settings.
toggle_focused_indicator :: proc(state: ^App_State, idx: int) {
	IND_KEYS :: [8]string{
		services.SETTING_SHOW_MA, services.SETTING_SHOW_BBANDS, services.SETTING_SHOW_VWAP,
		services.SETTING_SHOW_RSI, services.SETTING_SHOW_MACD, services.SETTING_SHOW_FUNDING,
		services.SETTING_SHOW_LIQ, services.SETTING_SHOW_TRADE_COUNTER,
	}
	IND_LABELS_ON :: [8]string{"MA: ON", "BBands: ON", "VWAP: ON", "RSI: ON", "MACD: ON", "Funding: ON", "Liq: ON", "Trade Counter: ON"}
	IND_LABELS_OFF :: [8]string{"MA: OFF", "BBands: OFF", "VWAP: OFF", "RSI: OFF", "MACD: OFF", "Funding: OFF", "Liq: OFF", "Trade Counter: OFF"}
	if idx < 0 || idx >= 8 do return

	// Resolve pointers: focused candle cell, else global.
	global_ptrs := [8]^bool{
		&state.indicators.show_ma, &state.indicators.show_bbands, &state.indicators.show_vwap, &state.indicators.show_rsi,
		&state.indicators.show_macd, &state.indicators.show_funding, &state.indicators.show_liq, &state.indicators.show_trade_counter,
	}
	fci := state.world.focused
	has_focus := fci >= 0 && fci < state.world.count && state.world.widgets[fci].kind == .Candle
	if has_focus {
		new_val := !global_ptrs[idx]^
		set_indicator_on_cell(&state.world.indicators[fci], idx, new_val)
		global_ptrs[idx]^ = new_val
	} else {
		global_ptrs[idx]^ = !global_ptrs[idx]^
	}
	keys := IND_KEYS
	on := IND_LABELS_ON
	off := IND_LABELS_OFF
	services.settings_set(&state.settings, keys[idx], global_ptrs[idx]^ ? "1" : "0")
	services.settings_flush(&state.settings)
	show_toast(state, global_ptrs[idx]^ ? on[idx] : off[idx])
}

// Initialize ECS world components for a cell with global defaults.
init_world_cell_defaults :: proc(state: ^App_State, ci: int, widget: Widget_Kind = .Empty, stream_idx: int = -1) {
	state.world.widgets[ci]    = Widget_Component{kind = widget}
	state.world.bindings[ci]   = Stream_Binding{
		stream_idx       = stream_idx,
		bound_venue_len  = 0,
		bound_symbol_len = 0,
	}
	state.world.views[ci]      = {}
	state.world.timeframes[ci] = Timeframe_Component{tf_idx = -1}
	state.world.getranges[ci]  = {}
	state.world.charts[ci]     = Chart_Component{
		show_vol              = state.chart_display.show_vol,
		show_heatmap          = state.chart_display.show_heatmap,
		show_vpvr             = state.chart_display.show_vpvr,
		heatmap_intensity_idx = state.chart_display.heatmap_intensity_idx,
	}
	state.world.indicators[ci] = Indicator_Component{
		show_ma            = state.indicators.show_ma,
		show_bbands        = state.indicators.show_bbands,
		show_vwap          = state.indicators.show_vwap,
		show_rsi           = state.indicators.show_rsi,
		show_macd          = state.indicators.show_macd,
		show_funding       = state.indicators.show_funding,
		show_liq           = state.indicators.show_liq,
		show_trade_counter = state.indicators.show_trade_counter,
	}
	state.world.ind_params[ci] = Indicator_Params{
		ma_periods  = state.indicators.ma_periods,
		bb_period   = state.indicators.bb_period,
		bb_sigma    = state.indicators.bb_sigma,
		rsi_period  = state.indicators.rsi_period,
		macd_fast   = state.indicators.macd_fast,
		macd_slow   = state.indicators.macd_slow,
		macd_signal = state.indicators.macd_signal,
	}
	state.world.subplots[ci]   = Subplot_Component{sub_resize_idx = -1}
	state.world.spans[ci]      = {}
	state.world.analytics[ci]  = {}
}

apply_add_compare_stream :: proc(state: ^App_State) {
	if !state.compare.active do return
	if state.compare.count >= 4 do return
	reg := state.stream_views
	if reg == nil do return

	// Find the next unused stream not already in compare slots.
	for i in 0 ..< STREAM_VIEW_CAP {
		if !reg.slots[i].used do continue
		sid := reg.slots[i].subject_id
		already := false
		for c in 0 ..< state.compare.count {
			if state.compare.slots[c] == sid {
				already = true
				break
			}
		}
		if already do continue
		si := state.compare.count
		state.compare.slots[si] = sid
		// Reset view state for the new slot so stale scroll/zoom doesn't carry over.
		state.compare.tf_idx[si] = -1 // S38: follow global TF by default
		state.compare.getranges[si] = {} // S39: reset per-pane getrange
		state.compare.ob_scroll[si] = 0
		state.compare.ob_grp[si] = 1
		state.compare.trade_scroll[si] = 0
		state.compare.trade_filter[si] = 0
		state.compare.scroll_x[si] = 0
		state.compare.zoom[si] = 0
		state.compare.show_vol[si] = state.chart_display.show_vol
		state.compare.show_heatmap[si] = state.chart_display.show_heatmap
		state.compare.show_vpvr[si] = state.chart_display.show_vpvr
		state.compare.heatmap_idx[si] = state.chart_display.heatmap_intensity_idx
		state.compare.count += 1
		// S41: Trigger initial backfill for the newly added pane.
		request_compare_pane_candle_range(state, si)
		return
	}
}
