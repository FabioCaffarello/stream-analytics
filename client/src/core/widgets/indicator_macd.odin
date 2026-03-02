package widgets

// MACD (Moving Average Convergence Divergence) sub-plot for candle chart.
// MACD line, signal line, and histogram bars in a dedicated sub-area.

import "core:fmt"
import "core:math"
import "mr:services"
import "mr:ui"

MACD_Config :: struct {
	fast_period:   int,    // default 12
	slow_period:   int,    // default 26
	signal_period: int,    // default 9
	visible:       bool,
	color_macd:    ui.Color,
	color_signal:  ui.Color,
	color_hist_pos: ui.Color,
	color_hist_neg: ui.Color,
}

MACD_DEFAULT :: MACD_Config{
	fast_period   = 12,
	slow_period   = 26,
	signal_period = 9,
	visible       = false, // off by default
	color_macd    = ui.Color{0.2, 0.6, 1.0, 0.85},
	color_signal  = ui.Color{1.0, 0.6, 0.2, 0.85},
	color_hist_pos = ui.Color{0.18, 0.74, 0.52, 0.6},
	color_hist_neg = ui.Color{0.96, 0.28, 0.37, 0.6},
}

// Pre-computed MACD data for a single visible point.
@(private = "file")
MACD_Point :: struct {
	macd:      f64,
	signal:    f64,
	histogram: f64,
	slot:      int,
}

MACD_MAX_POINTS :: 500

// Render MACD sub-plot in a dedicated rect below the main chart.
render_macd :: proc(
	buf: ^ui.Command_Buffer,
	rect: ui.Rect,
	ctx: ^Chart_Layer_Context,
	cfg: MACD_Config,
	measure_proc: proc(size: f32, text: string) -> ui.Vec2,
) -> bool {
	if !cfg.visible do return false
	if ctx.candles == nil || ctx.candle_count <= 1 do return false
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
	label_buf: [32]u8
	label := fmt.bprintf(label_buf[:], "MACD(%d,%d,%d)", cfg.fast_period, cfg.slow_period, cfg.signal_period)
	ui.push_text(buf, {rect.pos.x + 4, rect.pos.y + 10}, label,
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	y_axis_w := f32(30)
	chart_w := rect.size.x - y_axis_w
	if chart_w <= 0 do return false

	// Defensive: ensure fast < slow to prevent degenerate MACD.
	eff_fast := cfg.fast_period
	eff_slow := cfg.slow_period
	if eff_fast >= eff_slow {
		eff_fast, eff_slow = eff_slow, eff_fast
		if eff_fast >= eff_slow do eff_slow = eff_fast + 1
	}

	// Compute MACD from lookback.
	max_lookback := eff_slow + cfg.signal_period + 10
	lookback_start := max(ctx.candle_start - max_lookback, 0)
	data_end := min(ctx.candle_start + ctx.candle_count, ctx.candles.count)
	if data_end-lookback_start <= 1 do return false

	// Compute fast and slow EMA, then MACD line, then signal EMA.
	fast_mult := f64(2) / f64(eff_fast + 1)
	slow_mult := f64(2) / f64(eff_slow + 1)
	sig_mult := f64(2) / f64(cfg.signal_period + 1)

	fast_ema := f64(0)
	slow_ema := f64(0)
	sig_ema := f64(0)
	fast_init := false
	slow_init := false
	sig_init := false

	points: [MACD_MAX_POINTS]MACD_Point
	point_count := 0

	for i in lookback_start ..< data_end {
		if i >= ctx.candles.count do break
		c := services.get_candle(ctx.candles, i)
		close := c.close

		if !fast_init {
			fast_ema = close
			fast_init = true
		} else {
			fast_ema = close * fast_mult + fast_ema * (1.0 - fast_mult)
		}

		if !slow_init {
			slow_ema = close
			slow_init = true
		} else {
			slow_ema = close * slow_mult + slow_ema * (1.0 - slow_mult)
		}

		macd_val := fast_ema - slow_ema

		if !sig_init {
			sig_ema = macd_val
			sig_init = true
		} else {
			sig_ema = macd_val * sig_mult + sig_ema * (1.0 - sig_mult)
		}

		if i >= ctx.candle_start && i < ctx.candle_start + ctx.candle_count {
			if point_count < MACD_MAX_POINTS {
				slot := (i - ctx.candle_start) + ctx.slot_offset
				points[point_count] = MACD_Point{
					macd      = macd_val,
					signal    = sig_ema,
					histogram = macd_val - sig_ema,
					slot      = slot,
				}
				point_count += 1
			}
		}
	}

	if point_count <= 0 do return false

	// Find value range for scaling.
	val_max := f64(0)
	for pi in 0 ..< point_count {
		p := points[pi]
		val_max = math.max(val_max, math.abs(p.macd))
		val_max = math.max(val_max, math.abs(p.signal))
		val_max = math.max(val_max, math.abs(p.histogram))
	}
	if val_max <= 0 do val_max = 1

	pad_rect := ui.Rect{pos = {rect.pos.x, rect.pos.y + 4}, size = {chart_w, rect.size.y - 8}}
	zero_y := pad_rect.pos.y + pad_rect.size.y * 0.5

	// Zero line.
	ui.push(buf, ui.Cmd_Line{
		from      = {pad_rect.pos.x, zero_y},
		to        = {pad_rect.pos.x + chart_w, zero_y},
		color     = ui.with_alpha(ui.COL_WHITE, 0.10),
		thickness = 1,
	})

	// Histogram bars.
	bar_w := max(ctx.slot_width * 0.6, 1)
	for pi in 0 ..< point_count {
		p := points[pi]
		x := pad_rect.pos.x + f32(p.slot) * ctx.slot_width + ctx.slot_width * 0.5 - bar_w * 0.5
		h_val := f32(p.histogram / val_max) * pad_rect.size.y * 0.45
		bar_color := p.histogram >= 0 ? cfg.color_hist_pos : cfg.color_hist_neg
		if h_val >= 0 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x, zero_y - h_val}, size = {bar_w, h_val}},
				color = bar_color,
			})
		} else {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x, zero_y}, size = {bar_w, -h_val}},
				color = bar_color,
			})
		}
	}

	// MACD line.
	for pi in 1 ..< point_count {
		p0 := points[pi - 1]
		p1 := points[pi]
		x0 := pad_rect.pos.x + f32(p0.slot) * ctx.slot_width + ctx.slot_width * 0.5
		x1 := pad_rect.pos.x + f32(p1.slot) * ctx.slot_width + ctx.slot_width * 0.5
		y0 := zero_y - f32(p0.macd / val_max) * pad_rect.size.y * 0.45
		y1 := zero_y - f32(p1.macd / val_max) * pad_rect.size.y * 0.45
		ui.push(buf, ui.Cmd_Line{
			from      = {x0, y0},
			to        = {x1, y1},
			color     = cfg.color_macd,
			thickness = 1,
		})
	}

	// Signal line.
	for pi in 1 ..< point_count {
		p0 := points[pi - 1]
		p1 := points[pi]
		x0 := pad_rect.pos.x + f32(p0.slot) * ctx.slot_width + ctx.slot_width * 0.5
		x1 := pad_rect.pos.x + f32(p1.slot) * ctx.slot_width + ctx.slot_width * 0.5
		y0 := zero_y - f32(p0.signal / val_max) * pad_rect.size.y * 0.45
		y1 := zero_y - f32(p1.signal / val_max) * pad_rect.size.y * 0.45
		ui.push(buf, ui.Cmd_Line{
			from      = {x0, y0},
			to        = {x1, y1},
			color     = cfg.color_signal,
			thickness = 1,
		})
	}
	return true
}
