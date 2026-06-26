package app

import "mr:ports"
import "mr:ui"

// S113/S118: Top bar — global application context only.
// Workspace-level concerns (instrument hero, price, TF, indicators, layout presets)
// moved to workspace_toolbar.odin. Top bar retains: logo, connection, quick actions.
// S118: Removed app title ("Stream Analytics") — wasted space with no information value.

draw_top_bar :: proc(state: ^App_State, input: ports.Input_State, viewport_w: f32, compact: bool = false) {
	bar_w := viewport_w
	if bar_w <= 0 do bar_w = 800
	bar_h := compact ? TOP_BAR_H_COMPACT : TOP_BAR_H

	// S134: Background — unified chrome elevation with workspace toolbar.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, 0}, size = {bar_w, bar_h}},
		color = ui.COL_SURFACE_0H,
	})

	pointer := ui.Pointer_Input{
		pos           = input.mouse.pos,
		left_down     = input.mouse.buttons[.Left],
		left_pressed  = input.mouse.pressed[.Left],
		left_released = input.mouse.released[.Left],
	}

	row := ui.Rect{pos = {0, 0}, size = {bar_w, bar_h}}
	r := ui.rect_pad_xy(row, 8, 0)
	btn_h := f32(22)
	btn_y := (bar_h - btn_h) * 0.5

	// --- Left: Logo ---
	cursor_x := r.pos.x

	logo_text := "SA"
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

	// S127: Vertical separator after logo — visual grouping.
	sep_h := bar_h * 0.45
	sep_y := (bar_h - sep_h) * 0.5
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {cursor_x, sep_y}, size = {1, sep_h}},
		color = ui.with_alpha(ui.COL_WHITE, ui.CHROME_SEPARATOR_ALPHA),
	})
	cursor_x += 8

	// --- Right section (right-to-left): Connection + quick actions ---
	right_x := ui.rect_right(r)

	// Connection status badge (rightmost).
	conn_disp := current_conn_status_display(state)
	conn_label := conn_disp.label
	conn_dot_color := conn_disp.dot_color
	conn_text_color := conn_disp.text_color
	badge_w := ui.status_badge_width(conn_label, state.text.measure, ui.FONT_SIZE_XS)
	badge_h := f32(16)
	badge_x := right_x - badge_w
	badge_y := (bar_h - badge_h) * 0.5
	pill_rect := ui.Rect{pos = {badge_x - 2, badge_y - 1}, size = {badge_w + 4, badge_h + 2}}
	pill_hovered := ui.rect_contains(pill_rect, pointer.pos)
	pill_bg := ui.with_alpha(conn_dot_color, pill_hovered ? 0.30 : 0.18)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = pill_rect, color = pill_bg})
	ui.draw_rect_stroke(&state.cmd_buf, pill_rect, ui.with_alpha(conn_dot_color, 0.35))
	ui.status_badge(&state.cmd_buf,
		{pos = {badge_x, badge_y}, size = {badge_w, badge_h}},
		conn_label, conn_dot_color, conn_text_color, state.text.measure, ui.FONT_SIZE_XS)
	if pill_hovered && pointer.left_pressed {
		queue_ui_action(state, UI_Action{kind = .Toggle_Connection_Modal})
	}
	right_x = badge_x - 6

	// S20: Backend readiness badge.
	if state.bootstrap.has_session && !state.bootstrap.ready {
		rdy_label := "NOT READY"
		rdy_w := ui.status_badge_width(rdy_label, state.text.measure, ui.FONT_SIZE_XS)
		rdy_h := f32(16)
		rdy_x := right_x - rdy_w
		rdy_y := (bar_h - rdy_h) * 0.5
		ui.status_badge(&state.cmd_buf,
			{pos = {rdy_x, rdy_y}, size = {rdy_w, rdy_h}},
			rdy_label, ui.COL_RED, ui.COL_RED, state.text.measure, ui.FONT_SIZE_XS)
		right_x = rdy_x - 6
	}

	// Error indicator.
	if state.error_state.len > 0 && state.frame > 0 && (state.frame - state.error_state.frame) < 300 {
		err_dot_size := f32(4)
		err_dot_x := right_x - err_dot_size - 2
		err_dot_y := bar_h * 0.5 - err_dot_size * 0.5
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {err_dot_x, err_dot_y}, size = {err_dot_size, err_dot_size}},
			color = ui.COL_RED,
		})
		err_hit := ui.Rect{pos = {err_dot_x - 4, err_dot_y - 4}, size = {err_dot_size + 8, err_dot_size + 8}}
		if ui.rect_contains(err_hit, pointer.pos) {
			err_text := string(state.error_state.text[:state.error_state.len])
			ui.push_text(&state.cmd_buf, {err_dot_x - 200, err_dot_y + err_dot_size + 4},
				err_text, ui.COL_RED, ui.FONT_SIZE_XS, .Mono)
		}
		right_x = err_dot_x - 4
	}

	// Quick-action buttons (right-to-left): ? C F Z S
	qa_btn_w := f32(20)

	// Help button.
	help_res := ui.icon_button(&state.cmd_buf,
		ui.rect_xywh(right_x - qa_btn_w, btn_y, qa_btn_w, btn_h),
		"?", pointer, state.text.measure, ui.FONT_SIZE_XS, true)
	if help_res.clicked {
		queue_ui_action(state, UI_Action{kind = .Toggle_Help})
	}
	right_x -= qa_btn_w + 3

	// Compare toggle.
	if right_x - qa_btn_w > cursor_x + 10 {
		cmp_label := state.compare.active ? "C*" : "C"
		cmp_res := ui.icon_button(&state.cmd_buf,
			ui.rect_xywh(right_x - qa_btn_w, btn_y, qa_btn_w, btn_h),
			cmp_label, pointer, state.text.measure, ui.FONT_SIZE_XS, true)
		if cmp_res.clicked {
			queue_ui_action(state, UI_Action{kind = .Toggle_Compare})
		}
		right_x -= qa_btn_w + 3
	}

	// Focus mode toggle.
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

	// Zen mode toggle.
	if right_x - qa_btn_w > cursor_x + 10 {
		zen_label := state.zen.active ? "Z*" : "Z"
		zen_res := ui.icon_button(&state.cmd_buf,
			ui.rect_xywh(right_x - qa_btn_w, btn_y, qa_btn_w, btn_h),
			zen_label, pointer, state.text.measure, ui.FONT_SIZE_XS, true)
		if zen_res.clicked {
			queue_ui_action(state, UI_Action{kind = .Toggle_Zen_Mode})
		}
		right_x -= qa_btn_w + 3
	}

	// Detail panel toggle.
	if right_x - qa_btn_w > cursor_x + 10 {
		sb_label := state.chrome.detail_expanded ? "S*" : "S"
		sb_res := ui.icon_button(&state.cmd_buf,
			ui.rect_xywh(right_x - qa_btn_w, btn_y, qa_btn_w, btn_h),
			sb_label, pointer, state.text.measure, ui.FONT_SIZE_XS, true)
		if sb_res.clicked {
			queue_ui_action(state, UI_Action{kind = .Toggle_Detail_Panel})
		}
		right_x -= qa_btn_w + 3
	}

	// Compact mode: show TF inline (for zen mode).
	if compact {
		tf_count := f32(len(TF_OPTIONS))
		tf_w := tf_count * 28 + (tf_count - 1) * 2
		tf_x := right_x - tf_w - 4
		if tf_x > cursor_x + 10 {
			tf_h := f32(16)
			opts := TF_OPTIONS
			tf_res := ui.segmented_control(
				&state.cmd_buf,
				ui.rect_xywh(tf_x, btn_y, tf_w, tf_h),
				opts[:], state.active_tf_idx, pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono,
			)
			if tf_res.changed && tf_res.index >= 0 && tf_res.index < len(TF_OPTIONS) {
				queue_ui_action(state, UI_Action{kind = .Set_Timeframe, timeframe_idx = tf_res.index})
			}
		}
		// S134: Full-width accent line — subtler.
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {0, bar_h - 1}, size = {bar_w, 1}},
			color = ui.with_alpha(ui.COL_BLUE, 0.12),
		})
		return
	}

	// S134: Full-width bottom accent line — subtler professional edge.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, bar_h - 1}, size = {bar_w, 1}},
		color = ui.with_alpha(ui.COL_BLUE, 0.12),
	})
}
