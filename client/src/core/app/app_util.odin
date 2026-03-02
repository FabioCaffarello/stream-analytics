package app

import "core:fmt"
import "core:math"
import "mr:ports"
import "mr:services"
import "mr:ui"

// Parse a single-digit int from string, clamped to [lo, hi]. Returns lo on failure.
parse_small_int :: proc(s: string, lo, hi: int) -> int {
	if len(s) == 0 do return lo
	d := int(s[0]) - '0'
	if d < lo do return lo
	if d > hi do return hi
	return d
}

// Parse a multi-digit int from string, clamped to [lo, hi]. Returns fallback on failure.
parse_int_clamped :: proc(s: string, lo, hi, fallback: int) -> int {
	if len(s) == 0 do return fallback
	neg := false
	start := 0
	if s[0] == '-' {
		neg = true
		start = 1
	}
	if start >= len(s) do return fallback
	val := 0
	for i in start ..< len(s) {
		c := s[i]
		if c < '0' || c > '9' do break
		val = val * 10 + int(c - '0')
	}
	if neg do val = -val
	if val < lo do val = lo
	if val > hi do val = hi
	return val
}

// Parse a float from "N.N" format string, clamped. Returns fallback on failure.
parse_float_clamped :: proc(s: string, lo, hi, fallback: f64) -> f64 {
	if len(s) == 0 do return fallback
	whole := 0
	frac := f64(0)
	frac_div := f64(1)
	in_frac := false
	for i in 0 ..< len(s) {
		c := s[i]
		if c == '.' {
			in_frac = true
			continue
		}
		if c < '0' || c > '9' do break
		if in_frac {
			frac_div *= 10
			frac += f64(c - '0') / frac_div
		} else {
			whole = whole * 10 + int(c - '0')
		}
	}
	val := f64(whole) + frac
	if val < lo do val = lo
	if val > hi do val = hi
	return val
}

// Length of a null-terminated or zero-padded byte buffer.
cstring_len :: proc(buf: ^[12]u8) -> int {
	for i in 0 ..< 12 {
		if buf[i] == 0 do return i
	}
	return 12
}

// Auto price grouping: ~0.01% of price, snapped to 10^N.
// E.g. price=90000 → 10, price=3000 → 1, price=150 → 0.1, price=0.5 → 0.001
orderbook_auto_price_group :: proc(price: f64) -> f64 {
	if price <= 0 do return 1
	target := price * 0.0001
	exp := math.floor(math.log10(target))
	return math.pow(10, exp)
}

// Wider grouping for synthetic heatmap overlay (~1% of price, snapped to 10^N).
// E.g. BTC 90000 → 1000, ETH 3000 → 10, DOGE 0.08 → 0.001
synthetic_heatmap_price_group :: proc(price: f64) -> f64 {
	if price <= 0 do return 1
	target := price * 0.01
	exp := math.floor(math.log10(target))
	return math.pow(10, exp)
}

channel_short_label :: proc(ch: ports.MD_Channel) -> string {
	switch ch {
	case .Trades:
		return "trades"
	case .Orderbook:
		return "orderbook"
	case .Stats:
		return "stats"
	case .Heatmaps:
		return "heatmap"
	case .VPVR:
		return "vpvr"
	case .Candles:
		return "candles"
	}
	return "?"
}

parse_channel_short_label :: proc(s: string) -> (ports.MD_Channel, bool) {
	switch s {
	case "trades":
		return .Trades, true
	case "orderbook":
		return .Orderbook, true
	case "stats":
		return .Stats, true
	case "heatmap":
		return .Heatmaps, true
	case "vpvr":
		return .VPVR, true
	case "candles":
		return .Candles, true
	}
	return {}, false
}

format_timeframe_short_into :: proc(buf: []u8, tf_ms: i64) -> string {
	if tf_ms <= 0 do return ""
	if tf_ms % 86_400_000 == 0 do return fmt.bprintf(buf, "%dd", tf_ms / 86_400_000)
	if tf_ms % 3_600_000 == 0 do return fmt.bprintf(buf, "%dh", tf_ms / 3_600_000)
	if tf_ms % 60_000 == 0 do return fmt.bprintf(buf, "%dm", tf_ms / 60_000)
	if tf_ms % 1000 == 0 do return fmt.bprintf(buf, "%ds", tf_ms / 1000)
	return fmt.bprintf(buf, "%dms", tf_ms)
}

// Set a toast message for brief on-screen feedback (~90 frames / 1.5s).
show_toast :: proc(state: ^App_State, msg: string) {
	n := min(len(msg), len(state.toast_text))
	for i in 0 ..< n {
		state.toast_text[i] = msg[i]
	}
	state.toast_len = n
	state.toast_frame = state.frame
}

// Check if a market (venue + ticker) has a matching connected stream view slot.
markets_is_subscribed :: proc(state: ^App_State, venue, ticker: string) -> bool {
	reg := state.stream_views
	if reg == nil do return false
	for i in 0 ..< STREAM_VIEW_CAP {
		if !reg.slots[i].used do continue
		slot := &reg.slots[i]
		if !slot.has_stream_info {
			refresh_stream_info_for_slot(state, slot)
		}
		if slot.has_stream_info && slot.stream_info.venue == venue && slot.stream_info.symbol == ticker {
			return true
		}
	}
	return false
}

// Sync a LAYERS sidebar toggle to the global default + settings.
// idx: 0=vol, 1=heatmap, 2=vpvr, 3=ma, 4=bbands, 5=vwap, 6=rsi, 7=macd, 8=funding, 9=liq, 10=trade_counter
sync_layer_to_global :: proc(state: ^App_State, idx: int, value: bool) {
	LAYER_SETTINGS :: [11]string{
		services.SETTING_SHOW_CANDLE_VOL, services.SETTING_SHOW_CANDLE_HEATMAP, services.SETTING_SHOW_CANDLE_VPVR,
		services.SETTING_SHOW_MA, services.SETTING_SHOW_BBANDS, services.SETTING_SHOW_VWAP,
		services.SETTING_SHOW_RSI, services.SETTING_SHOW_MACD, services.SETTING_SHOW_FUNDING,
		services.SETTING_SHOW_LIQ, services.SETTING_SHOW_TRADE_COUNTER,
	}
	global_ptrs := [11]^bool{
		&state.show_candle_vol, &state.show_candle_heatmap, &state.show_candle_vpvr,
		&state.show_ma, &state.show_bbands, &state.show_vwap,
		&state.show_rsi, &state.show_macd, &state.show_funding,
		&state.show_liq, &state.show_trade_counter,
	}
	if idx < 0 || idx >= 11 do return
	global_ptrs[idx]^ = value
	keys := LAYER_SETTINGS
	services.settings_set(&state.settings, keys[idx], value ? "1" : "0")
	services.settings_flush(&state.settings)
}

panel_visibility_mask_encode_into :: proc(buf: []u8, visible: [ui.PANEL_COUNT]bool) -> string {
	if len(buf) < ui.PANEL_COUNT do return ""
	for i in 0 ..< ui.PANEL_COUNT {
		buf[i] = visible[i] ? u8('1') : u8('0')
	}
	return string(buf[:ui.PANEL_COUNT])
}

panel_visibility_mask_decode :: proc(mask: string, visible: ^[ui.PANEL_COUNT]bool) -> bool {
	if len(mask) < ui.PANEL_COUNT do return false
	visible_count := 0
	for i in 0 ..< ui.PANEL_COUNT {
		is_visible := mask[i] != '0'
		visible[i] = is_visible
		if is_visible do visible_count += 1
	}
	return visible_count > 0
}

// Persist col weights as comma-separated integers (weight * 100).
persist_col_weights :: proc(state: ^App_State, col_count: int) {
	buf: [128]u8
	off := 0
	for c in 0 ..< col_count {
		if c > 0 { buf[off] = ','; off += 1 }
		w := int(state.custom_grid_def.col_weights[c] * 100 + 0.5)
		if w > 999 { buf[off] = '0' + u8(w / 1000); off += 1 }
		if w > 99 { buf[off] = '0' + u8((w / 100) % 10); off += 1 }
		if w > 9 { buf[off] = '0' + u8((w / 10) % 10); off += 1 }
		buf[off] = '0' + u8(w % 10); off += 1
	}
	services.settings_set(&state.settings, services.SETTING_COL_WEIGHTS, string(buf[:off]))
	services.settings_flush(&state.settings)
}

// Restore col weights from comma-separated integers (weight * 100).
restore_col_weights :: proc(state: ^App_State) -> bool {
	v, ok := services.settings_get(&state.settings, services.SETTING_COL_WEIGHTS)
	if !ok || len(v) == 0 do return false

	ci := 0
	val := 0
	has_val := false
	for i in 0 ..< len(v) {
		c := v[i]
		if c == ',' {
			if has_val && ci < ui.GRID_MAX_COLS {
				state.custom_grid_def.col_weights[ci] = f32(val) / 100.0
				ci += 1
			}
			val = 0
			has_val = false
		} else if c >= '0' && c <= '9' {
			val = val * 10 + int(c - '0')
			has_val = true
		}
	}
	if has_val && ci < ui.GRID_MAX_COLS {
		state.custom_grid_def.col_weights[ci] = f32(val) / 100.0
		ci += 1
	}
	return ci > 0
}

// Persist row weights as comma-separated integers (weight * 100).
persist_row_weights :: proc(state: ^App_State, row_count: int) {
	buf: [128]u8
	off := 0
	for r in 0 ..< row_count {
		if r > 0 { buf[off] = ','; off += 1 }
		w := int(state.custom_grid_def.row_weights[r] * 100 + 0.5)
		if w > 999 { buf[off] = '0' + u8(w / 1000); off += 1 }
		if w > 99 { buf[off] = '0' + u8((w / 100) % 10); off += 1 }
		if w > 9 { buf[off] = '0' + u8((w / 10) % 10); off += 1 }
		buf[off] = '0' + u8(w % 10); off += 1
	}
	services.settings_set(&state.settings, services.SETTING_ROW_WEIGHTS, string(buf[:off]))
	services.settings_flush(&state.settings)
}

// Restore row weights from comma-separated integers (weight * 100).
restore_row_weights :: proc(state: ^App_State) -> bool {
	v, ok := services.settings_get(&state.settings, services.SETTING_ROW_WEIGHTS)
	if !ok || len(v) == 0 do return false

	ri := 0
	val := 0
	has_val := false
	for i in 0 ..< len(v) {
		c := v[i]
		if c == ',' {
			if has_val && ri < ui.GRID_MAX_ROWS {
				state.custom_grid_def.row_weights[ri] = f32(val) / 100.0
				ri += 1
			}
			val = 0
			has_val = false
		} else if c >= '0' && c <= '9' {
			val = val * 10 + int(c - '0')
			has_val = true
		}
	}
	if has_val && ri < ui.GRID_MAX_ROWS {
		state.custom_grid_def.row_weights[ri] = f32(val) / 100.0
		ri += 1
	}
	return ri > 0
}
