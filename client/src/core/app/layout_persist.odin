package app

import "core:fmt"
import "mr:services"
import "mr:ui"

// ---------------------------------------------------------------------------
// Layout persistence V1-V4
// Extracted from stream_views.odin for cohesion.
// ---------------------------------------------------------------------------

// Persist cell layout to settings. Format: "N:K0,K1,...,KN-1" where Ki = Widget_Kind enum value.
persist_layout :: proc(state: ^App_State) {
	buf: [128]u8
	off := 0
	// Write cell count.
	n := state.world.count
	if n > 9 {
		buf[off] = '0' + u8(n / 10); off += 1
	}
	buf[off] = '0' + u8(n % 10); off += 1
	buf[off] = ':'; off += 1
	// Write widget kinds.
	for i in 0 ..< n {
		if i > 0 { buf[off] = ','; off += 1 }
		k := int(state.world.widgets[i].kind)
		buf[off] = '0' + u8(k); off += 1
	}
	services.settings_set(&state.settings, services.SETTING_LAYOUT, string(buf[:off]))
	// Persist preset index.
	preset_buf: [4]u8
	services.settings_set(&state.settings, services.SETTING_LAYOUT_PRESET,
		fmt.bprintf(preset_buf[:], "%d", state.layout_preset))
	services.settings_flush(&state.settings)
}

// Restore cell layout from settings. Returns true if layout was restored.
restore_layout :: proc(state: ^App_State) -> bool {
	v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT)
	if !ok || len(v) < 3 do return false
	return restore_layout_from_string(state, v)
}

// Parse and apply a layout string. Format: "N:K0,K1,...,KN-1".
restore_layout_from_string :: proc(state: ^App_State, v: string) -> bool {
	if len(v) < 3 do return false

	// Parse cell count (1-2 digits before ':').
	colon_idx := -1
	for i in 0 ..< len(v) {
		if v[i] == ':' { colon_idx = i; break }
	}
	if colon_idx <= 0 || colon_idx > 2 do return false

	n := 0
	for i in 0 ..< colon_idx {
		d := int(v[i]) - '0'
		if d < 0 || d > 9 do return false
		n = n * 10 + d
	}
	if n <= 0 || n > CELL_MAX do return false

	// Parse widget kinds after colon.
	rest := v[colon_idx + 1:]
	kinds: [CELL_MAX]Widget_Kind
	ki := 0
	for c in rest {
		if c == ',' do continue
		d := int(c) - '0'
		if d < 0 || d > 7 do return false
		if ki >= n do break
		kinds[ki] = Widget_Kind(d)
		ki += 1
	}
	if ki != n do return false

	// Apply.
	state.world.count = n
	for i in 0 ..< n {
		write_default_cell_to_world(state, i, kinds[i])
	}
	return true
}

// ===============================================================
// V2 persistence — captures stream binding + indicator flags.
// Format: "V2|K:S:F|K:S:F|..." (pipe-delimited cells)
//   K = widget kind digit (0-8)
//   S = stream binding: "-1" for follow-active, or "venue/symbol"
//   F = indicator flags bitfield (8 bools packed into decimal int)
// ===============================================================

// Pack 8 indicator booleans into a single integer.
pack_indicator_flags :: proc(ind: ^Indicator_Component) -> int {
	f := 0
	if ind.show_ma             do f |= 1 << 0
	if ind.show_bbands         do f |= 1 << 1
	if ind.show_vwap           do f |= 1 << 2
	if ind.show_rsi            do f |= 1 << 3
	if ind.show_macd           do f |= 1 << 4
	if ind.show_funding        do f |= 1 << 5
	if ind.show_liq            do f |= 1 << 6
	if ind.show_trade_counter  do f |= 1 << 7
	return f
}

// Unpack indicator flags into an Indicator_Component.
unpack_indicator_flags :: proc(ind: ^Indicator_Component, f: int) {
	ind.show_ma            = (f & (1 << 0)) != 0
	ind.show_bbands        = (f & (1 << 1)) != 0
	ind.show_vwap          = (f & (1 << 2)) != 0
	ind.show_rsi           = (f & (1 << 3)) != 0
	ind.show_macd          = (f & (1 << 4)) != 0
	ind.show_funding       = (f & (1 << 5)) != 0
	ind.show_liq           = (f & (1 << 6)) != 0
	ind.show_trade_counter = (f & (1 << 7)) != 0
}

persist_layout_v2 :: proc(state: ^App_State) {
	buf: [1024]u8
	off := 0

	// Header: "V2"
	buf[off] = 'V'; off += 1
	buf[off] = '2'; off += 1

	n := state.world.count
	for i in 0 ..< n {
		buf[off] = '|'; off += 1

		// K: widget kind digit.
		buf[off] = '0' + u8(state.world.widgets[i].kind); off += 1
		buf[off] = ':'; off += 1

		// S: stream binding.
		if state.world.bindings[i].stream_idx < 0 {
			buf[off] = '-'; off += 1
			buf[off] = '1'; off += 1
		} else {
			// Look up venue/symbol from stream_views.
			reg := state.stream_views
			wrote_stream := false
			si := state.world.bindings[i].stream_idx
			if reg != nil && si >= 0 && si < STREAM_VIEW_CAP && reg.slots[si].used {
				slot := &reg.slots[si]
				if !slot.has_stream_info {
					refresh_stream_info_for_slot(state, slot)
				}
				if slot.has_stream_info && len(slot.stream_info.venue) > 0 && len(slot.stream_info.symbol) > 0 {
					// Write venue/symbol.
					v := slot.stream_info.venue
					s := slot.stream_info.symbol
					for vi in 0 ..< len(v) {
						if off < len(buf) { buf[off] = v[vi]; off += 1 }
					}
					buf[off] = '/'; off += 1
					for si2 in 0 ..< len(s) {
						if off < len(buf) { buf[off] = s[si2]; off += 1 }
					}
					wrote_stream = true
				}
			}
			if !wrote_stream {
				buf[off] = '-'; off += 1
				buf[off] = '1'; off += 1
			}
		}
		buf[off] = ':'; off += 1

		// F: indicator flags.
		flags := pack_indicator_flags(&state.world.indicators[i])
		if flags >= 100 {
			buf[off] = '0' + u8(flags / 100); off += 1
		}
		if flags >= 10 {
			buf[off] = '0' + u8((flags / 10) % 10); off += 1
		}
		buf[off] = '0' + u8(flags % 10); off += 1
	}

	services.settings_set(&state.settings, services.SETTING_LAYOUT_V2, string(buf[:off]))
	// Also export V1 layout for rollback tooling.
	persist_layout(state)
}

// ===============================================================
// V4 persistence — extends V3 with per-cell timeframe.
// Format: "V4|MODE|CW:w0,w1,...|RW:w0,w1,...|K:S:F:CS:RS:SM:SR:TF|..."
//   TF = tf_idx+1 (0 = follow global, 1-9 = per-cell TF 0-8)
// ===============================================================

persist_layout_v4 :: proc(state: ^App_State) {
	buf: [2048]u8
	off := 0

	// Header: "V4"
	buf[off] = 'V'; off += 1
	buf[off] = '4'; off += 1

	// MODE.
	buf[off] = '|'; off += 1
	buf[off] = state.layout_mode == .Custom ? 'C' : 'P'; off += 1

	// CW: col weights.
	buf[off] = '|'; off += 1
	buf[off] = 'C'; off += 1
	buf[off] = 'W'; off += 1
	buf[off] = ':'; off += 1
	for c in 0 ..< state.custom_grid_def.col_count {
		if c > 0 { buf[off] = ','; off += 1 }
		w := int(state.custom_grid_def.col_weights[c] * 100 + 0.5)
		off = write_int_to_buf(buf[:], off, w)
	}

	// RW: row weights.
	buf[off] = '|'; off += 1
	buf[off] = 'R'; off += 1
	buf[off] = 'W'; off += 1
	buf[off] = ':'; off += 1
	for r in 0 ..< state.custom_grid_def.row_count {
		if r > 0 { buf[off] = ','; off += 1 }
		w := int(state.custom_grid_def.row_weights[r] * 100 + 0.5)
		off = write_int_to_buf(buf[:], off, w)
	}

	// Cells: K:S:F:CS:RS:SM:SR:TF per cell.
	n := state.world.count
	for i in 0 ..< n {
		buf[off] = '|'; off += 1

		// K: widget kind.
		buf[off] = '0' + u8(state.world.widgets[i].kind); off += 1
		buf[off] = ':'; off += 1

		// S: stream binding — read from cell's bound fields (PRD-0009).
		bv := binding_venue(&state.world.bindings[i])
		bs := binding_symbol(&state.world.bindings[i])
		if len(bv) > 0 && len(bs) > 0 {
			for vi in 0 ..< len(bv) { if off < len(buf) { buf[off] = bv[vi]; off += 1 } }
			buf[off] = '/'; off += 1
			for si in 0 ..< len(bs) { if off < len(buf) { buf[off] = bs[si]; off += 1 } }
		} else {
			buf[off] = '-'; off += 1
			buf[off] = '1'; off += 1
		}
		buf[off] = ':'; off += 1

		// F: indicator flags.
		flags := pack_indicator_flags(&state.world.indicators[i])
		off = write_int_to_buf(buf[:], off, flags)
		buf[off] = ':'; off += 1

		// CS: col_span.
		cs := state.world.spans[i].col_span > 1 ? state.world.spans[i].col_span : 1
		off = write_int_to_buf(buf[:], off, cs)
		buf[off] = ':'; off += 1

		// RS: row_span.
		rs := state.world.spans[i].row_span > 1 ? state.world.spans[i].row_span : 1
		off = write_int_to_buf(buf[:], off, rs)
		buf[off] = ':'; off += 1

		// SM: sub_main_split (x1000).
		sm := int(state.world.subplots[i].sub_main_split * 1000 + 0.5)
		off = write_int_to_buf(buf[:], off, sm)
		buf[off] = ':'; off += 1

		// SR: sub_ratios (x1000, comma-separated).
		for sri in 0 ..< 5 {
			if sri > 0 { buf[off] = ','; off += 1 }
			sr := int(state.world.subplots[i].sub_ratios[sri] * 1000 + 0.5)
			off = write_int_to_buf(buf[:], off, sr)
		}
		buf[off] = ':'; off += 1

		// TF: tf_idx+1 (0 = global, 1-9 = per-cell).
		tf_val := state.world.timeframes[i].tf_idx + 1
		if tf_val < 0 { tf_val = 0 }
		off = write_int_to_buf(buf[:], off, tf_val)
	}

	services.settings_set(&state.settings, services.SETTING_LAYOUT_V4, string(buf[:off]))
	services.settings_set(&state.settings, services.SETTING_LAYOUT_MODE,
		state.layout_mode == .Custom ? "C" : "P")
}

// ===============================================================
// V3 persistence — extends V2 with layout mode, weights, and spans.
// Format: "V3|MODE|CW:w0,w1,...|RW:w0,w1,...|K:S:F:CS:RS|..."
//   MODE = P (Preset) or C (Custom)
//   CW: = col weights (comma-sep integers, weight*100)
//   RW: = row weights (comma-sep integers, weight*100)
//   K:S:F:CS:RS = widget kind, stream, flags, col_span, row_span
// ===============================================================

persist_layout_v3 :: proc(state: ^App_State) {
	buf: [2048]u8
	off := 0

	// Header: "V3"
	buf[off] = 'V'; off += 1
	buf[off] = '3'; off += 1

	// MODE.
	buf[off] = '|'; off += 1
	buf[off] = state.layout_mode == .Custom ? 'C' : 'P'; off += 1

	// CW: col weights.
	buf[off] = '|'; off += 1
	buf[off] = 'C'; off += 1
	buf[off] = 'W'; off += 1
	buf[off] = ':'; off += 1
	for c in 0 ..< state.custom_grid_def.col_count {
		if c > 0 { buf[off] = ','; off += 1 }
		w := int(state.custom_grid_def.col_weights[c] * 100 + 0.5)
		off = write_int_to_buf(buf[:], off, w)
	}

	// RW: row weights.
	buf[off] = '|'; off += 1
	buf[off] = 'R'; off += 1
	buf[off] = 'W'; off += 1
	buf[off] = ':'; off += 1
	for r in 0 ..< state.custom_grid_def.row_count {
		if r > 0 { buf[off] = ','; off += 1 }
		w := int(state.custom_grid_def.row_weights[r] * 100 + 0.5)
		off = write_int_to_buf(buf[:], off, w)
	}

	// Cells: K:S:F:CS:RS per cell.
	n := state.world.count
	for i in 0 ..< n {
		buf[off] = '|'; off += 1

		// K: widget kind.
		buf[off] = '0' + u8(state.world.widgets[i].kind); off += 1
		buf[off] = ':'; off += 1

		// S: stream binding — read from cell's bound fields (PRD-0009).
		bv := binding_venue(&state.world.bindings[i])
		bs := binding_symbol(&state.world.bindings[i])
		if len(bv) > 0 && len(bs) > 0 {
			for vi in 0 ..< len(bv) { if off < len(buf) { buf[off] = bv[vi]; off += 1 } }
			buf[off] = '/'; off += 1
			for si in 0 ..< len(bs) { if off < len(buf) { buf[off] = bs[si]; off += 1 } }
		} else {
			buf[off] = '-'; off += 1
			buf[off] = '1'; off += 1
		}
		buf[off] = ':'; off += 1

		// F: indicator flags.
		flags := pack_indicator_flags(&state.world.indicators[i])
		off = write_int_to_buf(buf[:], off, flags)
		buf[off] = ':'; off += 1

		// CS: col_span.
		cs := state.world.spans[i].col_span > 1 ? state.world.spans[i].col_span : 1
		off = write_int_to_buf(buf[:], off, cs)
		buf[off] = ':'; off += 1

		// RS: row_span.
		rs := state.world.spans[i].row_span > 1 ? state.world.spans[i].row_span : 1
		off = write_int_to_buf(buf[:], off, rs)
		buf[off] = ':'; off += 1

		// SM: sub_main_split (x1000).
		sm := int(state.world.subplots[i].sub_main_split * 1000 + 0.5)
		off = write_int_to_buf(buf[:], off, sm)
		buf[off] = ':'; off += 1

		// SR: sub_ratios (x1000, comma-separated).
		for sri in 0 ..< 5 {
			if sri > 0 { buf[off] = ','; off += 1 }
			sr := int(state.world.subplots[i].sub_ratios[sri] * 1000 + 0.5)
			off = write_int_to_buf(buf[:], off, sr)
		}
	}

	services.settings_set(&state.settings, services.SETTING_LAYOUT_V3, string(buf[:off]))
	services.settings_set(&state.settings, services.SETTING_LAYOUT_MODE,
		state.layout_mode == .Custom ? "C" : "P")
	// Also export V2/V1 layouts for rollback tooling.
	persist_layout_v2(state)
}

// Helper: write a non-negative integer to buf at off, returns new off.
write_int_to_buf :: proc(buf: []u8, start: int, val: int) -> int {
	off := start
	v := val
	if v <= 0 {
		if off < len(buf) { buf[off] = '0'; off += 1 }
		return off
	}
	// Write digits in reverse, then flip.
	d_start := off
	for v > 0 && off < len(buf) {
		buf[off] = '0' + u8(v % 10)
		off += 1
		v /= 10
	}
	// Reverse the digits.
	lo := d_start
	hi := off - 1
	for lo < hi {
		buf[lo], buf[hi] = buf[hi], buf[lo]
		lo += 1
		hi -= 1
	}
	return off
}

// Parse a non-negative integer from a string.
parse_int_from :: proc(s: string) -> int {
	v := 0
	for c in s {
		if c < '0' || c > '9' do break
		v = v * 10 + int(c - '0')
	}
	return v
}

restore_layout_v3 :: proc(state: ^App_State) -> bool {
	v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V3)
	if !ok || len(v) < 4 do return false
	if v[0] != 'V' || v[1] != '3' do return false

	rest := v[2:]
	pos := 0

	// Parse MODE.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos >= len(rest) do return false
	mode_ch := rest[pos]
	pos += 1
	if mode_ch == 'C' {
		state.layout_mode = .Custom
	} else {
		state.layout_mode = .Preset
	}

	// Parse CW: col weights.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos + 2 >= len(rest) || rest[pos] != 'C' || rest[pos + 1] != 'W' || rest[pos + 2] != ':' do return false
	pos += 3
	cw_start := pos
	for pos < len(rest) && rest[pos] != '|' { pos += 1 }
	cw_field := rest[cw_start:pos]
	// Parse comma-separated col weights.
	col_idx := 0
	seg_start := 0
	for ci in 0 ..= len(cw_field) {
		if ci == len(cw_field) || cw_field[ci] == ',' {
			if ci > seg_start && col_idx < ui.GRID_MAX_COLS {
				w := parse_int_from(cw_field[seg_start:ci])
				state.custom_grid_def.col_weights[col_idx] = f32(w) / 100.0
				col_idx += 1
			}
			seg_start = ci + 1
		}
	}
	if col_idx > 0 { state.custom_grid_def.col_count = col_idx }

	// Parse RW: row weights.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos + 2 >= len(rest) || rest[pos] != 'R' || rest[pos + 1] != 'W' || rest[pos + 2] != ':' do return false
	pos += 3
	rw_start := pos
	for pos < len(rest) && rest[pos] != '|' { pos += 1 }
	rw_field := rest[rw_start:pos]
	row_idx := 0
	seg_start = 0
	for ri in 0 ..= len(rw_field) {
		if ri == len(rw_field) || rw_field[ri] == ',' {
			if ri > seg_start && row_idx < ui.GRID_MAX_ROWS {
				w := parse_int_from(rw_field[seg_start:ri])
				state.custom_grid_def.row_weights[row_idx] = f32(w) / 100.0
				row_idx += 1
			}
			seg_start = ri + 1
		}
	}
	if row_idx > 0 { state.custom_grid_def.row_count = row_idx }

	// Parse cells — build directly into world components.
	cell_count := 0

	for cell_count < CELL_MAX && pos < len(rest) {
		if rest[pos] != '|' do break
		pos += 1
		if pos >= len(rest) do break

		// K: widget kind.
		k_digit := int(rest[pos]) - '0'
		if k_digit < 0 || k_digit > 8 do break
		pos += 1
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// S: stream binding (until next ':').
		s_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		s_field := rest[s_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// F: flags (until next ':').
		f_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		f_field := rest[f_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// CS: col_span (until next ':').
		cs_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		cs_field := rest[cs_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// RS: row_span (until next ':' or '|' or end).
		rs_start := pos
		for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
		rs_field := rest[rs_start:pos]

		// SM: sub_main_split (optional, x1000).
		sm_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sm_start := pos
			for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
			sm_field = rest[sm_start:pos]
		}

		// SR: sub_ratios (optional, comma-separated x1000).
		sr_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sr_start := pos
			for pos < len(rest) && rest[pos] != '|' { pos += 1 }
			sr_field = rest[sr_start:pos]
		}

		// Write into world components at cell_count.
		ci := cell_count
		write_default_cell_to_world(state, ci, Widget_Kind(k_digit))

		if s_field == "-1" {
			state.world.bindings[ci].stream_idx = -1
			binding_clear(&state.world.bindings[ci])
		} else {
			slash := -1
			for si in 0 ..< len(s_field) {
				if s_field[si] == '/' { slash = si; break }
			}
			if slash > 0 && slash < len(s_field) - 1 {
				binding_set(&state.world.bindings[ci], s_field[:slash], s_field[slash + 1:])
			}
			state.world.bindings[ci].stream_idx = -1
		}

		flags := parse_int_from(f_field)
		unpack_indicator_flags(&state.world.indicators[ci], flags)

		cs := parse_int_from(cs_field)
		rs := parse_int_from(rs_field)
		state.world.spans[ci].col_span = cs > 1 ? cs : 1
		state.world.spans[ci].row_span = rs > 1 ? rs : 1

		// Subplot ratios.
		if len(sm_field) > 0 {
			state.world.subplots[ci].sub_main_split = f32(parse_int_from(sm_field)) / 1000.0
		}
		if len(sr_field) > 0 {
			sr_idx := 0
			sr_seg_start := 0
			for si in 0 ..= len(sr_field) {
				if si == len(sr_field) || sr_field[si] == ',' {
					if si > sr_seg_start && sr_idx < 5 {
						state.world.subplots[ci].sub_ratios[sr_idx] = f32(parse_int_from(sr_field[sr_seg_start:si])) / 1000.0
						sr_idx += 1
					}
					sr_seg_start = si + 1
				}
			}
		}

		cell_count += 1
	}

	if cell_count <= 0 do return false

	state.world.count = cell_count
	return true
}

restore_layout_v4 :: proc(state: ^App_State) -> bool {
	v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V4)
	if !ok || len(v) < 4 do return false
	if v[0] != 'V' || v[1] != '4' do return false

	rest := v[2:]
	pos := 0

	// Parse MODE.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos >= len(rest) do return false
	mode_ch := rest[pos]
	pos += 1
	if mode_ch == 'C' {
		state.layout_mode = .Custom
	} else {
		state.layout_mode = .Preset
	}

	// Parse CW: col weights.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos + 2 >= len(rest) || rest[pos] != 'C' || rest[pos + 1] != 'W' || rest[pos + 2] != ':' do return false
	pos += 3
	cw_start := pos
	for pos < len(rest) && rest[pos] != '|' { pos += 1 }
	cw_field := rest[cw_start:pos]
	col_idx := 0
	seg_start := 0
	for ci in 0 ..= len(cw_field) {
		if ci == len(cw_field) || cw_field[ci] == ',' {
			if ci > seg_start && col_idx < ui.GRID_MAX_COLS {
				w := parse_int_from(cw_field[seg_start:ci])
				state.custom_grid_def.col_weights[col_idx] = f32(w) / 100.0
				col_idx += 1
			}
			seg_start = ci + 1
		}
	}
	if col_idx > 0 { state.custom_grid_def.col_count = col_idx }

	// Parse RW: row weights.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos + 2 >= len(rest) || rest[pos] != 'R' || rest[pos + 1] != 'W' || rest[pos + 2] != ':' do return false
	pos += 3
	rw_start := pos
	for pos < len(rest) && rest[pos] != '|' { pos += 1 }
	rw_field := rest[rw_start:pos]
	row_idx := 0
	seg_start = 0
	for ri in 0 ..= len(rw_field) {
		if ri == len(rw_field) || rw_field[ri] == ',' {
			if ri > seg_start && row_idx < ui.GRID_MAX_ROWS {
				w := parse_int_from(rw_field[seg_start:ri])
				state.custom_grid_def.row_weights[row_idx] = f32(w) / 100.0
				row_idx += 1
			}
			seg_start = ri + 1
		}
	}
	if row_idx > 0 { state.custom_grid_def.row_count = row_idx }

	// Parse cells — build directly into world components.
	cell_count := 0

	for cell_count < CELL_MAX && pos < len(rest) {
		if rest[pos] != '|' do break
		pos += 1
		if pos >= len(rest) do break

		// K: widget kind.
		k_digit := int(rest[pos]) - '0'
		if k_digit < 0 || k_digit > 8 do break
		pos += 1
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// S: stream binding (until next ':').
		s_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		s_field := rest[s_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// F: flags (until next ':').
		f_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		f_field := rest[f_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// CS: col_span (until next ':').
		cs_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		cs_field := rest[cs_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// RS: row_span (until next ':' or '|' or end).
		rs_start := pos
		for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
		rs_field := rest[rs_start:pos]

		// SM: sub_main_split (optional, x1000).
		sm_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sm_start := pos
			for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
			sm_field = rest[sm_start:pos]
		}

		// SR: sub_ratios (optional, comma-separated x1000).
		sr_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sr_start := pos
			for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
			sr_field = rest[sr_start:pos]
		}

		// TF: tf_idx+1 (0 = global, 1-9 = per-cell TF).
		tf_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			tf_start := pos
			for pos < len(rest) && rest[pos] != '|' { pos += 1 }
			tf_field = rest[tf_start:pos]
		}

		// Write into world components at cell_count.
		ci := cell_count
		write_default_cell_to_world(state, ci, Widget_Kind(k_digit))

		if s_field == "-1" {
			state.world.bindings[ci].stream_idx = -1
			binding_clear(&state.world.bindings[ci])
		} else {
			slash := -1
			for si in 0 ..< len(s_field) {
				if s_field[si] == '/' { slash = si; break }
			}
			if slash > 0 && slash < len(s_field) - 1 {
				// PRD-0009: store venue/symbol intent directly on cell.
				binding_set(&state.world.bindings[ci], s_field[:slash], s_field[slash + 1:])
			}
			state.world.bindings[ci].stream_idx = -1 // will be resolved lazily (M2)
		}

		flags := parse_int_from(f_field)
		unpack_indicator_flags(&state.world.indicators[ci], flags)

		cs := parse_int_from(cs_field)
		rs := parse_int_from(rs_field)
		state.world.spans[ci].col_span = cs > 1 ? cs : 1
		state.world.spans[ci].row_span = rs > 1 ? rs : 1

		// Subplot ratios.
		if len(sm_field) > 0 {
			state.world.subplots[ci].sub_main_split = f32(parse_int_from(sm_field)) / 1000.0
		}
		if len(sr_field) > 0 {
			sr_idx := 0
			sr_seg_start := 0
			for si in 0 ..= len(sr_field) {
				if si == len(sr_field) || sr_field[si] == ',' {
					if si > sr_seg_start && sr_idx < 5 {
						state.world.subplots[ci].sub_ratios[sr_idx] = f32(parse_int_from(sr_field[sr_seg_start:si])) / 1000.0
						sr_idx += 1
					}
					sr_seg_start = si + 1
				}
			}
		}

		// Per-cell TF: stored as tf_idx+1; 0 -> -1 (global), 1-9 -> 0-8.
		if len(tf_field) > 0 {
			tf_val := parse_int_from(tf_field)
			state.world.timeframes[ci].tf_idx = tf_val > 0 ? tf_val - 1 : -1
		}

		cell_count += 1
	}

	if cell_count <= 0 do return false

	state.world.count = cell_count
	return true
}

restore_layout_v2 :: proc(state: ^App_State) -> bool {
	v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V2)
	if !ok || len(v) < 4 do return false
	if v[0] != 'V' || v[1] != '2' do return false

	rest := v[2:]  // skip "V2"

	// Parse pipe-delimited cells.
	cell_count := 0
	stream_bindings: [CELL_MAX][2]string  // [venue, symbol] for re-association

	pos := 0
	for cell_count < CELL_MAX && pos < len(rest) {
		// Expect '|' before each cell.
		if rest[pos] != '|' do break
		pos += 1
		if pos >= len(rest) do break

		// Parse K (widget kind digit).
		k_digit := int(rest[pos]) - '0'
		if k_digit < 0 || k_digit > 8 do break
		pos += 1
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// Parse S (stream binding -- up to next ':').
		s_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' {
			pos += 1
		}
		s_field := rest[s_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// Parse F (flags -- digits until '|' or end).
		f_start := pos
		for pos < len(rest) && rest[pos] != '|' {
			pos += 1
		}
		f_field := rest[f_start:pos]

		// Write into world components.
		ci := cell_count
		write_default_cell_to_world(state, ci, Widget_Kind(k_digit))

		// Decode stream binding.
		if s_field == "-1" {
			state.world.bindings[ci].stream_idx = -1
		} else {
			// Parse "venue/symbol".
			slash := -1
			for si in 0 ..< len(s_field) {
				if s_field[si] == '/' { slash = si; break }
			}
			if slash > 0 && slash < len(s_field) - 1 {
				stream_bindings[cell_count][0] = s_field[:slash]
				stream_bindings[cell_count][1] = s_field[slash + 1:]
			}
			state.world.bindings[ci].stream_idx = -1  // will be resolved after all cells parsed
		}

		// Decode flags.
		flags := 0
		for fi in 0 ..< len(f_field) {
			d := int(f_field[fi]) - '0'
			if d < 0 || d > 9 do break
			flags = flags * 10 + d
		}
		unpack_indicator_flags(&state.world.indicators[ci], flags)

		cell_count += 1
	}

	if cell_count <= 0 do return false

	// Apply cells.
	state.world.count = cell_count

	// Re-associate stream bindings: find matching stream_view slot by venue/symbol.
	reg := state.stream_views
	if reg != nil {
		for i in 0 ..< cell_count {
			venue := stream_bindings[i][0]
			symbol := stream_bindings[i][1]
			if len(venue) == 0 || len(symbol) == 0 do continue
			// Find slot with matching venue/symbol.
			for si in 0 ..< STREAM_VIEW_CAP {
				if !reg.slots[si].used do continue
				slot := &reg.slots[si]
				if !slot.has_stream_info {
					refresh_stream_info_for_slot(state, slot)
				}
				if slot.has_stream_info &&
					normalized_venue(slot.stream_info.venue) == normalized_venue(venue) &&
					normalized_symbol(slot.stream_info.symbol) == normalized_symbol(symbol) {
					state.world.bindings[i].stream_idx = si
					break
				}
			}
		}
	}

	return true
}

// Export current layout V4 string to the system clipboard.
layout_export_to_clipboard :: proc(state: ^App_State) -> bool {
	// Ensure latest V4 is persisted.
	persist_layout_v4(state)
	v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V4)
	if !ok || len(v) < 4 do return false
	return services.settings_clipboard_write(&state.settings, v)
}

// Import layout from a V4 string (e.g. pasted from clipboard).
layout_import_from_string :: proc(state: ^App_State, v: string) -> bool {
	if len(v) < 4 do return false
	if v[0] != 'V' || v[1] != '4' do return false
	if !restore_layout_v4_from_string(state, v) do return false
	persist_layout_v4(state)
	reconcile_subscriptions(state)
	return true
}

// Restore V4 layout from an explicit string (decoupled from settings_get).
restore_layout_v4_from_string :: proc(state: ^App_State, v: string) -> bool {
	if len(v) < 4 do return false
	if v[0] != 'V' || v[1] != '4' do return false

	rest := v[2:]
	pos := 0

	// Parse MODE.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos >= len(rest) do return false
	mode_ch := rest[pos]
	pos += 1
	if mode_ch == 'C' {
		state.layout_mode = .Custom
	} else {
		state.layout_mode = .Preset
	}

	// Parse CW: col weights.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos + 2 >= len(rest) || rest[pos] != 'C' || rest[pos + 1] != 'W' || rest[pos + 2] != ':' do return false
	pos += 3
	cw_start := pos
	for pos < len(rest) && rest[pos] != '|' { pos += 1 }
	cw_field := rest[cw_start:pos]
	col_idx := 0
	seg_start := 0
	for ci in 0 ..= len(cw_field) {
		if ci == len(cw_field) || cw_field[ci] == ',' {
			if ci > seg_start && col_idx < ui.GRID_MAX_COLS {
				w := parse_int_from(cw_field[seg_start:ci])
				state.custom_grid_def.col_weights[col_idx] = f32(w) / 100.0
				col_idx += 1
			}
			seg_start = ci + 1
		}
	}
	if col_idx > 0 { state.custom_grid_def.col_count = col_idx }

	// Parse RW: row weights.
	if pos >= len(rest) || rest[pos] != '|' do return false
	pos += 1
	if pos + 2 >= len(rest) || rest[pos] != 'R' || rest[pos + 1] != 'W' || rest[pos + 2] != ':' do return false
	pos += 3
	rw_start := pos
	for pos < len(rest) && rest[pos] != '|' { pos += 1 }
	rw_field := rest[rw_start:pos]
	row_idx := 0
	seg_start = 0
	for ri in 0 ..= len(rw_field) {
		if ri == len(rw_field) || rw_field[ri] == ',' {
			if ri > seg_start && row_idx < ui.GRID_MAX_ROWS {
				w := parse_int_from(rw_field[seg_start:ri])
				state.custom_grid_def.row_weights[row_idx] = f32(w) / 100.0
				row_idx += 1
			}
			seg_start = ri + 1
		}
	}
	if row_idx > 0 { state.custom_grid_def.row_count = row_idx }

	// Parse cells.
	cell_count := 0
	for cell_count < CELL_MAX && pos < len(rest) {
		if rest[pos] != '|' do break
		pos += 1
		if pos >= len(rest) do break

		k_digit := int(rest[pos]) - '0'
		if k_digit < 0 || k_digit > 8 do break
		pos += 1
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		s_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		s_field := rest[s_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		f_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		f_field := rest[f_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		cs_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		cs_field := rest[cs_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		rs_start := pos
		for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
		rs_field := rest[rs_start:pos]

		sm_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sm_start := pos
			for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
			sm_field = rest[sm_start:pos]
		}

		sr_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sr_start := pos
			for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
			sr_field = rest[sr_start:pos]
		}

		tf_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			tf_start := pos
			for pos < len(rest) && rest[pos] != '|' { pos += 1 }
			tf_field = rest[tf_start:pos]
		}

		ci := cell_count
		write_default_cell_to_world(state, ci, Widget_Kind(k_digit))

		if s_field == "-1" {
			state.world.bindings[ci].stream_idx = -1
			binding_clear(&state.world.bindings[ci])
		} else {
			slash := -1
			for si in 0 ..< len(s_field) {
				if s_field[si] == '/' { slash = si; break }
			}
			if slash > 0 && slash < len(s_field) - 1 {
				binding_set(&state.world.bindings[ci], s_field[:slash], s_field[slash + 1:])
			}
			state.world.bindings[ci].stream_idx = -1
		}

		flags := parse_int_from(f_field)
		unpack_indicator_flags(&state.world.indicators[ci], flags)

		cs := parse_int_from(cs_field)
		rs := parse_int_from(rs_field)
		state.world.spans[ci].col_span = cs > 1 ? cs : 1
		state.world.spans[ci].row_span = rs > 1 ? rs : 1

		if len(sm_field) > 0 {
			state.world.subplots[ci].sub_main_split = f32(parse_int_from(sm_field)) / 1000.0
		}
		if len(sr_field) > 0 {
			sr_idx := 0
			sr_seg_start := 0
			for si in 0 ..= len(sr_field) {
				if si == len(sr_field) || sr_field[si] == ',' {
					if si > sr_seg_start && sr_idx < 5 {
						state.world.subplots[ci].sub_ratios[sr_idx] = f32(parse_int_from(sr_field[sr_seg_start:si])) / 1000.0
						sr_idx += 1
					}
					sr_seg_start = si + 1
				}
			}
		}

		if len(tf_field) > 0 {
			tf_val := parse_int_from(tf_field)
			state.world.timeframes[ci].tf_idx = tf_val > 0 ? tf_val - 1 : -1
		}

		cell_count += 1
	}

	if cell_count <= 0 do return false
	state.world.count = cell_count
	return true
}

// Custom preset settings keys indexed 0-3.
CUSTOM_LAYOUT_KEYS :: [4]string{
	services.SETTING_CUSTOM_LAYOUT_0,
	services.SETTING_CUSTOM_LAYOUT_1,
	services.SETTING_CUSTOM_LAYOUT_2,
	services.SETTING_CUSTOM_LAYOUT_3,
}

// Save current layout to a custom preset slot (0-3).
save_custom_preset :: proc(state: ^App_State, slot: int) {
	if slot < 0 || slot >= 4 do return
	// Build layout string.
	buf: [128]u8
	off := 0
	n := state.world.count
	if n > 9 { buf[off] = '0' + u8(n / 10); off += 1 }
	buf[off] = '0' + u8(n % 10); off += 1
	buf[off] = ':'; off += 1
	for i in 0 ..< n {
		if i > 0 { buf[off] = ','; off += 1 }
		buf[off] = '0' + u8(state.world.widgets[i].kind); off += 1
	}
	keys := CUSTOM_LAYOUT_KEYS
	services.settings_set(&state.settings, keys[slot], string(buf[:off]))
	services.settings_flush(&state.settings)
}

// Load a custom preset slot. Returns true if valid and applied.
load_custom_preset :: proc(state: ^App_State, slot: int) -> bool {
	if slot < 0 || slot >= 4 do return false
	keys := CUSTOM_LAYOUT_KEYS
	v, ok := services.settings_get(&state.settings, keys[slot])
	if !ok || len(v) < 3 do return false
	return restore_layout_from_string(state, v)
}

// Check if a custom preset slot has a valid saved layout.
custom_preset_valid :: proc(state: ^App_State, slot: int) -> bool {
	if slot < 0 || slot >= 4 do return false
	keys := CUSTOM_LAYOUT_KEYS
	v, ok := services.settings_get(&state.settings, keys[slot])
	return ok && len(v) >= 3
}

// Initialize world components from panel visibility defaults.
layout_from_panels :: proc(state: ^App_State) {
	PANEL_WIDGET_MAP :: [ui.PANEL_COUNT]Widget_Kind{
		.Candle, .Stats, .Counter, .Heatmap, .VPVR, .Trades, .Orderbook,
	}
	state.world.count = 0
	panel_map := PANEL_WIDGET_MAP
	for i in 0 ..< ui.PANEL_COUNT {
		if !state.chrome.panel_visible[i] do continue
		if state.world.count >= CELL_MAX do break
		ci := state.world.count
		write_default_cell_to_world(state, ci, panel_map[i])
		state.world.count += 1
	}
}

// ===============================================================
// Helper: write default cell state into world components at index ci.
// Replaces make_default_cell for ECS world component storage.
// ===============================================================

write_default_cell_to_world :: proc(state: ^App_State, ci: int, widget: Widget_Kind = .Empty, stream_idx: int = -1) {
	state.world.widgets[ci]    = Widget_Component{ kind = widget }
	state.world.bindings[ci]   = Stream_Binding{
		stream_idx       = stream_idx,
		bound_venue_len  = 0,
		bound_symbol_len = 0,
	}
	state.world.views[ci]      = {}
	state.world.charts[ci]     = Chart_Component{
		show_vol              = state.chart_display.show_vol,
		show_heatmap          = state.chart_display.show_heatmap,
		show_vpvr             = state.chart_display.show_vpvr,
		heatmap_intensity_idx = state.chart_display.heatmap_intensity_idx,
	}
	state.world.indicators[ci] = Indicator_Component{
		show_ma            = state.indicators.show_ma,
		show_bbands        = state.indicators.show_bbands,
		show_vwap          = state.indicators.show_vwap,
		show_rsi           = state.indicators.show_rsi,
		show_macd          = state.indicators.show_macd,
		show_funding       = state.indicators.show_funding,
		show_liq           = state.indicators.show_liq,
		show_trade_counter = state.indicators.show_trade_counter,
	}
	state.world.ind_params[ci] = Indicator_Params{
		ma_periods  = state.indicators.ma_periods,
		bb_period   = state.indicators.bb_period,
		bb_sigma    = state.indicators.bb_sigma,
		rsi_period  = state.indicators.rsi_period,
		macd_fast   = state.indicators.macd_fast,
		macd_slow   = state.indicators.macd_slow,
		macd_signal = state.indicators.macd_signal,
	}
	state.world.subplots[ci]   = Subplot_Component{ sub_resize_idx = -1 }
	state.world.spans[ci]      = {}
	state.world.timeframes[ci] = Timeframe_Component{ tf_idx = -1 }
	state.world.getranges[ci]  = {}
}
