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

	render_kind := Widget_Kind.Candle
	switch state.compare.widget_idx {
	case 0: render_kind = .Orderbook
	case 1: render_kind = .Trades
	case 2: render_kind = .Candle
	}

	for ci in 0 ..< state.compare.count {
		cell_rect := cmp_result.rects[ci]
		sid := state.compare.slots[ci]
		if sid == 0 do continue

		venue_label := "---"
		if reg := state.stream_views; reg != nil {
			slot_idx := stream_view_find_slot(reg, sid)
			if slot_idx >= 0 {
				slot := &reg.slots[slot_idx]
				if !slot.has_stream_info {
					refresh_stream_info_for_slot(state, slot)
				}
				if slot.has_stream_info && len(slot.stream_info.venue) > 0 {
					vl_buf: [64]u8
					venue_label = fmt.bprintf(vl_buf[:], "%s:%s", slot.stream_info.venue, slot.stream_info.symbol)
				}
			}
		}

		header_h := f32(18)
		header_rect := ui.rect_cut_top(&cell_rect, header_h)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = header_rect, color = ui.with_alpha(ui.COL_SURFACE_1, 0.9)})
		ui.push_text(&state.cmd_buf,
			{header_rect.pos.x + 6, header_rect.pos.y + header_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			venue_label, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)

		render_subject_layer_canvas(state, sid, render_kind, cell_rect)
	}
}
