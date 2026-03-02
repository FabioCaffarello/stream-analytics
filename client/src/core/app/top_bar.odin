package app

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:ui"

draw_top_bar :: proc(state: ^App_State, input: ports.Input_State, viewport_w: f32, compact: bool = false) {
	bar_w := viewport_w
	if bar_w <= 0 do bar_w = 800
	bar_h := compact ? TOP_BAR_H_COMPACT : TOP_BAR_H

	// Background.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, 0}, size = {bar_w, bar_h}},
		color = ui.COL_SURFACE_1,
	})

	pointer := ui.Pointer_Input{
		pos           = input.mouse.pos,
		left_down     = input.mouse.buttons[.Left],
		left_pressed  = input.mouse.pressed[.Left],
		left_released = input.mouse.released[.Left],
	}

	// ═══════════════════════════════════════════════════════════════
	// Single row (32px): Logo | hero+price | <> count | TF | DCAK+ | OK | MBVRIHJK | SFCZ? | LIVE
	// ═══════════════════════════════════════════════════════════════
	row := ui.Rect{pos = {0, 0}, size = {bar_w, bar_h}}
	r := ui.rect_pad_xy(row, 8, 0)
	btn_h := f32(16)
	btn_y := (bar_h - btn_h) * 0.5
	text_baseline := bar_h * 0.5 + ui.FONT_SIZE_SM * 0.35

	// --- Left section: Logo + hero + price ---
	cursor_x := r.pos.x

	// Compact "MR" logo in blue accent box.
	logo_text := "MR"
	logo_size := state.text.measure(ui.FONT_SIZE_SM, logo_text)
	logo_box_w := logo_size.x + 10
	logo_box_h := f32(18)
	logo_box_y := (bar_h - logo_box_h) * 0.5
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {cursor_x, logo_box_y}, size = {logo_box_w, logo_box_h}},
		color = ui.with_alpha(ui.COL_BLUE, 0.35),
	})
	ui.push_text(&state.cmd_buf, {cursor_x + 5, logo_box_y + logo_box_h * 0.5 + ui.FONT_SIZE_SM * 0.35},
		logo_text, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_SM, .Bold)
	cursor_x += logo_box_w + 8

	// Hero: active VENUE:SYMBOL.
	active_name := "---"
	if reg := state.stream_views; reg != nil && reg.count > 0 {
		if slot := stream_view_active_slot(reg); slot != nil {
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			if slot.has_stream_info {
				info := slot.stream_info
				name_buf: [128]u8
				if len(info.venue) > 0 && len(info.symbol) > 0 {
					active_name = fmt.bprintf(name_buf[:], "%s:%s", info.venue, info.symbol)
				}
			}
		}
	}
	ui.push_text(&state.cmd_buf, {cursor_x, text_baseline}, active_name,
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_SM, .Bold)
	cursor_x += state.text.measure(ui.FONT_SIZE_SM, active_name).x + 6

	// Price ticker: latest price with change % badge.
	if state.candle_store.count > 0 {
		latest := services.get_candle_newest(&state.candle_store, 0)
		decs := ui.auto_price_decimals(latest.close)
		pp_buf: [24]u8
		price_str := ui.format_price(pp_buf[:], latest.close, decs)
		bullish := latest.close >= latest.open
		price_color := bullish ? ui.COL_GREEN : ui.COL_RED
		ui.push_text(&state.cmd_buf, {cursor_x, text_baseline}, price_str,
			price_color, ui.FONT_SIZE_SM, .Bold)
		cursor_x += state.text.measure(ui.FONT_SIZE_SM, price_str).x + 4

		if latest.open > 0 {
			change_pct := (latest.close - latest.open) / latest.open * 100.0
			sign := change_pct >= 0 ? "+" : ""
			pct_buf: [16]u8
			pct_str := fmt.bprintf(pct_buf[:], "%s%.2f%%", sign, change_pct)
			pct_color := change_pct >= 0 ? ui.COL_GREEN : ui.COL_RED
			pct_w := state.text.measure(ui.FONT_SIZE_XS, pct_str).x + 8
			pct_h := f32(14)
			pct_y := (bar_h - pct_h) * 0.5
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {cursor_x, pct_y}, size = {pct_w, pct_h}},
				color = ui.with_alpha(pct_color, 0.15),
			})
			ui.push_text(&state.cmd_buf, {cursor_x + 4, pct_y + pct_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				pct_str, pct_color, ui.FONT_SIZE_XS, .Mono)
			cursor_x += pct_w + 6
		}
	}

	// --- Right section (built right-to-left): LIVE badge + quick actions ---
	right_x := ui.rect_right(r)

	// Connection status badge (rightmost).
	conn_status: ports.MD_Conn_Status = .Offline
	if state.marketdata.conn_status != nil {
		conn_status = state.marketdata.conn_status()
	}
	conn_label: string
	conn_dot_color: ui.Color
	conn_text_color: ui.Color
	switch conn_status {
	case .Connected:
		conn_label = "LIVE"
		conn_dot_color = ui.COL_GREEN
		conn_text_color = ui.COL_GREEN
	case .Connecting:
		conn_label = "CONNECTING"
		conn_dot_color = ui.COL_YELLOW_ACCENT
		conn_text_color = ui.COL_YELLOW_ACCENT
	case .Reconnecting:
		conn_label = "RECONNECTING"
		conn_dot_color = ui.COL_YELLOW_ACCENT
		conn_text_color = ui.COL_YELLOW_ACCENT
	case .Offline:
		conn_label = "OFFLINE"
		conn_dot_color = ui.with_alpha(ui.COL_WHITE, 0.35)
		conn_text_color = ui.COL_TEXT_MUTED
	}
	badge_w := ui.status_badge_width(conn_label, state.text.measure, ui.FONT_SIZE_XS)
	badge_h := f32(16)
	badge_x := right_x - badge_w
	badge_y := (bar_h - badge_h) * 0.5
	pill_rect := ui.Rect{pos = {badge_x - 2, badge_y - 1}, size = {badge_w + 4, badge_h + 2}}
	pill_hovered := ui.rect_contains(pill_rect, pointer.pos)
	pill_bg := ui.with_alpha(conn_dot_color, pill_hovered ? 0.22 : 0.12)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = pill_rect, color = pill_bg})
	ui.status_badge(&state.cmd_buf,
		{pos = {badge_x, badge_y}, size = {badge_w, badge_h}},
		conn_label, conn_dot_color, conn_text_color, state.text.measure, ui.FONT_SIZE_XS)
	if pill_hovered && pointer.left_pressed {
		queue_ui_action(state, UI_Action{kind = .Toggle_Connection_Modal})
	}
	right_x = badge_x - 6

	// Quick-action buttons (right-to-left): ? C F S
	qa_btn_w := f32(20)

	help_res := ui.icon_button(&state.cmd_buf,
		ui.rect_xywh(right_x - qa_btn_w, btn_y, qa_btn_w, btn_h),
		"?", pointer, state.text.measure, ui.FONT_SIZE_XS, true)
	if help_res.clicked {
		queue_ui_action(state, UI_Action{kind = .Toggle_Help})
	}
	right_x -= qa_btn_w + 3

	if right_x - qa_btn_w > cursor_x + 10 {
		cmp_label := state.compare_mode ? "C*" : "C"
		cmp_res := ui.icon_button(&state.cmd_buf,
			ui.rect_xywh(right_x - qa_btn_w, btn_y, qa_btn_w, btn_h),
			cmp_label, pointer, state.text.measure, ui.FONT_SIZE_XS, true)
		if cmp_res.clicked {
			queue_ui_action(state, UI_Action{kind = .Toggle_Compare})
		}
		right_x -= qa_btn_w + 3
	}

	if right_x - qa_btn_w > cursor_x + 10 {
		fm_label := state.focus_mode ? "F*" : "F"
		fm_res := ui.icon_button(&state.cmd_buf,
			ui.rect_xywh(right_x - qa_btn_w, btn_y, qa_btn_w, btn_h),
			fm_label, pointer, state.text.measure, ui.FONT_SIZE_XS, true)
		if fm_res.clicked {
			queue_ui_action(state, UI_Action{kind = .Toggle_Focus_Mode})
		}
		right_x -= qa_btn_w + 3
	}

	if right_x - qa_btn_w > cursor_x + 10 {
		sb_label := state.detail_panel_expanded ? "S*" : "S"
		sb_res := ui.icon_button(&state.cmd_buf,
			ui.rect_xywh(right_x - qa_btn_w, btn_y, qa_btn_w, btn_h),
			sb_label, pointer, state.text.measure, ui.FONT_SIZE_XS, true)
		if sb_res.clicked {
			queue_ui_action(state, UI_Action{kind = .Toggle_Detail_Panel})
		}
		right_x -= qa_btn_w + 3
	}

	// Compact mode: skip middle section (just TF inline).
	if compact {
		tf_w := f32(160)
		tf_x := right_x - tf_w - 4
		if tf_x > cursor_x + 10 {
			tf_h := f32(16)
			opts := TF_OPTIONS
			tf_res := ui.segmented_control(
				&state.cmd_buf,
				ui.rect_xywh(tf_x, btn_y, tf_w, tf_h),
				opts[:], state.active_tf_idx, pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono,
			)
			if tf_res.changed {
				queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = tf_res.index})
			}
		}
		// Accent line.
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {0, bar_h - 1}, size = {bar_w * 0.5, 1}},
			color = ui.with_alpha(ui.COL_BLUE, 0.30),
		})
		return
	}

	// --- Middle section (left-to-right, between hero+price and quick actions) ---
	// Available space: cursor_x to right_x.
	mid_limit := right_x - 4

	// Divider.
	cursor_x += 4

	// Stream navigation: < > count
	stream_controls_enabled := false
	if state.stream_views != nil && state.stream_views.count > 1 {
		stream_controls_enabled = true
	}
	if bar_w >= 400 {
		nav_btn_w := f32(16)
		prev_res := ui.icon_button(&state.cmd_buf,
			ui.rect_xywh(cursor_x, btn_y, nav_btn_w, btn_h),
			"<", pointer, state.text.measure, ui.FONT_SIZE_XS, stream_controls_enabled)
		if prev_res.clicked {
			queue_ui_action(state, UI_Action{kind = .Cycle_Stream_Prev})
		}
		cursor_x += nav_btn_w + 2

		next_res := ui.icon_button(&state.cmd_buf,
			ui.rect_xywh(cursor_x, btn_y, nav_btn_w, btn_h),
			">", pointer, state.text.measure, ui.FONT_SIZE_XS, stream_controls_enabled)
		if next_res.clicked {
			queue_ui_action(state, UI_Action{kind = .Cycle_Stream_Next})
		}
		cursor_x += nav_btn_w + 4
	}
	if state.stream_views != nil && state.stream_views.count > 0 {
		count_buf: [12]u8
		count_str := fmt.bprintf(count_buf[:], "%d/%d",
			stream_view_active_index(state.stream_views) + 1, state.stream_views.count)
		ui.push_text(&state.cmd_buf, {cursor_x, btn_y + btn_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			count_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		cursor_x += state.text.measure(ui.FONT_SIZE_XS, count_str).x + 8
	}

	// Timeframe segmented control (slightly narrower: 216px target, 24px per segment).
	tf_w := f32(216)
	if bar_w < 700 {
		tf_w = min(f32(200), mid_limit - cursor_x - 80)
	}
	if bar_w < 500 {
		tf_w = max(mid_limit - cursor_x - 40, 100)
	}
	if tf_w > 0 && cursor_x + tf_w < mid_limit {
		tf_rect := ui.rect_xywh(cursor_x, btn_y, tf_w, btn_h)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect  = ui.rect_xywh(cursor_x - 2, btn_y - 1, tf_w + 4, btn_h + 2),
			color = ui.with_alpha(ui.COL_SURFACE_2, 0.5),
		})
		opts := TF_OPTIONS
		tf_res := ui.segmented_control(
			&state.cmd_buf, tf_rect, opts[:], state.active_tf_idx,
			pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono,
		)
		if tf_res.changed {
			queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = tf_res.index})
		}
		cursor_x += tf_w + 6
	}

	// Layout preset selector: [D] [C] [A] [K] + [+] (smaller: 14px buttons).
	if bar_w >= 600 && cursor_x + 80 < mid_limit {
		preset_labels := [4]string{"D", "C", "A", "K"}
		lp_btn_w := f32(14)
		for pi in 0 ..< 4 {
			if cursor_x + lp_btn_w + 40 > mid_limit do break
			is_active := state.layout_preset == pi
			lp_rect := ui.rect_xywh(cursor_x, btn_y, lp_btn_w, btn_h)
			if is_active {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = lp_rect, color = ui.with_alpha(ui.COL_BLUE, 0.25),
				})
			}
			lp_res := ui.icon_button(&state.cmd_buf, lp_rect,
				preset_labels[pi], pointer, state.text.measure, ui.FONT_SIZE_XS, true)
			if lp_res.clicked && !is_active {
				queue_ui_action(state, UI_Action{kind = .Set_Layout_Preset, layout_preset = pi})
			}
			cursor_x += lp_btn_w + 2
		}

		// [+] Add widget.
		if cursor_x + 18 < mid_limit {
			add_btn_w := f32(16)
			add_res := ui.icon_button(&state.cmd_buf,
				ui.rect_xywh(cursor_x, btn_y, add_btn_w, btn_h),
				"+", pointer, state.text.measure, ui.FONT_SIZE_XS, true)
			if add_res.clicked {
				queue_ui_action(state, UI_Action{kind = .Toggle_Widget_Catalog})
			}
			cursor_x += add_btn_w + 4
		}
		cursor_x += 4
	}

	// Candle health badge.
	candle_health_label, _, candle_health_color := build_candle_health_ui(state)
	health_badge_w := ui.status_badge_width(candle_health_label, state.text.measure, ui.FONT_SIZE_XS)
	if cursor_x + health_badge_w + 20 < mid_limit {
		ui.status_badge(&state.cmd_buf,
			ui.rect_xywh(cursor_x, btn_y, health_badge_w, btn_h),
			candle_health_label, candle_health_color, candle_health_color,
			state.text.measure, ui.FONT_SIZE_XS)
		cursor_x += health_badge_w + 6
	}

	// Active indicator pills (smaller: 12px squares).
	if bar_w >= 500 {
		Ind_Pill :: struct { key: string, active: bool, color: ui.Color }
		fci := state.focused_candle_cell_idx
		fc_ok := fci >= 0 && fci < state.cell_count && state.cell_assignments[fci].widget == .Candle
		ind_pills := [8]Ind_Pill{
			{"M", fc_ok ? state.cell_assignments[fci].show_ma      : state.show_ma,      {0.98, 0.85, 0.2, 1}},
			{"B", fc_ok ? state.cell_assignments[fci].show_bbands  : state.show_bbands,  {0.7, 0.4, 1.0, 1}},
			{"V", fc_ok ? state.cell_assignments[fci].show_vwap    : state.show_vwap,    ui.COL_ACCENT_CYAN},
			{"R", fc_ok ? state.cell_assignments[fci].show_rsi     : state.show_rsi,     ui.COL_GREEN},
			{"I", fc_ok ? state.cell_assignments[fci].show_macd    : state.show_macd,    ui.COL_RED},
			{"H", fc_ok ? state.cell_assignments[fci].show_funding : state.show_funding, {0.2, 0.75, 0.95, 1}},
			{"J", fc_ok ? state.cell_assignments[fci].show_liq     : state.show_liq,     {0.96, 0.65, 0.2, 1}},
			{"K", fc_ok ? state.cell_assignments[fci].show_trade_counter : state.show_trade_counter, {0.85, 0.55, 0.95, 1}},
		}
		pill_w := f32(12)
		for ip in ind_pills {
			if cursor_x + pill_w + 10 > mid_limit do break
			pill_color := ip.active ? ip.color : ui.with_alpha(ui.COL_WHITE, 0.12)
			text_color := ip.active ? ui.COL_BLACK : ui.COL_TEXT_MUTED
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect  = ui.rect_xywh(cursor_x, btn_y + 1, pill_w, btn_h - 2),
				color = ui.with_alpha(pill_color, ip.active ? 0.7 : 0.15),
			})
			ui.push_text(&state.cmd_buf,
				{cursor_x + 2, btn_y + btn_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				ip.key, text_color, ui.FONT_SIZE_XS, .Mono)
			cursor_x += pill_w + 2
		}
	}

	// ═══════════════════════════════════════════════════════════════
	// Bottom accent line (1px gradient: blue → transparent).
	// ═══════════════════════════════════════════════════════════════
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, bar_h - 1}, size = {bar_w * 0.5, 1}},
		color = ui.with_alpha(ui.COL_BLUE, 0.30),
	})
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {bar_w * 0.5, bar_h - 1}, size = {bar_w * 0.5, 1}},
		color = ui.with_alpha(ui.COL_BLUE, 0.08),
	})
}
