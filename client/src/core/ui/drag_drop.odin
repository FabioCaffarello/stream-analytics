package ui

// Panel drag-drop — swap two panels by long-pressing a panel header and dragging to another.
// State machine: Idle -> Dragging -> Hovering_Drop -> (apply swap or cancel).

Drag_Phase :: enum u8 {
	Idle,
	Dragging,
	Hovering_Drop,
}

Panel_Drag_State :: struct {
	phase:        Drag_Phase,
	source_panel: int,         // PANEL_* index being dragged
	drag_offset:  Vec2,        // offset from panel origin to grab point
	start_pos:    Vec2,        // where the drag started
	current_pos:  Vec2,        // current pointer position
	drop_target:  int,         // PANEL_* index under cursor, -1 if none
	hold_start_ms: i64,        // when the press started (for long-press)
	hold_threshold_ms: i64,    // ms needed to trigger drag (default 300)
}

DRAG_HOLD_THRESHOLD_MS :: i64(300)
DRAG_MIN_DISTANCE      :: f32(8) // min pixels moved before drag triggers

init_drag_state :: proc(state: ^Panel_Drag_State) {
	state.phase = .Idle
	state.source_panel = -1
	state.drop_target = -1
	state.hold_threshold_ms = DRAG_HOLD_THRESHOLD_MS
}

// Find which panel the pointer is over based on the computed grid rects.
// Returns PANEL_* index or -1 if not over any panel header.
find_panel_at :: proc(
	grid_rects: [GRID_MAX_CELLS]Rect,
	panel_visible: [PANEL_COUNT]bool,
	pos: Vec2,
	header_h: f32,
) -> int {
	cell_idx := 0
	for i in 0 ..< PANEL_COUNT {
		if !panel_visible[i] {
			continue
		}
		r := grid_rects[cell_idx]
		header_rect := Rect{pos = r.pos, size = {r.size.x, header_h}}
		if rect_contains(header_rect, pos) {
			return i
		}
		cell_idx += 1
	}
	return -1
}

// Find which panel cell the pointer is over (full cell, not just header).
find_drop_target :: proc(
	grid_rects: [GRID_MAX_CELLS]Rect,
	panel_visible: [PANEL_COUNT]bool,
	pos: Vec2,
	source: int,
) -> int {
	cell_idx := 0
	for i in 0 ..< PANEL_COUNT {
		if !panel_visible[i] {
			continue
		}
		if i == source {
			cell_idx += 1
			continue
		}
		if rect_contains(grid_rects[cell_idx], pos) {
			return i
		}
		cell_idx += 1
	}
	return -1
}

// Apply a panel swap: exchange positions of panels a and b in the grid definition.
apply_panel_swap :: proc(
	grid_def: ^Grid_Def,
	a: int,
	b: int,
) {
	if a < 0 || a >= PANEL_COUNT || b < 0 || b >= PANEL_COUNT do return
	if a == b do return
	// Swap the cell definitions.
	tmp := grid_def.cells[a]
	grid_def.cells[a] = grid_def.cells[b]
	grid_def.cells[b] = tmp
}

// Update drag state machine given current pointer input and time.
// Returns true if a swap was just applied.
update_drag :: proc(
	state: ^Panel_Drag_State,
	grid_rects: [GRID_MAX_CELLS]Rect,
	panel_visible: [PANEL_COUNT]bool,
	pointer: Pointer_Input,
	now_ms: i64,
	header_h: f32,
) -> (swapped: bool, swap_a: int, swap_b: int) {
	swapped = false
	swap_a = -1
	swap_b = -1

	switch state.phase {
	case .Idle:
		if pointer.left_pressed {
			panel := find_panel_at(grid_rects, panel_visible, pointer.pos, header_h)
			if panel >= 0 {
				state.hold_start_ms = now_ms
				state.start_pos = pointer.pos
				state.source_panel = panel
			}
		}
		// Check for long-press transition.
		if pointer.left_down && state.source_panel >= 0 {
			elapsed := now_ms - state.hold_start_ms
			dist_x := pointer.pos.x - state.start_pos.x
			dist_y := pointer.pos.y - state.start_pos.y
			if dist_x < 0 do dist_x = -dist_x
			if dist_y < 0 do dist_y = -dist_y
			dist := dist_x + dist_y
			if elapsed >= state.hold_threshold_ms {
				state.phase = .Dragging
				state.current_pos = pointer.pos
				state.drag_offset = {pointer.pos.x - state.start_pos.x, pointer.pos.y - state.start_pos.y}
			}
		}
		if !pointer.left_down {
			state.source_panel = -1
		}

	case .Dragging:
		state.current_pos = pointer.pos
		state.drop_target = find_drop_target(grid_rects, panel_visible, pointer.pos, state.source_panel)
		if state.drop_target >= 0 {
			state.phase = .Hovering_Drop
		}
		if !pointer.left_down {
			// Cancelled — released without valid target.
			init_drag_state(state)
		}

	case .Hovering_Drop:
		state.current_pos = pointer.pos
		state.drop_target = find_drop_target(grid_rects, panel_visible, pointer.pos, state.source_panel)
		if state.drop_target < 0 {
			state.phase = .Dragging
		}
		if !pointer.left_down {
			if state.drop_target >= 0 {
				// Swap panels.
				swapped = true
				swap_a = state.source_panel
				swap_b = state.drop_target
			}
			init_drag_state(state)
		}
	}

	return
}

// Render drag visual feedback: source panel ghost + drop target highlight.
draw_drag_feedback :: proc(
	buf: ^Command_Buffer,
	state: ^Panel_Drag_State,
	grid_rects: [GRID_MAX_CELLS]Rect,
	panel_visible: [PANEL_COUNT]bool,
) {
	if state.phase == .Idle do return

	// Source panel at 50% opacity.
	src_cell := panel_cell_idx(panel_visible, state.source_panel)
	if src_cell >= 0 {
		r := grid_rects[src_cell]
		push(buf, Cmd_Rect_Filled{
			rect  = r,
			color = with_alpha(COL_BLUE, 0.08),
		})
		draw_rect_stroke(buf, r, with_alpha(COL_BLUE, 0.25), 2)
	}

	// Ghost outline at cursor.
	if src_cell >= 0 {
		r := grid_rects[src_cell]
		ghost := Rect{
			pos  = {state.current_pos.x - r.size.x * 0.5, state.current_pos.y - 12},
			size = {r.size.x, 24},
		}
		push(buf, Cmd_Rect_Filled{rect = ghost, color = with_alpha(COL_BLUE, 0.15)})
		draw_rect_stroke(buf, ghost, with_alpha(COL_BLUE, 0.4))
	}

	// Drop target highlight.
	if state.drop_target >= 0 {
		tgt_cell := panel_cell_idx(panel_visible, state.drop_target)
		if tgt_cell >= 0 {
			r := grid_rects[tgt_cell]
			push(buf, Cmd_Rect_Filled{
				rect  = r,
				color = with_alpha(COL_BLUE, 0.12),
			})
			draw_rect_stroke(buf, r, COL_BLUE, 2)
		}
	}
}

// Helper: find the cell index for a given panel index.
@(private = "file")
panel_cell_idx :: proc(visible: [PANEL_COUNT]bool, panel_idx: int) -> int {
	if panel_idx < 0 || panel_idx >= PANEL_COUNT do return -1
	if !visible[panel_idx] do return -1
	cell := 0
	for i in 0 ..< panel_idx {
		if visible[i] do cell += 1
	}
	return cell
}
