package app

// Cell rendering through layer canvas (hard cutover).

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:ui"

CELL_HDR_H :: f32(20)

render_cell_widget :: proc(
	state: ^App_State,
	ci: int,
	cell_vp_in: ui.Rect,
	pointer: ui.Pointer_Input,
	input: ports.Input_State,
	sync_price: f64,
	sync_active: bool,
) {
	_ = sync_price
	_ = sync_active

	if state == nil do return
	if ci < 0 || ci >= state.world.count do return

	bind := &state.world.bindings[ci]
	wid := state.world.widgets[ci].kind
	tf_comp := &state.world.timeframes[ci]

	cell_vp := cell_vp_in
	if cell_vp.size.x <= 0 || cell_vp.size.y <= 0 do return

	is_cell_focused := ui.rect_contains(cell_vp, input.mouse.pos)
	cell_border_color := is_cell_focused ? ui.COL_BORDER_STRONG : ui.COL_BORDER_SUBTLE
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = {pos = cell_vp.pos, size = {cell_vp.size.x, 1}}, color = cell_border_color})
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = {pos = {cell_vp.pos.x, cell_vp.pos.y + cell_vp.size.y - 1}, size = {cell_vp.size.x, 1}}, color = cell_border_color})
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = {pos = cell_vp.pos, size = {1, cell_vp.size.y}}, color = cell_border_color})
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = {pos = {cell_vp.pos.x + cell_vp.size.x - 1, cell_vp.pos.y}, size = {1, cell_vp.size.y}}, color = cell_border_color})

	hdr_rect := ui.rect_cut_top(&cell_vp, CELL_HDR_H)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = hdr_rect, color = ui.with_alpha(ui.COL_SURFACE_2, 0.7)})

	// S43: Surface view resolved once, used for all header elements (identity, composition, health).
	sv := resolve_cell_surface_view(state, ci)

	badge_label := "~ Active"
	badge_buf: [40]u8
	if sv.stream_bound && len(sv.venue) > 0 {
		badge_label = fmt.bprintf(badge_buf[:], "%s:%s", sv.venue, sv.symbol)
	}
	badge_w := min(state.text.measure(ui.FONT_SIZE_XS, badge_label).x + 12, hdr_rect.size.x * 0.5)
	badge_rect := ui.rect_xywh(hdr_rect.pos.x + 2, hdr_rect.pos.y + 1, badge_w, CELL_HDR_H - 2)
	badge_hovered := ui.rect_contains(badge_rect, pointer.pos)
	badge_bg := badge_hovered ? ui.with_alpha(ui.COL_BLUE, 0.2) : ui.with_alpha(ui.COL_BLUE, 0.1)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = badge_rect, color = badge_bg})
	ui.push_text(&state.cmd_buf,
		{badge_rect.pos.x + 6, hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35},
		badge_label, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	if badge_hovered && pointer.left_pressed {
		queue_ui_action(state, UI_Action{kind = .Open_Cell_Stream_Picker, cell_idx = ci})
	}

	// S37/S53: Composition badge + health dot from Cell_Surface_View read model.
	hdr_cursor := ui.rect_right(badge_rect) + 4
	hdr_text_y := hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35
	hdr_cursor += draw_composition_badge(&state.cmd_buf, hdr_cursor, hdr_text_y, sv.composition, state.text.measure)
	hdr_cursor += draw_health_dot(&state.cmd_buf, hdr_cursor, hdr_rect.pos.y + CELL_HDR_H * 0.5, 6, sv.health_level, sv.has_live_data, sv.composition)

	close_inset := f32(0)
	if state.world.count > 1 {
		close_sz := f32(14)
		close_x := ui.rect_right(hdr_rect) - close_sz - 2
		close_y := hdr_rect.pos.y + (CELL_HDR_H - close_sz) * 0.5
		close_res := ui.icon_button(&state.cmd_buf,
			ui.rect_xywh(close_x, close_y, close_sz, close_sz),
			"x", pointer, state.text.measure, ui.FONT_SIZE_XS)
		if close_res.clicked {
			queue_ui_action(state, UI_Action{kind = .Remove_Cell, cell_idx = ci})
		}
		close_inset = close_sz + 4
	}

	tf_inset := f32(0)
	if wid == .Candle && cell_vp.size.x >= 120 {
		tf_opts := TF_OPTIONS
		eff_tf := cell_effective_tf_idx(state, ci)
		tf_str := tf_opts[eff_tf] if eff_tf >= 0 && eff_tf < len(tf_opts) else tf_opts[0]
		is_per_cell_tf := tf_comp.tf_idx >= 0
		tf_color := is_per_cell_tf ? ui.COL_BLUE : ui.COL_YELLOW_ACCENT
		tf_w := state.text.measure(ui.FONT_SIZE_XS, tf_str).x + 8
		tf_x := ui.rect_right(hdr_rect) - tf_w - 4 - close_inset
		tf_rect := ui.rect_xywh(tf_x, hdr_rect.pos.y + 1, tf_w, CELL_HDR_H - 2)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = tf_rect, color = ui.with_alpha(tf_color, 0.12)})
		ui.push_text(&state.cmd_buf,
			{tf_x + 4, hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35},
			tf_str, tf_color, ui.FONT_SIZE_XS, .Mono)
		if is_per_cell_tf {
			ui.push(&state.cmd_buf, ui.Cmd_Line{
				from = {tf_rect.pos.x, ui.rect_bottom(tf_rect)},
				to   = {tf_rect.pos.x + tf_w, ui.rect_bottom(tf_rect)},
				color = tf_color, thickness = 1,
			})
		}
		tf_inset = tf_w + 4
		if is_cell_focused && pointer.left_pressed && ui.rect_contains(tf_rect, pointer.pos) {
			next_tf: int
			if is_per_cell_tf {
				next_tf = tf_comp.tf_idx + 1
				if next_tf >= len(tf_opts) do next_tf = -1
			} else {
				next_tf = (eff_tf + 1) % len(tf_opts)
			}
			queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = ci, timeframe_idx = next_tf})
		}
	}

	// S55: Analytics cells get interactive kind badge + history toggle.
	// Non-analytics cells get a static widget short label.
	analytics_inset := f32(0)
	if wid == .Analytics && cell_vp.size.x >= 80 {
		ANALYTICS_SHORT :: [4]string{"OI", "DV", "CVD", "BS"}
		analytics_short := ANALYTICS_SHORT
		ak := int(state.world.analytics[ci].analytics_kind)
		wlabel := analytics_short[ak] if ak >= 0 && ak < len(analytics_short) else "OI"

		// Clickable kind badge (cycles OI → DV → CVD → BS → OI).
		ak_w := state.text.measure(ui.FONT_SIZE_XS, wlabel).x + 8
		ak_x := ui.rect_right(hdr_rect) - ak_w - 4 - close_inset - tf_inset
		ak_rect := ui.rect_xywh(ak_x, hdr_rect.pos.y + 1, ak_w, CELL_HDR_H - 2)
		ak_hov := ui.rect_contains(ak_rect, pointer.pos)
		ak_bg := ak_hov ? ui.with_alpha(ui.COL_ACCENT_CYAN, 0.2) : ui.with_alpha(ui.COL_ACCENT_CYAN, 0.08)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ak_rect, color = ak_bg})
		ui.push_text(&state.cmd_buf,
			{ak_x + 4, hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35},
			wlabel, ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Mono)
		if ak_hov && pointer.left_pressed {
			next_ak := services.Analytics_Kind((ak + 1) % 4)
			queue_ui_action(state, UI_Action{
				kind           = .Set_Cell_Widget,
				cell_idx       = ci,
				widget_kind    = .Analytics,
				analytics_kind = next_ak,
			})
		}
		analytics_inset = ak_w + 4

		// History toggle: "H" button.
		show_hist := state.world.analytics[ci].show_history
		h_w := f32(16)
		h_x := ak_x - h_w - 2
		h_rect := ui.rect_xywh(h_x, hdr_rect.pos.y + 1, h_w, CELL_HDR_H - 2)
		h_hov := ui.rect_contains(h_rect, pointer.pos)
		h_col := show_hist ? ui.COL_ACCENT_CYAN : ui.COL_TEXT_MUTED
		h_bg := h_hov ? ui.with_alpha(h_col, 0.2) : ui.with_alpha(h_col, 0.06)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = h_rect, color = h_bg})
		ui.push_text(&state.cmd_buf,
			{h_x + 4, hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35},
			"H", h_col, ui.FONT_SIZE_XS, .Mono)
		if h_hov && pointer.left_pressed {
			state.world.analytics[ci].show_history = !show_hist
		}
		analytics_inset += h_w + 2
	} else if wid == .Session_VPVR || wid == .TPO {
		PROFILE_SHORT :: [2]string{"SVPVR", "TPO"}
		profile_short := PROFILE_SHORT
		pidx := wid == .TPO ? 1 : 0
		wlabel := profile_short[pidx]
		wlabel_w := state.text.measure(ui.FONT_SIZE_XS, wlabel).x
		wlabel_x := ui.rect_right(hdr_rect) - wlabel_w - 4 - close_inset - tf_inset
		ui.push_text(&state.cmd_buf,
			{wlabel_x, hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35},
			wlabel, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	} else {
		WIDGET_SHORT :: [12]string{"Candle", "Stats", "Counter", "HM", "VPVR", "Trades", "OB", "DOM", "--", "Analytics", "SVPVR", "TPO"}
		widget_short := WIDGET_SHORT
		wlabel := widget_short[int(wid)]
		wlabel_w := state.text.measure(ui.FONT_SIZE_XS, wlabel).x
		wlabel_x := ui.rect_right(hdr_rect) - wlabel_w - 4 - close_inset - tf_inset
		ui.push_text(&state.cmd_buf,
			{wlabel_x, hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35},
			wlabel, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}

	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {hdr_rect.pos.x, hdr_rect.pos.y + CELL_HDR_H},
		to   = {ui.rect_right(hdr_rect), hdr_rect.pos.y + CELL_HDR_H},
		color = ui.COL_DIVIDER, thickness = 1,
	})

	// S48: Analytics widgets render directly from cell stores (no layer canvas).
	// S49: Session profile widgets render directly from cell stores.
	if wid == .Analytics {
		render_analytics_cell(state, ci, cell_vp)
	} else if wid == .Session_VPVR || wid == .TPO {
		render_session_profile_cell(state, ci, wid, cell_vp)
	} else {
		render_cell_layer_canvas(state, ci, wid, cell_vp)
	}

}
