package widgets

// Candlestick chart interaction: current price, high/low labels, crosshair,
// sync crosshair, OHLCV bar, zoom/pan input.

import "core:fmt"
import "core:math"
import "mr:services"
import "mr:ui"

draw_candle_current_price :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	if ctx.end_idx <= 0 do return

	latest := services.get_candle(ctx.store, ctx.end_idx - 1)
	curr_y := ctx.inner.pos.y + f32((ctx.price_hi - latest.close) / ctx.price_range) * ctx.price_h
	if curr_y < ctx.inner.pos.y || curr_y > ctx.inner.pos.y + ctx.price_h do return

	dash_len := f32(6)
	gap_len := f32(4)
	x := ctx.inner.pos.x
	for x < ctx.inner.pos.x + ctx.chart_w {
		x_end := min(x + dash_len, ctx.inner.pos.x + ctx.chart_w)
		ui.push(buf, ui.Cmd_Line{
			from      = {x, curr_y},
			to        = {x_end, curr_y},
			color     = ui.COL_YELLOW_ACCENT,
			thickness = 1,
		})
		x += dash_len + gap_len
	}

	curr_pbuf: [16]u8
	price_str := ui.format_price(curr_pbuf[:], latest.close, ui.auto_price_decimals(latest.close))
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {ctx.inner.pos.x + ctx.chart_w + 1, curr_y - 8}, size = {Y_AXIS_WIDTH - 2, 16}},
		color = ui.COL_YELLOW_ACCENT,
	})
	ui.push_text(buf, {ctx.inner.pos.x + ctx.chart_w + 4, curr_y + 4}, price_str,
		ui.COL_BLACK, ui.FONT_SIZE_XS, .Mono)

	if data.now_ms <= 0 || data.timeframe_ms <= 0 do return

	next_close_ms := latest.window_start_ts + data.timeframe_ms
	remaining_ms := next_close_ms - data.now_ms
	if remaining_ms < 0 do remaining_ms = 0
	remaining_sec := remaining_ms / 1000
	cd_min := remaining_sec / 60
	cd_sec := remaining_sec % 60
	cd_buf: [8]u8
	cd_str := fmt.bprintf(cd_buf[:], "%02d:%02d", cd_min, cd_sec)
	cd_y := curr_y + 12
	if cd_y + 16 > ctx.inner.pos.y + ctx.price_h do return

	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {ctx.inner.pos.x + ctx.chart_w + 1, cd_y - 2}, size = {Y_AXIS_WIDTH - 2, 14}},
		color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.7),
	})
	ui.push_text(buf, {ctx.inner.pos.x + ctx.chart_w + 4, cd_y + 8}, cd_str,
		ui.COL_BLACK, ui.FONT_SIZE_XS, .Mono)
}

draw_candle_high_low_labels :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	if ctx.end_idx <= ctx.start_idx || ctx.price_range <= 0 do return
	if ctx.raw_high <= ctx.raw_low do return

	// Skip if latest close is at the extreme (current price label already covers it).
	latest := services.get_candle(ctx.store, ctx.end_idx - 1)
	high_matches_close := math.abs(ctx.raw_high - latest.close) < ctx.price_range * 0.005
	low_matches_close := math.abs(ctx.raw_low - latest.close) < ctx.price_range * 0.005

	label_x := ctx.inner.pos.x + ctx.chart_w + 4

	if !high_matches_close {
		high_y := ctx.inner.pos.y + f32((ctx.price_hi - ctx.raw_high) / ctx.price_range) * ctx.price_h
		if high_y >= ctx.inner.pos.y && high_y <= ctx.inner.pos.y + ctx.price_h {
			// Dashed leader line from candle to right margin.
			high_slot := (ctx.raw_high_idx - ctx.start_idx) + ctx.slot_offset
			cx := ctx.inner.pos.x + f32(high_slot) * ctx.slot_w + ctx.slot_w * 0.5
			dash := f32(4)
			gap := f32(3)
			x := cx
			for x < ctx.inner.pos.x + ctx.chart_w {
				x_end := min(x + dash, ctx.inner.pos.x + ctx.chart_w)
				ui.push(buf, ui.Cmd_Line{
					from      = {x, high_y},
					to        = {x_end, high_y},
					color     = ui.with_alpha(ui.COL_GREEN, 0.35),
					thickness = 1,
				})
				x += dash + gap
			}
			// Price label in right margin.
			pbuf: [16]u8
			price_str := ui.format_price(pbuf[:], ctx.raw_high, ui.auto_price_decimals(ctx.raw_high))
			ui.push_text(buf, {label_x, high_y - 5}, price_str,
				ui.with_alpha(ui.COL_GREEN, 0.7), ui.FONT_SIZE_XS, .Mono)
			// Small "H" marker.
			ui.push_text(buf, {label_x - 10, high_y - 5}, "H",
				ui.with_alpha(ui.COL_GREEN, 0.5), ui.FONT_SIZE_XS, .Mono)
		}
	}

	if !low_matches_close {
		low_y := ctx.inner.pos.y + f32((ctx.price_hi - ctx.raw_low) / ctx.price_range) * ctx.price_h
		if low_y >= ctx.inner.pos.y && low_y <= ctx.inner.pos.y + ctx.price_h {
			low_slot := (ctx.raw_low_idx - ctx.start_idx) + ctx.slot_offset
			cx := ctx.inner.pos.x + f32(low_slot) * ctx.slot_w + ctx.slot_w * 0.5
			dash := f32(4)
			gap := f32(3)
			x := cx
			for x < ctx.inner.pos.x + ctx.chart_w {
				x_end := min(x + dash, ctx.inner.pos.x + ctx.chart_w)
				ui.push(buf, ui.Cmd_Line{
					from      = {x, low_y},
					to        = {x_end, low_y},
					color     = ui.with_alpha(ui.COL_RED, 0.35),
					thickness = 1,
				})
				x += dash + gap
			}
			pbuf: [16]u8
			price_str := ui.format_price(pbuf[:], ctx.raw_low, ui.auto_price_decimals(ctx.raw_low))
			ui.push_text(buf, {label_x, low_y - 5}, price_str,
				ui.with_alpha(ui.COL_RED, 0.7), ui.FONT_SIZE_XS, .Mono)
			ui.push_text(buf, {label_x - 10, low_y - 5}, "L",
				ui.with_alpha(ui.COL_RED, 0.5), ui.FONT_SIZE_XS, .Mono)
		}
	}
}

draw_candle_crosshair :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	state := data.crosshair
	if state == nil do return

	mx := data.input.mouse.pos.x
	my := data.input.mouse.pos.y
	chart_left := ctx.inner.pos.x
	chart_right := ctx.inner.pos.x + ctx.chart_w
	chart_top := ctx.inner.pos.y
	chart_bot := ctx.inner.pos.y + ctx.price_h

	in_chart := mx >= chart_left && mx <= chart_right && my >= chart_top && my <= chart_bot
	state.active = in_chart
	state.mouse_pos = {mx, my}
	state.hovered_idx = -1

	if !in_chart do return

	// Price at cursor Y.
	y_pct := f64(my - chart_top) / f64(ctx.price_h)
	state.price_at_y = ctx.price_hi - y_pct * ctx.price_range

	// Find hovered candle slot.
	rel_x := mx - chart_left
	slot := int(rel_x / ctx.slot_w) - ctx.slot_offset
	candle_idx := ctx.start_idx + slot
	if candle_idx >= ctx.start_idx && candle_idx < ctx.end_idx {
		state.hovered_idx = candle_idx
	}

	// Vertical crosshair line (dashed).
	dash_len := f32(4)
	gap_len := f32(3)
	y := chart_top
	for y < chart_bot {
		y_end := min(y + dash_len, chart_bot)
		ui.push(buf, ui.Cmd_Line{
			from      = {mx, y},
			to        = {mx, y_end},
			color     = ui.COL_CROSS_HAIR,
			thickness = 1,
		})
		y += dash_len + gap_len
	}

	// Horizontal crosshair line (dashed).
	x := chart_left
	for x < chart_right {
		x_end := min(x + dash_len, chart_right)
		ui.push(buf, ui.Cmd_Line{
			from      = {x, my},
			to        = {x_end, my},
			color     = ui.COL_CROSS_HAIR,
			thickness = 1,
		})
		x += dash_len + gap_len
	}

	// Y-axis price label at crosshair level.
	cross_pbuf: [16]u8
	price_str := ui.format_price(cross_pbuf[:], state.price_at_y, ui.auto_price_decimals(state.price_at_y))
	label_w := data.text.measure(ui.FONT_SIZE_XS, price_str).x + 8
	label_h := f32(14)
	label_x := chart_right + 1
	label_y := my - label_h * 0.5
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {label_x, label_y}, size = {label_w, label_h}},
		color = ui.with_alpha(ui.COL_CROSS_HAIR, 0.8),
	})
	ui.push_text(buf, {label_x + 4, label_y + label_h - 3}, price_str,
		ui.COL_BLACK, ui.FONT_SIZE_XS, .Mono)

	// X-axis time label at crosshair position.
	if state.hovered_idx >= 0 && state.hovered_idx < ctx.store.count {
		c := services.get_candle(ctx.store, state.hovered_idx)
		ts_sec := c.window_start_ts / 1000
		hours := (ts_sec / 3600) % 24
		mins := (ts_sec / 60) % 60
		cross_tbuf: [12]u8
		time_str: string
		if ctx.timeframe_ms > 0 && ctx.timeframe_ms < 60_000 {
			secs := ts_sec % 60
			time_str = fmt.bprintf(cross_tbuf[:], "%02d:%02d:%02d", hours, mins, secs)
		} else {
			time_str = fmt.bprintf(cross_tbuf[:], "%02d:%02d", hours, mins)
		}
		time_w := data.text.measure(ui.FONT_SIZE_XS, time_str).x + 8
		time_h := f32(14)
		time_x := mx - time_w * 0.5
		time_y := chart_bot + 2
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {time_x, time_y}, size = {time_w, time_h}},
			color = ui.with_alpha(ui.COL_CROSS_HAIR, 0.8),
		})
		ui.push_text(buf, {time_x + 4, time_y + time_h - 3}, time_str,
			ui.COL_BLACK, ui.FONT_SIZE_XS, .Mono)
	}

	// OHLCV tooltip for hovered candle.
	if state.hovered_idx >= 0 && state.hovered_idx < ctx.store.count {
		c := services.get_candle(ctx.store, state.hovered_idx)
		decs := ui.auto_price_decimals(c.close)
		tip_o: [16]u8
		tip_h: [16]u8
		tip_l: [16]u8
		tip_c: [16]u8
		tip: ui.Tooltip_Data
		tip.lines[0] = {label = "O: ", value = ui.format_price(tip_o[:], c.open, decs),  color = ui.COL_TEXT_PRIMARY}
		tip.lines[1] = {label = "H: ", value = ui.format_price(tip_h[:], c.high, decs),  color = ui.COL_TEXT_PRIMARY}
		tip.lines[2] = {label = "L: ", value = ui.format_price(tip_l[:], c.low, decs),   color = ui.COL_TEXT_PRIMARY}
		tip.lines[3] = {label = "C: ", value = ui.format_price(tip_c[:], c.close, decs), color = c.close >= c.open ? ui.COL_GREEN : ui.COL_RED}
		tip_v: [16]u8
		tip.lines[4] = {label = "V: ", value = fmt.bprintf(tip_v[:], "%.1f", c.volume), color = ui.COL_TEXT_SECONDARY}
		tip.count = 5
		ui.draw_tooltip(buf, {mx, my}, tip, data.text.measure, ctx.inner)
	}
}

// Sync crosshair: dimmed horizontal line from another chart's crosshair price.
draw_candle_sync_crosshair :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	if !data.sync_active || data.sync_price <= 0 do return
	// Don't draw sync line if local crosshair is active (avoid clutter).
	if data.crosshair != nil && data.crosshair.active do return
	if ctx.price_range <= 0 do return

	y_pct := f32((ctx.price_hi - data.sync_price) / ctx.price_range)
	sync_y := ctx.inner.pos.y + y_pct * ctx.price_h
	if sync_y < ctx.inner.pos.y || sync_y > ctx.inner.pos.y + ctx.price_h do return

	// Dimmed dashed horizontal line.
	dash_len := f32(6)
	gap_len := f32(4)
	chart_right := ctx.inner.pos.x + ctx.chart_w
	x := ctx.inner.pos.x
	for x < chart_right {
		x_end := min(x + dash_len, chart_right)
		ui.push(buf, ui.Cmd_Line{
			from      = {x, sync_y},
			to        = {x_end, sync_y},
			color     = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.35),
			thickness = 1,
		})
		x += dash_len + gap_len
	}

	// Small price label on the right.
	cross_pbuf: [16]u8
	price_str := ui.format_price(cross_pbuf[:], data.sync_price, ui.auto_price_decimals(data.sync_price))
	label_w := data.text.measure(ui.FONT_SIZE_XS, price_str).x + 6
	label_h := f32(12)
	label_x := chart_right + 1
	label_y := sync_y - label_h * 0.5
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {label_x, label_y}, size = {label_w, label_h}},
		color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.25),
	})
	ui.push_text(buf, {label_x + 3, label_y + label_h - 2}, price_str,
		ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Mono)
}

// Persistent OHLCV data bar at chart top-left (shows hovered or latest candle).
draw_candle_ohlcv_bar :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	if ctx.store == nil || ctx.store.count <= 0 do return

	// Use hovered candle if crosshair active, otherwise latest.
	idx := ctx.end_idx - 1
	if data.crosshair != nil && data.crosshair.active && data.crosshair.hovered_idx >= 0 {
		idx = data.crosshair.hovered_idx
	}
	if idx < 0 || idx >= ctx.store.count do return

	c := services.get_candle(ctx.store, idx)
	decs := ui.auto_price_decimals(c.close)
	bullish := c.close >= c.open
	close_color := bullish ? ui.COL_GREEN : ui.COL_RED

	x := ctx.inner.pos.x + 4
	y := ctx.inner.pos.y + 14

	// Semi-transparent backdrop for readability.
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {ctx.inner.pos.x, ctx.inner.pos.y}, size = {ctx.chart_w, 20}},
		color = ui.with_alpha(ui.COL_SURFACE_0, 0.7),
	})

	o_buf: [16]u8
	o_str := ui.format_price(o_buf[:], c.open, decs)
	ui.push_text(buf, {x, y}, "O", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, "O").x + 2
	ui.push_text(buf, {x, y}, o_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, o_str).x + 6

	h_buf: [16]u8
	h_str := ui.format_price(h_buf[:], c.high, decs)
	ui.push_text(buf, {x, y}, "H", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, "H").x + 2
	ui.push_text(buf, {x, y}, h_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, h_str).x + 6

	l_buf: [16]u8
	l_str := ui.format_price(l_buf[:], c.low, decs)
	ui.push_text(buf, {x, y}, "L", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, "L").x + 2
	ui.push_text(buf, {x, y}, l_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, l_str).x + 6

	c_buf: [16]u8
	c_str := ui.format_price(c_buf[:], c.close, decs)
	ui.push_text(buf, {x, y}, "C", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, "C").x + 2
	ui.push_text(buf, {x, y}, c_str, close_color, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, c_str).x + 6

	v_buf: [16]u8
	v_str := fmt.bprintf(v_buf[:], "%.1f", c.volume)
	ui.push_text(buf, {x, y}, "V", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, "V").x + 2
	ui.push_text(buf, {x, y}, v_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, v_str).x + 6

	// Cross-indicator readout: append active indicator values when crosshair hovers a candle.
	is_hover := data.crosshair != nil && data.crosshair.active && data.crosshair.hovered_idx >= 0
	if is_hover && idx < ctx.store.count {
		ind_color := ui.COL_TEXT_SECONDARY

		// MA (EMA9).
		if data.show_ma && data.ma_periods[0] > 0 {
			ema_val := readout_ema_at(ctx.store, data.ma_periods[0], idx)
			if ema_val > 0 {
				ema_buf: [24]u8
				ema_str := fmt.bprintf(ema_buf[:], "EMA%d:", data.ma_periods[0])
				ui.push_text(buf, {x, y}, ema_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
				x += data.text.measure(ui.FONT_SIZE_XS, ema_str).x + 2
				ev_buf: [16]u8
				ev_str := ui.format_price(ev_buf[:], ema_val, decs)
				ui.push_text(buf, {x, y}, ev_str, {0.98, 0.85, 0.2, 0.9}, ui.FONT_SIZE_XS, .Mono)
				x += data.text.measure(ui.FONT_SIZE_XS, ev_str).x + 6
			}
		}

		// RSI.
		if data.show_rsi && data.rsi_period > 0 {
			rsi_val := readout_rsi_at(ctx.store, data.rsi_period, idx)
			if rsi_val >= 0 {
				ui.push_text(buf, {x, y}, "RSI:", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
				x += data.text.measure(ui.FONT_SIZE_XS, "RSI:").x + 2
				rsi_buf: [8]u8
				rsi_str := fmt.bprintf(rsi_buf[:], "%.1f", rsi_val)
				rsi_color := rsi_val >= 70 ? ui.COL_RED : (rsi_val <= 30 ? ui.COL_GREEN : ind_color)
				ui.push_text(buf, {x, y}, rsi_str, rsi_color, ui.FONT_SIZE_XS, .Mono)
				x += data.text.measure(ui.FONT_SIZE_XS, rsi_str).x + 6
			}
		}

		// MACD.
		if data.show_macd && data.macd_fast > 0 && data.macd_slow > 0 {
			macd_val := readout_macd_at(ctx.store, data.macd_fast, data.macd_slow, idx)
			ui.push_text(buf, {x, y}, "MACD:", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			x += data.text.measure(ui.FONT_SIZE_XS, "MACD:").x + 2
			sign := macd_val >= 0 ? "+" : ""
			macd_buf: [16]u8
			macd_str := fmt.bprintf(macd_buf[:], "%s%.1f", sign, macd_val)
			macd_color := macd_val >= 0 ? ui.COL_GREEN : ui.COL_RED
			ui.push_text(buf, {x, y}, macd_str, macd_color, ui.FONT_SIZE_XS, .Mono)
			x += data.text.measure(ui.FONT_SIZE_XS, macd_str).x + 6
		}
	}
}

apply_candle_zoom_input :: proc(data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	mx := data.input.mouse.pos.x
	my := data.input.mouse.pos.y
	mouse_in_chart := mx >= ctx.inner.pos.x && mx <= ctx.inner.pos.x + ctx.inner.size.x &&
		my >= ctx.inner.pos.y && my <= ctx.inner.pos.y + ctx.inner.size.y
	if !mouse_in_chart do return

	// Double-click on Y-axis area resets to auto-fit mode.
	DBLCLICK_MS :: i64(400)
	yaxis_left := ctx.inner.pos.x + ctx.chart_w
	in_yaxis := mx >= yaxis_left && my >= ctx.inner.pos.y && my <= ctx.inner.pos.y + ctx.price_h
	if in_yaxis && data.input.mouse.pressed[.Left] && data.crosshair != nil {
		now := data.now_ms
		if now > 0 && data.crosshair.last_yaxis_click_ms > 0 &&
			(now - data.crosshair.last_yaxis_click_ms) < DBLCLICK_MS {
			data.zoom_level^ = 0
			data.scroll_x^ = 0
			data.crosshair.last_yaxis_click_ms = 0
			return
		}
		data.crosshair.last_yaxis_click_ms = now
	}

	visible_count := max(int(ctx.zoom), 1)
	max_scroll := f32(max(ctx.store.count - visible_count, 0))
	mouse_x_ratio := clamp((mx - ctx.inner.pos.x) / max(ctx.chart_w, 1), f32(0), f32(0.999))
	anchor_idx := ctx.start_idx
	if ctx.actual_visible > 0 {
		anchor_rel := clamp(int(mouse_x_ratio * f32(ctx.actual_visible)), 0, ctx.actual_visible - 1)
		anchor_idx = ctx.start_idx + anchor_rel
	}

	// Pan horizontally by dragging the chart with left mouse button.
	if data.input.mouse.buttons[.Left] {
		drag_slots := data.input.mouse.delta.x / max(ctx.slot_w, 1)
		if drag_slots != 0 {
			data.scroll_x^ = clamp(data.scroll_x^ + drag_slots, 0, max_scroll)
		}
	}

	// Shift + wheel (or horizontal wheel) also pans in candle slots.
	pan_wheel := f32(0)
	if data.input.modifiers.shift {
		pan_wheel = -data.input.mouse.scroll.y
	} else if data.input.mouse.scroll.x != 0 {
		pan_wheel = data.input.mouse.scroll.x
	}
	if pan_wheel != 0 {
		data.scroll_x^ = clamp(data.scroll_x^ + pan_wheel * 3, 0, max_scroll)
	}

	// Vertical wheel keeps zoom behavior (unless Shift is held for pan).
	wheel := data.input.mouse.scroll.y
	if data.input.modifiers.shift do wheel = 0
	if wheel == 0 do return
	zoom_delta := -wheel * ctx.zoom * 0.1
	data.zoom_level^ = clamp(ctx.zoom + zoom_delta, CANDLE_MIN_ZOOM, CANDLE_MAX_ZOOM)

	next_visible := max(int(data.zoom_level^), 1)
	next_start_max := max(ctx.store.count - next_visible, 0)
	next_start := anchor_idx - int(mouse_x_ratio * f32(next_visible))
	next_start = clamp(next_start, 0, next_start_max)
	next_end := next_start + next_visible
	data.scroll_x^ = f32(max(ctx.store.count - next_end, 0))
	next_max_scroll := f32(max(ctx.store.count - next_visible, 0))
	data.scroll_x^ = clamp(data.scroll_x^, 0, next_max_scroll)
}
