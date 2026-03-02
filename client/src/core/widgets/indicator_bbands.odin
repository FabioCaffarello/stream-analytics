package widgets

// Bollinger Bands indicator layer for candle chart.
// Upper/lower bands as semi-transparent filled area, middle as SMA line.

import "core:math"
import "mr:services"
import "mr:ui"

BBands_Config :: struct {
	period:    int,   // default 20
	std_mult:  f64,   // default 2.0
	color_mid: ui.Color,
	color_fill: ui.Color,
	visible:   bool,
}

BBANDS_DEFAULT :: BBands_Config{
	period    = 20,
	std_mult  = 2.0,
	color_mid = ui.Color{0.95, 0.65, 0.15, 0.7},
	color_fill = ui.Color{0.95, 0.65, 0.15, 0.08},
	visible   = true,
}

// Render Bollinger Bands on the chart.
render_bbands :: proc(
	buf: ^ui.Command_Buffer,
	ctx: ^Chart_Layer_Context,
	cfg: BBands_Config,
) {
	if !cfg.visible do return
	if ctx.candles == nil || ctx.candle_count <= 1 do return
	if cfg.period <= 0 || cfg.period > MA_MAX_PERIOD do return

	lookback_start := max(ctx.candle_start - cfg.period, 0)
	data_count := (ctx.candle_start + ctx.candle_count) - lookback_start
	if data_count < cfg.period do return

	prev_mid_x, prev_mid_y: f32
	prev_upper_y, prev_lower_y: f32
	has_prev := false

	// Running sum and sum-of-squares for O(n) BBands computation.
	run_sum := f64(0)
	run_sq  := f64(0)
	period_f := f64(cfg.period)

	// Seed the running window with the first (period-1) values.
	seed_end := min(lookback_start + cfg.period - 1, lookback_start + data_count)
	for si in lookback_start ..< seed_end {
		if si >= ctx.candles.count do break
		c := services.get_candle(ctx.candles, si).close
		run_sum += c
		run_sq  += c * c
	}

	for i in lookback_start ..< lookback_start + data_count {
		if i >= ctx.candles.count do break
		age := i - lookback_start

		if age < cfg.period - 1 do continue

		// Add the newest value to the running window.
		new_val := services.get_candle(ctx.candles, i).close
		if age == cfg.period - 1 {
			// First full window — the seed loop covered [lookback_start, i-1],
			// now add i itself.
			run_sum += new_val
			run_sq  += new_val * new_val
		} else {
			// Slide: add new, remove oldest.
			old_val := services.get_candle(ctx.candles, i - cfg.period).close
			run_sum += new_val - old_val
			run_sq  += new_val * new_val - old_val * old_val
		}

		sma := run_sum / period_f
		variance := run_sq / period_f - sma * sma
		if variance < 0 do variance = 0 // guard against floating-point drift
		std_dev := math.sqrt(variance)
		upper := sma + cfg.std_mult * std_dev
		lower := sma - cfg.std_mult * std_dev

		if i >= ctx.candle_start && i < ctx.candle_start + ctx.candle_count {
			x := chart_index_to_x(ctx, i)
			mid_y := chart_price_to_y(ctx, sma)
			upper_y := chart_price_to_y(ctx, upper)
			lower_y := chart_price_to_y(ctx, lower)

			// Per-candle fill column (eliminates stairstep artifacts).
			fill_h := lower_y - upper_y
			if fill_h > 0 {
				ui.push(buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {x - ctx.slot_width * 0.5, upper_y}, size = {ctx.slot_width, fill_h}},
					color = cfg.color_fill,
				})
			}

			if has_prev {
				// Middle SMA line.
				ui.push(buf, ui.Cmd_Line{
					from      = {prev_mid_x, prev_mid_y},
					to        = {x, mid_y},
					color     = cfg.color_mid,
					thickness = 1,
				})

				// Upper band line.
				ui.push(buf, ui.Cmd_Line{
					from      = {prev_mid_x, prev_upper_y},
					to        = {x, upper_y},
					color     = ui.with_alpha(cfg.color_mid, 0.35),
					thickness = 1,
				})

				// Lower band line.
				ui.push(buf, ui.Cmd_Line{
					from      = {prev_mid_x, prev_lower_y},
					to        = {x, lower_y},
					color     = ui.with_alpha(cfg.color_mid, 0.35),
					thickness = 1,
				})
			}

			prev_mid_x = x
			prev_mid_y = mid_y
			prev_upper_y = upper_y
			prev_lower_y = lower_y
			has_prev = true
		}
	}
}
