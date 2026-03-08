package widgets

// S82: Open Interest (OI) sub-plot for candle chart.
// Top portion: OI absolute line. Bottom portion: OI delta bars.
// Confidence shown as a thin colored bar at the top edge (2px).

import "core:math"
import "mr:services"
import "mr:ui"

OI_Config :: struct {
	visible:          bool,
	color_line:       ui.Color,
	color_delta_buy:  ui.Color,
	color_delta_sell: ui.Color,
}

OI_DEFAULT :: OI_Config{
	visible          = false,
	color_line       = ui.Color{0.0, 0.8, 0.8, 0.85},    // cyan
	color_delta_buy  = ui.Color{0.18, 0.74, 0.52, 0.65},  // green
	color_delta_sell = ui.Color{0.96, 0.28, 0.37, 0.65},  // red
}

OI_MAX_POINTS :: 64

// Render OI sub-plot in a dedicated rect below the main chart.
// Top 60%: OI absolute line. Bottom 40%: OI delta bars.
// Confidence bar (2px) at the very top edge.
render_oi :: proc(
	buf: ^ui.Command_Buffer,
	rect: ui.Rect,
	ctx: ^Chart_Layer_Context,
	cfg: OI_Config,
	measure_proc: proc(size: f32, text: string) -> ui.Vec2,
) -> bool {
	if !cfg.visible do return false
	if ctx.analytics_store == nil do return false
	if rect.size.x <= 0 || rect.size.y <= 0 do return false

	store := ctx.analytics_store

	// Collect OI entries with timestamps.
	xs: [OI_MAX_POINTS]f32
	oi_vals: [OI_MAX_POINTS]f64
	deltas: [OI_MAX_POINTS]f64
	confs: [OI_MAX_POINTS]u8
	count := 0
	for i := 0; i < store.count; i += 1 {
		e := services.get_analytics(store, i)
		if e.kind != .Open_Interest do continue
		ts := e.ts_ms
		if ts <= 0 do continue
		x := stats_ts_to_x(ctx, ts)
		if x < rect.pos.x || x > rect.pos.x + rect.size.x do continue
		if count >= OI_MAX_POINTS do break
		xs[count] = x
		oi_vals[count] = e.values[0]   // slot 0 = open_interest
		deltas[count] = e.values[1]    // slot 1 = delta
		confs[count] = e.confidence
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
	ui.push_text(buf, {rect.pos.x + 4, rect.pos.y + 10}, "OI",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	if count < 2 {
		ui.push_text(buf, {rect.pos.x + 24, rect.pos.y + 10}, "waiting...",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return false
	}

	// Confidence bar at top edge (2px height). Use most recent entry's confidence.
	conf_color: ui.Color
	switch confs[0] {
	case 1:  conf_color = ui.Color{0.0, 0.8, 0.0, 0.7}   // high = green
	case 2:  conf_color = ui.Color{0.8, 0.8, 0.0, 0.7}   // medium = yellow
	case 3:  conf_color = ui.Color{0.4, 0.4, 0.4, 0.5}   // low = dim gray
	case:    conf_color = ui.Color{0.25, 0.25, 0.25, 0.4} // unknown
	}
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = ui.rect_xywh(rect.pos.x, rect.pos.y, rect.size.x, 2),
		color = conf_color,
	})

	// Split rect: top 60% for OI line, bottom 40% for delta bars.
	oi_h := rect.size.y * 0.6
	delta_h := rect.size.y - oi_h
	oi_rect := ui.Rect{pos = {rect.pos.x, rect.pos.y + 4}, size = {rect.size.x, oi_h - 8}}
	delta_rect := ui.Rect{pos = {rect.pos.x, rect.pos.y + oi_h}, size = {rect.size.x, delta_h - 4}}

	// --- OI LINE (top portion) ---
	oi_min := oi_vals[0]
	oi_max := oi_vals[0]
	for i in 1 ..< count {
		if oi_vals[i] < oi_min do oi_min = oi_vals[i]
		if oi_vals[i] > oi_max do oi_max = oi_vals[i]
	}
	oi_range := oi_max - oi_min
	if oi_range < 1.0 do oi_range = 1.0

	// Draw OI line segments (entries are newest-first, draw reversed for left-to-right).
	for i in 0 ..< count - 1 {
		i0 := count - 1 - i
		i1 := count - 2 - i
		t0 := f32((oi_vals[i0] - oi_min) / oi_range)
		t1 := f32((oi_vals[i1] - oi_min) / oi_range)
		y0 := oi_rect.pos.y + oi_rect.size.y * (1.0 - t0)
		y1 := oi_rect.pos.y + oi_rect.size.y * (1.0 - t1)

		ui.push(buf, ui.Cmd_Line{
			from      = {xs[i0], y0},
			to        = {xs[i1], y1},
			color     = cfg.color_line,
			thickness = 1,
		})
	}

	// --- DELTA BARS (bottom portion) ---
	max_abs_delta := f64(0)
	for i in 0 ..< count {
		a := math.abs(deltas[i])
		if a > max_abs_delta do max_abs_delta = a
	}
	if max_abs_delta < 0.001 do max_abs_delta = 0.001

	mid_y := delta_rect.pos.y + delta_rect.size.y * 0.5

	// Zero line.
	ui.push(buf, ui.Cmd_Line{
		from      = {delta_rect.pos.x, mid_y},
		to        = {delta_rect.pos.x + delta_rect.size.x, mid_y},
		color     = ui.with_alpha(ui.COL_WHITE, 0.10),
		thickness = 1,
	})

	// Delta bars.
	bar_w := max(ctx.slot_width * 0.6, 2)
	for i in 0 ..< count {
		ri := count - 1 - i  // reverse for oldest-left
		v := deltas[ri]
		frac := f32(v / max_abs_delta) * 0.45
		bar_h := math.abs(frac) * delta_rect.size.y
		bx := xs[ri] - bar_w * 0.5
		col := v >= 0 ? cfg.color_delta_buy : cfg.color_delta_sell
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
