package widgets

// Candlestick chart axes: price grid, day boundaries, time axis labels, count indicator.

import "core:fmt"
import "core:math"
import "mr:services"
import "mr:ui"

draw_candle_price_grid :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	if ctx.price_range <= 0 do return
	step := compute_round_grid_step(ctx.price_range)
	if step <= 0 do return
	first := math.ceil(ctx.price_lo / step) * step
	count := 0
	for gp := first; gp < ctx.price_hi && count < 12; gp += step {
		count += 1
		t := f32((ctx.price_hi - gp) / ctx.price_range)
		gy := ctx.inner.pos.y + t * ctx.price_h
		ui.push(buf, ui.Cmd_Line{
			from      = {ctx.inner.pos.x, gy},
			to        = {ctx.inner.pos.x + ctx.chart_w, gy},
			color     = ui.with_alpha(ui.COL_WHITE, 0.06),
			thickness = 1,
		})
		grid_pbuf: [16]u8
		price_str := ui.format_price(grid_pbuf[:], gp, ui.auto_price_decimals(gp))
		ui.push_text(buf, {ctx.inner.pos.x + ctx.chart_w + 4, gy - 5}, price_str,
			ui.with_alpha(ui.COL_WHITE, 0.5), ui.FONT_SIZE_XS, .Mono)
	}
}

// Compute a "round" grid step for ~5-6 grid lines.
// Returns steps like 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25, 50, 100, 250, 500, 1000, 5000.
compute_round_grid_step :: proc(price_range: f64) -> f64 {
	if price_range <= 0 do return 1
	target := price_range / 6
	mag := math.pow(10.0, math.floor(math.log10(target)))
	if mag <= 0 do mag = 1
	normalized := target / mag
	if normalized < 1.5 do return mag
	if normalized < 3.5 do return mag * 2.5
	if normalized < 7.5 do return mag * 5
	return mag * 10
}

draw_candle_day_boundaries :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	if ctx.actual_visible <= 0 do return
	MS_PER_DAY :: i64(86_400_000)
	prev_day := i64(-1)
	for i in ctx.start_idx ..< ctx.end_idx {
		c := services.get_candle(ctx.store, i)
		day := c.window_start_ts / MS_PER_DAY
		if prev_day >= 0 && day != prev_day {
			slot := (i - ctx.start_idx) + ctx.slot_offset
			x := ctx.inner.pos.x + f32(slot) * ctx.slot_w
			// Dashed vertical line at day boundary.
			dash_len := f32(4)
			gap_len := f32(3)
			y := ctx.inner.pos.y
			for y < ctx.inner.pos.y + ctx.price_h {
				y_end := min(y + dash_len, ctx.inner.pos.y + ctx.price_h)
				ui.push(buf, ui.Cmd_Line{
					from      = {x, y},
					to        = {x, y_end},
					color     = ui.with_alpha(ui.COL_WHITE, 0.15),
					thickness = 1,
				})
				y += dash_len + gap_len
			}
			// Date label (MM/DD) below chart area.
			m, d := unix_ms_to_mday(c.window_start_ts)
			date_buf: [8]u8
			date_str := fmt.bprintf(date_buf[:], "%02d/%02d", m, d)
			ui.push_text(buf, {x + 2, ctx.inner.pos.y + ctx.chart_h + 14}, date_str,
				ui.with_alpha(ui.COL_WHITE, 0.5), ui.FONT_SIZE_XS, .Mono)
		}
		prev_day = day
	}
}

// Convert unix milliseconds to (month, day) using civil_from_days algorithm.
unix_ms_to_mday :: proc(ts_ms: i64) -> (month: int, day: int) {
	z := ts_ms / 86_400_000 + 719468
	era: i64
	if z >= 0 {
		era = z / 146097
	} else {
		era = (z - 146096) / 146097
	}
	doe := z - era * 146097
	yoe := (doe - doe / 1461 + doe / 36524 - doe / 146097) / 365
	doy := doe - (365 * yoe + yoe / 4 - yoe / 100)
	mp := (5 * doy + 2) / 153
	day = int(doy - (153 * mp + 2) / 5 + 1)
	if mp < 10 {
		month = int(mp + 3)
	} else {
		month = int(mp - 9)
	}
	return
}

draw_candle_time_axis_labels :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	if ctx.actual_visible <= 0 do return

	label_count := min(5, ctx.actual_visible)
	step := ctx.actual_visible / label_count
	if step < 1 do step = 1

	sub_minute := ctx.timeframe_ms > 0 && ctx.timeframe_ms < 60_000
	for j := 0; j < ctx.actual_visible; j += step {
		idx := ctx.start_idx + j
		if idx >= ctx.end_idx do break
		c := services.get_candle(ctx.store, idx)
		ts_sec := c.window_start_ts / 1000
		hours := (ts_sec / 3600) % 24
		mins := (ts_sec / 60) % 60
		axis_tbuf: [12]u8
		time_str: string
		if sub_minute {
			secs := ts_sec % 60
			time_str = fmt.bprintf(axis_tbuf[:], "%02d:%02d:%02d", hours, mins, secs)
		} else {
			time_str = fmt.bprintf(axis_tbuf[:], "%02d:%02d", hours, mins)
		}
		slot := j + ctx.slot_offset
		lx := ctx.inner.pos.x + f32(slot) * ctx.slot_w + ctx.slot_w * 0.5 - 12
		ly := ctx.inner.pos.y + ctx.chart_h + 14
		ui.push_text(buf, {lx, ly}, time_str,
			ui.with_alpha(ui.COL_WHITE, 0.4), ui.FONT_SIZE_XS, .Mono)
	}
}

draw_candle_count_indicator :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	cnt_buf: [16]u8
	count_str := fmt.bprintf(cnt_buf[:], "%d candles", ctx.store.count)
	ui.push_text(buf, {ctx.inner.pos.x + ctx.chart_w - 80, ctx.inner.pos.y + 4}, count_str,
		ui.with_alpha(ui.COL_WHITE, 0.35), ui.FONT_SIZE_XS, .Mono)
}
