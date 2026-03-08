package app

import "mr:ui"

// S53: Grid resize handles — extracted from build_dashboard.odin.
// Column and row resize via drag on grid borders.

@(private = "package")
update_grid_col_resize :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input, grid_def: ui.Grid_Def, gap: f32) {
	if grid_def.col_count < 2 do return

	RESIZE_HIT_W :: f32(6)
	if state.grid_col_resize >= 0 {
		// Active resize drag.
		if pointer.left_down {
			ci := state.grid_col_resize
			total_w := workspace.size.x - gap * f32(grid_def.col_count - 1)
			if total_w > 0 {
				// S63: Cache weight sum once per frame (was called 4x).
				s := col_weight_sum(state, grid_def.col_count)
				left_x := workspace.pos.x
				for c in 0 ..< ci {
					left_x += total_w * (state.custom_grid_def.col_weights[c] / s) + gap
				}
				new_left_w := pointer.pos.x - left_x
				right_edge := left_x + total_w * (state.custom_grid_def.col_weights[ci] / s) + gap + total_w * (state.custom_grid_def.col_weights[ci + 1] / s)
				new_right_w := right_edge - pointer.pos.x - gap
				min_w := total_w * 0.08
				if new_left_w >= min_w && new_right_w >= min_w {
					state.custom_grid_def.col_weights[ci]     = (new_left_w / total_w) * s
					state.custom_grid_def.col_weights[ci + 1] = (new_right_w / total_w) * s
				}
			}
		} else {
			state.grid_col_resize = -1
			persist_col_weights(state, grid_def.col_count)
		}
	} else {
		// S63: Pre-compute cumulative column positions once (was O(n^2) re-accumulation).
		total_w_detect := workspace.size.x - gap * f32(grid_def.col_count - 1)
		cw_sum_detect := col_weight_sum(state, grid_def.col_count)
		cum_x := workspace.pos.x
		for ci in 0 ..< grid_def.col_count - 1 {
			if ci > 0 do cum_x += gap
			cum_x += total_w_detect * (state.custom_grid_def.col_weights[ci] / cw_sum_detect)
			border_x := cum_x
			hit := ui.Rect{pos = {border_x - RESIZE_HIT_W * 0.5, workspace.pos.y}, size = {RESIZE_HIT_W, workspace.size.y}}
			if ui.rect_contains(hit, pointer.pos) {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = {pos = {border_x - 1, workspace.pos.y}, size = {2, workspace.size.y}},
					color = ui.with_alpha(ui.COL_BLUE, 0.35),
				})
				if pointer.left_pressed {
					state.grid_col_resize = ci
				}
				break
			}
		}
	}
}

@(private = "package")
update_grid_row_resize :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input, grid_def: ui.Grid_Def, grid: ui.Grid_Result, gap: f32) {
	if grid_def.row_count < 2 do return

	RESIZE_HIT_H :: f32(6)
	if state.grid_row_resize >= 0 {
		// Active resize drag.
		if pointer.left_down {
			ri := state.grid_row_resize
			total_h := workspace.size.y - gap * f32(grid_def.row_count - 1)
			if total_h > 0 {
				// S63: Cache weight sum once per frame (was called 4x).
				s := row_weight_sum(state, grid_def.row_count)
				top_y := workspace.pos.y
				for r in 0 ..< ri {
					top_y += total_h * (state.custom_grid_def.row_weights[r] / s) + gap
				}
				new_top_h := pointer.pos.y - top_y
				bottom_edge := top_y + total_h * (state.custom_grid_def.row_weights[ri] / s) + gap + total_h * (state.custom_grid_def.row_weights[ri + 1] / s)
				new_bottom_h := bottom_edge - pointer.pos.y - gap
				min_h := total_h * 0.06
				if new_top_h >= min_h && new_bottom_h >= min_h {
					state.custom_grid_def.row_weights[ri]     = (new_top_h / total_h) * s
					state.custom_grid_def.row_weights[ri + 1] = (new_bottom_h / total_h) * s
				}
			}
		} else {
			state.grid_row_resize = -1
			persist_row_weights(state, grid_def.row_count)
		}
	} else {
		// Detect hover on row borders.
		for ri in 0 ..< grid_def.row_count - 1 {
			border_y := f32(0)
			found_border := false
			for gi in 0 ..< grid_def.cell_count {
				gc := grid_def.cells[gi]
				if gc.row == ri && gc.row_span == 1 {
					border_y = ui.rect_bottom(grid.rects[gi])
					found_border = true
					break
				}
			}
			if !found_border do continue
			hit := ui.Rect{pos = {workspace.pos.x, border_y - RESIZE_HIT_H * 0.5}, size = {workspace.size.x, RESIZE_HIT_H}}
			if ui.rect_contains(hit, pointer.pos) {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = {pos = {workspace.pos.x, border_y - 1}, size = {workspace.size.x, 2}},
					color = ui.with_alpha(ui.COL_BLUE, 0.35),
				})
				if pointer.left_pressed {
					state.grid_row_resize = ri
				}
				break
			}
		}
	}
}
