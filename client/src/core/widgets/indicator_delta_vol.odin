package widgets

// S81: Delta Volume sub-plot for candle chart.
// Renders buy/sell delta bars from analytics store aligned to candle timestamps.

import "core:math"
import "mr:services"
import "mr:ui"

Delta_Vol_Config :: struct {
	visible:       bool,
	color_buy:     ui.Color,
	color_sell:    ui.Color,
}

DELTA_VOL_DEFAULT :: Delta_Vol_Config{
	visible    = false,
	color_buy  = ui.Color{0.18, 0.74, 0.52, 0.65},
	color_sell = ui.Color{0.96, 0.28, 0.37, 0.65},
}

DELTA_VOL_MAX_POINTS :: 64

// Render Delta Volume sub-plot in a dedicated rect below the main chart.
render_delta_vol :: proc(
	buf: ^ui.Command_Buffer,
	rect: ui.Rect,
	ctx: ^Chart_Layer_Context,
	cfg: Delta_Vol_Config,
	measure_proc: proc(size: f32, text: string) -> ui.Vec2,
) -> bool {
	if !cfg.visible do return false
	if ctx.analytics_store == nil do return false
	if rect.size.x <= 0 || rect.size.y <= 0 do return false

	store := ctx.analytics_store

	// Collect Delta Volume entries.
	xs: [DELTA_VOL_MAX_POINTS]f32
	deltas: [DELTA_VOL_MAX_POINTS]f64
	count := 0
	for i := 0; i < store.count; i += 1 {
		e := services.get_analytics(store, i)
		if e.kind != .Delta_Volume do continue
		delta_val := e.values[2]  // slot 2 = delta_vol (buy - sell)
		ts := e.ts_ms
		if ts <= 0 do continue
		x := stats_ts_to_x(ctx, ts)
		if x < rect.pos.x || x > rect.pos.x + rect.size.x do continue
		if count >= DELTA_VOL_MAX_POINTS do break
		xs[count] = x
		deltas[count] = delta_val
		count += 1
	}

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
	ui.push_text(buf, {rect.pos.x + 4, rect.pos.y + 10}, "DV",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	if count < 1 {
		ui.push_text(buf, {rect.pos.x + 24, rect.pos.y + 10}, "waiting...",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return false
	}

	// Find max absolute value for scaling.
	max_abs := f64(0)
	for i in 0 ..< count {
		a := math.abs(deltas[i])
		if a > max_abs do max_abs = a
	}
	if max_abs <= 0 do max_abs = 1

	pad_rect := ui.Rect{pos = {rect.pos.x, rect.pos.y + 4}, size = {rect.size.x, rect.size.y - 8}}
	mid_y := pad_rect.pos.y + pad_rect.size.y * 0.5

	// Zero line.
	ui.push(buf, ui.Cmd_Line{
		from      = {pad_rect.pos.x, mid_y},
		to        = {pad_rect.pos.x + pad_rect.size.x, mid_y},
		color     = ui.with_alpha(ui.COL_WHITE, 0.10),
		thickness = 1,
	})

	// Delta bars (entries are newest-first, draw reversed for left-to-right).
	bar_w := max(ctx.slot_width * 0.6, 2)
	for i in 0 ..< count {
		ri := count - 1 - i  // reverse for oldest-left
		v := deltas[ri]
		frac := f32(v / max_abs) * 0.45
		bar_h := math.abs(frac) * pad_rect.size.y
		bx := xs[ri] - bar_w * 0.5
		col := v >= 0 ? cfg.color_buy : cfg.color_sell
		if v >= 0 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = ui.rect_xywh(bx, mid_y - bar_h, bar_w, bar_h),
				color = col,
			})
		} else {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = ui.rect_xywh(bx, mid_y, bar_w, bar_h),
				color = col,
			})
		}
	}

	return true
}
