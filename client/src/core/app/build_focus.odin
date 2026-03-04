package app

import "mr:ports"
import "mr:ui"

// Focus mode rendering — layer canvas (candle 75% + orderbook 25%).

build_focus_mode :: proc(
	state: ^App_State,
	input: ports.Input_State,
	workspace: ui.Rect,
	pointer: ui.Pointer_Input,
) {
	_ = input
	_ = pointer
	if state == nil do return

	focus_gap := f32(4)
	candle_w := (workspace.size.x - focus_gap) * 0.75
	ob_w := workspace.size.x - candle_w - focus_gap

	candle_rect := ui.rect_xywh(workspace.pos.x, workspace.pos.y, candle_w, workspace.size.y)
	ob_rect := ui.rect_xywh(workspace.pos.x + candle_w + focus_gap, workspace.pos.y, ob_w, workspace.size.y)

	subject_id := resolve_cell_subject_id(state, 0)
	render_subject_layer_canvas(state, subject_id, .Candle, candle_rect)
	render_subject_layer_canvas(state, subject_id, .Orderbook, ob_rect)

	focus_label := "FOCUS  Esc:exit"
	flw := state.text.measure(ui.FONT_SIZE_XS, focus_label).x
	ui.push_text(&state.cmd_buf, {workspace.pos.x + workspace.size.x - flw - 4, workspace.pos.y + 12},
		focus_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
}
