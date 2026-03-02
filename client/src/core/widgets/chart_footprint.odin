package widgets

// Footprint chart rendering — per-candle volume distribution by price level.
// Two modes: Standard (buy/sell horizontal bars) and Delta (net delta bars).
// POC (Point of Control) marking + imbalance detection.

import "core:fmt"
import "core:math"
import "mr:services"
import "mr:ui"

FOOTPRINT_IMBALANCE_RATIO :: f64(3.0)  // highlight when buy/sell ratio > 3x
FOOTPRINT_LABEL_MIN_H     :: f32(13)   // minimum bin height to render text label
FOOTPRINT_LABEL_MIN_W     :: f32(28)   // minimum slot width to render text label

// Standard footprint: side-by-side buy/sell horizontal bars per price level per candle.
draw_candle_bars_footprint :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context, fp_store: ^services.Footprint_Store) {
	if fp_store == nil do return

	for i in ctx.start_idx ..< ctx.end_idx {
		c := services.get_candle(ctx.store, i)
		slot := (i - ctx.start_idx) + ctx.slot_offset
		cx := ctx.inner.pos.x + f32(slot) * ctx.slot_w + ctx.slot_w * 0.5

		// Draw wick line (high to low) as thin background.
		y_high := ctx.inner.pos.y + f32((ctx.price_hi - c.high) / ctx.price_range) * ctx.price_h
		y_low  := ctx.inner.pos.y + f32((ctx.price_hi - c.low) / ctx.price_range) * ctx.price_h
		ui.push(buf, ui.Cmd_Line{
			from      = {cx, y_high},
			to        = {cx, y_low},
			color     = ui.with_alpha(ui.COL_TEXT_MUTED, 0.5),
			thickness = 1,
		})

		entry, ok := services.footprint_store_get(fp_store, c.window_start_ts)
		if !ok || entry.level_count <= 0 {
			draw_footprint_fallback_body(buf, ctx, c, cx)
			continue
		}

		// Find max volume for normalization within this candle.
		max_vol := f64(0)
		poc_idx := 0
		poc_vol := f64(0)
		for li in 0 ..< entry.level_count {
			lv := entry.levels[li]
			total := lv.buy_vol + lv.sell_vol
			if total > max_vol do max_vol = total
			if total > poc_vol {
				poc_vol = total
				poc_idx = li
			}
		}
		if max_vol <= 0 {
			draw_footprint_fallback_body(buf, ctx, c, cx)
			continue
		}

		half_w := ctx.slot_w * 0.45  // leave a small gap between candles
		bin_h := footprint_bin_height(ctx, entry)

		// Draw buy/sell bars per level.
		for li in 0 ..< entry.level_count {
			lv := entry.levels[li]
			y_center := ctx.inner.pos.y + f32((ctx.price_hi - lv.price) / ctx.price_range) * ctx.price_h
			y_top := y_center - bin_h * 0.5
			if y_top > ctx.inner.pos.y + ctx.price_h || y_center < ctx.inner.pos.y do continue

			buy_frac := f32(lv.buy_vol / max_vol)
			sell_frac := f32(lv.sell_vol / max_vol)

			// Buy bar: extends left from center.
			if buy_frac > 0 {
				bw := half_w * buy_frac
				alpha := f32(0.25) + buy_frac * 0.55
				is_imbalance := li > 0 && lv.buy_vol > entry.levels[li - 1].sell_vol * FOOTPRINT_IMBALANCE_RATIO
				col := is_imbalance ? ui.Color{0.2, 1.0, 0.4, alpha} : ui.with_alpha(ui.COL_GREEN, alpha)
				ui.push(buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {cx - bw, y_top}, size = {bw, bin_h}},
					color = col,
				})
			}

			// Sell bar: extends right from center.
			if sell_frac > 0 {
				sw := half_w * sell_frac
				alpha := f32(0.25) + sell_frac * 0.55
				is_imbalance := li + 1 < entry.level_count && lv.sell_vol > entry.levels[li + 1].buy_vol * FOOTPRINT_IMBALANCE_RATIO
				col := is_imbalance ? ui.Color{1.0, 0.3, 0.2, alpha} : ui.with_alpha(ui.COL_RED, alpha)
				ui.push(buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {cx, y_top}, size = {sw, bin_h}},
					color = col,
				})
			}

			// POC marker: outline rect at highest total volume level.
			if li == poc_idx {
				poc_rect := ui.Rect{pos = {cx - half_w, y_top}, size = {half_w * 2, bin_h}}
				ui.draw_rect_stroke(buf, poc_rect, ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.7))
			}

			// Text labels when there's enough room.
			if bin_h >= FOOTPRINT_LABEL_MIN_H && ctx.slot_w >= FOOTPRINT_LABEL_MIN_W {
				footprint_draw_vol_label(buf, lv.buy_vol, cx - half_w + 1, y_top, bin_h, true)
				footprint_draw_vol_label(buf, lv.sell_vol, cx + 1, y_top, bin_h, false)
			}
		}
	}
}

// Delta footprint: single bar per level, magnitude = |buy-sell|, color by sign.
draw_candle_bars_footprint_delta :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context, fp_store: ^services.Footprint_Store) {
	if fp_store == nil do return

	for i in ctx.start_idx ..< ctx.end_idx {
		c := services.get_candle(ctx.store, i)
		slot := (i - ctx.start_idx) + ctx.slot_offset
		cx := ctx.inner.pos.x + f32(slot) * ctx.slot_w + ctx.slot_w * 0.5

		// Wick line.
		y_high := ctx.inner.pos.y + f32((ctx.price_hi - c.high) / ctx.price_range) * ctx.price_h
		y_low  := ctx.inner.pos.y + f32((ctx.price_hi - c.low) / ctx.price_range) * ctx.price_h
		ui.push(buf, ui.Cmd_Line{
			from      = {cx, y_high},
			to        = {cx, y_low},
			color     = ui.with_alpha(ui.COL_TEXT_MUTED, 0.5),
			thickness = 1,
		})

		entry, ok := services.footprint_store_get(fp_store, c.window_start_ts)
		if !ok || entry.level_count <= 0 {
			draw_footprint_fallback_body(buf, ctx, c, cx)
			continue
		}

		// Find max |delta| for normalization.
		max_delta := f64(0)
		poc_idx := 0
		poc_vol := f64(0)
		for li in 0 ..< entry.level_count {
			lv := entry.levels[li]
			delta := math.abs(lv.buy_vol - lv.sell_vol)
			if delta > max_delta do max_delta = delta
			total := lv.buy_vol + lv.sell_vol
			if total > poc_vol {
				poc_vol = total
				poc_idx = li
			}
		}
		if max_delta <= 0 {
			draw_footprint_fallback_body(buf, ctx, c, cx)
			continue
		}

		bar_max_w := ctx.slot_w * 0.8
		bin_h := footprint_bin_height(ctx, entry)

		for li in 0 ..< entry.level_count {
			lv := entry.levels[li]
			y_center := ctx.inner.pos.y + f32((ctx.price_hi - lv.price) / ctx.price_range) * ctx.price_h
			y_top := y_center - bin_h * 0.5
			if y_top > ctx.inner.pos.y + ctx.price_h || y_center < ctx.inner.pos.y do continue

			delta := lv.buy_vol - lv.sell_vol
			frac := f32(math.abs(delta) / max_delta)
			bw := bar_max_w * frac
			alpha := f32(0.30) + frac * 0.50
			col := delta >= 0 ? ui.with_alpha(ui.COL_GREEN, alpha) : ui.with_alpha(ui.COL_RED, alpha)

			bar_x := cx - bw * 0.5
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {bar_x, y_top}, size = {bw, bin_h}},
				color = col,
			})

			// POC marker.
			if li == poc_idx {
				poc_rect := ui.Rect{pos = {cx - bar_max_w * 0.5, y_top}, size = {bar_max_w, bin_h}}
				ui.draw_rect_stroke(buf, poc_rect, ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.6))
			}

			// Delta text label.
			if bin_h >= FOOTPRINT_LABEL_MIN_H && ctx.slot_w >= FOOTPRINT_LABEL_MIN_W {
				footprint_draw_vol_label(buf, math.abs(delta), bar_x + 1, y_top, bin_h, delta >= 0)
			}
		}
	}
}

// Thin fallback body when no footprint data available for a candle.
@(private = "file")
draw_footprint_fallback_body :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context, c: services.Candle_Entry, cx: f32) {
	bullish := c.close >= c.open
	y_open  := ctx.inner.pos.y + f32((ctx.price_hi - c.open) / ctx.price_range) * ctx.price_h
	y_close := ctx.inner.pos.y + f32((ctx.price_hi - c.close) / ctx.price_range) * ctx.price_h
	body_top := min(y_open, y_close)
	body_bot := max(y_open, y_close)
	body_h := max(body_bot - body_top, 1)
	col := bullish ? ui.COL_GREEN : ui.COL_RED
	ui.push(buf, ui.Cmd_Line{
		from      = {cx, body_top},
		to        = {cx, body_top + body_h},
		color     = ui.with_alpha(col, 0.5),
		thickness = 2,
	})
}

// Calculate bin height in pixels from footprint entry price group.
@(private = "file")
footprint_bin_height :: proc(ctx: ^Candle_Render_Context, entry: ^services.Footprint_Entry) -> f32 {
	if entry.price_group <= 0 || ctx.price_range <= 0 do return f32(2)
	h := f32(entry.price_group / ctx.price_range) * ctx.price_h
	return clamp(h, 1, ctx.price_h * 0.15)
}

// Compact volume label rendered at given position.
@(private = "file")
footprint_draw_vol_label :: proc(buf: ^ui.Command_Buffer, vol: f64, x, y, h: f32, is_green: bool) {
	if vol <= 0 do return
	vl_buf: [12]u8
	label: string
	if vol >= 1_000_000 {
		label = fmt.bprintf(vl_buf[:], "%.1fM", vol / 1_000_000)
	} else if vol >= 1_000 {
		label = fmt.bprintf(vl_buf[:], "%.1fK", vol / 1_000)
	} else if vol >= 1 {
		label = fmt.bprintf(vl_buf[:], "%.0f", vol)
	} else {
		return
	}
	col := is_green ? ui.with_alpha(ui.COL_GREEN, 0.9) : ui.with_alpha(ui.COL_RED, 0.9)
	ty := y + (h - ui.FONT_SIZE_XS) * 0.5
	ui.push_text(buf, {x, ty}, label, col, ui.FONT_SIZE_XS, .Mono)
}
