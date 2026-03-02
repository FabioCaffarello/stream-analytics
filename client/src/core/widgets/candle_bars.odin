package widgets

// Candlestick chart bar rendering: OHLC, line, Heiken Ashi, volume bars.

import "mr:services"
import "mr:ui"

draw_candle_bars :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	switch ctx.chart_type {
	case .Line:
		draw_candle_bars_line(buf, ctx)
	case .Heiken_Ashi:
		draw_candle_bars_ha(buf, ctx)
	case .Candlesticks:
		draw_candle_bars_ohlc(buf, ctx)
	case .Footprint:
		draw_candle_bars_footprint(buf, ctx, ctx.footprint_store)
	case .Footprint_Delta:
		draw_candle_bars_footprint_delta(buf, ctx, ctx.footprint_store)
	}
}

@(private = "file")
draw_candle_bars_ohlc :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	line_only_mode := ctx.body_w < CANDLE_BODY_LINE_ONLY_MIN_W
	for i in ctx.start_idx ..< ctx.end_idx {
		c := services.get_candle(ctx.store, i)
		slot := (i - ctx.start_idx) + ctx.slot_offset
		cx := ctx.inner.pos.x + f32(slot) * ctx.slot_w + ctx.slot_w * 0.5

		bullish := c.close >= c.open
		color := bullish ? ui.COL_GREEN : ui.COL_RED

		y_high  := ctx.inner.pos.y + f32((ctx.price_hi - c.high) / ctx.price_range) * ctx.price_h
		y_low   := ctx.inner.pos.y + f32((ctx.price_hi - c.low) / ctx.price_range) * ctx.price_h
		y_open  := ctx.inner.pos.y + f32((ctx.price_hi - c.open) / ctx.price_range) * ctx.price_h
		y_close := ctx.inner.pos.y + f32((ctx.price_hi - c.close) / ctx.price_range) * ctx.price_h

		body_top := min(y_open, y_close)
		body_bot := max(y_open, y_close)
		body_height := max(body_bot - body_top, 1)

		ui.push(buf, ui.Cmd_Line{
			from      = {cx, y_high},
			to        = {cx, y_low},
			color     = color,
			thickness = 1,
		})

		if line_only_mode {
			ui.push(buf, ui.Cmd_Line{
				from      = {cx, body_top},
				to        = {cx, body_top + body_height},
				color     = color,
				thickness = 2,
			})
		} else {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {cx - ctx.body_w * 0.5, body_top}, size = {ctx.body_w, body_height}},
				color = color,
			})
		}

		draw_candle_volume_bar(buf, ctx, i, cx)
	}
}

@(private = "file")
draw_candle_bars_line :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	prev_x := f32(0)
	prev_y := f32(0)
	has_prev := false

	for i in ctx.start_idx ..< ctx.end_idx {
		c := services.get_candle(ctx.store, i)
		slot := (i - ctx.start_idx) + ctx.slot_offset
		cx := ctx.inner.pos.x + f32(slot) * ctx.slot_w + ctx.slot_w * 0.5
		cy := ctx.inner.pos.y + f32((ctx.price_hi - c.close) / ctx.price_range) * ctx.price_h

		if has_prev {
			ui.push(buf, ui.Cmd_Line{
				from      = {prev_x, prev_y},
				to        = {cx, cy},
				color     = ui.COL_ACCENT_CYAN,
				thickness = 1,
			})
		}

		// Filled gradient area below line.
		if has_prev {
			fill_top := min(prev_y, cy)
			fill_bot := ctx.inner.pos.y + ctx.price_h
			if fill_bot > fill_top {
				ui.push(buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {prev_x, fill_top}, size = {cx - prev_x, fill_bot - fill_top}},
					color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.05),
				})
			}
		}

		prev_x = cx
		prev_y = cy
		has_prev = true

		draw_candle_volume_bar(buf, ctx, i, cx)
	}
}

@(private = "file")
draw_candle_bars_ha :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	// Heiken Ashi formula:
	// HA_Close = (O + H + L + C) / 4
	// HA_Open  = (prev_HA_Open + prev_HA_Close) / 2
	// HA_High  = max(H, HA_Open, HA_Close)
	// HA_Low   = min(L, HA_Open, HA_Close)
	line_only_mode := ctx.body_w < CANDLE_BODY_LINE_ONLY_MIN_W

	ha_open := f64(0)
	ha_close := f64(0)
	ha_initialized := false

	// Start one candle early if possible to initialize HA.
	render_start := max(ctx.start_idx - 1, 0)

	for i in render_start ..< ctx.end_idx {
		if i >= ctx.store.count do break
		c := services.get_candle(ctx.store, i)

		new_ha_close := (c.open + c.high + c.low + c.close) / 4.0
		new_ha_open: f64
		if !ha_initialized {
			new_ha_open = (c.open + c.close) / 2.0
			ha_initialized = true
		} else {
			new_ha_open = (ha_open + ha_close) / 2.0
		}
		ha_high := max(c.high, max(new_ha_open, new_ha_close))
		ha_low := min(c.low, min(new_ha_open, new_ha_close))

		ha_open = new_ha_open
		ha_close = new_ha_close

		if i < ctx.start_idx do continue // skip pre-render candle

		slot := (i - ctx.start_idx) + ctx.slot_offset
		cx := ctx.inner.pos.x + f32(slot) * ctx.slot_w + ctx.slot_w * 0.5

		bullish := ha_close >= ha_open
		color := bullish ? ui.COL_GREEN : ui.COL_RED

		y_high  := ctx.inner.pos.y + f32((ctx.price_hi - ha_high) / ctx.price_range) * ctx.price_h
		y_low   := ctx.inner.pos.y + f32((ctx.price_hi - ha_low) / ctx.price_range) * ctx.price_h
		y_open  := ctx.inner.pos.y + f32((ctx.price_hi - ha_open) / ctx.price_range) * ctx.price_h
		y_close := ctx.inner.pos.y + f32((ctx.price_hi - ha_close) / ctx.price_range) * ctx.price_h

		body_top := min(y_open, y_close)
		body_bot := max(y_open, y_close)
		body_height := max(body_bot - body_top, 1)

		ui.push(buf, ui.Cmd_Line{
			from      = {cx, y_high},
			to        = {cx, y_low},
			color     = color,
			thickness = 1,
		})

		if line_only_mode {
			ui.push(buf, ui.Cmd_Line{
				from      = {cx, body_top},
				to        = {cx, body_top + body_height},
				color     = color,
				thickness = 2,
			})
		} else {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {cx - ctx.body_w * 0.5, body_top}, size = {ctx.body_w, body_height}},
				color = color,
			})
		}

		draw_candle_volume_bar(buf, ctx, i, cx)
	}
}

// Shared volume bar rendering for all chart types.
// Stacked buy/sell split when buy_vol/sell_vol are available.
draw_candle_volume_bar :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context, i: int, cx: f32) {
	if !ctx.show_vol || ctx.vol_max <= 0 do return
	c := services.get_candle(ctx.store, i)
	if c.volume <= 0 do return
	vol_pct := f32(c.volume / ctx.vol_max)
	bar_h := vol_pct * ctx.vol_h
	vol_y := ctx.inner.pos.y + ctx.price_h + ctx.vol_h - bar_h
	// Gradient alpha: scale from 0.15 (below avg) to 0.55 (3x avg or more).
	vol_ratio := clamp(f32(c.volume / max(ctx.vol_avg, 1)), 0, 3)
	vol_alpha := f32(0.15) + vol_ratio * (f32(0.55) - f32(0.15)) / f32(3)
	bar_x := cx - ctx.body_w * 0.5

	// Stacked buy/sell when data is available.
	total_bs := c.buy_vol + c.sell_vol
	if total_bs > 0 {
		buy_frac := f32(c.buy_vol / total_bs)
		buy_h := bar_h * buy_frac
		sell_h := bar_h - buy_h
		// Sell (red) on top, buy (green) on bottom.
		if sell_h > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {bar_x, vol_y}, size = {ctx.body_w, sell_h}},
				color = ui.with_alpha(ui.COL_RED, vol_alpha),
			})
		}
		if buy_h > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {bar_x, vol_y + sell_h}, size = {ctx.body_w, buy_h}},
				color = ui.with_alpha(ui.COL_GREEN, vol_alpha),
			})
		}
		return
	}

	// Fallback: single color based on candle direction.
	bullish := c.close >= c.open
	vol_color := bullish ? ui.with_alpha(ui.COL_GREEN, vol_alpha) : ui.with_alpha(ui.COL_RED, vol_alpha)
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {bar_x, vol_y}, size = {ctx.body_w, bar_h}},
		color = vol_color,
	})
}

draw_candle_volume_separator :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	if !ctx.show_vol do return
	sep_y := ctx.inner.pos.y + ctx.price_h
	ui.push(buf, ui.Cmd_Line{
		from      = {ctx.inner.pos.x, sep_y},
		to        = {ctx.inner.pos.x + ctx.chart_w, sep_y},
		color     = ui.with_alpha(ui.COL_WHITE, 0.1),
		thickness = 1,
	})

	// Volume average line — dashed horizontal at vol_avg level.
	if ctx.vol_avg > 0 && ctx.vol_max > 0 && ctx.vol_h > 4 {
		avg_ratio := f32(ctx.vol_avg / ctx.vol_max)
		if avg_ratio > 0.01 && avg_ratio < 0.99 {
			avg_y := sep_y + ctx.vol_h * (1.0 - avg_ratio)
			dash := f32(4)
			gap := f32(4)
			x := ctx.inner.pos.x
			for x < ctx.inner.pos.x + ctx.chart_w {
				x_end := min(x + dash, ctx.inner.pos.x + ctx.chart_w)
				ui.push(buf, ui.Cmd_Line{
					from      = {x, avg_y},
					to        = {x_end, avg_y},
					color     = ui.with_alpha(ui.COL_WHITE, 0.18),
					thickness = 1,
				})
				x += dash + gap
			}
		}
	}
}
