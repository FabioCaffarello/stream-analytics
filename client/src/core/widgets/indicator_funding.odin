package widgets

// Funding Rate sub-plot for candle chart.
// Renders funding rate history from stats store as a line chart
// in a dedicated sub-area below the main chart.

import "core:fmt"
import "mr:services"
import "mr:ui"

Funding_Config :: struct {
	visible: bool,
	color:   ui.Color,
}

FUNDING_DEFAULT :: Funding_Config{
	visible = false,
	color   = ui.Color{0.2, 0.75, 0.95, 0.9}, // cyan
}

// Render funding rate sub-plot in a dedicated rect below the main chart area.
render_funding :: proc(
	buf: ^ui.Command_Buffer,
	rect: ui.Rect,
	ctx: ^Chart_Layer_Context,
	stats: ^services.Stats_Store,
	cfg: Funding_Config,
	measure_proc: proc(size: f32, text: string) -> ui.Vec2,
) -> bool {
	if !cfg.visible do return false
	if stats == nil || stats.count <= 1 do return false
	if rect.size.x <= 0 || rect.size.y <= 0 do return false

	// Background.
	ui.push(buf, ui.Cmd_Rect_Filled{rect = rect, color = ui.with_alpha(ui.COL_SURFACE_0, 0.6)})

	// Top separator.
	ui.push(buf, ui.Cmd_Line{
		from      = {rect.pos.x, rect.pos.y},
		to        = {ui.rect_right(rect), rect.pos.y},
		color     = ui.COL_DIVIDER,
		thickness = 1,
	})

	// Label.
	ui.push_text(buf, {rect.pos.x + 4, rect.pos.y + 10}, "Funding Rate",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	y_axis_w := f32(50)
	chart_w := rect.size.x - y_axis_w
	if chart_w <= 0 do return false

	pad := ui.Rect{pos = {rect.pos.x, rect.pos.y + 2}, size = {chart_w, rect.size.y - 4}}

	// Collect funding values within visible candle time range.
	// Stats are stored newest-first; we iterate all and filter by time range.
	// Use candle timestamps to define the visible window.
	vis_start_ts := i64(0)
	vis_end_ts := i64(0)
	if ctx.candles != nil && ctx.candle_count > 0 {
		c_first := services.get_candle(ctx.candles, ctx.candle_start)
		c_last := services.get_candle(ctx.candles, ctx.candle_start + ctx.candle_count - 1)
		vis_start_ts = c_first.window_start_ts
		vis_end_ts = c_last.window_end_ts
	}
	has_visible_window := vis_start_ts > 0 && vis_end_ts > vis_start_ts

	// Gather funding points in time window.
	FUNDING_MAX_POINTS :: 128
	fund_vals: [FUNDING_MAX_POINTS]f64
	fund_ts: [FUNDING_MAX_POINTS]i64
	fund_count := 0

	use_window_filter := has_visible_window
	for i in 0 ..< stats.count {
		s := services.get_stats(stats, i)
		// Stats unix is in seconds; candle timestamps are in ms.
		s_ms := s.unix * 1000 if s.unix < 1_000_000_000_000 else s.unix
		if use_window_filter && (s_ms < vis_start_ts || s_ms > vis_end_ts) do continue
		if fund_count >= FUNDING_MAX_POINTS do break
		fund_vals[fund_count] = s.funding
		fund_ts[fund_count] = s_ms
		fund_count += 1
	}
	// Fallback: if the visible candle window has no stats, render most-recent stats anyway.
	if fund_count <= 1 && use_window_filter {
		use_window_filter = false
		fund_count = 0
		for i in 0 ..< stats.count {
			s := services.get_stats(stats, i)
			s_ms := s.unix * 1000 if s.unix < 1_000_000_000_000 else s.unix
			if fund_count >= FUNDING_MAX_POINTS do break
			fund_vals[fund_count] = s.funding
			fund_ts[fund_count] = s_ms
			fund_count += 1
		}
	}
	if fund_count <= 1 do return false

	// Find min/max for Y scaling.
	val_min := fund_vals[0]
	val_max := fund_vals[0]
	for i in 1 ..< fund_count {
		if fund_vals[i] < val_min do val_min = fund_vals[i]
		if fund_vals[i] > val_max do val_max = fund_vals[i]
	}
	// Add 10% buffer.
	val_range := val_max - val_min
	if val_range <= 0 {
		val_range = 0.0001
		val_min -= 0.00005
		val_max += 0.00005
	} else {
		val_min -= val_range * 0.1
		val_max += val_range * 0.1
		val_range = val_max - val_min
	}

	// Zero line.
	if val_min < 0 && val_max > 0 {
		zero_t := f32((0 - val_min) / val_range)
		zero_y := pad.pos.y + pad.size.y * (1.0 - zero_t)
		ui.push(buf, ui.Cmd_Line{
			from      = {pad.pos.x, zero_y},
			to        = {pad.pos.x + chart_w, zero_y},
			color     = ui.with_alpha(ui.COL_WHITE, 0.08),
			thickness = 1,
		})
	}

	// Y-axis labels (top = max, bottom = min).
	top_buf: [16]u8
	top_str := fmt.bprintf(top_buf[:], "%.4f%%", val_max * 100)
	ui.push_text(buf, {pad.pos.x + chart_w + 2, pad.pos.y + 10}, top_str,
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	bot_buf: [16]u8
	bot_str := fmt.bprintf(bot_buf[:], "%.4f%%", val_min * 100)
	ui.push_text(buf, {pad.pos.x + chart_w + 2, ui.rect_bottom(pad) - 4}, bot_str,
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	// Render line — align stats points to candle slots via binary search.
	prev_x := f32(0)
	prev_y := f32(0)
	has_prev := false

	for i in 0 ..< fund_count {
		x := f32(0)
		if has_visible_window {
			// Binary search candle slot alignment (PRD-0007 M3.4).
			x = stats_ts_to_x(ctx, fund_ts[i])
		} else {
			den := f32(max(fund_count - 1, 1))
			slot_span := f32(max(ctx.candle_count - 1, 0))
			slot_f := f32(ctx.slot_offset) + (f32(i) / den) * slot_span
			x = pad.pos.x + slot_f * ctx.slot_width + ctx.slot_width * 0.5
		}
		t_y := f32((fund_vals[i] - val_min) / val_range)
		y := pad.pos.y + pad.size.y * (1.0 - t_y)

		// Color: green for positive funding, red for negative.
		line_color := cfg.color
		if fund_vals[i] < 0 {
			line_color = ui.COL_RED
		} else if fund_vals[i] > 0 {
			line_color = ui.COL_GREEN
		}

		if has_prev {
			ui.push(buf, ui.Cmd_Line{
				from      = {prev_x, prev_y},
				to        = {x, y},
				color     = line_color,
				thickness = 1,
			})
		}
		prev_x = x
		prev_y = y
		has_prev = true
	}
	return has_prev
}
