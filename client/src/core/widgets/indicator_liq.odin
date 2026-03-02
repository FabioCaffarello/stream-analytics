package widgets

// Liquidation Volume sub-plot for candle chart.
// Renders buy/sell liquidation volumes from stats store as stacked bars
// in a dedicated sub-area below the main chart.

import "core:fmt"
import "core:math"
import "mr:services"
import "mr:ui"

Liq_Config :: struct {
	visible:   bool,
	color_buy:  ui.Color,
	color_sell: ui.Color,
}

LIQ_DEFAULT :: Liq_Config{
	visible    = false,
	color_buy  = ui.Color{0.18, 0.74, 0.52, 0.7}, // green
	color_sell = ui.Color{0.96, 0.28, 0.37, 0.7}, // red
}

// Render liquidation volume sub-plot in a dedicated rect below the main chart.
render_liq :: proc(
	buf: ^ui.Command_Buffer,
	rect: ui.Rect,
	ctx: ^Chart_Layer_Context,
	stats: ^services.Stats_Store,
	cfg: Liq_Config,
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
	ui.push_text(buf, {rect.pos.x + 4, rect.pos.y + 10}, "Liquidations",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	y_axis_w := f32(50)
	chart_w := rect.size.x - y_axis_w
	if chart_w <= 0 do return false

	pad := ui.Rect{pos = {rect.pos.x, rect.pos.y + 2}, size = {chart_w, rect.size.y - 4}}

	// Collect liq values within visible candle time range.
	vis_start_ts := i64(0)
	vis_end_ts := i64(0)
	if ctx.candles != nil && ctx.candle_count > 0 {
		c_first := services.get_candle(ctx.candles, ctx.candle_start)
		c_last := services.get_candle(ctx.candles, ctx.candle_start + ctx.candle_count - 1)
		vis_start_ts = c_first.window_start_ts
		vis_end_ts = c_last.window_end_ts
	}
	has_visible_window := vis_start_ts > 0 && vis_end_ts > vis_start_ts

	LIQ_MAX_POINTS :: 128
	liq_buys: [LIQ_MAX_POINTS]f64
	liq_sells: [LIQ_MAX_POINTS]f64
	liq_ts: [LIQ_MAX_POINTS]i64
	liq_count := 0

	use_window_filter := has_visible_window
	for i in 0 ..< stats.count {
		s := services.get_stats(stats, i)
		s_ms := s.unix * 1000 if s.unix < 1_000_000_000_000 else s.unix
		if use_window_filter && (s_ms < vis_start_ts || s_ms > vis_end_ts) do continue
		if liq_count >= LIQ_MAX_POINTS do break
		liq_buys[liq_count] = s.liq_buy
		liq_sells[liq_count] = s.liq_sell
		liq_ts[liq_count] = s_ms
		liq_count += 1
	}
	// Fallback: render the most recent points when the visible candle window has no stats.
	if liq_count <= 0 && use_window_filter {
		use_window_filter = false
		liq_count = 0
		for i in 0 ..< stats.count {
			s := services.get_stats(stats, i)
			s_ms := s.unix * 1000 if s.unix < 1_000_000_000_000 else s.unix
			if liq_count >= LIQ_MAX_POINTS do break
			liq_buys[liq_count] = s.liq_buy
			liq_sells[liq_count] = s.liq_sell
			liq_ts[liq_count] = s_ms
			liq_count += 1
		}
	}
	if liq_count <= 0 do return false

	// Find max for Y scaling (stacked bars: max of buy + sell).
	vol_max := f64(0)
	for i in 0 ..< liq_count {
		total := liq_buys[i] + liq_sells[i]
		if total > vol_max do vol_max = total
	}
	if vol_max <= 0 do return false

	// Add 10% headroom.
	vol_max *= 1.1

	// Y-axis label.
	top_buf: [16]u8
	top_str := fmt.bprintf(top_buf[:], "%.0f", vol_max)
	ui.push_text(buf, {pad.pos.x + chart_w + 2, pad.pos.y + 10}, top_str,
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	// Bar rendering — align to candle slots via binary search.
	// Compute bar width from avg spacing.
	bar_w := chart_w / f32(max(liq_count, 1)) * 0.7
	bar_w = clamp(bar_w, 2, 20)

	for i in 0 ..< liq_count {
		x := f32(0)
		if has_visible_window {
			// Binary search candle slot alignment (PRD-0007 M3.4).
			x = stats_ts_to_x(ctx, liq_ts[i]) - bar_w * 0.5
		} else {
			den := f32(max(liq_count - 1, 1))
			slot_span := f32(max(ctx.candle_count - 1, 0))
			slot_f := f32(ctx.slot_offset) + (f32(i) / den) * slot_span
			x = pad.pos.x + slot_f * ctx.slot_width + ctx.slot_width * 0.5 - bar_w * 0.5
		}

		total := liq_buys[i] + liq_sells[i]
		if total <= 0 do continue

		total_h := f32(total / vol_max) * pad.size.y
		buy_h := total_h * f32(liq_buys[i] / total)
		sell_h := total_h - buy_h

		bar_bot := ui.rect_bottom(pad)

		// Buy (green) on bottom, sell (red) on top.
		if buy_h > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x, bar_bot - buy_h}, size = {bar_w, buy_h}},
				color = cfg.color_buy,
			})
		}
		if sell_h > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x, bar_bot - total_h}, size = {bar_w, sell_h}},
				color = cfg.color_sell,
			})
		}
	}
	return true
}
