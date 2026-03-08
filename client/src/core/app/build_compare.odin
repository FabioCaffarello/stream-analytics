package app

import "core:fmt"
import "mr:ports"
import "mr:ui"

// Compare mode rendering — side-by-side layer canvas comparison.

build_compare_mode :: proc(
	state: ^App_State,
	input: ports.Input_State,
	workspace: ui.Rect,
	pointer: ui.Pointer_Input,
	gap: f32,
) {
	_ = input
	if state == nil do return
	workspace := workspace

	ctrl_h := f32(22)
	ctrl_rect := ui.rect_cut_top(&workspace, ctrl_h)
	ui.rect_cut_top(&workspace, 4)

	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ctrl_rect, color = ui.COL_SURFACE_1})

	cr := ui.rect_pad_xy(ctrl_rect, 8, 2)
	cmp_label := "COMPARE"
	ui.push_text(&state.cmd_buf, {cr.pos.x, cr.pos.y + ctrl_h * 0.5 + ui.FONT_SIZE_XS * 0.25},
		cmp_label, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
	cmp_cursor := cr.pos.x + state.text.measure(ui.FONT_SIZE_XS, cmp_label).x + 10

	cmp_opts := COMPARE_WIDGET_OPTIONS
	seg_w := f32(150)
	seg_rect := ui.rect_xywh(cmp_cursor, cr.pos.y, seg_w, cr.size.y)
	seg_res := ui.segmented_control(&state.cmd_buf, seg_rect, cmp_opts[:], state.compare.widget_idx,
		pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if seg_res.changed {
		state.compare.widget_idx = seg_res.index
	}
	cmp_cursor += seg_w + 10

	count_buf: [48]u8
	count_str := fmt.bprintf(count_buf[:], "%d streams  Tab:add  Esc:exit", state.compare.count)
	ui.push_text(&state.cmd_buf, {cmp_cursor, cr.pos.y + ctrl_h * 0.5 + ui.FONT_SIZE_XS * 0.25},
		count_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	cmp_grid := ui.build_compare_grid(state.compare.count, gap)
	cmp_result := ui.compute_grid(cmp_grid, workspace)

	// S62: Use canonical compare_widget_kind_for_idx (widget_channels.odin).
	render_kind := compare_widget_kind_for_idx(state.compare.widget_idx)

	for ci in 0 ..< state.compare.count {
		cell_rect := cmp_result.rects[ci]
		if state.compare.slots[ci] == 0 do continue

		// S39: Click-to-focus — if pointer is inside this pane and mouse pressed, focus it.
		is_focused := ci == state.compare.focused_pane
		if ui.rect_contains(cell_rect, pointer.pos) && pointer.left_pressed {
			state.compare.focused_pane = ci
			is_focused = true
		}

		// S38: Resolve effective subject_id (per-pane TF aware) for rendering.
		sid := compare_pane_resolve_subject_id(state, ci)
		if sid == 0 do continue

		// S38: Surface view uses per-pane TF for health/staleness.
		sv := resolve_compare_surface_view(state, ci)

		venue_label := "---"
		vl_buf: [64]u8
		if len(sv.venue) > 0 {
			venue_label = fmt.bprintf(vl_buf[:], "%s:%s", sv.venue, sv.symbol)
		}

		header_h := f32(18)
		header_rect := ui.rect_cut_top(&cell_rect, header_h)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = header_rect, color = ui.with_alpha(ui.COL_SURFACE_1, 0.9)})

		// S37: Venue:Symbol label.
		cursor_x := header_rect.pos.x + 6
		text_y := header_rect.pos.y + header_h * 0.5 + ui.FONT_SIZE_XS * 0.35
		ui.push_text(&state.cmd_buf, {cursor_x, text_y},
			venue_label, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
		cursor_x += state.text.measure(ui.FONT_SIZE_XS, venue_label).x + 6

		// S38: Per-pane TF badge.
		tf_opts := TF_OPTIONS
		eff_tf := compare_pane_effective_tf_idx(state, ci)
		tf_str := tf_opts[eff_tf] if eff_tf >= 0 && eff_tf < len(tf_opts) else tf_opts[0]
		is_per_pane_tf := state.compare.tf_idx[ci] >= 0
		tf_color := is_per_pane_tf ? ui.COL_BLUE : ui.COL_YELLOW_ACCENT
		ui.push_text(&state.cmd_buf, {cursor_x, text_y},
			tf_str, tf_color, ui.FONT_SIZE_XS, .Mono)
		cursor_x += state.text.measure(ui.FONT_SIZE_XS, tf_str).x + 4

		// S37/S53: Composition badge (shared proc).
		cursor_x += draw_composition_badge(&state.cmd_buf, cursor_x, text_y, sv.composition, state.text.measure)

		// S42: Recovery badge (RCVR/XHST) — surfaces per-pane recovery status.
		switch sv.recovery_status {
		case .Recovering:
			rcvr_label :: "RCVR"
			ui.push_text(&state.cmd_buf, {cursor_x, text_y},
				rcvr_label, ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
			cursor_x += state.text.measure(ui.FONT_SIZE_XS, rcvr_label).x + 4
		case .Exhausted:
			xhst_label :: "XHST"
			ui.push_text(&state.cmd_buf, {cursor_x, text_y},
				xhst_label, ui.COL_RED, ui.FONT_SIZE_XS, .Mono)
			cursor_x += state.text.measure(ui.FONT_SIZE_XS, xhst_label).x + 4
		case .None:
		}

		// S37/S53: Health dot (shared proc).
		draw_health_dot(&state.cmd_buf, cursor_x, header_rect.pos.y + header_h * 0.5, 6, sv.health_level, sv.has_live_data, sv.composition)

		render_subject_layer_canvas(state, sid, render_kind, cell_rect)

		// S39: Focused pane border highlight.
		if is_focused {
			border_color := ui.with_alpha(ui.COL_BLUE, 0.6)
			b := f32(1)
			full_rect := cmp_result.rects[ci]
			// Top
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(full_rect.pos.x, full_rect.pos.y, full_rect.size.x, b), color = border_color})
			// Bottom
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(full_rect.pos.x, full_rect.pos.y + full_rect.size.y - b, full_rect.size.x, b), color = border_color})
			// Left
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(full_rect.pos.x, full_rect.pos.y, b, full_rect.size.y), color = border_color})
			// Right
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(full_rect.pos.x + full_rect.size.x - b, full_rect.pos.y, b, full_rect.size.y), color = border_color})
		}
	}
}
