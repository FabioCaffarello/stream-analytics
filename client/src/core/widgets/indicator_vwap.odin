package widgets

// VWAP (Volume Weighted Average Price) indicator layer for candle chart.
// Renders as a single colored line. Resets daily (at 00:00 UTC).

import "mr:services"
import "mr:ui"

VWAP_Config :: struct {
	color:   ui.Color,
	visible: bool,
}

VWAP_DEFAULT :: VWAP_Config{
	color   = ui.Color{0.0, 0.75, 0.95, 0.85},
	visible = true,
}

// Render VWAP line on the chart.
render_vwap :: proc(
	buf: ^ui.Command_Buffer,
	ctx: ^Chart_Layer_Context,
	cfg: VWAP_Config,
) {
	if !cfg.visible do return
	if ctx.candles == nil || ctx.candle_count <= 1 do return

	// We need to accumulate from the start of the current day.
	// Find the earliest candle in the visible range's day.
	first_visible := services.get_candle(ctx.candles, ctx.candle_start)
	day_start_ms := day_start_utc(first_visible.window_start_ts)

	// Walk backwards to find start of day (lookback scaled by timeframe).
	accum_start := ctx.candle_start
	candles_per_day := ctx.timeframe_ms > 0 ? int(86_400_000 / ctx.timeframe_ms) : 1440
	max_lookback := min(ctx.candle_start, candles_per_day)
	for i := ctx.candle_start - 1; i >= ctx.candle_start - max_lookback && i >= 0; i -= 1 {
		c := services.get_candle(ctx.candles, i)
		if c.window_start_ts < day_start_ms do break
		accum_start = i
	}

	cum_vol := f64(0)
	cum_pv := f64(0)
	prev_x := f32(0)
	prev_y := f32(0)
	has_prev := false

	for i in accum_start ..< ctx.candle_start + ctx.candle_count {
		if i >= ctx.candles.count do break
		c := services.get_candle(ctx.candles, i)

		// Daily reset.
		c_day := day_start_utc(c.window_start_ts)
		if c_day != day_start_ms || cum_vol <= 0 {
			cum_vol = 0
			cum_pv = 0
			day_start_ms = c_day
		}

		typical_price := (c.high + c.low + c.close) / 3.0
		cum_pv += typical_price * c.volume
		cum_vol += c.volume

		if cum_vol <= 0 do continue

		vwap_val := cum_pv / cum_vol

		// Only render for visible candles.
		if i >= ctx.candle_start {
			x := chart_index_to_x(ctx, i)
			y := chart_price_to_y(ctx, vwap_val)
			if has_prev {
				ui.push(buf, ui.Cmd_Line{
					from      = {prev_x, prev_y},
					to        = {x, y},
					color     = cfg.color,
					thickness = 1,
				})
			}
			prev_x = x
			prev_y = y
			has_prev = true
		}
	}
}

// Get the start of the UTC day (in ms) for a given timestamp (ms).
@(private = "file")
day_start_utc :: proc(ts_ms: i64) -> i64 {
	ms_per_day :: i64(86_400_000)
	if ts_ms <= 0 do return 0
	return (ts_ms / ms_per_day) * ms_per_day
}
