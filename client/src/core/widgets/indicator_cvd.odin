package widgets

// S81: CVD (Cumulative Volume Delta) sub-plot for candle chart.
// Renders CVD line from analytics store entries aligned to candle timestamps.

import "mr:services"
import "mr:ui"

CVD_Config :: struct {
	visible:   bool,
	color_cvd: ui.Color,
}

CVD_DEFAULT :: CVD_Config{
	visible   = false,
	color_cvd = ui.Color{0.2, 0.75, 0.95, 0.85},
}

CVD_MAX_POINTS :: 64

// Render CVD sub-plot in a dedicated rect below the main chart.
render_cvd :: proc(
	buf: ^ui.Command_Buffer,
	rect: ui.Rect,
	ctx: ^Chart_Layer_Context,
	cfg: CVD_Config,
	measure_proc: proc(size: f32, text: string) -> ui.Vec2,
) -> bool {
	if !cfg.visible do return false
	if ctx.analytics_store == nil do return false
	if rect.size.x <= 0 || rect.size.y <= 0 do return false

	store := ctx.analytics_store

	// Collect CVD entries with timestamps.
	xs: [CVD_MAX_POINTS]f32
	ys: [CVD_MAX_POINTS]f64
	count := 0
	for i := 0; i < store.count; i += 1 {
		e := services.get_analytics(store, i)
		if e.kind != .CVD do continue
		cvd_val := e.values[1]  // slot 1 = cumulative CVD
		ts := e.ts_ms
		if ts <= 0 do continue
		// Map timestamp to X via candle binary search.
		x := stats_ts_to_x(ctx, ts)
		if x < rect.pos.x || x > rect.pos.x + rect.size.x do continue
		if count >= CVD_MAX_POINTS do break
		xs[count] = x
		ys[count] = cvd_val
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
	ui.push_text(buf, {rect.pos.x + 4, rect.pos.y + 10}, "CVD",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	if count < 2 {
		ui.push_text(buf, {rect.pos.x + 30, rect.pos.y + 10}, "waiting...",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return false
	}

	// Find value range.
	min_v := ys[0]
	max_v := ys[0]
	for i in 1 ..< count {
		if ys[i] < min_v do min_v = ys[i]
		if ys[i] > max_v do max_v = ys[i]
	}
	val_range := max_v - min_v
	if val_range <= 0 do val_range = 1

	pad_rect := ui.Rect{pos = {rect.pos.x, rect.pos.y + 4}, size = {rect.size.x, rect.size.y - 8}}

	// Zero line (if range straddles zero).
	if min_v < 0 && max_v > 0 {
		zero_t := f32((0 - min_v) / val_range)
		zero_y := pad_rect.pos.y + pad_rect.size.y * (1.0 - zero_t)
		ui.push(buf, ui.Cmd_Line{
			from      = {pad_rect.pos.x, zero_y},
			to        = {pad_rect.pos.x + pad_rect.size.x, zero_y},
			color     = ui.with_alpha(ui.COL_WHITE, 0.10),
			thickness = 1,
		})
	}

	// CVD line (entries are newest-first from get_analytics, draw reversed).
	for i in 0 ..< count - 1 {
		i0 := count - 1 - i
		i1 := count - 2 - i
		t0 := f32((ys[i0] - min_v) / val_range)
		t1 := f32((ys[i1] - min_v) / val_range)
		y0 := pad_rect.pos.y + pad_rect.size.y * (1.0 - t0)
		y1 := pad_rect.pos.y + pad_rect.size.y * (1.0 - t1)

		// Color: green above zero, red below.
		mid_val := (ys[i0] + ys[i1]) * 0.5
		line_color := mid_val >= 0 ? cfg.color_cvd : ui.Color{0.95, 0.35, 0.35, 0.85}

		ui.push(buf, ui.Cmd_Line{
			from      = {xs[i0], y0},
			to        = {xs[i1], y1},
			color     = line_color,
			thickness = 1,
		})
	}

	return true
}
