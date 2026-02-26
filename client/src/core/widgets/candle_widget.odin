package widgets

// Candlestick chart widget — pure RCL rendering.
// Emits candle bodies (Cmd_Rect_Filled), wicks (Cmd_Line), volume bars,
// price/time axis labels, and current price line.

import "core:fmt"
import "core:math"
import "mr:ports"
import "mr:services"
import "mr:ui"

// --- Configuration ---

CANDLE_MIN_ZOOM     :: f32(10)   // minimum candles visible
CANDLE_MAX_ZOOM     :: f32(500)  // maximum candles visible
CANDLE_DEFAULT_ZOOM :: f32(60)   // initial candles visible
CANDLE_BODY_PCT     :: f32(0.7)  // body width as fraction of slot width
VOLUME_HEIGHT_PCT   :: f32(0.18) // volume bars take 18% of viewport height
PRICE_MARGIN_PCT    :: f64(0.05) // 5% buffer above/below price range
Y_AXIS_WIDTH        :: f32(70)   // reserved right margin for price labels
X_AXIS_HEIGHT       :: f32(20)   // reserved bottom margin for time labels

// --- Widget data ---

Candle_Widget_Data :: struct {
	store:         ^services.Candle_Store,
	viewport:      ui.Rect,
	text:          ports.Text_Port,
	input:         ports.Input_State,
	scroll_x:      ^f32,  // horizontal pan offset (candle units, 0 = rightmost)
	zoom_level:    ^f32,  // candles per viewport width
	health_label:  string,
	health_detail: string,
	health_color:  ui.Color,
	tf_label:      string,  // e.g. "1m", "5m", "1h" — defaults to "1m" if empty
}

// --- Main draw procedure ---

candle_widget :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data) {
	store := data.store
	vp := data.viewport

	// Always draw background + title.
	ui.push(buf, ui.Cmd_Clip_Push{rect = vp})
	ui.push(buf, ui.Cmd_Rect_Filled{rect = vp, color = ui.COL_PANEL_BG})
	title_buf: [32]u8
	tf := data.tf_label if len(data.tf_label) > 0 else "1m"
	title := fmt.bprintf(title_buf[:], "Candles (%s) [1-6]", tf)
	ui.push_text(buf, {vp.pos.x + 4, vp.pos.y + 14}, title,
		ui.with_alpha(ui.COL_WHITE, 0.6), ui.FONT_SIZE_SM)
	if len(data.health_label) > 0 {
		ui.push_text(buf, {vp.pos.x + 120, vp.pos.y + 14}, data.health_label,
			data.health_color, ui.FONT_SIZE_XS, .Mono)
	}
	if len(data.health_detail) > 0 {
		ui.push_text(buf, {vp.pos.x + 4, vp.pos.y + 27}, data.health_detail,
			ui.with_alpha(ui.COL_WHITE, 0.45), ui.FONT_SIZE_XS, .Mono)
	}

	// Empty state.
	if store == nil || store.count == 0 {
		msg :: "Waiting for candle data..."
		ui.push_text(buf,
			{vp.pos.x + vp.size.x * 0.5 - 80, vp.pos.y + vp.size.y * 0.5},
			msg, ui.with_alpha(ui.COL_WHITE, 0.3), ui.FONT_SIZE_SM)
		ui.push(buf, ui.Cmd_Clip_Pop{})
		return
	}

	// Initialize zoom/scroll defaults.
	if data.zoom_level^ <= 0 {
		data.zoom_level^ = CANDLE_DEFAULT_ZOOM
	}

	chart_w := vp.size.x - Y_AXIS_WIDTH
	chart_h := vp.size.y - X_AXIS_HEIGHT
	if chart_w <= 0 || chart_h <= 0 {
		ui.push(buf, ui.Cmd_Clip_Pop{})
		return
	}

	price_h := chart_h * (1.0 - VOLUME_HEIGHT_PCT)
	vol_h := chart_h * VOLUME_HEIGHT_PCT

	zoom := clamp(data.zoom_level^, CANDLE_MIN_ZOOM, CANDLE_MAX_ZOOM)
	data.zoom_level^ = zoom

	visible_count := int(zoom)
	slot_w := chart_w / zoom
	body_w := max(slot_w * CANDLE_BODY_PCT, 1)

	// Scroll: 0 = newest candle at right edge.
	scroll := int(data.scroll_x^)
	max_scroll := max(store.count - visible_count, 0)
	scroll = clamp(scroll, 0, max_scroll)
	data.scroll_x^ = f32(scroll)

	// Determine visible range (indices into store, 0 = oldest).
	end_idx := store.count - scroll       // exclusive, newest visible + 1
	start_idx := max(end_idx - visible_count, 0)
	actual_visible := end_idx - start_idx

	// Find price range and max volume in visible window.
	price_lo := math.F64_MAX
	price_hi := -math.F64_MAX
	vol_max := f64(0)
	for i in start_idx ..< end_idx {
		c := services.get_candle(store, i)
		if c.low < price_lo do price_lo = c.low
		if c.high > price_hi do price_hi = c.high
		if c.volume > vol_max do vol_max = c.volume
	}
	if price_hi <= price_lo {
		price_hi = price_lo + 1
	}

	// Add buffer to price range.
	price_range := price_hi - price_lo
	price_lo -= price_range * PRICE_MARGIN_PCT
	price_hi += price_range * PRICE_MARGIN_PCT
	price_range = price_hi - price_lo
	if price_range <= 0 do price_range = 1

	// --- Price grid lines ---
	grid_lines := 5
	for g in 1 ..< grid_lines {
		gy := vp.pos.y + f32(g) * price_h / f32(grid_lines)
		ui.push(buf, ui.Cmd_Line{
			from      = {vp.pos.x, gy},
			to        = {vp.pos.x + chart_w, gy},
			color     = ui.with_alpha(ui.COL_WHITE, 0.06),
			thickness = 1,
		})
		grid_price := price_hi - (f64(g) / f64(grid_lines)) * price_range
		price_str := fmt.tprintf("%.1f", grid_price)
		ui.push_text(buf, {vp.pos.x + chart_w + 4, gy - 5}, price_str,
			ui.with_alpha(ui.COL_WHITE, 0.5), ui.FONT_SIZE_XS, .Mono)
	}

	// --- Draw candles ---
	// When fewer candles than visible slots, right-align them.
	slot_offset := visible_count - actual_visible
	for i in start_idx ..< end_idx {
		c := services.get_candle(store, i)
		slot := (i - start_idx) + slot_offset
		cx := vp.pos.x + f32(slot) * slot_w + slot_w * 0.5 // center of slot

		bullish := c.close >= c.open
		color := bullish ? ui.COL_GREEN : ui.COL_RED

		// Map prices to y coordinates (top = high price, bottom = low price).
		y_high  := vp.pos.y + f32((price_hi - c.high) / price_range) * price_h
		y_low   := vp.pos.y + f32((price_hi - c.low) / price_range) * price_h
		y_open  := vp.pos.y + f32((price_hi - c.open) / price_range) * price_h
		y_close := vp.pos.y + f32((price_hi - c.close) / price_range) * price_h

		body_top := min(y_open, y_close)
		body_bot := max(y_open, y_close)
		body_height := max(body_bot - body_top, 1) // min 1px body

		// Wick (high to low).
		ui.push(buf, ui.Cmd_Line{
			from      = {cx, y_high},
			to        = {cx, y_low},
			color     = color,
			thickness = 1,
		})

		// Body.
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {cx - body_w * 0.5, body_top}, size = {body_w, body_height}},
			color = color,
		})

		// --- Volume bar ---
		if vol_max > 0 && c.volume > 0 {
			vol_pct := f32(c.volume / vol_max)
			bar_h := vol_pct * vol_h
			vol_y := vp.pos.y + price_h + vol_h - bar_h
			vol_color := bullish ? ui.with_alpha(ui.COL_GREEN, 0.35) : ui.with_alpha(ui.COL_RED, 0.35)
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {cx - body_w * 0.5, vol_y}, size = {body_w, bar_h}},
				color = vol_color,
			})
		}
	}

	// --- Volume/price separator line ---
	sep_y := vp.pos.y + price_h
	ui.push(buf, ui.Cmd_Line{
		from      = {vp.pos.x, sep_y},
		to        = {vp.pos.x + chart_w, sep_y},
		color     = ui.with_alpha(ui.COL_WHITE, 0.1),
		thickness = 1,
	})

	// --- Current price line (most recent candle's close) ---
	if end_idx > 0 {
		latest := services.get_candle(store, end_idx - 1)
		curr_y := vp.pos.y + f32((price_hi - latest.close) / price_range) * price_h
		if curr_y >= vp.pos.y && curr_y <= vp.pos.y + price_h {
			// Dashed effect: draw multiple short segments.
			dash_len := f32(6)
			gap_len := f32(4)
			x := vp.pos.x
			for x < vp.pos.x + chart_w {
				x_end := min(x + dash_len, vp.pos.x + chart_w)
				ui.push(buf, ui.Cmd_Line{
					from      = {x, curr_y},
					to        = {x_end, curr_y},
					color     = ui.COL_YELLOW_ACCENT,
					thickness = 1,
				})
				x += dash_len + gap_len
			}

			// Price label on Y-axis.
			price_str := fmt.tprintf("%.1f", latest.close)
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {vp.pos.x + chart_w + 1, curr_y - 8}, size = {Y_AXIS_WIDTH - 2, 16}},
				color = ui.COL_YELLOW_ACCENT,
			})
			ui.push_text(buf, {vp.pos.x + chart_w + 4, curr_y + 4}, price_str,
				ui.COL_BLACK, ui.FONT_SIZE_XS, .Mono)
		}
	}

	// --- Time axis labels ---
	if actual_visible > 0 {
		// Show ~5 time labels spread across visible candles.
		label_count := min(5, actual_visible)
		step := actual_visible / label_count
		if step < 1 do step = 1

		for j := 0; j < actual_visible; j += step {
			idx := start_idx + j
			if idx >= end_idx do break
			c := services.get_candle(store, idx)
			// Convert window_start_ts (ms) to HH:MM.
			ts_sec := c.window_start_ts / 1000
			hours := (ts_sec / 3600) % 24
			mins := (ts_sec / 60) % 60
			time_str := fmt.tprintf("%02d:%02d", hours, mins)
			slot := j + slot_offset
			lx := vp.pos.x + f32(slot) * slot_w + slot_w * 0.5 - 12
			ly := vp.pos.y + chart_h + 14
			ui.push_text(buf, {lx, ly}, time_str,
				ui.with_alpha(ui.COL_WHITE, 0.4), ui.FONT_SIZE_XS, .Mono)
		}
	}

	// --- Candle count indicator (top-right of chart) ---
	count_str := fmt.tprintf("%d candles", store.count)
	ui.push_text(buf, {vp.pos.x + chart_w - 80, vp.pos.y + 27}, count_str,
		ui.with_alpha(ui.COL_WHITE, 0.35), ui.FONT_SIZE_XS, .Mono)

	// --- Handle pan/zoom input ---
	mx := data.input.mouse.pos.x
	my := data.input.mouse.pos.y
	mouse_in_chart := mx >= vp.pos.x && mx <= vp.pos.x + vp.size.x &&
	                  my >= vp.pos.y && my <= vp.pos.y + vp.size.y
	if mouse_in_chart {
		// Scroll wheel → zoom.
		wheel := data.input.mouse.scroll.y
		if wheel != 0 {
			zoom_delta := -wheel * zoom * 0.1
			data.zoom_level^ = clamp(zoom + zoom_delta, CANDLE_MIN_ZOOM, CANDLE_MAX_ZOOM)
		}
	}

	ui.push(buf, ui.Cmd_Clip_Pop{})
}
