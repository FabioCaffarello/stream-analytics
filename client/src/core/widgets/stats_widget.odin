package widgets

// Stats widget — compact panel showing mark price, funding rate,
// liquidation volumes, and buy/sell ratio bar. Inspired by MarketMonkey.
// Pure RCL: no platform imports.

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:ui"

Stats_Widget_Data :: struct {
	store:    ^services.Stats_Store,
	viewport: ui.Rect,
	text:     ports.Text_Port,
	stream_id: string,
	stream_state: streams.Stream_State,
}

stats_widget :: proc(buf: ^ui.Command_Buffer, data: Stats_Widget_Data) {
	vp := data.viewport
	store := data.store

	inner, _ := ui.panel_v2(buf, vp, ui.Panel_V2_Config{
		title        = "Stats",
		title_height = data.text.line_height(ui.FONT_SIZE_SM),
		bg_color     = ui.COL_PANEL_BG,
		pad          = 4,
	}, data.text.measure, ui.FONT_SIZE_SM)

	if store == nil || store.count == 0 {
		msg :: "Waiting for stats..."
		ui.push_text(buf,
			{vp.pos.x + vp.size.x * 0.5 - 60, vp.pos.y + vp.size.y * 0.5},
			msg, ui.with_alpha(ui.COL_WHITE, 0.3), ui.FONT_SIZE_SM)
		return
	}

	latest := services.get_stats(store, 0)
	prev := store.count >= 2 ? services.get_stats(store, 1) : latest
	tbuf: [64]u8
	row_h := data.text.line_height(ui.FONT_SIZE_SM) + 2
	y := inner.pos.y

	// Mark price (large) with delta arrow.
	price_decs := ui.auto_price_decimals(latest.mark_price)
	price_str := ui.format_price(tbuf[:], latest.mark_price, price_decs)
	ui.push_text(buf, {inner.pos.x, y + data.text.line_height(ui.FONT_SIZE_LG)},
		price_str, ui.COL_WHITE, ui.FONT_SIZE_LG, .Mono)
	// Delta percentage next to price.
	if prev.mark_price > 0 {
		pct_change := (latest.mark_price - prev.mark_price) / prev.mark_price * 100
		arrow := pct_change >= 0 ? "^" : "v"
		delta_color := pct_change >= 0 ? ui.COL_GREEN : ui.COL_RED
		delta_str := fmt.bprintf(tbuf[:], "%s%.2f%%", arrow, abs(pct_change))
		price_w := data.text.measure(ui.FONT_SIZE_LG, price_str).x
		ui.push_text(buf, {inner.pos.x + price_w + 6, y + data.text.line_height(ui.FONT_SIZE_LG)},
			delta_str, delta_color, ui.FONT_SIZE_SM, .Mono)
	}
	y += data.text.line_height(ui.FONT_SIZE_LG) + 4

	// Funding rate with delta arrow.
	label_x := inner.pos.x
	value_x := inner.pos.x + data.text.measure(ui.FONT_SIZE_SM, "Funding ").x
	label_color := ui.with_alpha(ui.COL_WHITE, 0.5)

	ui.push_text(buf, {label_x, y + row_h}, "Funding", label_color, ui.FONT_SIZE_SM, .Mono)
	funding_str := fmt.bprintf(tbuf[:], "%.4f%%", latest.funding * 100)
	funding_color := latest.funding >= 0 ? ui.COL_GREEN : ui.COL_RED
	ui.push_text(buf, {value_x, y + row_h}, funding_str, funding_color, ui.FONT_SIZE_SM, .Mono)
	// Funding delta arrow.
	if prev.funding != latest.funding {
		f_arrow := latest.funding > prev.funding ? "^" : "v"
		f_delta_color := latest.funding > prev.funding ? ui.COL_GREEN : ui.COL_RED
		funding_w := data.text.measure(ui.FONT_SIZE_SM, funding_str).x
		ui.push_text(buf, {value_x + funding_w + 4, y + row_h}, f_arrow,
			f_delta_color, ui.FONT_SIZE_SM, .Mono)
	}
	y += row_h

	// Liq Buy / Sell — side-by-side when wide enough.
	two_col := inner.size.x >= 180
	if two_col {
		col_w := inner.size.x * 0.5
		right_x := label_x + col_w
		right_val_x := right_x + data.text.measure(ui.FONT_SIZE_SM, "Sell ").x
		ui.push_text(buf, {label_x, y + row_h}, "Buy", label_color, ui.FONT_SIZE_SM, .Mono)
		buy_str := fmt.bprintf(tbuf[:], "%.2f", latest.liq_buy)
		ui.push_text(buf, {value_x, y + row_h}, buy_str, ui.COL_GREEN, ui.FONT_SIZE_SM, .Mono)
		ui.push_text(buf, {right_x, y + row_h}, "Sell", label_color, ui.FONT_SIZE_SM, .Mono)
		sell_str := fmt.bprintf(tbuf[:], "%.2f", latest.liq_sell)
		ui.push_text(buf, {right_val_x, y + row_h}, sell_str, ui.COL_RED, ui.FONT_SIZE_SM, .Mono)
		y += row_h + 2
	} else {
		ui.push_text(buf, {label_x, y + row_h}, "Liq Buy", label_color, ui.FONT_SIZE_SM, .Mono)
		buy_str := fmt.bprintf(tbuf[:], "%.2f", latest.liq_buy)
		ui.push_text(buf, {value_x, y + row_h}, buy_str, ui.COL_GREEN, ui.FONT_SIZE_SM, .Mono)
		y += row_h
		ui.push_text(buf, {label_x, y + row_h}, "Liq Sell", label_color, ui.FONT_SIZE_SM, .Mono)
		sell_str := fmt.bprintf(tbuf[:], "%.2f", latest.liq_sell)
		ui.push_text(buf, {value_x, y + row_h}, sell_str, ui.COL_RED, ui.FONT_SIZE_SM, .Mono)
		y += row_h + 2
	}

	// 24h High / Low (from available stats data) — side-by-side when wide.
	if store.count >= 2 && y + row_h * 2 < ui.rect_bottom(inner) {
		hi := latest.mark_price
		lo := latest.mark_price
		for si in 0 ..< store.count {
			s := services.get_stats(store, si)
			if s.mark_price > hi do hi = s.mark_price
			if s.mark_price < lo && s.mark_price > 0 do lo = s.mark_price
		}
		if two_col {
			col_w := inner.size.x * 0.5
			right_x := label_x + col_w
			right_val_x := right_x + data.text.measure(ui.FONT_SIZE_SM, "Low ").x
			ui.push_text(buf, {label_x, y + row_h}, "High", label_color, ui.FONT_SIZE_SM, .Mono)
			hi_str := ui.format_price(tbuf[:], hi, price_decs)
			ui.push_text(buf, {value_x, y + row_h}, hi_str, ui.COL_GREEN, ui.FONT_SIZE_SM, .Mono)
			ui.push_text(buf, {right_x, y + row_h}, "Low", label_color, ui.FONT_SIZE_SM, .Mono)
			lo_str := ui.format_price(tbuf[:], lo, price_decs)
			ui.push_text(buf, {right_val_x, y + row_h}, lo_str, ui.COL_RED, ui.FONT_SIZE_SM, .Mono)
			y += row_h + 2
		} else {
			ui.push_text(buf, {label_x, y + row_h}, "High", label_color, ui.FONT_SIZE_SM, .Mono)
			hi_str := ui.format_price(tbuf[:], hi, price_decs)
			ui.push_text(buf, {value_x, y + row_h}, hi_str, ui.COL_GREEN, ui.FONT_SIZE_SM, .Mono)
			y += row_h
			ui.push_text(buf, {label_x, y + row_h}, "Low", label_color, ui.FONT_SIZE_SM, .Mono)
			lo_str := ui.format_price(tbuf[:], lo, price_decs)
			ui.push_text(buf, {value_x, y + row_h}, lo_str, ui.COL_RED, ui.FONT_SIZE_SM, .Mono)
			y += row_h + 2
		}

		// 24h range bar: visual bar showing current price position within high/low.
		range_val := hi - lo
		if range_val > 0 && y + 12 < ui.rect_bottom(inner) {
			bar_h := f32(6)
			bar_w := inner.size.x
			// Background bar.
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {inner.pos.x, y}, size = {bar_w, bar_h}},
				color = ui.with_alpha(ui.COL_WHITE, 0.08),
			})
			// Gradient fill from green (low) to red (high).
			pos_pct := clamp(f32((latest.mark_price - lo) / range_val), 0, 1)
			filled_w := bar_w * pos_pct
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {inner.pos.x, y}, size = {filled_w, bar_h}},
				color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.4),
			})
			// Marker for current price.
			marker_x := inner.pos.x + filled_w - 1
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {marker_x, y - 1}, size = {3, bar_h + 2}},
				color = ui.COL_WHITE,
			})
			y += bar_h + 4
		}
	}

	// Mini sparkline (last 30 mark prices).
	if store.count >= 3 && y + 24 < ui.rect_bottom(inner) {
		SPARK_COUNT :: 30
		spark_n := min(store.count, SPARK_COUNT)
		spark_hi := f64(0)
		spark_lo := f64(1e18)
		for si in 0 ..< spark_n {
			s := services.get_stats(store, si)
			if s.mark_price > spark_hi do spark_hi = s.mark_price
			if s.mark_price < spark_lo && s.mark_price > 0 do spark_lo = s.mark_price
		}
		spark_range := spark_hi - spark_lo
		if spark_range <= 0 do spark_range = 1
		spark_w := inner.size.x
		spark_h := f32(20)
		spark_y := y
		step := spark_w / f32(spark_n - 1)
		prev_sx := f32(0)
		prev_sy := f32(0)
		has_prev := false
		for si := spark_n - 1; si >= 0; si -= 1 {
			s := services.get_stats(store, si)
			sx := inner.pos.x + f32(spark_n - 1 - si) * step
			sy := spark_y + spark_h * (1.0 - f32((s.mark_price - spark_lo) / spark_range))
			if has_prev {
				ui.push(buf, ui.Cmd_Line{
					from      = {prev_sx, prev_sy},
					to        = {sx, sy},
					color     = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.5),
					thickness = 1,
				})
			}
			prev_sx = sx
			prev_sy = sy
			has_prev = true
		}
		y += spark_h + 4
	}

	// Buy/Sell ratio bar.
	total := latest.liq_buy + latest.liq_sell
	if total > 0 && y + 10 < ui.rect_bottom(inner) {
		bar_h := f32(8)
		bar_w := inner.size.x
		buy_pct := f32(latest.liq_buy / total)

		// Buy (green, left).
		if buy_pct > 0 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {inner.pos.x, y}, size = {bar_w * buy_pct, bar_h}},
				color = ui.COL_GREEN,
			})
		}
		// Sell (red, right).
		if buy_pct < 1 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {inner.pos.x + bar_w * buy_pct, y}, size = {bar_w * (1 - buy_pct), bar_h}},
				color = ui.COL_RED,
			})
		}
	}
}
