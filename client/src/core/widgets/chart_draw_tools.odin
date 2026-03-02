package widgets

// Chart draw tools — user-placed annotations on the candle chart.
// Horizontal price lines: double-click to add.
// Rectangle zones: Shift+drag to create.
// Color palette: 8 colors, applies to new and selected tools.
// Persistence: hlines serialized to settings store; rects are session-only.

import "core:fmt"
import "mr:ui"

MAX_DRAW_HLINES :: 16
MAX_DRAW_RECTS  :: 8

// 8-color palette for annotations.
DRAW_PALETTE :: [8]ui.Color{
	{1.0, 1.0, 1.0, 1.0},    // 0: white
	{1.0, 0.3, 0.3, 1.0},    // 1: red
	{0.3, 1.0, 0.3, 1.0},    // 2: green
	{0.4, 0.6, 1.0, 1.0},    // 3: blue
	{1.0, 1.0, 0.3, 1.0},    // 4: yellow
	{0.3, 1.0, 1.0, 1.0},    // 5: cyan
	{1.0, 0.3, 1.0, 1.0},    // 6: magenta
	{1.0, 0.6, 0.2, 1.0},    // 7: orange
}

SWATCH_SIZE :: f32(12)
SWATCH_GAP  :: f32(3)

Draw_Tool_HLine :: struct {
	price:       f64,
	palette_idx: int,
	selected:    bool,
	active:      bool,
}

Draw_Tool_Rect :: struct {
	price_top:   f64,
	price_bot:   f64,
	idx_left:    int,
	idx_right:   int,
	palette_idx: int,
	selected:    bool,
	active:      bool,
}

Draw_Tools_State :: struct {
	hlines:      [MAX_DRAW_HLINES]Draw_Tool_HLine,
	hline_count: int,
	rects:       [MAX_DRAW_RECTS]Draw_Tool_Rect,
	rect_count:  int,
	palette_idx: int, // active color index (0-7)
	// Shift+drag rect creation.
	rect_creating:    bool,
	rect_start_price: f64,
	rect_start_idx:   int,
	// Double-click detection.
	last_click_ms: i64,
	last_click_y:  f32,
	// Persistence.
	dirty: bool,
}

// Render all draw tools and handle interaction.
render_draw_tools :: proc(
	buf: ^ui.Command_Buffer,
	ctx: ^Chart_Layer_Context,
	state: ^Draw_Tools_State,
	pointer: ui.Pointer_Input,
	now_ms: i64,
	measure_proc: proc(size: f32, text: string) -> ui.Vec2,
	shift_held: bool = false,
) {
	if state == nil do return

	chart_left := ctx.chart_rect.pos.x
	chart_right := chart_left + ctx.chart_rect.size.x
	chart_top := ctx.chart_rect.pos.y
	chart_bot := chart_top + ctx.price_height

	clicked_tool := false

	// --- Render + select hlines ---
	for i in 0 ..< state.hline_count {
		hl := &state.hlines[i]
		if !hl.active do continue
		if hl.price < ctx.price_min || hl.price > ctx.price_max do continue

		y := chart_price_to_y(ctx, hl.price)
		if y < chart_top || y > chart_bot do continue

		pal := DRAW_PALETTE
		color := pal[clamp(hl.palette_idx, 0, 7)]
		line_alpha := hl.selected ? f32(1.0) : f32(0.6)
		thickness := hl.selected ? f32(2) : f32(1)

		// Dashed line.
		dash := f32(8)
		gap := f32(4)
		x := chart_left
		for x < chart_right {
			x_end := min(x + dash, chart_right)
			ui.push(buf, ui.Cmd_Line{
				from      = {x, y},
				to        = {x_end, y},
				color     = ui.with_alpha(color, line_alpha),
				thickness = thickness,
			})
			x += dash + gap
		}

		// Price label.
		price_buf: [16]u8
		price_str := fmt.bprintf(price_buf[:], "%.2f", hl.price)
		label_w := measure_proc(ui.FONT_SIZE_XS, price_str).x + 6
		label_h := f32(14)
		label_x := chart_right - label_w - 2
		label_y := y - label_h * 0.5
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {label_x, label_y}, size = {label_w, label_h}},
			color = ui.with_alpha(color, 0.7),
		})
		ui.push_text(buf, {label_x + 3, label_y + label_h - 3}, price_str,
			ui.COL_BLACK, ui.FONT_SIZE_XS, .Mono)

		// Click to select/deselect.
		if pointer.left_pressed && !clicked_tool {
			in_line := pointer.pos.x >= chart_left && pointer.pos.x <= chart_right &&
				pointer.pos.y >= y - 4 && pointer.pos.y <= y + 4
			if in_line {
				was_selected := hl.selected
				deselect_all(state)
				hl.selected = !was_selected
				clicked_tool = true
			}
		}
	}

	// --- Render + select rects ---
	for i in 0 ..< state.rect_count {
		r := &state.rects[i]
		if !r.active do continue

		x1 := chart_index_to_x(ctx, r.idx_left) - ctx.slot_width * 0.5
		x2 := chart_index_to_x(ctx, r.idx_right) + ctx.slot_width * 0.5
		y1 := chart_price_to_y(ctx, r.price_top)
		y2 := chart_price_to_y(ctx, r.price_bot)

		// Clip to chart area.
		rx := max(x1, chart_left)
		ry := max(y1, chart_top)
		rw := min(x2, chart_right) - rx
		rh := min(y2, chart_bot) - ry
		if rw <= 0 || rh <= 0 do continue

		pal := DRAW_PALETTE
		color := pal[clamp(r.palette_idx, 0, 7)]
		fill_alpha := r.selected ? f32(0.2) : f32(0.1)
		border_alpha := r.selected ? f32(1.0) : f32(0.5)
		border_thick := r.selected ? f32(2) : f32(1)

		screen_rect := ui.Rect{pos = {rx, ry}, size = {rw, rh}}
		ui.push(buf, ui.Cmd_Rect_Filled{rect = screen_rect, color = ui.with_alpha(color, fill_alpha)})
		ui.draw_rect_stroke(buf, screen_rect, ui.with_alpha(color, border_alpha), border_thick)

		// Click to select.
		if pointer.left_pressed && !clicked_tool {
			if pointer.pos.x >= rx && pointer.pos.x <= rx + rw &&
				pointer.pos.y >= ry && pointer.pos.y <= ry + rh {
				was_selected := r.selected
				deselect_all(state)
				r.selected = !was_selected
				clicked_tool = true
			}
		}
	}

	// --- Color palette (when any tool is selected) ---
	has_sel := has_selection(state)
	if has_sel {
		render_palette(buf, ctx, state, pointer, &clicked_tool)
	}

	in_chart := pointer.pos.x >= chart_left && pointer.pos.x <= chart_right &&
		pointer.pos.y >= chart_top && pointer.pos.y <= chart_bot

	// --- Shift+drag rect creation ---
	if state.rect_creating {
		if pointer.left_released || !pointer.left_down {
			// Finalize rect.
			cur_price := price_at_y(ctx, pointer.pos.y)
			cur_idx := idx_at_x(ctx, pointer.pos.x)

			p_top := max(state.rect_start_price, cur_price)
			p_bot := min(state.rect_start_price, cur_price)
			i_left := min(state.rect_start_idx, cur_idx)
			i_right := max(state.rect_start_idx, cur_idx)

			// Only create if price span is meaningful.
			if p_top - p_bot > ctx.price_range * 0.002 && state.rect_count < MAX_DRAW_RECTS {
				state.rects[state.rect_count] = Draw_Tool_Rect{
					price_top   = p_top,
					price_bot   = p_bot,
					idx_left    = i_left,
					idx_right   = i_right,
					palette_idx = state.palette_idx,
					selected    = false,
					active      = true,
				}
				state.rect_count += 1
				state.dirty = true
			}
			state.rect_creating = false
		} else {
			// Preview rect while dragging.
			render_rect_preview(buf, ctx, state, pointer)
		}
	} else if !clicked_tool && shift_held && pointer.left_pressed && in_chart {
		// Start rect creation.
		state.rect_creating = true
		state.rect_start_price = price_at_y(ctx, pointer.pos.y)
		state.rect_start_idx = idx_at_x(ctx, pointer.pos.x)
		deselect_all(state)
	}

	// --- Double-click detection: add new hline ---
	if !clicked_tool && !state.rect_creating && pointer.left_pressed && in_chart {
		if now_ms > 0 && state.last_click_ms > 0 {
			dt := now_ms - state.last_click_ms
			dy := pointer.pos.y - state.last_click_y
			if dy < 0 do dy = -dy
			if dt < 350 && dy < 8 {
				if state.hline_count < MAX_DRAW_HLINES {
					state.hlines[state.hline_count] = Draw_Tool_HLine{
						price       = price_at_y(ctx, pointer.pos.y),
						palette_idx = state.palette_idx,
						selected    = false,
						active      = true,
					}
					state.hline_count += 1
					state.dirty = true
				}
				state.last_click_ms = 0
			} else {
				state.last_click_ms = now_ms
				state.last_click_y = pointer.pos.y
			}
		} else {
			state.last_click_ms = now_ms
			state.last_click_y = pointer.pos.y
		}
	}
}

// Remove the currently selected tool (hline or rect).
remove_selected_tool :: proc(state: ^Draw_Tools_State) {
	if state == nil do return
	for i in 0 ..< state.hline_count {
		if state.hlines[i].selected && state.hlines[i].active {
			for j in i ..< state.hline_count - 1 {
				state.hlines[j] = state.hlines[j + 1]
			}
			state.hline_count -= 1
			state.dirty = true
			return
		}
	}
	for i in 0 ..< state.rect_count {
		if state.rects[i].selected && state.rects[i].active {
			for j in i ..< state.rect_count - 1 {
				state.rects[j] = state.rects[j + 1]
			}
			state.rect_count -= 1
			state.dirty = true
			return
		}
	}
}

// --- Serialization (hlines only, rects are session-only) ---

// Serialize hlines to a settings-compatible string.
// Format: "h:<palette_idx>:<price>;h:<palette_idx>:<price>;..."
draw_tools_serialize :: proc(state: ^Draw_Tools_State, buf: []u8) -> string {
	if state == nil do return ""
	pos := 0
	first := true
	for i in 0 ..< state.hline_count {
		hl := &state.hlines[i]
		if !hl.active do continue
		if !first && pos < len(buf) {
			buf[pos] = ';'
			pos += 1
		}
		s := fmt.bprintf(buf[pos:], "h:%d:%.8f", hl.palette_idx, hl.price)
		pos += len(s)
		first = false
	}
	return string(buf[:pos])
}

// Deserialize hlines from a settings string.
draw_tools_deserialize :: proc(state: ^Draw_Tools_State, data: string) {
	if state == nil do return
	state.hline_count = 0
	state.rect_count = 0
	state.dirty = false
	if len(data) == 0 do return

	rest := data
	for len(rest) > 0 {
		end := 0
		for end < len(rest) && rest[end] != ';' {
			end += 1
		}
		entry := rest[:end]
		rest = end < len(rest) ? rest[end + 1:] : ""

		if len(entry) >= 5 && entry[0] == 'h' && entry[1] == ':' {
			parse_hline_entry(state, entry[2:])
		}
	}
}

// --- Internal helpers ---

@(private = "file")
has_selection :: proc(state: ^Draw_Tools_State) -> bool {
	for i in 0 ..< state.hline_count {
		if state.hlines[i].selected do return true
	}
	for i in 0 ..< state.rect_count {
		if state.rects[i].selected do return true
	}
	return false
}

@(private = "file")
deselect_all :: proc(state: ^Draw_Tools_State) {
	for i in 0 ..< state.hline_count { state.hlines[i].selected = false }
	for i in 0 ..< state.rect_count { state.rects[i].selected = false }
}

@(private = "file")
price_at_y :: proc(ctx: ^Chart_Layer_Context, y: f32) -> f64 {
	y_pct := f64(y - ctx.chart_rect.pos.y) / f64(ctx.price_height)
	return ctx.price_max - y_pct * ctx.price_range
}

@(private = "file")
idx_at_x :: proc(ctx: ^Chart_Layer_Context, x: f32) -> int {
	idx := ctx.candle_start + int((x - ctx.chart_rect.pos.x) / ctx.slot_width)
	return clamp(idx, ctx.candle_start, ctx.candle_start + ctx.candle_count - 1)
}

@(private = "file")
render_palette :: proc(
	buf: ^ui.Command_Buffer,
	ctx: ^Chart_Layer_Context,
	state: ^Draw_Tools_State,
	pointer: ui.Pointer_Input,
	clicked_tool: ^bool,
) {
	total_w := f32(8) * SWATCH_SIZE + f32(7) * SWATCH_GAP
	px := ctx.chart_rect.pos.x + ctx.chart_rect.size.x - total_w - 8
	py := ctx.chart_rect.pos.y + 6

	// Background bar.
	bar := ui.Rect{pos = {px - 4, py - 4}, size = {total_w + 8, SWATCH_SIZE + 8}}
	ui.push(buf, ui.Cmd_Rect_Filled{rect = bar, color = ui.with_alpha(ui.COL_BLACK, 0.7)})

	for i in 0 ..< 8 {
		sx := px + f32(i) * (SWATCH_SIZE + SWATCH_GAP)
		swatch := ui.Rect{pos = {sx, py}, size = {SWATCH_SIZE, SWATCH_SIZE}}

		pal := DRAW_PALETTE
		ui.push(buf, ui.Cmd_Rect_Filled{rect = swatch, color = pal[i]})

		// Active indicator.
		if state.palette_idx == i {
			ui.draw_rect_stroke(buf, swatch, ui.COL_WHITE)
		}

		// Click to select color.
		if pointer.left_pressed && !clicked_tool^ {
			if ui.rect_contains(swatch, pointer.pos) {
				state.palette_idx = i
				// Apply color to selected tools.
				for j in 0 ..< state.hline_count {
					if state.hlines[j].selected {
						state.hlines[j].palette_idx = i
						state.dirty = true
					}
				}
				for j in 0 ..< state.rect_count {
					if state.rects[j].selected {
						state.rects[j].palette_idx = i
						state.dirty = true
					}
				}
				clicked_tool^ = true
			}
		}
	}
}

@(private = "file")
render_rect_preview :: proc(
	buf: ^ui.Command_Buffer,
	ctx: ^Chart_Layer_Context,
	state: ^Draw_Tools_State,
	pointer: ui.Pointer_Input,
) {
	cur_price := price_at_y(ctx, pointer.pos.y)
	cur_idx := idx_at_x(ctx, pointer.pos.x)

	p_top := max(state.rect_start_price, cur_price)
	p_bot := min(state.rect_start_price, cur_price)
	i_left := min(state.rect_start_idx, cur_idx)
	i_right := max(state.rect_start_idx, cur_idx)

	chart_left := ctx.chart_rect.pos.x
	chart_right := chart_left + ctx.chart_rect.size.x
	chart_top := ctx.chart_rect.pos.y
	chart_bot := chart_top + ctx.price_height

	x1 := chart_index_to_x(ctx, i_left) - ctx.slot_width * 0.5
	x2 := chart_index_to_x(ctx, i_right) + ctx.slot_width * 0.5
	y1 := chart_price_to_y(ctx, p_top)
	y2 := chart_price_to_y(ctx, p_bot)

	rx := max(x1, chart_left)
	ry := max(y1, chart_top)
	rw := min(x2, chart_right) - rx
	rh := min(y2, chart_bot) - ry
	if rw <= 0 || rh <= 0 do return

	pal := DRAW_PALETTE
	color := pal[clamp(state.palette_idx, 0, 7)]
	preview := ui.Rect{pos = {rx, ry}, size = {rw, rh}}
	ui.push(buf, ui.Cmd_Rect_Filled{rect = preview, color = ui.with_alpha(color, 0.15)})
	ui.draw_rect_stroke(buf, preview, ui.with_alpha(color, 0.6))
}

@(private = "file")
parse_hline_entry :: proc(state: ^Draw_Tools_State, s: string) {
	if state.hline_count >= MAX_DRAW_HLINES do return
	// Format: "<palette_idx>:<price>"
	sep := -1
	for i in 0 ..< len(s) {
		if s[i] == ':' { sep = i; break }
	}
	if sep < 0 do return

	pidx, ok1 := parse_draw_int(s[:sep])
	if !ok1 do return
	price, ok2 := parse_draw_f64(s[sep + 1:])
	if !ok2 do return

	state.hlines[state.hline_count] = Draw_Tool_HLine{
		price       = price,
		palette_idx = clamp(pidx, 0, 7),
		selected    = false,
		active      = true,
	}
	state.hline_count += 1
}

@(private = "file")
parse_draw_int :: proc(s: string) -> (int, bool) {
	if len(s) == 0 do return 0, false
	neg := false
	i := 0
	if s[0] == '-' { neg = true; i = 1 }
	val := 0
	for i < len(s) {
		if s[i] < '0' || s[i] > '9' do return 0, false
		val = val * 10 + int(s[i] - '0')
		i += 1
	}
	if neg do val = -val
	return val, true
}

@(private = "file")
parse_draw_f64 :: proc(s: string) -> (f64, bool) {
	if len(s) == 0 do return 0, false
	neg := false
	i := 0
	if s[0] == '-' { neg = true; i = 1 }
	else if s[0] == '+' { i = 1 }

	whole: f64 = 0
	for i < len(s) && s[i] != '.' {
		if s[i] < '0' || s[i] > '9' do return 0, false
		whole = whole * 10 + f64(s[i] - '0')
		i += 1
	}
	frac: f64 = 0
	if i < len(s) && s[i] == '.' {
		i += 1
		scale: f64 = 0.1
		for i < len(s) {
			if s[i] < '0' || s[i] > '9' do return 0, false
			frac += f64(s[i] - '0') * scale
			scale *= 0.1
			i += 1
		}
	}
	val := whole + frac
	if neg do val = -val
	return val, true
}
