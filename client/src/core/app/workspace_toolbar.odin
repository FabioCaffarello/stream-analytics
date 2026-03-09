package app

// S113/S118: Workspace Toolbar — workspace-level context bar.
// Renders between top bar and workspace area on Dashboard route.
// Contains: hero instrument, price ticker, TF selector, layout presets, indicator pills.
// S118: Removed candle health badge and freshness badge (redundant with status bar).
// Raised indicator pill viewport threshold to 550px for breathing room.

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:ui"

WORKSPACE_TOOLBAR_H :: f32(28)

@(private = "package")
draw_workspace_toolbar :: proc(state: ^App_State, input: ports.Input_State, y: f32, width: f32) {
	bar_w := width
	if bar_w <= 0 do bar_w = 800
	bar_h := WORKSPACE_TOOLBAR_H

	pointer := ui.Pointer_Input{
		pos           = input.mouse.pos,
		left_down     = input.mouse.buttons[.Left],
		left_pressed  = input.mouse.pressed[.Left],
		left_released = input.mouse.released[.Left],
	}

	// Background — slightly darker than top bar for visual separation.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, y}, size = {bar_w, bar_h}},
		color = ui.COL_SURFACE_0,
	})
	// Top border (subtle).
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, y}, size = {bar_w, 1}},
		color = ui.COL_BORDER_SUBTLE,
	})

	r := ui.Rect{pos = {0, y}, size = {bar_w, bar_h}}
	r = ui.rect_pad_xy(r, 8, 0)
	btn_h := f32(20)
	btn_y := y + (bar_h - btn_h) * 0.5
	text_baseline := y + bar_h * 0.5 + ui.FONT_SIZE_XS * 0.35

	cursor_x := r.pos.x

	// --- Hero: active VENUE:SYMBOL ---
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
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
	cursor_x += state.text.measure(ui.FONT_SIZE_XS, active_name).x + 6

	// --- Price ticker ---
	cs := active_candle_store(state)
	if cs != nil && cs.count > 0 {
		latest := services.get_candle_newest(cs, 0)
		decs := ui.auto_price_decimals(latest.close)
		pp_buf: [24]u8
		price_str := ui.format_price(pp_buf[:], latest.close, decs)
		bullish := latest.close >= latest.open
		price_color := bullish ? ui.COL_GREEN : ui.COL_RED
		ui.push_text(&state.cmd_buf, {cursor_x, text_baseline}, price_str,
			price_color, ui.FONT_SIZE_XS, .Bold)
		cursor_x += state.text.measure(ui.FONT_SIZE_XS, price_str).x + 4

		if latest.open > 0 {
			change_pct := (latest.close - latest.open) / latest.open * 100.0
			sign := change_pct >= 0 ? "+" : ""
			pct_buf: [16]u8
			pct_str := fmt.bprintf(pct_buf[:], "%s%.2f%%", sign, change_pct)
			pct_color := change_pct >= 0 ? ui.COL_GREEN : ui.COL_RED
			pct_w := state.text.measure(ui.FONT_SIZE_XS, pct_str).x + 6
			pct_h := f32(14)
			pct_y := y + (bar_h - pct_h) * 0.5
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {cursor_x, pct_y}, size = {pct_w, pct_h}},
				color = ui.with_alpha(pct_color, 0.15),
			})
			ui.push_text(&state.cmd_buf, {cursor_x + 3, pct_y + pct_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				pct_str, pct_color, ui.FONT_SIZE_XS, .Mono)
			cursor_x += pct_w + 4
		}

		// Volume badge.
		total_vol := f64(0)
		for vi in 0 ..< cs.count {
			c := services.get_candle(cs, vi)
			total_vol += c.volume
		}
		if total_vol > 0 && bar_w >= 500 {
			vol_buf: [24]u8
			vol_str: string
			if total_vol >= 1_000_000 {
				vol_str = fmt.bprintf(vol_buf[:], "V:%.1fM", total_vol / 1_000_000)
			} else if total_vol >= 1_000 {
				vol_str = fmt.bprintf(vol_buf[:], "V:%.1fK", total_vol / 1_000)
			} else {
				vol_str = fmt.bprintf(vol_buf[:], "V:%.1f", total_vol)
			}
			vol_w := state.text.measure(ui.FONT_SIZE_XS, vol_str).x + 6
			vol_pill_h := f32(14)
			vol_pill_y := y + (bar_h - vol_pill_h) * 0.5
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {cursor_x, vol_pill_y}, size = {vol_w, vol_pill_h}},
				color = ui.with_alpha(ui.COL_WHITE, 0.06),
			})
			ui.push_text(&state.cmd_buf, {cursor_x + 3, vol_pill_y + vol_pill_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				vol_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			cursor_x += vol_w + 4
		}
	}

	// --- Divider ---
	cursor_x += 4

	// --- Stream navigation: < > count ---
	stream_controls_enabled := false
	if state.stream_views != nil && state.stream_views.count > 1 {
		stream_controls_enabled = true
	}
	if bar_w >= 400 {
		nav_btn_w := f32(14)
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
		cursor_x += nav_btn_w + 3
	}
	if state.stream_views != nil && state.stream_views.count > 0 {
		count_buf: [12]u8
		count_str := fmt.bprintf(count_buf[:], "%d/%d",
			stream_view_active_index(state.stream_views) + 1, state.stream_views.count)
		ui.push_text(&state.cmd_buf, {cursor_x, text_baseline},
			count_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		cursor_x += state.text.measure(ui.FONT_SIZE_XS, count_str).x + 6
	}

	// --- Right section (right-to-left) ---
	right_x := ui.rect_right(r)

	// S113: Context stack toggle button (rightmost on toolbar).
	ctx_btn_w := f32(18)
	ctx_label := state.chrome.context_stack.expanded ? "P*" : "P"
	ctx_res := ui.icon_button(&state.cmd_buf,
		ui.rect_xywh(right_x - ctx_btn_w, btn_y, ctx_btn_w, btn_h),
		ctx_label, pointer, state.text.measure, ui.FONT_SIZE_XS, true)
	if ctx_res.clicked {
		queue_ui_action(state, UI_Action{kind = .Toggle_Context_Stack})
	}
	right_x -= ctx_btn_w + 4

	mid_limit := right_x - 4

	// --- Timeframe selector ---
	tf_count := f32(len(TF_OPTIONS))
	tf_w := tf_count * 24 + (tf_count - 1) * 1
	if tf_w > 0 && cursor_x + tf_w < mid_limit {
		tf_rect := ui.rect_xywh(cursor_x, btn_y, tf_w, btn_h)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect  = ui.rect_xywh(cursor_x - 1, btn_y - 1, tf_w + 2, btn_h + 2),
			color = ui.with_alpha(ui.COL_SURFACE_2, 0.5),
		})
		opts := TF_OPTIONS
		tf_res := ui.segmented_control(
			&state.cmd_buf, tf_rect, opts[:], state.active_tf_idx,
			pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono,
		)
		if tf_res.changed && tf_res.index >= 0 && tf_res.index < len(TF_OPTIONS) {
			queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = tf_res.index})
		}
		// TF change flash.
		flash_age := state.frame - state.tf_flash_frame
		if state.tf_flash_frame > 0 && flash_age < 6 {
			flash_alpha := f32(0.25) * (1.0 - f32(flash_age) / 6.0)
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect  = ui.rect_xywh(cursor_x - 1, btn_y - 1, tf_w + 2, btn_h + 2),
				color = ui.with_alpha(ui.COL_ACCENT_CYAN, flash_alpha),
			})
		}
		cursor_x += tf_w + 6
	}

	// --- Layout presets: D C A K W + ---
	if bar_w >= 500 && cursor_x + 70 < mid_limit {
		preset_labels := [5]string{"D", "C", "A", "K", "W"}  // S119: W = Workstation
		lp_btn_w := f32(14)
		for pi in 0 ..< 5 {
			if cursor_x + lp_btn_w + 30 > mid_limit do break
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
		if cursor_x + 16 < mid_limit {
			add_btn_w := f32(14)
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

	// --- Indicator pills ---
	if bar_w >= 550 {
		Ind_Pill :: struct { key: string, active: bool, color: ui.Color }
		fci := state.world.focused
		fc_ok := fci >= 0 && fci < state.world.count && state.world.widgets[fci].kind == .Candle
		ind_pills := [11]Ind_Pill{
			{"M", fc_ok ? state.world.indicators[fci].show_ma      : state.indicators.show_ma,      {0.98, 0.85, 0.2, 1}},
			{"B", fc_ok ? state.world.indicators[fci].show_bbands  : state.indicators.show_bbands,  {0.7, 0.4, 1.0, 1}},
			{"V", fc_ok ? state.world.indicators[fci].show_vwap    : state.indicators.show_vwap,    ui.COL_ACCENT_CYAN},
			{"R", fc_ok ? state.world.indicators[fci].show_rsi     : state.indicators.show_rsi,     ui.COL_GREEN},
			{"I", fc_ok ? state.world.indicators[fci].show_macd    : state.indicators.show_macd,    ui.COL_RED},
			{"H", fc_ok ? state.world.indicators[fci].show_funding : state.indicators.show_funding, {0.2, 0.75, 0.95, 1}},
			{"J", fc_ok ? state.world.indicators[fci].show_liq     : state.indicators.show_liq,     {0.96, 0.65, 0.2, 1}},
			{"K", fc_ok ? state.world.indicators[fci].show_trade_counter : state.indicators.show_trade_counter, {0.85, 0.55, 0.95, 1}},
			{"C", fc_ok ? state.world.indicators[fci].show_cvd       : state.indicators.show_cvd,       {0.3, 0.9, 0.5, 1}},
			{"D", fc_ok ? state.world.indicators[fci].show_delta_vol : state.indicators.show_delta_vol, {0.9, 0.4, 0.3, 1}},
			{"O", fc_ok ? state.world.indicators[fci].show_oi        : state.indicators.show_oi,        {0.4, 0.7, 0.95, 1}},
		}
		pill_w := f32(14)
		pill_h := f32(16)
		pill_y := y + (bar_h - pill_h) * 0.5
		for ip, pi in ind_pills {
			if cursor_x + pill_w + 6 > mid_limit do break
			pill_rect := ui.rect_xywh(cursor_x, pill_y, pill_w, pill_h)
			hovered := ui.rect_contains(pill_rect, pointer.pos)
			pill_color := ip.active ? ip.color : ui.with_alpha(ui.COL_WHITE, 0.12)
			text_color := ip.active ? ui.COL_BLACK : ui.COL_TEXT_MUTED
			pill_alpha := ip.active ? f32(0.7) : f32(0.15)
			if !fc_ok do pill_alpha *= 0.4
			if fc_ok && hovered do pill_alpha += 0.15
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect  = pill_rect,
				color = ui.with_alpha(pill_color, pill_alpha),
			})
			label_size := state.text.measure(ui.FONT_SIZE_XS, ip.key)
			label_x := cursor_x + (pill_w - label_size.x) * 0.5
			label_y := pill_y + pill_h * 0.5 + ui.FONT_SIZE_XS * 0.35
			ui.push_text(&state.cmd_buf, {label_x, label_y},
				ip.key, text_color, ui.FONT_SIZE_XS, .Mono)
			if fc_ok && hovered && pointer.left_pressed {
				toggle_focused_indicator(state, pi)
			}
			cursor_x += pill_w + 2
		}
	}

	// Bottom accent line.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, y + bar_h - 1}, size = {bar_w * 0.4, 1}},
		color = ui.with_alpha(ui.COL_BLUE, 0.15),
	})
}
