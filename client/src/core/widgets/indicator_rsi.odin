package widgets

// RSI (Relative Strength Index) sub-plot for candle chart.
// Rendered in a dedicated sub-area below the main chart.

import "core:fmt"
import "mr:services"
import "mr:ui"

RSI_Config :: struct {
	period:  int,     // default 14
	visible: bool,
	color:   ui.Color,
}

RSI_DEFAULT :: RSI_Config{
	period  = 14,
	visible = false, // off by default, toggled from sidebar
	color   = ui.Color{0.65, 0.45, 0.95, 0.9},
}

RSI_OVERSOLD  :: f64(30)
RSI_OVERBOUGHT :: f64(70)

// Render RSI sub-plot in a dedicated rect below the main chart area.
render_rsi :: proc(
	buf: ^ui.Command_Buffer,
	rect: ui.Rect,
	ctx: ^Chart_Layer_Context,
	cfg: RSI_Config,
	measure_proc: proc(size: f32, text: string) -> ui.Vec2,
) -> bool {
	if !cfg.visible do return false
	if ctx.candles == nil || ctx.candle_count <= 1 do return false
	if cfg.period <= 0 || cfg.period > MA_MAX_PERIOD do return false
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
	label_buf: [16]u8
	label := fmt.bprintf(label_buf[:], "RSI(%d)", cfg.period)
	ui.push_text(buf, {rect.pos.x + 4, rect.pos.y + 10}, label,
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	// Reference lines at 30 and 70.
	pad := ui.Rect{pos = {rect.pos.x, rect.pos.y + 2}, size = {rect.size.x, rect.size.y - 4}}
	y_axis_w := f32(30)
	chart_w := pad.size.x - y_axis_w
	if chart_w <= 0 do return false

	y30 := rsi_value_to_y(pad, 30)
	y70 := rsi_value_to_y(pad, 70)
	y50 := rsi_value_to_y(pad, 50)

	// Oversold/overbought zone fills.
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {pad.pos.x, pad.pos.y}, size = {chart_w, y70 - pad.pos.y}},
		color = ui.with_alpha(ui.COL_RED, 0.04),
	})
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {pad.pos.x, y30}, size = {chart_w, ui.rect_bottom(pad) - y30}},
		color = ui.with_alpha(ui.COL_GREEN, 0.04),
	})

	// Reference lines.
	ref_ys := [3]f32{y30, y50, y70}
	for ri in 0 ..< 3 {
		ui.push(buf, ui.Cmd_Line{
			from      = {pad.pos.x, ref_ys[ri]},
			to        = {pad.pos.x + chart_w, ref_ys[ri]},
			color     = ui.with_alpha(ui.COL_WHITE, 0.08),
			thickness = 1,
		})
	}

	// Y-axis labels.
	ui.push_text(buf, {pad.pos.x + chart_w + 2, y70 + 3}, "70",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	ui.push_text(buf, {pad.pos.x + chart_w + 2, y30 + 3}, "30",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	// Compute and render RSI.
	lookback_start := max(ctx.candle_start - cfg.period - 1, 0)
	data_count := (ctx.candle_start + ctx.candle_count) - lookback_start
	if data_count < cfg.period + 1 do return false

	avg_gain := f64(0)
	avg_loss := f64(0)
	prev_x := f32(0)
	prev_y := f32(0)
	has_prev := false

	for i in lookback_start + 1 ..< lookback_start + data_count {
		if i >= ctx.candles.count do break
		c := services.get_candle(ctx.candles, i)
		prev_c := services.get_candle(ctx.candles, i - 1)
		change := c.close - prev_c.close
		gain := change > 0 ? change : 0
		loss := change < 0 ? -change : 0

		age := i - lookback_start - 1
		if age < cfg.period {
			// Initial SMA period.
			avg_gain += gain
			avg_loss += loss
			if age == cfg.period - 1 {
				avg_gain /= f64(cfg.period)
				avg_loss /= f64(cfg.period)
			}
			continue
		}

		// Smoothed moving average.
		avg_gain = (avg_gain * f64(cfg.period - 1) + gain) / f64(cfg.period)
		avg_loss = (avg_loss * f64(cfg.period - 1) + loss) / f64(cfg.period)

		rsi_val := f64(50)
		if avg_loss > 0 {
			rs := avg_gain / avg_loss
			rsi_val = 100.0 - (100.0 / (1.0 + rs))
		} else if avg_gain > 0 {
			rsi_val = 100.0
		}

		if i >= ctx.candle_start && i < ctx.candle_start + ctx.candle_count {
			slot := (i - ctx.candle_start) + ctx.slot_offset
			x := pad.pos.x + f32(slot) * ctx.slot_width + ctx.slot_width * 0.5
			y := rsi_value_to_y(pad, f32(rsi_val))

			// Color: green <30, red >70, default otherwise.
			line_color := cfg.color
			if rsi_val < RSI_OVERSOLD {
				line_color = ui.COL_GREEN
			} else if rsi_val > RSI_OVERBOUGHT {
				line_color = ui.COL_RED
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
	}
	return has_prev
}

// Map RSI value (0-100) to Y pixel in the sub-plot rect.
@(private = "file")
rsi_value_to_y :: proc(rect: ui.Rect, value: f32) -> f32 {
	t := clamp(value / 100.0, 0, 1)
	return rect.pos.y + (1.0 - t) * rect.size.y
}
