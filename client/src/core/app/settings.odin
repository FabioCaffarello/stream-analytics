package app

import "core:fmt"
import "mr:services"
import "mr:ui"

// App-level config (non-visual). Visual tokens live in ui/styles.odin.

MAX_VISIBLE_BARS         :: 600
FETCH_CANDLES_RANGE_LEN  :: 750
FETCH_HEATMAPS_RANGE_LEN :: 200
CANDLE_WIDTH_PCT         :: 0.4
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
	// Section: General
	// ═══════════════════════════════════════════════════════════
	ui.push_text(&state.cmd_buf, {x, y}, "GENERAL",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	// Toggle: Show candle volume.
	toggle_w := f32(160)
	toggle_h := f32(22)
	vol_res := ui.toggle(&state.cmd_buf,
		ui.rect_xywh(x, y, toggle_w, toggle_h),
		"  Show Candle Vol", state.show_candle_vol,
		pointer, state.text.measure, ui.FONT_SIZE_XS)
	if vol_res.changed {
		state.show_candle_vol = vol_res.value
		services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_VOL,
			state.show_candle_vol ? "1" : "0")
		services.settings_flush(&state.settings)
	}
	y += toggle_h + 6

	// Toggle: Candle heatmap overlay.
	hm_res := ui.toggle(&state.cmd_buf,
		ui.rect_xywh(x, y, toggle_w, toggle_h),
		"  Candle Heatmap", state.show_candle_heatmap,
		pointer, state.text.measure, ui.FONT_SIZE_XS)
	if hm_res.changed {
		state.show_candle_heatmap = hm_res.value
		services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_HEATMAP,
			state.show_candle_heatmap ? "1" : "0")
		services.settings_flush(&state.settings)
	}
	y += toggle_h + 6

	// Toggle: Candle VPVR overlay.
	vpvr_res := ui.toggle(&state.cmd_buf,
		ui.rect_xywh(x, y, toggle_w, toggle_h),
		"  Candle VPVR", state.show_candle_vpvr,
		pointer, state.text.measure, ui.FONT_SIZE_XS)
	if vpvr_res.changed {
		state.show_candle_vpvr = vpvr_res.value
		services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_VPVR,
			state.show_candle_vpvr ? "1" : "0")
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
		intensity_opts[:], state.candle_heatmap_intensity_idx,
		pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if intensity_seg_res.changed {
		state.candle_heatmap_intensity_idx = intensity_seg_res.index
		idx_buf: [4]u8
		services.settings_set(&state.settings, services.SETTING_CANDLE_HEATMAP_INTENSITY_IDX,
			fmt.bprintf(idx_buf[:], "%d", state.candle_heatmap_intensity_idx))
		services.settings_flush(&state.settings)
	}
	y += toggle_h + 6

	// Toggle: Detail panel default expanded.
	detail_res := ui.toggle(&state.cmd_buf,
		ui.rect_xywh(x, y, toggle_w, toggle_h),
		"  Detail Panel", state.detail_panel_expanded,
		pointer, state.text.measure, ui.FONT_SIZE_XS)
	if detail_res.changed {
		state.detail_panel_expanded = detail_res.value
		services.settings_set(&state.settings, services.SETTING_SIDEBAR_EXPANDED,
			state.detail_panel_expanded ? "1" : "0")
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
		{"  EMA / SMA",       services.SETTING_SHOW_MA,      &state.show_ma},
		{"  Bollinger Bands", services.SETTING_SHOW_BBANDS,  &state.show_bbands},
		{"  VWAP",            services.SETTING_SHOW_VWAP,    &state.show_vwap},
		{"  RSI",             services.SETTING_SHOW_RSI,     &state.show_rsi},
		{"  MACD",            services.SETTING_SHOW_MACD,    &state.show_macd},
		{"  Funding Rate",    services.SETTING_SHOW_FUNDING, &state.show_funding},
		{"  Liquidations",    services.SETTING_SHOW_LIQ,     &state.show_liq},
		{"  Trade Counter",   services.SETTING_SHOW_TRADE_COUNTER, &state.show_trade_counter},
	}
	for &is in ind_settings {
		res := ui.toggle(&state.cmd_buf,
			ui.rect_xywh(x, y, toggle_w, toggle_h),
			is.label, is.ptr^,
			pointer, state.text.measure, ui.FONT_SIZE_XS)
		if res.changed {
			is.ptr^ = res.value
			services.settings_set(&state.settings, is.key, res.value ? "1" : "0")
			services.settings_flush(&state.settings)
		}
		y += toggle_h + 6
	}
	y += 6

	// OB Grouping index.
	ob_label := "OB Group: "
	ob_label_w := state.text.measure(ui.FONT_SIZE_XS, ob_label).x
	ui.push_text(&state.cmd_buf, {x, y + toggle_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
		ob_label, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

	ob_opts := [5]string{"0.1x", "1x", "10x", "100x", "1000x"}
	seg_w := f32(140)
	ob_seg_res := ui.segmented_control(&state.cmd_buf,
		ui.rect_xywh(x + ob_label_w + 4, y, seg_w, toggle_h),
		ob_opts[:], state.ob_group_idx,
		pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if ob_seg_res.changed {
		state.ob_group_idx = ob_seg_res.index
		idx_buf: [4]u8
		services.settings_set(&state.settings, services.SETTING_OB_GROUP_IDX,
			fmt.bprintf(idx_buf[:], "%d", state.ob_group_idx))
		services.settings_flush(&state.settings)
	}
	y += toggle_h + SETTINGS_SECTION_GAP

	// ═══════════════════════════════════════════════════════════
	// Section: Theme
	// ═══════════════════════════════════════════════════════════
	ui.push_text(&state.cmd_buf, {x, y}, "THEME",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	ui.push_text(&state.cmd_buf, {x + 4, y + 10},
		"Dark (default)", ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
}

// Markets page — live stream list with last price and quick-switch.
build_markets_page :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = workspace, color = ui.COL_SURFACE_1})

	x := workspace.pos.x + SETTINGS_PAD_X
	y := workspace.pos.y + 24
	content_w := workspace.size.x - SETTINGS_PAD_X * 2
	if content_w < 100 do content_w = 100

	ui.push_text(&state.cmd_buf, {x, y}, "Markets",
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_LG, .Bold)
	y += 32

	reg := state.stream_views
	if reg == nil || reg.count == 0 {
		ui.push_text(&state.cmd_buf, {x, y},
			"No streams connected — waiting for market data",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}

	// Column headers.
	col_venue_w := f32(120)
	col_price_w := f32(100)
	col_change_w := f32(80)
	hdr_color := ui.with_alpha(ui.COL_WHITE, 0.5)
	ui.push_text(&state.cmd_buf, {x, y + 10}, "Venue:Symbol", hdr_color, ui.FONT_SIZE_XS, .Mono)
	ui.push_text(&state.cmd_buf, {x + col_venue_w, y + 10}, "Last Price", hdr_color, ui.FONT_SIZE_XS, .Mono)
	ui.push_text(&state.cmd_buf, {x + col_venue_w + col_price_w, y + 10}, "Change", hdr_color, ui.FONT_SIZE_XS, .Mono)
	y += 22
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x, y}, to = {x + content_w, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 4

	row_h := f32(28)
	for i in 0 ..< STREAM_VIEW_CAP {
		if !reg.slots[i].used do continue
		if y + row_h > workspace.pos.y + workspace.size.y do break

		slot := &reg.slots[i]
		if !slot.has_stream_info {
			refresh_stream_info_for_slot(state, slot)
		}
		is_active := reg.has_active && slot.subject_id == reg.active_subject_id
		row_rect := ui.rect_xywh(x - 4, y, content_w + 8, row_h)
		hovered := ui.rect_contains(row_rect, pointer.pos)

		if is_active {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect = row_rect, color = ui.with_alpha(ui.COL_BLUE, 0.12),
			})
		} else if hovered {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect = row_rect, color = ui.with_alpha(ui.COL_WHITE, 0.04),
			})
		}

		// Click to switch.
		if hovered && pointer.left_pressed && !is_active {
			queue_ui_action(state, UI_Action{kind = .Pick_Stream, subject_id = slot.subject_id})
		}

		text_y := y + row_h * 0.5 + ui.FONT_SIZE_XS * 0.35
		label := "---"
		sl_buf: [64]u8
		if slot.has_stream_info && len(slot.stream_info.venue) > 0 {
			label = fmt.bprintf(sl_buf[:], "%s:%s", slot.stream_info.venue, slot.stream_info.symbol)
		}
		text_color := is_active ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY
		ui.push_text(&state.cmd_buf, {x, text_y}, label, text_color, ui.FONT_SIZE_XS, .Mono)

		// Last price from candle store.
		if slot.candle_store.count > 0 {
			c := services.get_candle_newest(&slot.candle_store, 0)
			decs := ui.auto_price_decimals(c.close)
			pp_buf: [24]u8
			price_str := ui.format_price(pp_buf[:], c.close, decs)
			bullish := c.close >= c.open
			price_color := bullish ? ui.COL_GREEN : ui.COL_RED
			ui.push_text(&state.cmd_buf, {x + col_venue_w, text_y}, price_str,
				price_color, ui.FONT_SIZE_XS, .Mono)

			// Change %.
			if c.open > 0 {
				change_pct := (c.close - c.open) / c.open * 100.0
				sign := change_pct >= 0 ? "+" : ""
				pct_buf: [16]u8
				pct_str := fmt.bprintf(pct_buf[:], "%s%.2f%%", sign, change_pct)
				pct_color := change_pct >= 0 ? ui.COL_GREEN : ui.COL_RED
				ui.push_text(&state.cmd_buf, {x + col_venue_w + col_price_w, text_y},
					pct_str, pct_color, ui.FONT_SIZE_XS, .Mono)
			}
		}

		// Active dot.
		if is_active {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect = {pos = {x + content_w - 4, y + row_h * 0.5 - 3}, size = {6, 6}},
				color = ui.COL_GREEN,
			})
		}

		y += row_h
	}
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
