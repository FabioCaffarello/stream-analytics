package app

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:ui"

// Dashboard detail panel: structured sections with collapsible groups.
@(private = "package")
draw_dashboard_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	y := rect.pos.y

	// ===================================================
	// Section 1: Market Info (always visible)
	// ===================================================
	reg := state.stream_views
	active_name := "---"
	if reg != nil && reg.has_active {
		if slot := stream_view_active_slot(reg); slot != nil {
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			if slot.has_stream_info {
				info := slot.stream_info
				if len(info.venue) > 0 && len(info.symbol) > 0 {
					name_buf: [64]u8
					active_name = fmt.bprintf(name_buf[:], "%s:%s", info.venue, info.symbol)
				}
			}
		}
	}

	// Venue:Symbol (bold).
	ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 14},
		active_name, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_SM, .Bold)
	y += 20

	// Connection dot + stream count.
	conn_status: ports.MD_Conn_Status = .Offline
	if state.marketdata.conn_status != nil {
		conn_status = state.marketdata.conn_status()
	}
	dot_color := conn_status == .Connected ? ui.COL_GREEN : ui.with_alpha(ui.COL_WHITE, 0.35)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {rect.pos.x + 4, y + 4}, size = {6, 6}},
		color = dot_color,
	})
	status_label := conn_status == .Connected ? "LIVE" : "OFFLINE"
	ui.push_text(&state.cmd_buf, {rect.pos.x + 14, y + 10},
		status_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	y += 16

	// TF label.
	tf_opts := TF_OPTIONS
	tf_buf: [16]u8
	tf_str := fmt.bprintf(tf_buf[:], "TF: %s", tf_opts[state.active_tf_idx])
	ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10},
		tf_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	y += 20

	// ===================================================
	// Section 2: Streams (collapsible, with price + click-to-switch)
	// ===================================================
	remaining_h := (rect.pos.y + rect.size.y) - y
	stream_hdr_buf: [24]u8
	stream_count := 0
	if reg != nil { stream_count = reg.count }
	stream_hdr_label := fmt.bprintf(stream_hdr_buf[:], "STREAMS (%d)", stream_count)
	stream_sec := ui.collapsible_section(&state.cmd_buf,
		ui.Rect{pos = {rect.pos.x, y}, size = {rect.size.x, remaining_h}},
		stream_hdr_label, &state.chrome.section_streams, pointer, state.text.measure)
	y += 22 // header height
	if state.chrome.section_streams.expanded && reg != nil && reg.count > 0 {
		item_h := f32(24)
		for i in 0 ..< STREAM_VIEW_CAP {
			if !reg.slots[i].used do continue
			slot := &reg.slots[i]
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			is_active := reg.has_active && slot.subject_id == reg.active_subject_id
			label := "---"
			if slot.has_stream_info && len(slot.stream_info.venue) > 0 {
				sl_buf: [64]u8
				label = fmt.bprintf(sl_buf[:], "%s:%s", slot.stream_info.venue, slot.stream_info.symbol)
			}

			item_rect := ui.Rect{pos = {rect.pos.x + 4, y}, size = {rect.size.x - 8, item_h}}
			item_hovered := ui.rect_contains(item_rect, pointer.pos)
			if is_active {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_BLUE, 0.15)})
			} else if item_hovered {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05)})
			}

			// Click to switch active stream.
			if item_hovered && pointer.left_pressed && !is_active {
				queue_ui_action(state, UI_Action{kind = .Pick_Stream, subject_id = slot.subject_id})
			}

			// Activity dot: green if seen within last 120 frames (~2s), dim otherwise.
			fresh := state.frame > 0 && slot.last_seen_frame > 0 && (state.frame - slot.last_seen_frame) < 120
			dot_color := fresh ? ui.COL_GREEN : ui.with_alpha(ui.COL_WHITE, 0.2)
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect = {pos = {item_rect.pos.x + 2, y + 6}, size = {5, 5}},
				color = dot_color,
			})

			// Venue:Symbol label.
			text_color := is_active ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY
			ui.push_text(&state.cmd_buf, {item_rect.pos.x + 10, y + 10},
				label, text_color, ui.FONT_SIZE_XS, .Mono)

			// Latest price + change from candle store.
			latest := services.get_candle_newest(&slot.candle_store, 0)
			if latest.close > 0 {
				price_buf: [20]u8
				price_str := fmt.bprintf(price_buf[:], "%.2f", latest.close)
				price_w := state.text.measure(ui.FONT_SIZE_XS, price_str).x
				ui.push_text(&state.cmd_buf,
					{item_rect.pos.x + item_rect.size.x - price_w - 2, y + 10},
					price_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

				// Percent change (close vs open of latest candle).
				if latest.open > 0 {
					pct := (latest.close - latest.open) / latest.open * 100
					pct_buf: [12]u8
					pct_str := pct >= 0 ? fmt.bprintf(pct_buf[:], "+%.2f%%", pct) : fmt.bprintf(pct_buf[:], "%.2f%%", pct)
					pct_color := pct >= 0 ? ui.COL_GREEN : ui.COL_RED
					pct_w := state.text.measure(ui.FONT_SIZE_XS, pct_str).x
					ui.push_text(&state.cmd_buf,
						{item_rect.pos.x + item_rect.size.x - pct_w - 2, y + 20},
						pct_str, pct_color, ui.FONT_SIZE_XS, .Mono)
				}
			}

			y += item_h
		}
		y += 4
	}

	// ===================================================
	// Section 3: Chart Layers (collapsible)
	// ===================================================
	remaining_h = (rect.pos.y + rect.size.y) - y
	if remaining_h > 22 {
		ui.collapsible_section(&state.cmd_buf,
			ui.Rect{pos = {rect.pos.x, y}, size = {rect.size.x, remaining_h}},
			"LAYERS", &state.chrome.section_layers, pointer, state.text.measure)
		y += 22
		if state.chrome.section_layers.expanded {
			toggle_w := rect.size.x - 8
			toggle_h := f32(20)
			tx := rect.pos.x + 4

			LAYER_TOGGLES :: [11]struct{label: string, field_offset: int}{
				{label = "  Volume",    field_offset = 0},
				{label = "  Heatmap",   field_offset = 1},
				{label = "  VPVR",      field_offset = 2},
				{label = "  EMA/SMA",   field_offset = 3},
				{label = "  Bollinger", field_offset = 4},
				{label = "  VWAP",      field_offset = 5},
				{label = "  RSI",       field_offset = 6},
				{label = "  MACD",      field_offset = 7},
				{label = "  Funding",   field_offset = 8},
				{label = "  Liq Vol",   field_offset = 9},
				{label = "  Trade Cnt", field_offset = 10},
			}
			// Point toggles at focused candle cell when available, else global.
			fci := state.world.focused
			has_focus := fci >= 0 && fci < state.world.count && state.world.widgets[fci].kind == .Candle
			layer_ptrs: [11]^bool
			if has_focus {
				fc_ch := &state.world.charts[fci]
				fc_ind := &state.world.indicators[fci]
				layer_ptrs = {
					&fc_ch.show_vol, &fc_ch.show_heatmap, &fc_ch.show_vpvr,
					&fc_ind.show_ma, &fc_ind.show_bbands, &fc_ind.show_vwap,
					&fc_ind.show_rsi, &fc_ind.show_macd, &fc_ind.show_funding,
					&fc_ind.show_liq, &fc_ind.show_trade_counter,
				}
			} else {
				layer_ptrs = {
					&state.chart_display.show_vol, &state.chart_display.show_heatmap, &state.chart_display.show_vpvr,
					&state.indicators.show_ma, &state.indicators.show_bbands, &state.indicators.show_vwap,
					&state.indicators.show_rsi, &state.indicators.show_macd, &state.indicators.show_funding,
					&state.indicators.show_liq, &state.indicators.show_trade_counter,
				}
			}
			layer_labels := LAYER_TOGGLES
			step_h := f32(16)
			for li in 0 ..< 11 {
				if y + toggle_h > rect.pos.y + rect.size.y do break
				tr := ui.toggle(&state.cmd_buf,
					ui.rect_xywh(tx, y, toggle_w, toggle_h),
					layer_labels[li].label, layer_ptrs[li]^,
					pointer, state.text.measure, ui.FONT_SIZE_XS)
				if tr.changed {
					layer_ptrs[li]^ = tr.value
					// Sync to global defaults for persistence.
					sync_layer_to_global(state, li, tr.value)
					// Sync indicator state to focused candle cell (li 3-10 → indicator 0-7).
					if li >= 3 && fci >= 0 && fci < state.world.count && state.world.widgets[fci].kind == .Candle {
						set_indicator_on_cell(&state.world.indicators[fci], li - 3, tr.value)
					}
				}
				y += toggle_h + 2

				// Inline settings when layer is active.
				if !layer_ptrs[li]^ do continue
				sx := tx + 14
				sw := toggle_w - 18

				// Get parameter pointers (focused cell or global).
				fc_indp := &state.world.ind_params[fci]
				ma_per := has_focus ? &fc_indp.ma_periods : &state.indicators.ma_periods
				bb_per := has_focus ? &fc_indp.bb_period  : &state.indicators.bb_period
				bb_sig := has_focus ? &fc_indp.bb_sigma   : &state.indicators.bb_sigma
				rsi_per := has_focus ? &fc_indp.rsi_period : &state.indicators.rsi_period
				macd_f := has_focus ? &fc_indp.macd_fast   : &state.indicators.macd_fast
				macd_s := has_focus ? &fc_indp.macd_slow   : &state.indicators.macd_slow
				macd_sig := has_focus ? &fc_indp.macd_signal : &state.indicators.macd_signal

				switch li {
				case 3: // EMA/SMA
					ma_keys := [3]string{services.SETTING_MA_PERIOD_0, services.SETTING_MA_PERIOD_1, services.SETTING_MA_PERIOD_2}
					ma_labels := [3]string{"MA1", "MA2", "MA3"}
					for mi in 0 ..< 3 {
						if y + step_h > rect.pos.y + rect.size.y do break
						sr := ui.stepper(&state.cmd_buf, ui.rect_xywh(sx, y, sw, step_h),
							ma_labels[mi], ma_per[mi], 2, 200,
							pointer, state.text.measure)
						if sr.changed {
							ma_per[mi] = sr.value
							state.indicators.ma_periods[mi] = sr.value
							buf: [4]u8
							services.settings_set(&state.settings, ma_keys[mi], fmt.bprintf(buf[:], "%d", sr.value))
						}
						y += step_h + 1
					}
				case 4: // Bollinger
					if y + step_h > rect.pos.y + rect.size.y do continue
					sr := ui.stepper(&state.cmd_buf, ui.rect_xywh(sx, y, sw, step_h),
						"Per", bb_per^, 2, 200,
						pointer, state.text.measure)
					if sr.changed {
						bb_per^ = sr.value
						state.indicators.bb_period = sr.value
						buf: [4]u8
						services.settings_set(&state.settings, services.SETTING_BB_PERIOD, fmt.bprintf(buf[:], "%d", sr.value))
					}
					y += step_h + 1
					if y + step_h > rect.pos.y + rect.size.y do continue
					sf := ui.stepper_float(&state.cmd_buf, ui.rect_xywh(sx, y, sw, step_h),
						"Sig", bb_sig^, 0.5, 5.0, 0.5,
						pointer, state.text.measure)
					if sf.changed {
						bb_sig^ = sf.value
						state.indicators.bb_sigma = sf.value
						buf: [4]u8
						services.settings_set(&state.settings, services.SETTING_BB_SIGMA, fmt.bprintf(buf[:], "%.1f", sf.value))
					}
					y += step_h + 1
				case 6: // RSI
					if y + step_h > rect.pos.y + rect.size.y do continue
					sr := ui.stepper(&state.cmd_buf, ui.rect_xywh(sx, y, sw, step_h),
						"Per", rsi_per^, 2, 100,
						pointer, state.text.measure)
					if sr.changed {
						rsi_per^ = sr.value
						state.indicators.rsi_period = sr.value
						buf: [4]u8
						services.settings_set(&state.settings, services.SETTING_RSI_PERIOD, fmt.bprintf(buf[:], "%d", sr.value))
					}
					y += step_h + 1
				case 7: // MACD
					macd_params := [3]struct{label: string, ptr: ^int, global_ptr: ^int, key: string, lo, hi: int}{
						{"Fst", macd_f, &state.indicators.macd_fast, services.SETTING_MACD_FAST, 2, 100},
						{"Slw", macd_s, &state.indicators.macd_slow, services.SETTING_MACD_SLOW, 2, 200},
						{"Sig", macd_sig, &state.indicators.macd_signal, services.SETTING_MACD_SIGNAL, 2, 100},
					}
					for &mp in macd_params {
						if y + step_h > rect.pos.y + rect.size.y do break
						sr := ui.stepper(&state.cmd_buf, ui.rect_xywh(sx, y, sw, step_h),
							mp.label, mp.ptr^, mp.lo, mp.hi,
							pointer, state.text.measure)
						if sr.changed {
							mp.ptr^ = sr.value
							mp.global_ptr^ = sr.value
							buf: [4]u8
							services.settings_set(&state.settings, mp.key, fmt.bprintf(buf[:], "%d", sr.value))
						}
						y += step_h + 1
					}
				}
			}
			y += 4
		}
	}

	// ===================================================
	// Section 4: Panel Layout (collapsible)
	// ===================================================
	remaining_h = (rect.pos.y + rect.size.y) - y
	if remaining_h > 22 {
		ui.collapsible_section(&state.cmd_buf,
			ui.Rect{pos = {rect.pos.x, y}, size = {rect.size.x, remaining_h}},
			"PANELS", &state.chrome.section_panels, pointer, state.text.measure)
		y += 22
		if state.chrome.section_panels.expanded {
			// Layout preset selector.
			if y + 18 < rect.pos.y + rect.size.y {
				preset_labels := ui.LAYOUT_PRESET_LABELS
				seg_w := min(rect.size.x - 8, f32(180))
				seg_rect := ui.rect_xywh(rect.pos.x + 4, y, seg_w, f32(16))
				seg_res := ui.segmented_control(&state.cmd_buf, seg_rect,
					preset_labels[:], state.layout_preset,
					pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
				if seg_res.changed {
					queue_ui_action(state, UI_Action{kind = .Set_Layout_Preset, layout_preset = seg_res.index})
				}
				y += 22
			}

			// Custom preset slots (C1-C4): click to load, shift+click to save.
			if y + 22 < rect.pos.y + rect.size.y {
				ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10},
					"Custom:", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
				cp_x := rect.pos.x + 4 + state.text.measure(ui.FONT_SIZE_XS, "Custom:").x + 6
				cp_btn_w := f32(24)
				cp_btn_h := f32(16)
				CP_LABELS :: [4]string{"C1", "C2", "C3", "C4"}
				cp_labels := CP_LABELS
				for ci in 0 ..< 4 {
					valid := custom_preset_valid(state, ci)
					btn_rect := ui.rect_xywh(cp_x + f32(ci) * (cp_btn_w + 3), y, cp_btn_w, cp_btn_h)
					btn_res := ui.button(&state.cmd_buf, btn_rect, cp_labels[ci], pointer,
						state.text.measure, ui.FONT_SIZE_XS, .Mono, true)
					if valid {
						// Blue dot indicator for saved presets.
						ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
							rect = {pos = {btn_rect.pos.x + btn_rect.size.x - 5, btn_rect.pos.y + 2}, size = {3, 3}},
							color = ui.COL_BLUE,
						})
					}
					if btn_res.clicked {
						if valid {
							load_custom_preset(state, ci)
						} else {
							save_custom_preset(state, ci)
						}
					}
				}
				// Save button.
				save_x := cp_x + 4 * (cp_btn_w + 3) + 4
				save_w := f32(32)
				save_res := ui.button(&state.cmd_buf,
					ui.rect_xywh(save_x, y, save_w, cp_btn_h),
					"Sav", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
				if save_res.clicked {
					// Save to first empty slot, or overwrite last.
					saved := false
					for ci in 0 ..< 4 {
						if !custom_preset_valid(state, ci) {
							save_custom_preset(state, ci)
							saved = true
							break
						}
					}
					if !saved {
						save_custom_preset(state, 3)
					}
				}
				y += 22
			}

			toggle_rect := ui.Rect{
				pos  = {rect.pos.x, y},
				size = {rect.size.x, rect.pos.y + rect.size.y - y},
			}
			sidebar_res := ui.draw_sidebar(&state.cmd_buf, toggle_rect, &state.chrome.sidebar, pointer, state.text.measure)
			if sidebar_res.toggled_panel >= 0 {
				queue_ui_action(state, UI_Action{kind = .Toggle_Panel, panel_idx = sidebar_res.toggled_panel})
			}
		}
	}
}
