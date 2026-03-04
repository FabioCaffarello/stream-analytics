package app

import "core:fmt"
import "core:strings"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:ui"
import "mr:widgets"

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
	) {
	case .Open_Connection_Manager:
		queue_ui_action(state, UI_Action{kind = .Toggle_Connection_Modal})
	case .Toggle_Stream_Picker:
		queue_ui_action(state, UI_Action{kind = .Toggle_Stream_Picker})
	case .Resync_Active_Stream:
		queue_ui_action(state, UI_Action{kind = .Resync_Active_Stream})
	case .Toggle_Telemetry_HUD:
		queue_ui_action(state, UI_Action{kind = .Toggle_Telemetry_HUD})
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
	// Number keys: Shift+N = set focused cell TF, plain N = set global TF.
	if input.modifiers.shift {
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
			state.overlays.show_help = !state.overlays.show_help
		case .Toggle_Telemetry_HUD:
			state.telemetry.hud_enabled = !state.telemetry.hud_enabled
			show_toast(state, state.telemetry.hud_enabled ? "Telemetry HUD: ON" : "Telemetry HUD: OFF")
		case .Toggle_Compare:
			if state.compare.active {
				state.compare.active = false
				show_toast(state, "Compare: OFF")
			} else {
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
			state.chrome.active_route = action.route
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
			persist_layout_v4(state)
		case .Toggle_Connection_Modal:
			state.overlays.show_exchange_manager = !state.overlays.show_exchange_manager
		case .Select_Profile:
			if services.profile_store_set_active(&state.profiles, action.profile_idx) {
				state.connection_manager_selected_profile = action.profile_idx
				services.profile_store_save(&state.profiles, &state.settings)
				services.settings_flush(&state.settings)
			}
		case .Add_Profile:
			if active_slot := stream_view_active_slot(state.stream_views); active_slot != nil {
				if !active_slot.has_stream_info {
					refresh_stream_info_for_slot(state, active_slot)
				}
				if active_slot.has_stream_info {
					name_buf: [32]u8
					pname := fmt.bprintf(name_buf[:], "P%d", state.profiles.count + 1)
					ws_url := ""
					if state.conn.runtime_ws_url_len > 0 {
						ws_url = string(state.conn.runtime_ws_url[:int(state.conn.runtime_ws_url_len)])
					}
					_, market_type := streams.split_symbol_market_type(active_slot.stream_info.symbol)
					profile := services.profile_make(
						pname,
						ws_url,
						active_slot.stream_info.venue,
						active_slot.stream_info.symbol,
						market_type,
						"",
						true,
					)
					if services.profile_store_upsert(&state.profiles, profile) {
						state.connection_manager_selected_profile = state.profiles.count - 1
						services.profile_store_save(&state.profiles, &state.settings)
						services.settings_flush(&state.settings)
						show_toast(state, "Profile saved")
					}
				}
			}
		case .Remove_Profile:
			if services.profile_store_remove(&state.profiles, action.profile_idx) {
				state.connection_manager_selected_profile = clamp(state.connection_manager_selected_profile, 0, max(state.profiles.count - 1, 0))
				services.profile_store_save(&state.profiles, &state.settings)
				services.settings_flush(&state.settings)
			}
		case .Apply_Profile:
			if services.profile_store_set_active(&state.profiles, action.profile_idx) {
				state.connection_manager_selected_profile = action.profile_idx
				if profile := services.profile_store_active(&state.profiles); profile != nil {
					if state.world.count > 0 {
						binding_set(&state.world.bindings[0], services.profile_venue(profile), services.profile_symbol(profile))
						state.world.bindings[0].stream_idx = -1
					}
					services.profile_store_save(&state.profiles, &state.settings)
					services.settings_flush(&state.settings)
					reconcile_subscriptions(state)
					request_active_stream_candle_range(state)
					show_toast(state, "Profile applied")
				}
			}
		case .Connect_Profile:
			if services.profile_store_set_active(&state.profiles, action.profile_idx) {
				if profile := services.profile_store_active(&state.profiles); profile != nil {
					ws_url := services.profile_ws_url(profile)
					api_key := services.profile_api_key_ref(profile)
					jwt_token := services.profile_jwt_token(profile)
					if state.marketdata.reconnect_transport != nil {
						_ = state.marketdata.reconnect_transport(ws_url, api_key, jwt_token)
					}
					state.connection_manager_selected_profile = action.profile_idx
					if state.world.count > 0 {
						binding_set(&state.world.bindings[0], services.profile_venue(profile), services.profile_symbol(profile))
						state.world.bindings[0].stream_idx = -1
					}
					services.settings_set(&state.settings, services.SETTING_AUTO_CONNECT, "1")
					services.profile_store_save(&state.profiles, &state.settings)
					services.settings_flush(&state.settings)
					reconcile_subscriptions(state)
					request_active_stream_candle_range(state)
					show_toast(state, "Connecting...")
				}
			}
		case .Disconnect_Profile:
			if state.marketdata.disconnect_transport != nil {
				_ = state.marketdata.disconnect_transport()
			}
			state.active_metrics.has_live_stats = false
			state.active_metrics.has_live_heatmap = false
			state.active_metrics.has_live_vpvr = false
			state.active_metrics.has_live_candle = false
			state.active_metrics.context_stage = .Empty
			state.active_metrics.last_stats_ts_ms = 0
			state.active_metrics.last_orderbook_ts_ms = 0
			services.settings_set(&state.settings, services.SETTING_AUTO_CONNECT, "0")
			services.settings_flush(&state.settings)
			show_toast(state, "Disconnected")
		case .Set_Cell_Widget:
			ci := action.cell_idx
			if ci >= 0 && ci < state.world.count {
				state.world.widgets[ci].kind = action.widget_kind
				persist_layout_v4(state)
				reconcile_subscriptions(state)
			}
		case .Set_Cell_Stream:
			ci := action.cell_idx
			if ci >= 0 && ci < state.world.count {
				bind := &state.world.bindings[ci]
				old_stream_idx := bind.stream_idx
				old_has_binding := binding_has(bind)
				// PRD-0009: if action carries venue/symbol, set binding and reset stream_idx for lazy resolution.
				if len(action.bind_venue) > 0 && len(action.bind_symbol) > 0 {
					binding_set(bind, action.bind_venue, action.bind_symbol)
					bind.stream_idx = -1
				} else if action.stream_idx < 0 {
					// "Follow Active" — clear binding.
					binding_clear(bind)
					bind.stream_idx = -1
				} else {
					bind.stream_idx = action.stream_idx
				}
				// Reset DOM/footprint stores when stream actually changes.
				stream_changed := bind.stream_idx != old_stream_idx || (len(action.bind_venue) > 0) || old_has_binding
				if stream_changed {
					services.dom_store_reset(&state.stores.dom)
					services.footprint_store_reset(&state.stores.footprint)
				}
				state.world.getranges[ci].pending = false
				state.world.getranges[ci].seeded = false
				state.world.getranges[ci].oldest_ts = 0
				persist_layout_v4(state)
				reconcile_subscriptions(state)
				request_cell_candle_range(state, ci)
			}
		case .Add_Cell:
			if state.world.count < CELL_MAX {
				ci := state.world.count
				init_world_cell_defaults(state, ci, action.widget_kind, action.stream_idx)
				// PRD-0009: if action carries venue/symbol, set binding.
				if len(action.bind_venue) > 0 && len(action.bind_symbol) > 0 {
					binding_set(&state.world.bindings[ci], action.bind_venue, action.bind_symbol)
					state.world.bindings[ci].stream_idx = -1
				}
				state.world.count += 1
				persist_layout_v4(state)
				reconcile_subscriptions(state)
				request_cell_candle_range(state, ci)
			} else {
				show_toast(state, "Max 12 cells")
			}
		case .Remove_Cell:
			ci := action.cell_idx
			if ci >= 0 && ci < state.world.count && state.world.count > 1 {
				// BUG-15: Adjust focused cell index before compacting.
				if state.world.focused == ci {
					state.world.focused = -1
				} else if state.world.focused > ci {
					state.world.focused -= 1
				}
				for j in ci ..< state.world.count - 1 {
					state.world.widgets[j]    = state.world.widgets[j + 1]
					state.world.bindings[j]   = state.world.bindings[j + 1]
					state.world.views[j]      = state.world.views[j + 1]
					state.world.indicators[j] = state.world.indicators[j + 1]
					state.world.ind_params[j] = state.world.ind_params[j + 1]
					state.world.charts[j]     = state.world.charts[j + 1]
					state.world.subplots[j]   = state.world.subplots[j + 1]
					state.world.spans[j]      = state.world.spans[j + 1]
					state.world.timeframes[j] = state.world.timeframes[j + 1]
					state.world.getranges[j]  = state.world.getranges[j + 1]
				}
				state.world.count -= 1
				last := state.world.count
				state.world.widgets[last]    = {}
				state.world.bindings[last]   = {}
				state.world.views[last]      = {}
				state.world.indicators[last] = {}
				state.world.ind_params[last] = {}
				state.world.charts[last]     = {}
				state.world.subplots[last]   = {}
				state.world.spans[last]      = {}
				state.world.timeframes[last] = {}
				state.world.getranges[last]  = {}
				persist_layout_v4(state)
				reconcile_subscriptions(state)
			}
		case .Toggle_Focus_Mode:
			state.focus_mode = !state.focus_mode
			show_toast(state, state.focus_mode ? "Focus Mode" : "Normal Mode")
		case .Toggle_Stream_Picker:
			state.overlays.show_stream_picker = !state.overlays.show_stream_picker
		case .Pick_Stream:
			sid := action.subject_id
			if sid != 0 {
				reg := state.stream_views
				if reg != nil {
					reg.active_subject_id = sid
					reg.has_active = true
					sync_active_stream_view_to_global_stores(state)
					persist_active_stream_subject(state)
						state.active_metrics.has_live_stats = false
						state.active_metrics.has_live_heatmap = false
						state.active_metrics.has_live_vpvr = false
						state.active_metrics.has_live_candle = false
						state.active_metrics.context_stage = .Empty
						state.active_metrics.last_stats_ts_ms = 0
					state.active_metrics.last_orderbook_ts_ms = 0
					state.getrange.pending = false
					state.getrange.seeded = false
					state.getrange.subject_id = 0
					state.getrange.oldest_ts = 0
					ensure_active_candle_subject_id(state)
					state.candle_health = .No_Data
					state.stream_switches_total += 1
					if now_ms := current_now_ms(state); now_ms > 0 {
						state.candle_last_recv_local_ms = now_ms
					}
					if state.stores.candle.count <= 0 {
						request_active_stream_candle_range(state)
					}
				}
			}
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
			mi := action.market_entry_idx
			if mi >= 0 && mi < state.stores.markets.count {
				entry := state.stores.markets.entries[mi]
				venue := normalized_venue(entry.venue)
				symbol := entry.ticker
				symbol_buf: [80]u8
				if len(entry.market_type) > 0 && !strings.contains(symbol, ":") {
					symbol = fmt.bprintf(symbol_buf[:], "%s:%s", symbol, entry.market_type)
				}
				if state.marketdata.subscribe != nil {
					state.marketdata.subscribe(venue, symbol, .Trades)
					state.marketdata.subscribe(venue, symbol, .Orderbook)
					state.marketdata.subscribe(venue, symbol, .Candles)
					state.marketdata.subscribe(venue, symbol, .Stats)
					state.marketdata.subscribe(venue, symbol, .Heatmaps)
					state.marketdata.subscribe(venue, symbol, .VPVR)
					state.marketdata.subscribe(venue, symbol, .Evidence)
					state.marketdata.subscribe(venue, symbol, .Signals)
				}
				sub_buf: [64]u8
				show_toast(state, fmt.bprintf(sub_buf[:], "%s:%s", venue, symbol))
			}
			state.overlays.show_stream_picker = false
		case .Unsubscribe_Market:
			mi := action.market_entry_idx
			if mi >= 0 && mi < state.stores.markets.count {
				entry := state.stores.markets.entries[mi]
				venue := normalized_venue(entry.venue)
				symbol := entry.ticker
				symbol_buf: [80]u8
				if len(entry.market_type) > 0 && !strings.contains(symbol, ":") {
					symbol = fmt.bprintf(symbol_buf[:], "%s:%s", symbol, entry.market_type)
				}
				if state.marketdata.unsubscribe != nil {
					state.marketdata.unsubscribe(venue, symbol, .Trades)
					state.marketdata.unsubscribe(venue, symbol, .Orderbook)
					state.marketdata.unsubscribe(venue, symbol, .Candles)
					state.marketdata.unsubscribe(venue, symbol, .Stats)
					state.marketdata.unsubscribe(venue, symbol, .Heatmaps)
					state.marketdata.unsubscribe(venue, symbol, .VPVR)
					state.marketdata.unsubscribe(venue, symbol, .Evidence)
					state.marketdata.unsubscribe(venue, symbol, .Signals)
				}
				sub_buf: [64]u8
				show_toast(state, fmt.bprintf(sub_buf[:], "Unsub %s:%s", venue, symbol))
			}
		case .Toggle_Widget_Catalog:
			state.overlays.show_widget_catalog = !state.overlays.show_widget_catalog
			state.overlays.catalog_step = 0
		case .Open_Cell_Stream_Picker:
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
		case .Resync_Active_Stream:
			current_ack_metric := 0
			if state.marketdata.metrics != nil {
				metrics: ports.MD_Runtime_Metrics
				if state.marketdata.metrics(&metrics) {
					current_ack_metric = max(metrics.subscribe_ack_count, 0)
				}
			}
			state.active_metrics.last_ack_metric = current_ack_metric
			state.active_metrics.subscribe_acks = 0
			state.active_metrics.has_live_stats = false
			state.active_metrics.has_live_heatmap = false
			state.active_metrics.has_live_vpvr = false
			state.active_metrics.has_live_candle = false
			state.active_metrics.context_stage = .Empty
			state.active_metrics.last_stats_ts_ms = 0
			state.active_metrics.last_orderbook_ts_ms = 0
			state.synth_heatmap_last_window = 0
			state.getrange.pending = false
			state.getrange.seeded = false
			state.getrange.subject_id = 0
			state.getrange.oldest_ts = 0
			ensure_active_candle_subject_id(state)
			if now_ms := current_now_ms(state); now_ms > 0 {
				state.candle_last_recv_local_ms = now_ms
			}
			if active := streams.registry_active(&state.stream_registry); active != nil {
				streams.controller_mark_desync(&active.status, .Manual)
				active.status.last_snapshot_ts_ms = 0
				active.status.last_local_ts_ms = 0
				active.status.last_server_ts_ms = 0
				active.status.last_message_age_ms = 0
				active.status.last_seq = 0
				active.status.subscribe_acks = 0
			}
			state.active_metrics.state = .Desync
			state.active_metrics.desync_reason = .Manual
			state.prev_subs_count = 0 // force full re-subscribe path
			reconcile_subscriptions(state)
			request_active_stream_candle_range(state)
			show_toast(state, "Resync requested")
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
	state.compare.slots[0] = reg.active_subject_id
	for i in 0 ..< len(state.compare.show_vol) {
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
		return
	}
}
