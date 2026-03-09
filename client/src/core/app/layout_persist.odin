package app

import "mr:services"
import "mr:ui"

// S122: Layout persistence — V6 format only.
// Legacy V1-V5 persist/restore functions removed (dead code since S111).
// CRC integrity footer (|CK:) added for corruption detection.

// Pack 11 indicator booleans into a single integer.
// S81: bits 8-9 added for show_cvd, show_delta_vol.
// S82: bit 10 added for show_oi.
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
	if ind.show_cvd            do f |= 1 << 8
	if ind.show_delta_vol      do f |= 1 << 9
	if ind.show_oi             do f |= 1 << 10
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
	ind.show_cvd           = (f & (1 << 8)) != 0
	ind.show_delta_vol     = (f & (1 << 9)) != 0
	ind.show_oi            = (f & (1 << 10)) != 0
}

// ===============================================================
// V6 persistence — canonical layout format.
// Format: "V6|MODE|CW:w0,...|RW:w0,...|K:S:F:CS:RS:SM:SR:TF:CD|...|LK:flag|CK:XXXXXXXX"
//   CD = chart display packed int (vol/heatmap/vpvr/heatmap_idx/ob_grp/dom_grp/trade_filter)
//   LK = signal-evidence link flag
//   CK = FNV-1a integrity checksum (S122)
// ===============================================================

// Build V6 layout string into buf, return bytes written.
build_layout_v6_string :: proc(state: ^App_State, buf: []u8) -> int {
	off := 0

	// Header: "V6"
	buf[off] = 'V'; off += 1
	buf[off] = '6'; off += 1

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
		off = write_int_to_buf(buf, off, w)
	}

	// RW: row weights.
	buf[off] = '|'; off += 1
	buf[off] = 'R'; off += 1
	buf[off] = 'W'; off += 1
	buf[off] = ':'; off += 1
	for r in 0 ..< state.custom_grid_def.row_count {
		if r > 0 { buf[off] = ','; off += 1 }
		w := int(state.custom_grid_def.row_weights[r] * 100 + 0.5)
		off = write_int_to_buf(buf, off, w)
	}

	// Cells: K:S:F:CS:RS:SM:SR:TF:CD per cell.
	n := state.world.count
	for i in 0 ..< n {
		buf[off] = '|'; off += 1

		// K: widget kind (multi-digit safe for Session_VPVR=10, TPO=11).
		off = write_int_to_buf(buf, off, int(state.world.widgets[i].kind))
		buf[off] = ':'; off += 1

		// S: stream binding — normalize symbol to strip market type suffix.
		bv := binding_venue(&state.world.bindings[i])
		bs := normalized_symbol(binding_symbol(&state.world.bindings[i]))
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
		off = write_int_to_buf(buf, off, flags)
		buf[off] = ':'; off += 1

		// CS: col_span.
		cs := state.world.spans[i].col_span > 1 ? state.world.spans[i].col_span : 1
		off = write_int_to_buf(buf, off, cs)
		buf[off] = ':'; off += 1

		// RS: row_span.
		rs := state.world.spans[i].row_span > 1 ? state.world.spans[i].row_span : 1
		off = write_int_to_buf(buf, off, rs)
		buf[off] = ':'; off += 1

		// SM: sub_main_split (x1000).
		sm := int(state.world.subplots[i].sub_main_split * 1000 + 0.5)
		off = write_int_to_buf(buf, off, sm)
		buf[off] = ':'; off += 1

		// SR: sub_ratios (x1000, comma-separated).
		for sri in 0 ..< 5 {
			if sri > 0 { buf[off] = ','; off += 1 }
			sr := int(state.world.subplots[i].sub_ratios[sri] * 1000 + 0.5)
			off = write_int_to_buf(buf, off, sr)
		}
		buf[off] = ':'; off += 1

		// TF: tf_idx+1 (0 = global, 1-9 = per-cell).
		tf_val := state.world.timeframes[i].tf_idx + 1
		if tf_val < 0 { tf_val = 0 }
		off = write_int_to_buf(buf, off, tf_val)
		buf[off] = ':'; off += 1

		// CD: chart display packed (V6 + S48 analytics_kind bits 17-18).
		cd := pack_chart_display_with_analytics(&state.world.charts[i], &state.world.analytics[i])
		off = write_int_to_buf(buf, off, cd)
	}

	// LK: signal-evidence link.
	buf[off] = '|'; off += 1
	buf[off] = 'L'; off += 1
	buf[off] = 'K'; off += 1
	buf[off] = ':'; off += 1
	buf[off] = state.signal_evidence_link_enabled ? '1' : '0'; off += 1

	return off
}

persist_layout_v6 :: proc(state: ^App_State) {
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])

	// S122: Append CRC integrity footer.
	body := string(buf[:off])
	off = artifact_write_ck_suffix(buf[:], off, body)

	services.settings_set(&state.settings, services.SETTING_LAYOUT_V6, string(buf[:off]))
	services.settings_set(&state.settings, services.SETTING_LAYOUT_MODE,
		state.layout_mode == .Custom ? "C" : "P")

	// Schema version marker.
	schema_buf: [4]u8
	schema_off := write_int_to_buf(schema_buf[:], 0, WORKSPACE_SCHEMA_VERSION)
	services.settings_set(&state.settings, services.SETTING_SETTINGS_VERSION, string(schema_buf[:schema_off]))

	// S122: Stamp artifact fingerprint for idempotent comparison.
	workspace_artifact_stamp(state)
}

restore_layout_v6 :: proc(state: ^App_State) -> bool {
	return persist_result_ok(restore_workspace(state))
}

// S111: Primary restore entry point — returns structured result.
restore_workspace :: proc(state: ^App_State) -> Persist_Result {
	v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V6)
	if !ok || len(v) < 4 do return .No_Data
	return restore_layout_v6_validated(state, v)
}

// S111: Validated restore with structured result.
// S122: Validates CRC integrity if |CK: suffix present.
restore_layout_v6_validated :: proc(state: ^App_State, v: string) -> Persist_Result {
	if len(v) < 4 do return .No_Data

	// S122: Validate and strip CRC suffix if present.
	body, ck_valid, _ := artifact_validate_ck(v)

	if !ck_valid {
		return .Corrupted
	}

	if len(body) < 4 do return .No_Data
	if body[0] != 'V' || body[1] != '6' {
		// Check for a future version header (e.g. "V7", "V8", ...).
		if len(body) >= 2 && body[0] == 'V' && body[1] > '6' && body[1] <= '9' {
			return .Version_Mismatch
		}
		return .Corrupted
	}
	ok := restore_layout_v6_from_string_inner(state, body)
	if ok {
		// S122: Stamp fingerprint so subsequent persist checks are idempotent.
		workspace_artifact_stamp(state)
	}
	return ok ? .Ok : .Corrupted
}

restore_layout_v6_from_string :: proc(state: ^App_State, v: string) -> bool {
	return persist_result_ok(restore_layout_v6_validated(state, v))
}

restore_layout_v6_from_string_inner :: proc(state: ^App_State, v: string) -> bool {
	if len(v) < 4 do return false
	if v[0] != 'V' || v[1] != '6' do return false

	// Strip |LK: suffix.
	base := v
	link_enabled := true
	for i := 2; i + 3 < len(v); i += 1 {
		if v[i] == '|' && v[i + 1] == 'L' && v[i + 2] == 'K' && v[i + 3] == ':' {
			base = v[:i]
			val := v[i + 4:]
			if len(val) > 0 {
				link_enabled = val[0] != '0'
			}
			break
		}
	}

	rest := base[2:] // skip "V6"
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

		// K: widget kind (multi-digit for Session_VPVR=10, TPO=11).
		k_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		k_digit := parse_int_from(rest[k_start:pos])
		if k_digit < 0 || k_digit > int(max(Widget_Kind)) do break
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// S: stream binding.
		s_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		s_field := rest[s_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// F: indicator flags.
		f_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		f_field := rest[f_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// CS: col_span.
		cs_start := pos
		for pos < len(rest) && rest[pos] != ':' && rest[pos] != '|' { pos += 1 }
		cs_field := rest[cs_start:pos]
		if pos >= len(rest) || rest[pos] != ':' do break
		pos += 1

		// RS: row_span.
		rs_start := pos
		for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
		rs_field := rest[rs_start:pos]

		// SM: sub_main_split (optional).
		sm_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sm_start := pos
			for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
			sm_field = rest[sm_start:pos]
		}

		// SR: sub_ratios (optional).
		sr_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			sr_start := pos
			for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
			sr_field = rest[sr_start:pos]
		}

		// TF: timeframe (optional).
		tf_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			tf_start := pos
			for pos < len(rest) && rest[pos] != '|' && rest[pos] != ':' { pos += 1 }
			tf_field = rest[tf_start:pos]
		}

		// CD: chart display (V6, optional for forward compat).
		cd_field := ""
		if pos < len(rest) && rest[pos] == ':' {
			pos += 1
			cd_start := pos
			for pos < len(rest) && rest[pos] != '|' { pos += 1 }
			cd_field = rest[cd_start:pos]
		}

		ci := cell_count
		write_default_cell_to_world(state, ci, Widget_Kind(k_digit))

		// Decode stream binding.
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

		// Decode indicator flags.
		flags := parse_int_from(f_field)
		unpack_indicator_flags(&state.world.indicators[ci], flags)

		// Decode spans.
		cs := parse_int_from(cs_field)
		rs := parse_int_from(rs_field)
		state.world.spans[ci].col_span = cs > 1 ? cs : 1
		state.world.spans[ci].row_span = rs > 1 ? rs : 1

		// Decode subplots.
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

		// Decode per-cell TF.
		if len(tf_field) > 0 {
			tf_val := parse_int_from(tf_field)
			state.world.timeframes[ci].tf_idx = tf_val > 0 ? tf_val - 1 : -1
		}

		// Decode chart display (V6 + S48 analytics_kind).
		if len(cd_field) > 0 {
			cd := parse_int_from(cd_field)
			unpack_chart_display_with_analytics(&state.world.charts[ci], &state.world.analytics[ci], cd)
		}

		cell_count += 1
	}

	if cell_count <= 0 do return false
	state.world.count = cell_count
	state.signal_evidence_link_enabled = link_enabled
	return true
}

// ===============================================================
// Helpers
// ===============================================================

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

// ===============================================================
// Import / Export
// ===============================================================

// Export current layout V6 string to the system clipboard.
layout_export_to_clipboard :: proc(state: ^App_State) -> bool {
	persist_layout_v6(state)
	v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V6)
	if !ok || len(v) < 4 do return false
	return services.settings_clipboard_write(&state.settings, v)
}

// Import layout from a V6 string (e.g. pasted from clipboard).
// S122: V6-only import — legacy V5/V4 no longer accepted.
layout_import_from_string :: proc(state: ^App_State, v: string) -> bool {
	if len(v) < 4 do return false
	if v[0] != 'V' do return false

	// S122: Strip CRC if present, then check header.
	body, ck_valid, _ := artifact_validate_ck(v)
	if !ck_valid do return false

	if len(body) >= 2 && body[0] == 'V' && body[1] == '6' {
		if !restore_layout_v6_from_string(state, body) do return false
	} else {
		return false
	}
	persist_layout_v6(state)
	reconcile_subscriptions(state)
	return true
}

// ===============================================================
// Custom Presets
// ===============================================================

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
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	// S122: Append CRC to custom preset too.
	body := string(buf[:off])
	off = artifact_write_ck_suffix(buf[:], off, body)
	keys := CUSTOM_LAYOUT_KEYS
	services.settings_set(&state.settings, keys[slot], string(buf[:off]))
	services.settings_flush(&state.settings)
}

// Load a custom preset slot. Returns true if valid and applied.
// S122: V6-only — legacy V4/V1 presets no longer loaded.
load_custom_preset :: proc(state: ^App_State, slot: int) -> bool {
	if slot < 0 || slot >= 4 do return false
	keys := CUSTOM_LAYOUT_KEYS
	v, ok := services.settings_get(&state.settings, keys[slot])
	if !ok || len(v) < 3 do return false

	// S122: Strip CRC if present.
	body, ck_valid, _ := artifact_validate_ck(v)
	if !ck_valid do return false

	if len(body) >= 4 && body[0] == 'V' && body[1] == '6' {
		return restore_layout_v6_from_string(state, body)
	}
	return false
}

// Check if a custom preset slot has a valid saved layout.
custom_preset_valid :: proc(state: ^App_State, slot: int) -> bool {
	if slot < 0 || slot >= 4 do return false
	keys := CUSTOM_LAYOUT_KEYS
	v, ok := services.settings_get(&state.settings, keys[slot])
	return ok && len(v) >= 3
}

// ===============================================================
// Default Layout
// ===============================================================

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
		show_cvd           = state.indicators.show_cvd,
		show_delta_vol     = state.indicators.show_delta_vol,
		show_oi            = state.indicators.show_oi,
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
	state.world.analytics[ci]  = {}
	state.world.getranges[ci]  = {}
}
