package widgets

// Trade counter bar chart — migrated from MM trade_counter_layer.odin.
// Pure RCL: no implot, no platform imports. Emits Cmd_Rect_Filled bars.

import "core:math"
import "mr:model"
import "mr:ports"
import "mr:ui"

Trade_Counter_Data :: struct {
	stats:         []model.Stat,
	viewport:      ui.Rect,
	timeframe:     i64,
	x_min:         f64, // visible range min (unix seconds, f64)
	x_max:         f64, // visible range max
	bar_width_pct: f64, // bar width as fraction of timeframe (e.g. 0.4)
	text:          ports.Text_Port,
}

trade_counter :: proc(buf: ^ui.Command_Buffer, data: Trade_Counter_Data) {
	if len(data.stats) == 0 do return
	range_span := data.x_max - data.x_min
	if range_span <= 0 do return

	vp := data.viewport
	half_h := vp.size.y * 0.5
	center_y := vp.pos.y + half_h

	bar_w := f32((f64(data.timeframe) / range_span) * f64(vp.size.x) * data.bar_width_pct)
	bar_w = math.max(bar_w, 1)

	// First pass: find max count for y-axis scaling.
	limit: f64 = 1
	for s in data.stats {
		fx := f64(s.unix)
		if fx < data.x_min || fx > data.x_max do continue
		limit = math.max(limit, s.tbuy)
		limit = math.max(limit, s.tsell)
	}

	// Clip to viewport.
	ui.push(buf, ui.Cmd_Clip_Push{rect = vp})

	// Label — positioned using text measurement.
	label :: "Trade Counter"
	label_size := data.text.measure(ui.FONT_SIZE_BASE, label)
	ui.push_text(buf, {vp.pos.x + 4, vp.pos.y + label_size.y}, label,
		ui.with_alpha(ui.COL_WHITE, 0.6), ui.FONT_SIZE_BASE)

	// Center line.
	ui.push(buf, ui.Cmd_Line{
		from      = {vp.pos.x, center_y},
		to        = {vp.pos.x + vp.size.x, center_y},
		color     = ui.with_alpha(ui.COL_WHITE, 0.15),
		thickness = 1,
	})

	// Second pass: emit bars.
	for s in data.stats {
		fx := f64(s.unix)
		if fx < data.x_min || fx > data.x_max do continue

		t := f32((fx - data.x_min) / range_span)
		cx := vp.pos.x + t * vp.size.x - bar_w * 0.5

		// Buy bar (green, grows upward from center).
		buy_h := f32(s.tbuy / limit) * half_h
		if buy_h > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {cx, center_y - buy_h}, size = {bar_w, buy_h}},
				color = ui.COL_GREEN,
			})
		}

		// Sell bar (red, grows downward from center).
		sell_h := f32(s.tsell / limit) * half_h
		if sell_h > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {cx, center_y}, size = {bar_w, sell_h}},
				color = ui.COL_RED,
			})
		}
	}

	ui.push(buf, ui.Cmd_Clip_Pop{})
}
