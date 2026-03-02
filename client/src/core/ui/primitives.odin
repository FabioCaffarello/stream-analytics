package ui

// Reusable UI primitives: panel, scroll area, table layout.
// No ports import — accepts raw f32 values and proc pointers to avoid circular deps.

import "core:math"

// --- Panel ---

Panel_Config :: struct {
	title:        string,
	title_height: f32,   // caller pre-computes via text.line_height()
	bg_color:     Color,
	pad:          f32,
}

// Draw a panel background + title + separator. Returns the inner content rect.
panel :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	cfg: Panel_Config,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32,
) -> Rect {
	// Background.
	push(buf, Cmd_Rect_Filled{rect = rect, color = cfg.bg_color})

	inner := rect_pad(rect, cfg.pad)

	if len(cfg.title) > 0 {
		hdr_h := cfg.title_height + 4
		title_y := inner.pos.y + cfg.title_height
		push_text(buf, {inner.pos.x, title_y}, cfg.title,
			with_alpha(COL_WHITE, 0.5), font_size, .Mono)

		// Separator line.
		sep_y := inner.pos.y + hdr_h
		push(buf, Cmd_Line{
			from      = {inner.pos.x, sep_y},
			to        = {rect_right(inner), sep_y},
			color     = with_alpha(COL_WHITE, 0.15),
			thickness = 1,
		})

		inner.pos.y  += hdr_h + 2
		inner.size.y -= hdr_h + 2
	}

	return inner
}

// --- Panel V2: header bar with title + optional inline controls area ---

Panel_V2_Config :: struct {
	title:        string,
	title_height: f32,
	bg_color:     Color,
	pad:          f32,
	header_h:     f32,   // total header height (0 = auto from title_height)
}

// Draw a panel with a proper header bar. Returns (inner content rect, header control rect).
// The control_rect is the area to the right of the title within the header — caller can
// render segmented_control, toggle, etc. into it.
panel_v2 :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	cfg: Panel_V2_Config,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32,
) -> (inner: Rect, control_rect: Rect) {
	push(buf, Cmd_Rect_Filled{rect = rect, color = cfg.bg_color})

	// Panel border (4 edges).
	push(buf, Cmd_Line{from = {rect.pos.x, rect.pos.y}, to = {rect_right(rect), rect.pos.y}, color = COL_BORDER_SUBTLE, thickness = 1})
	push(buf, Cmd_Line{from = {rect_right(rect), rect.pos.y}, to = {rect_right(rect), rect_bottom(rect)}, color = COL_BORDER_SUBTLE, thickness = 1})
	push(buf, Cmd_Line{from = {rect_right(rect), rect_bottom(rect)}, to = {rect.pos.x, rect_bottom(rect)}, color = COL_BORDER_SUBTLE, thickness = 1})
	push(buf, Cmd_Line{from = {rect.pos.x, rect_bottom(rect)}, to = {rect.pos.x, rect.pos.y}, color = COL_BORDER_SUBTLE, thickness = 1})

	padded := rect_pad(rect, cfg.pad)

	hdr_h := cfg.header_h
	if hdr_h <= 0 {
		hdr_h = max(cfg.title_height + 8, 26) // 26px min header height
	}

	if len(cfg.title) > 0 {
		// Header background (elevated surface).
		hdr_rect := Rect{pos = padded.pos, size = {padded.size.x, hdr_h}}
		push(buf, Cmd_Rect_Filled{rect = hdr_rect, color = with_alpha(COL_WHITE, 0.03)})

		// Top accent line (2px, blue at 0.3 alpha).
		push(buf, Cmd_Rect_Filled{
			rect  = {pos = {padded.pos.x, padded.pos.y}, size = {padded.size.x, 2}},
			color = with_alpha(COL_BLUE, 0.3),
		})

		// Title with 8px left padding.
		title_y := padded.pos.y + cfg.title_height + 2
		push_text(buf, {padded.pos.x + SPACING_MD, title_y}, cfg.title,
			COL_TEXT_SECONDARY, font_size, .Mono)

		// Separator line below header.
		sep_y := padded.pos.y + hdr_h
		push(buf, Cmd_Line{
			from      = {padded.pos.x, sep_y},
			to        = {rect_right(padded), sep_y},
			color     = COL_DIVIDER,
			thickness = 1,
		})

		// Control area: right side of header, leaving title space.
		title_w := measure_proc(font_size, cfg.title).x + 16
		control_rect = Rect{
			pos  = {padded.pos.x + title_w, padded.pos.y + 1},
			size = {padded.size.x - title_w - 4, hdr_h - 2},
		}

		inner = Rect{
			pos  = {padded.pos.x, sep_y + 2},
			size = {padded.size.x, padded.size.y - hdr_h - 2},
		}
	} else {
		inner = padded
		control_rect = {}
	}

	return
}

// --- Scroll Area ---

Scroll_State :: struct {
	offset_y: f32,
}

// Begin a scrollable region. Pushes a clip rect. Returns the visible content rect.
// Caller must call scroll_area_end() after emitting content.
scroll_area_begin :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	content_h: f32,
	state: ^Scroll_State,
	mouse_pos: Vec2,
	scroll_delta: f32,
	row_h: f32,
) -> (visible: Rect, scroll_offset: f32) {
	// Handle scrolling when mouse is over the area.
	if rect_contains(rect, mouse_pos) {
		state.offset_y -= scroll_delta * row_h * 3
	}
	max_scroll := math.max(content_h - rect.size.y, 0)
	state.offset_y = clamp(state.offset_y, 0, max_scroll)

	push(buf, Cmd_Clip_Push{rect = rect})
	return rect, state.offset_y
}

// End a scrollable region. Pops the clip rect.
scroll_area_end :: proc(buf: ^Command_Buffer) {
	push(buf, Cmd_Clip_Pop{})
}

// --- Table Layout ---

MAX_TABLE_COLS :: 8

Table_Layout :: struct {
	col_widths: [MAX_TABLE_COLS]f32,
	col_count:  int,
	row_h:      f32,
	x_origin:   f32,
	y_cursor:   f32,
	gap:        f32,
}

// Create a table layout. col_widths slice is copied into fixed array.
table_begin :: proc(rect: Rect, col_widths: []f32, row_h: f32, gap: f32 = 0) -> Table_Layout {
	tbl: Table_Layout
	tbl.col_count = min(len(col_widths), MAX_TABLE_COLS)
	for i in 0 ..< tbl.col_count {
		tbl.col_widths[i] = col_widths[i]
	}
	tbl.row_h    = row_h
	tbl.x_origin = rect.pos.x
	tbl.y_cursor = rect.pos.y
	tbl.gap      = gap
	return tbl
}

// Advance to the next row. Returns the row rect.
table_next_row :: proc(tbl: ^Table_Layout) -> Rect {
	total_w: f32 = 0
	for i in 0 ..< tbl.col_count {
		total_w += tbl.col_widths[i]
	}
	row := Rect{
		pos  = {tbl.x_origin, tbl.y_cursor},
		size = {total_w, tbl.row_h},
	}
	tbl.y_cursor += tbl.row_h + tbl.gap
	return row
}

// Get the rect for a specific column within the current row.
// Call after table_next_row — uses (y_cursor - row_h - gap) as row y.
table_cell :: proc(tbl: ^Table_Layout, col: int) -> Rect {
	if col >= tbl.col_count do return {}
	x := tbl.x_origin
	for i in 0 ..< col {
		x += tbl.col_widths[i]
	}
	row_y := tbl.y_cursor - tbl.row_h - tbl.gap
	return Rect{
		pos  = {x, row_y},
		size = {tbl.col_widths[col], tbl.row_h},
	}
}

// Convenience: get the text baseline position for a cell (bottom-aligned).
table_cell_text_pos :: proc(tbl: ^Table_Layout, col: int) -> Vec2 {
	cell := table_cell(tbl, col)
	return {cell.pos.x, cell.pos.y + cell.size.y - 2}
}
