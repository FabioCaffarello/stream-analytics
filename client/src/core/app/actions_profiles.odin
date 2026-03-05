package app

import "core:fmt"
import "mr:services"
import "mr:streams"

apply_select_profile_action :: proc(state: ^App_State, profile_idx: int) {
	if state == nil do return
	if services.profile_store_set_active(&state.profiles, profile_idx) {
		state.connection_manager_selected_profile = profile_idx
		services.profile_store_save(&state.profiles, &state.settings)
		services.settings_flush(&state.settings)
	}
}

apply_add_profile_action :: proc(state: ^App_State) {
	if state == nil do return
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
}

apply_remove_profile_action :: proc(state: ^App_State, profile_idx: int) {
	if state == nil do return
	if services.profile_store_remove(&state.profiles, profile_idx) {
		state.connection_manager_selected_profile = clamp(state.connection_manager_selected_profile, 0, max(state.profiles.count - 1, 0))
		services.profile_store_save(&state.profiles, &state.settings)
		services.settings_flush(&state.settings)
	}
}

apply_apply_profile_action :: proc(state: ^App_State, profile_idx: int) {
	if state == nil do return
	if services.profile_store_set_active(&state.profiles, profile_idx) {
		state.connection_manager_selected_profile = profile_idx
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
}

apply_connect_profile_action :: proc(state: ^App_State, profile_idx: int) {
	if state == nil do return
	if services.profile_store_set_active(&state.profiles, profile_idx) {
		if profile := services.profile_store_active(&state.profiles); profile != nil {
			ws_url := services.profile_ws_url(profile)
			api_key := services.profile_api_key_ref(profile)
			jwt_token := services.profile_jwt_token(profile)
			if state.marketdata.reconnect_transport != nil {
				_ = state.marketdata.reconnect_transport(ws_url, api_key, jwt_token)
			}
			state.connection_manager_selected_profile = profile_idx
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
}

apply_disconnect_profile_action :: proc(state: ^App_State) {
	if state == nil do return
	if state.marketdata.disconnect_transport != nil {
		_ = state.marketdata.disconnect_transport()
	}
	reset_active_stream_live_metrics(state)
	services.settings_set(&state.settings, services.SETTING_AUTO_CONNECT, "0")
	services.settings_flush(&state.settings)
	show_toast(state, "Disconnected")
}
