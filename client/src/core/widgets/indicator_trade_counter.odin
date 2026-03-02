package widgets

// Trade Counter sub-plot for candle chart.
// Renders buy/sell trade volumes from candle store as diverging bars:
// green bars above center = buy volume, red bars below = sell volume.

import "core:fmt"
import "core:math"
import "mr:services"
import "mr:ui"

Trade_Counter_Config :: struct {
	visible:    bool,
	color_buy:  ui.Color,
	color_sell: ui.Color,
}

TRADE_COUNTER_DEFAULT :: Trade_Counter_Config{
	visible    = false,
	color_buy  = ui.Color{0.18, 0.74, 0.52, 0.7}, // green
	color_sell = ui.Color{0.96, 0.28, 0.37, 0.7}, // red
}

// Render trade counter sub-plot in a dedicated rect below the main chart.
// Sources buy_vol/sell_vol from candle entries (not stats store).
render_trade_counter :: proc(
	buf: ^ui.Command_Buffer,
	rect: ui.Rect,
	ctx: ^Chart_Layer_Context,
	cfg: Trade_Counter_Config,
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
	ui.push_text(buf, {rect.pos.x + 4, rect.pos.y + 10}, "Trade Counter",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	y_axis_w := f32(50)
	chart_w := rect.size.x - y_axis_w
	if chart_w <= 0 do return false

	pad := ui.Rect{pos = {rect.pos.x, rect.pos.y + 2}, size = {chart_w, rect.size.y - 4}}

	// Scan visible candles for buy_vol/sell_vol.
	vol_max := f64(0)
	for i in ctx.candle_start ..< ctx.candle_start + ctx.candle_count {
		if i >= ctx.candles.count do break
		c := services.get_candle(ctx.candles, i)
		vol_max = math.max(vol_max, c.buy_vol)
		vol_max = math.max(vol_max, c.sell_vol)
	}
	if vol_max <= 0 do return false
	vol_max *= 1.1

	half_h := pad.size.y * 0.5
	center_y := pad.pos.y + half_h

	// Center line.
	ui.push(buf, ui.Cmd_Line{
		from      = {pad.pos.x, center_y},
		to        = {pad.pos.x + chart_w, center_y},
		color     = ui.with_alpha(ui.COL_WHITE, 0.08),
		thickness = 1,
	})

	// Y-axis labels.
	top_buf: [16]u8
	top_str := fmt.bprintf(top_buf[:], "%.1f", vol_max)
	ui.push_text(buf, {pad.pos.x + chart_w + 2, pad.pos.y + 10}, top_str,
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	bot_buf: [16]u8
	bot_str := fmt.bprintf(bot_buf[:], "-%.1f", vol_max)
	ui.push_text(buf, {pad.pos.x + chart_w + 2, ui.rect_bottom(pad) - 4}, bot_str,
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	// Bar rendering — one bar per visible candle, aligned with chart X positions.
	for i in ctx.candle_start ..< ctx.candle_start + ctx.candle_count {
		if i >= ctx.candles.count do break
		c := services.get_candle(ctx.candles, i)

		x := chart_index_to_x(ctx, i)
		bw := ctx.slot_width * 0.7
		if bw < 2 do bw = 2

		// Buy bar (green, grows upward from center).
		buy_h := f32(c.buy_vol / vol_max) * half_h
		if buy_h > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x - bw * 0.5, center_y - buy_h}, size = {bw, buy_h}},
				color = cfg.color_buy,
			})
		}

		// Sell bar (red, grows downward from center).
		sell_h := f32(c.sell_vol / vol_max) * half_h
		if sell_h > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x - bw * 0.5, center_y}, size = {bw, sell_h}},
				color = cfg.color_sell,
			})
		}
	}
	return true
}
