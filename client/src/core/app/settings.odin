package app

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:ui"

// App-level config (non-visual). Visual tokens live in ui/styles.odin.

MAX_VISIBLE_BARS         :: 600
FETCH_CANDLES_RANGE_LEN  :: 750
FETCH_HEATMAPS_RANGE_LEN :: 200
STATUS_BAR_HEIGHT        :: 30

SETTINGS_ROW_H      :: f32(28)
SETTINGS_SECTION_GAP :: f32(16)
SETTINGS_PAD_X       :: f32(16)

// Full settings page with General, Theme sections. Connection moved to modal overlay.
build_settings_page :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	// Background.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = workspace, color = ui.COL_SURFACE_1})

	x := workspace.pos.x + SETTINGS_PAD_X
	y := workspace.pos.y + 24
	content_w := workspace.size.x - SETTINGS_PAD_X * 2
	if content_w < 100 do content_w = 100

	// Page title.
	ui.push_text(&state.cmd_buf, {x, y}, "Settings",
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_LG, .Bold)
	y += 32

	// ═══════════════════════════════════════════════════════════
	// Section: Connection
	// ═══════════════════════════════════════════════════════════
	ui.push_text(&state.cmd_buf, {x, y}, "CONNECTION",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	toggle_w := f32(160)
	toggle_h := f32(22)

	// Server URL (read-only label).
	conn_url_str := "Not configured"
	url_buf: [64]u8
	if profile := services.profile_store_active(&state.profiles); profile != nil {
		ws_url := services.profile_ws_url(profile)
		if len(ws_url) > 0 {
			conn_url_str = fmt.bprintf(url_buf[:], "URL: %s", ws_url[:min(len(ws_url), 48)])
		}
	}
	ui.push_text(&state.cmd_buf, {x + 4, y + toggle_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
		conn_url_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	y += toggle_h + 4

	// Status badge.
	conn_disp := current_conn_status_display(state)
	status_buf: [32]u8
	status_str := fmt.bprintf(status_buf[:], "Status: %s", conn_disp.label)
	ui.push_text(&state.cmd_buf, {x + 4, y + toggle_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
		status_str, conn_disp.text_color, ui.FONT_SIZE_XS, .Mono)
	y += toggle_h + 4

	// Transport + auth mode indicator (when connected).
	if current_conn_status(state) == .Connected {
		m: ports.MD_Runtime_Metrics
		if state.marketdata.metrics != nil && state.marketdata.metrics(&m) {
			transport_label := m.transport_mode == 0 ? "Terminal_V1" : "Legacy_JSON"
			auth_label := "none"
			if m.auth_mode == 1 do auth_label = "apikey"
			if m.auth_mode == 2 do auth_label = "jwt"
			ta_buf: [48]u8
			ta_str := fmt.bprintf(ta_buf[:], "Mode: %s  Auth: %s", transport_label, auth_label)
			ui.push_text(&state.cmd_buf, {x + 4, y + toggle_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				ta_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			y += toggle_h + 4
		}
	}

	// Auto-connect toggle.
	auto_val, _ := services.settings_get(&state.settings, services.SETTING_AUTO_CONNECT)
	auto_on := auto_val == "1"
	ac_res := ui.toggle(&state.cmd_buf,
		ui.rect_xywh(x, y, toggle_w, toggle_h),
		"  Auto-connect", auto_on,
		pointer, state.text.measure, ui.FONT_SIZE_XS)
	if ac_res.changed {
		services.settings_set(&state.settings, services.SETTING_AUTO_CONNECT, ac_res.value ? "1" : "0")
		services.settings_flush(&state.settings)
	}
	y += toggle_h + 6

	assist_res := ui.toggle(&state.cmd_buf,
		ui.rect_xywh(x, y, toggle_w, toggle_h),
		"  Assist mode (BP)", state.bp_assist.user_enabled,
		pointer, state.text.measure, ui.FONT_SIZE_XS)
	if assist_res.changed {
		state.bp_assist.user_enabled = assist_res.value
		if !state.bp_assist.user_enabled {
			state.bp_assist.enabled = false
			state.bp_assist.degrade_heatmap = false
			state.bp_assist.degrade_vpvr = false
			state.bp_assist.getrange_divisor = 1
			reconcile_subscriptions(state)
		}
		services.settings_set(&state.settings, services.SETTING_ASSIST_MODE, assist_res.value ? "1" : "0")
		services.settings_flush(&state.settings)
	}
	y += toggle_h + 6

	// Connect / Disconnect button.
	btn_w := f32(100)
	btn_h := f32(22)
	conn_status := current_conn_status(state)
	is_connected := conn_status == .Connected || conn_status == .Connecting || conn_status == .Reconnecting
	conn_btn_label := is_connected ? "Disconnect" : "Connect"
	conn_btn_res := ui.button(&state.cmd_buf,
		ui.rect_xywh(x, y, btn_w, btn_h),
		conn_btn_label, pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if conn_btn_res.clicked {
		if is_connected {
			queue_ui_action(state, UI_Action{kind = .Disconnect_Profile})
		} else {
			queue_ui_action(state, UI_Action{kind = .Connect_Profile, profile_idx = state.connection_manager_selected_profile})
		}
	}
	y += btn_h + SETTINGS_SECTION_GAP

	// ═══════════════════════════════════════════════════════════
	// Section: General
	// ═══════════════════════════════════════════════════════════
	ui.push_text(&state.cmd_buf, {x, y}, "GENERAL",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	// Toggle: Show candle volume.
	vol_res := ui.toggle(&state.cmd_buf,
		ui.rect_xywh(x, y, toggle_w, toggle_h),
		"  Show Candle Vol", state.chart_display.show_vol,
		pointer, state.text.measure, ui.FONT_SIZE_XS)
	if vol_res.changed {
		state.chart_display.show_vol = vol_res.value
		services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_VOL,
			state.chart_display.show_vol ? "1" : "0")
		services.settings_flush(&state.settings)
	}
	y += toggle_h + 6

	// Toggle: Candle heatmap overlay.
	hm_res := ui.toggle(&state.cmd_buf,
		ui.rect_xywh(x, y, toggle_w, toggle_h),
		"  Candle Heatmap", state.chart_display.show_heatmap,
		pointer, state.text.measure, ui.FONT_SIZE_XS)
	if hm_res.changed {
		state.chart_display.show_heatmap = hm_res.value
		services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_HEATMAP,
			state.chart_display.show_heatmap ? "1" : "0")
		services.settings_flush(&state.settings)
	}
	y += toggle_h + 6

	// Toggle: Candle VPVR overlay.
	vpvr_res := ui.toggle(&state.cmd_buf,
		ui.rect_xywh(x, y, toggle_w, toggle_h),
		"  Candle VPVR", state.chart_display.show_vpvr,
		pointer, state.text.measure, ui.FONT_SIZE_XS)
	if vpvr_res.changed {
		state.chart_display.show_vpvr = vpvr_res.value
		services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_VPVR,
			state.chart_display.show_vpvr ? "1" : "0")
		services.settings_flush(&state.settings)
	}
	y += toggle_h + 6

	// Heatmap intensity.
	intensity_label := "HM Intensity: "
	intensity_label_w := state.text.measure(ui.FONT_SIZE_XS, intensity_label).x
	ui.push_text(&state.cmd_buf, {x, y + toggle_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
		intensity_label, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	intensity_opts := [3]string{"LOW", "MID", "HIGH"}
	intensity_seg_res := ui.segmented_control(&state.cmd_buf,
		ui.rect_xywh(x + intensity_label_w + 4, y, f32(126), toggle_h),
		intensity_opts[:], state.chart_display.heatmap_intensity_idx,
		pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if intensity_seg_res.changed {
		state.chart_display.heatmap_intensity_idx = intensity_seg_res.index
		idx_buf: [4]u8
		services.settings_set(&state.settings, services.SETTING_CANDLE_HEATMAP_INTENSITY_IDX,
			fmt.bprintf(idx_buf[:], "%d", state.chart_display.heatmap_intensity_idx))
		services.settings_flush(&state.settings)
	}
	y += toggle_h + 6

	// Toggle: Detail panel default expanded.
	detail_res := ui.toggle(&state.cmd_buf,
		ui.rect_xywh(x, y, toggle_w, toggle_h),
		"  Detail Panel", state.chrome.detail_expanded,
		pointer, state.text.measure, ui.FONT_SIZE_XS)
	if detail_res.changed {
		state.chrome.detail_expanded = detail_res.value
		services.settings_set(&state.settings, services.SETTING_SIDEBAR_EXPANDED,
			state.chrome.detail_expanded ? "1" : "0")
		services.settings_flush(&state.settings)
	}
	y += toggle_h + SETTINGS_SECTION_GAP

	// ═══════════════════════════════════════════════════════════
	// Section: Indicators
	// ═══════════════════════════════════════════════════════════
	ui.push_text(&state.cmd_buf, {x, y}, "INDICATORS",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	Ind_Setting :: struct { label: string, key: string, ptr: ^bool }
	ind_settings := [8]Ind_Setting{
		{"  EMA / SMA",       services.SETTING_SHOW_MA,      &state.indicators.show_ma},
		{"  Bollinger Bands", services.SETTING_SHOW_BBANDS,  &state.indicators.show_bbands},
		{"  VWAP",            services.SETTING_SHOW_VWAP,    &state.indicators.show_vwap},
		{"  RSI",             services.SETTING_SHOW_RSI,     &state.indicators.show_rsi},
		{"  MACD",            services.SETTING_SHOW_MACD,    &state.indicators.show_macd},
		{"  Funding Rate",    services.SETTING_SHOW_FUNDING, &state.indicators.show_funding},
		{"  Liquidations",    services.SETTING_SHOW_LIQ,     &state.indicators.show_liq},
		{"  Trade Counter",   services.SETTING_SHOW_TRADE_COUNTER, &state.indicators.show_trade_counter},
	}
	for idx in 0 ..< len(ind_settings) {
		is := &ind_settings[idx]
		res := ui.toggle(&state.cmd_buf,
			ui.rect_xywh(x, y, toggle_w, toggle_h),
			is.label, is.ptr^,
			pointer, state.text.measure, ui.FONT_SIZE_XS)
		if res.changed {
			is.ptr^ = res.value
			services.settings_set(&state.settings, is.key, res.value ? "1" : "0")
			services.settings_flush(&state.settings)
			// Sync to focused candle cell (mirror toggle_focused_indicator pattern).
			fci := state.world.focused
			if fci >= 0 && fci < state.world.count && state.world.widgets[fci].kind == .Candle {
				set_indicator_on_cell(&state.world.indicators[fci], idx, res.value)
			}
		}
		y += toggle_h + 6
	}
	y += 6

	y += SETTINGS_SECTION_GAP

	// ═══════════════════════════════════════════════════════════
	// Section: Layout
	// ═══════════════════════════════════════════════════════════
	ui.push_text(&state.cmd_buf, {x, y}, "LAYOUT",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	layout_btn_w := f32(120)
	layout_btn_h := f32(22)
	export_res := ui.button(&state.cmd_buf,
		ui.rect_xywh(x, y, layout_btn_w, layout_btn_h),
		"Copy Layout", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if export_res.clicked {
		if layout_export_to_clipboard(state) {
			show_toast(state, "Layout copied")
		} else {
			show_toast(state, "Copy failed")
		}
	}
	y += layout_btn_h + SETTINGS_SECTION_GAP

	// ═══════════════════════════════════════════════════════════
	// Section: Theme
	// ═══════════════════════════════════════════════════════════
	ui.push_text(&state.cmd_buf, {x, y}, "THEME",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	ui.push_text(&state.cmd_buf, {x + 4, y + 10},
		"Dark (default)", ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
}

// Settings detail panel content (category list in sidebar).
draw_settings_detail :: proc(state: ^App_State, rect: ui.Rect) {
	y := rect.pos.y
	ui.push_text(&state.cmd_buf, {rect.pos.x + 2, y + 14}, "SETTINGS",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 24

	categories := [?]string{"General", "Theme"}
	for cat in categories {
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10},
			cat, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		y += 20
	}
}
