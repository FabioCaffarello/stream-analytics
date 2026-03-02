package widgets

// EMA / SMA moving average indicator layer for candle chart.
// Renders as colored polylines over the price area.

import "mr:services"
import "mr:ui"

MA_MAX_PERIOD :: 200
MA_MAX_VISIBLE :: 500 // max visible data points to compute

MA_Type :: enum u8 {
	SMA,
	EMA,
}

MA_Line :: struct {
	ma_type: MA_Type,
	period:  int,
	color:   ui.Color,
	visible: bool,
}

// Default MA lines: EMA(9) yellow, EMA(21) cyan, SMA(50) purple.
MA_DEFAULT_LINES :: [3]MA_Line{
	{ma_type = .EMA, period = 9,  color = ui.Color{0.98, 0.85, 0.2, 0.8}, visible = true},
	{ma_type = .EMA, period = 21, color = ui.Color{0.2, 0.8, 0.95, 0.8},  visible = true},
	{ma_type = .SMA, period = 50, color = ui.Color{0.7, 0.4, 1.0, 0.6},   visible = true},
}

// Render moving average lines on the chart.
render_ma_lines :: proc(
	buf: ^ui.Command_Buffer,
	ctx: ^Chart_Layer_Context,
	lines: []MA_Line,
) {
	if ctx.candles == nil || ctx.candle_count <= 1 do return

	for li in 0 ..< len(lines) {
		line := lines[li]
		if !line.visible do continue
		if line.period <= 0 || line.period > MA_MAX_PERIOD do continue

		// Compute MA values for visible range + lookback.
		lookback_start := max(ctx.candle_start - line.period, 0)
		data_count := (ctx.candle_start + ctx.candle_count) - lookback_start
		if data_count < line.period do continue

		// We compute MA for all data points from lookback_start, but only render
		// the visible portion (ctx.candle_start .. ctx.candle_start + ctx.candle_count).
		prev_x := f32(0)
		prev_y := f32(0)
		has_prev := false

		switch line.ma_type {
		case .SMA:
			// Running SMA: sum of close prices / period.
			sum := f64(0)
			for i in lookback_start ..< lookback_start + data_count {
				if i >= ctx.candles.count do break
				c := services.get_candle(ctx.candles, i)
				sum += c.close

				age := i - lookback_start
				if age >= line.period {
					// Subtract the oldest value.
					old_c := services.get_candle(ctx.candles, i - line.period)
					sum -= old_c.close
				}

				if age < line.period - 1 do continue // not enough data yet
				sma_val := sum / f64(line.period)

				if i >= ctx.candle_start && i < ctx.candle_start + ctx.candle_count {
					x := chart_index_to_x(ctx, i)
					y := chart_price_to_y(ctx, sma_val)
					if has_prev {
						ui.push(buf, ui.Cmd_Line{
							from      = {prev_x, prev_y},
							to        = {x, y},
							color     = line.color,
							thickness = 1,
						})
					}
					prev_x = x
					prev_y = y
					has_prev = true
				}
			}

		case .EMA:
			// EMA: multiplier = 2 / (period + 1), ema = close * mult + prev_ema * (1 - mult)
			mult := f64(2) / f64(line.period + 1)
			ema_val := f64(0)
			ema_initialized := false

			for i in lookback_start ..< lookback_start + data_count {
				if i >= ctx.candles.count do break
				c := services.get_candle(ctx.candles, i)

				if !ema_initialized {
					ema_val = c.close
					ema_initialized = true
				} else {
					ema_val = c.close * mult + ema_val * (1.0 - mult)
				}

				age := i - lookback_start
				if age < line.period - 1 do continue

				if i >= ctx.candle_start && i < ctx.candle_start + ctx.candle_count {
					x := chart_index_to_x(ctx, i)
					y := chart_price_to_y(ctx, ema_val)
					if has_prev {
						ui.push(buf, ui.Cmd_Line{
							from      = {prev_x, prev_y},
							to        = {x, y},
							color     = line.color,
							thickness = 1,
						})
					}
					prev_x = x
					prev_y = y
					has_prev = true
				}
			}
		}
	}
}
