package widgets

// Chart draw tools — user-placed annotations on the candle chart.
// Data management + persistence only. Rendering is handled by
// the layers pipeline (render_draw_tools was removed in S9).

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

// Serialize hlines to a settings string.
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
parse_hline_entry :: proc(state: ^Draw_Tools_State, s: string) {
	if state.hline_count >= MAX_DRAW_HLINES do return
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
