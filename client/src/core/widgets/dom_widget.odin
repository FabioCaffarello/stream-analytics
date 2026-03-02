package widgets

// DOM (Depth of Market) widget — 5-column depth ladder with fill tracking.
// Columns: Buy Fills | Bid Size | Price | Ask Size | Sell Fills
// Centered on mid-price with heatmap coloring for bid/ask volume.

import "core:fmt"
import "core:math"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:ui"

DOM_MAX_ROWS :: 50
DOM_AGG_CAP  :: 128

@(private = "file")
DOM_Row :: struct {
	price:     f64,
	bid_size:  f64,
	ask_size:  f64,
	buy_fill:  f64,
	sell_fill: f64,
}

DOM_Widget_Data :: struct {
	orderbook:            ^services.Orderbook_Store,
	dom:                  ^services.DOM_Store,
	viewport:             ui.Rect,
	text:                 ports.Text_Port,
	input:                ports.Input_State,
	pointer:              ui.Pointer_Input,
	group_options:        []string,
	group_idx:            ^int,
	price_group:          f64,
	stream_id:            string,
	stream_state:         streams.Stream_State,
	stream_desync_reason: streams.Stream_Desync_Reason,
	empty_reason:         string,
}

dom_widget :: proc(buf: ^ui.Command_Buffer, data: DOM_Widget_Data) {
	store := data.orderbook
	if store == nil || (store.ask_count == 0 && store.bid_count == 0) {
		vp := data.viewport
		inner, _ := ui.panel_v2(buf, vp, ui.Panel_V2_Config{
			title        = "DOM",
			title_height = data.text.line_height(ui.FONT_SIZE_SM),
			bg_color     = ui.COL_PANEL_BG,
			pad          = 4,
		}, data.text.measure, ui.FONT_SIZE_SM)
		msg := data.empty_reason
		if len(msg) == 0 do msg = "Waiting for orderbook..."
		ui.push_text(buf,
			{inner.pos.x + inner.size.x * 0.5 - 80, inner.pos.y + inner.size.y * 0.5},
			msg, ui.with_alpha(ui.COL_WHITE, 0.3), ui.FONT_SIZE_SM)
		return
	}

	price_group := data.price_group > 0 ? data.price_group : 10.0
	row_h := f32(16)
	font_size := ui.FONT_SIZE_XS

	// Panel with header controls.
	inner, ctrl_rect := ui.panel_v2(buf, data.viewport, ui.Panel_V2_Config{
		title        = "DOM",
		title_height = data.text.line_height(ui.FONT_SIZE_SM),
		bg_color     = ui.COL_PANEL_BG,
		pad          = 2,
	}, data.text.measure, ui.FONT_SIZE_SM)

	// Grouping segmented control in header.
	if len(data.group_options) > 0 && data.group_idx != nil && ctrl_rect.size.x > 60 {
		// Reset button on far right.
		reset_w := f32(36)
		reset_rect := ui.Rect{
			pos  = {ui.rect_right(ctrl_rect) - reset_w, ctrl_rect.pos.y + (ctrl_rect.size.y - 14) * 0.5},
			size = {reset_w, 14},
		}
		reset_res := ui.button(buf, reset_rect, "Reset", data.pointer,
			data.text.measure, ui.FONT_SIZE_XS, .Mono)
		if reset_res.clicked && data.dom != nil {
			services.dom_store_reset(data.dom)
		}

		seg_w := min(ctrl_rect.size.x - reset_w - 8, f32(len(data.group_options)) * 32)
		seg_rect := ui.Rect{
			pos  = {ui.rect_right(ctrl_rect) - reset_w - 4 - seg_w, ctrl_rect.pos.y + (ctrl_rect.size.y - 14) * 0.5},
			size = {seg_w, 14},
		}
		res := ui.segmented_control(buf, seg_rect, data.group_options, data.group_idx^,
			data.pointer, data.text.measure, ui.FONT_SIZE_XS, .Mono)
		if res.changed {
			data.group_idx^ = res.index
		}
	}

	ui.push(buf, ui.Cmd_Clip_Push{rect = data.viewport})

	// Reserve footer for VWAP/TWAP.
	footer_h := f32(20)
	body_h := inner.size.y - footer_h
	if body_h < row_h * 3 do body_h = inner.size.y

	// Number of visible rows.
	max_rows := int(body_h / row_h)
	if max_rows < 3 do max_rows = 3
	if max_rows > DOM_MAX_ROWS do max_rows = DOM_MAX_ROWS

	// Center price.
	mid := services.mid_price(store)
	if mid <= 0 do mid = store.last_price
	if mid <= 0 {
		ui.push(buf, ui.Cmd_Clip_Pop{})
		return
	}

	// Generate price levels centered on mid.
	center_bucket := math.floor(mid / price_group) * price_group
	half_rows := max_rows / 2

	// Column widths: BuyFill | Bid | Price | Ask | SellFill
	price_col_w := data.text.measure(font_size, "00000.00").x + 6
	fill_col_w := data.text.measure(font_size, "00.000").x + 4
	size_col_w := (inner.size.x - price_col_w - fill_col_w * 2) * 0.5
	if size_col_w < 30 do size_col_w = 30

	// Recalculate fill_col_w to fill remaining space.
	remaining := inner.size.x - price_col_w - size_col_w * 2
	fill_col_w = remaining * 0.5
	if fill_col_w < 20 do fill_col_w = 20

	col_x := [5]f32{
		inner.pos.x,
		inner.pos.x + fill_col_w,
		inner.pos.x + fill_col_w + size_col_w,
		inner.pos.x + fill_col_w + size_col_w + price_col_w,
		inner.pos.x + fill_col_w + size_col_w + price_col_w + size_col_w,
	}
	col_w := [5]f32{fill_col_w, size_col_w, price_col_w, size_col_w, fill_col_w}

	// Aggregate orderbook into price grid.
	rows: [DOM_MAX_ROWS]DOM_Row
	row_count := 0

	// Build price levels from top (highest) to bottom (lowest).
	top_price := center_bucket + f64(half_rows) * price_group
	for ri in 0 ..< max_rows {
		p := top_price - f64(ri) * price_group
		rows[ri] = DOM_Row{price = p}
		row_count += 1
	}

	// Fill bid/ask sizes from orderbook (aggregated to price group).
	for i in 0 ..< store.bid_count {
		bucket := math.floor(store.bid_prices[i] / price_group) * price_group
		for ri in 0 ..< row_count {
			if rows[ri].price == bucket {
				rows[ri].bid_size += store.bid_sizes[i]
				break
			}
		}
	}
	for i in 0 ..< store.ask_count {
		bucket := math.floor(store.ask_prices[i] / price_group) * price_group
		for ri in 0 ..< row_count {
			if rows[ri].price == bucket {
				rows[ri].ask_size += store.ask_sizes[i]
				break
			}
		}
	}

	// Fill buy/sell fills from DOM store.
	if data.dom != nil {
		for ri in 0 ..< row_count {
			buy_v, sell_v := services.dom_store_get_fill(data.dom, rows[ri].price)
			rows[ri].buy_fill = buy_v
			rows[ri].sell_fill = sell_v
		}
	}

	// Find max sizes for heatmap normalization.
	max_bid := f64(0.001)
	max_ask := f64(0.001)
	max_buy_fill := f64(0.001)
	max_sell_fill := f64(0.001)
	for ri in 0 ..< row_count {
		max_bid = max(max_bid, rows[ri].bid_size)
		max_ask = max(max_ask, rows[ri].ask_size)
		max_buy_fill = max(max_buy_fill, rows[ri].buy_fill)
		max_sell_fill = max(max_sell_fill, rows[ri].sell_fill)
	}

	// Auto-detect price precision.
	price_decimals := dom_auto_decimals(price_group)

	// Column header.
	hdr_y := inner.pos.y
	hdr_h := row_h
	hdr_color := ui.with_alpha(ui.COL_WHITE, 0.4)
	hdr_labels := [5]string{"BuyFill", "Bid", "Price", "Ask", "SellFill"}
	for ci in 0 ..< 5 {
		ui.push_text(buf, {col_x[ci] + 2, hdr_y + hdr_h * 0.5 + font_size * 0.35},
			hdr_labels[ci], hdr_color, font_size, .Mono)
	}

	// Render rows.
	body_y := hdr_y + hdr_h
	tbuf: [64]u8

	for ri in 0 ..< row_count {
		ry := body_y + f32(ri) * row_h
		if ry + row_h > ui.rect_bottom(inner) do break
		row := rows[ri]
		is_center := row.price == center_bucket

		// Center row highlight.
		if is_center {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {inner.pos.x, ry}, size = {inner.size.x, row_h}},
				color = ui.with_alpha(ui.COL_PRIMARY, 0.3),
			})
		}

		// Alternating row background.
		if ri % 2 == 1 && !is_center {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {inner.pos.x, ry}, size = {inner.size.x, row_h}},
				color = ui.with_alpha(ui.COL_WHITE, 0.02),
			})
		}

		text_y := ry + row_h * 0.5 + font_size * 0.35

		// Col 0: Buy fills — green bar + text.
		if row.buy_fill > 0 {
			t := f32(math.min(row.buy_fill / max_buy_fill, 1.0))
			bar_w := col_w[0] * t
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {col_x[0] + col_w[0] - bar_w, ry}, size = {bar_w, row_h - 1}},
				color = ui.with_alpha(ui.COL_GREEN, 0.15 + t * 0.25),
			})
			fill_str := dom_format_size(tbuf[:], row.buy_fill)
			tw := data.text.measure(font_size, fill_str).x
			ui.push_text(buf, {col_x[0] + col_w[0] - tw - 2, text_y},
				fill_str, ui.with_alpha(ui.COL_GREEN, 0.7), font_size, .Mono)
		}

		// Col 1: Bid size — green heatmap bar.
		if row.bid_size > 0 {
			t := f32(math.min(row.bid_size / max_bid, 1.0))
			bar_w := col_w[1] * t
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {col_x[1] + col_w[1] - bar_w, ry}, size = {bar_w, row_h - 1}},
				color = dom_heatmap_green(t),
			})
			size_str := dom_format_size(tbuf[:], row.bid_size)
			tw := data.text.measure(font_size, size_str).x
			text_color := t > 0.5 ? ui.COL_WHITE : ui.with_alpha(ui.COL_WHITE, 0.8)
			ui.push_text(buf, {col_x[1] + col_w[1] - tw - 2, text_y},
				size_str, text_color, font_size, .Mono)
		}

		// Col 2: Price.
		price_str := dom_format_price(tbuf[:], row.price, price_decimals)
		price_color := is_center ? ui.COL_WHITE : ui.COL_TEXT_SECONDARY
		ui.push_text(buf, {col_x[2] + 2, text_y}, price_str, price_color, font_size, .Mono)

		// Col 3: Ask size — red heatmap bar.
		if row.ask_size > 0 {
			t := f32(math.min(row.ask_size / max_ask, 1.0))
			bar_w := col_w[3] * t
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {col_x[3], ry}, size = {bar_w, row_h - 1}},
				color = dom_heatmap_red(t),
			})
			size_str := dom_format_size(tbuf[:], row.ask_size)
			text_color := t > 0.5 ? ui.COL_WHITE : ui.with_alpha(ui.COL_WHITE, 0.8)
			ui.push_text(buf, {col_x[3] + 2, text_y},
				size_str, text_color, font_size, .Mono)
		}

		// Col 4: Sell fills — red bar + text.
		if row.sell_fill > 0 {
			t := f32(math.min(row.sell_fill / max_sell_fill, 1.0))
			bar_w := col_w[4] * t
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {col_x[4], ry}, size = {bar_w, row_h - 1}},
				color = ui.with_alpha(ui.COL_RED, 0.15 + t * 0.25),
			})
			fill_str := dom_format_size(tbuf[:], row.sell_fill)
			ui.push_text(buf, {col_x[4] + 2, text_y},
				fill_str, ui.with_alpha(ui.COL_RED, 0.7), font_size, .Mono)
		}
	}

	// Column separators.
	sep_color := ui.with_alpha(ui.COL_DIVIDER, 0.3)
	for ci in 1 ..< 5 {
		ui.push(buf, ui.Cmd_Line{
			from      = {col_x[ci], inner.pos.y},
			to        = {col_x[ci], ui.rect_bottom(inner)},
			color     = sep_color,
			thickness = 1,
		})
	}

	// Footer: VWAP, TWAP, trade count.
	if body_h < inner.size.y && data.dom != nil {
		footer_y := ui.rect_bottom(inner) - footer_h
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {inner.pos.x, footer_y}, size = {inner.size.x, footer_h}},
			color = ui.COL_SURFACE_2,
		})
		ui.push(buf, ui.Cmd_Line{
			from      = {inner.pos.x, footer_y},
			to        = {ui.rect_right(inner), footer_y},
			color     = ui.COL_DIVIDER,
			thickness = 1,
		})

		fy := footer_y + footer_h * 0.5 + ui.FONT_SIZE_XS * 0.35
		fx := inner.pos.x + 4

		vwap := services.dom_store_vwap(data.dom)
		if vwap > 0 {
			vwap_str := fmt.bprintf(tbuf[:], "VWAP:%.1f", vwap)
			ui.push_text(buf, {fx, fy}, vwap_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
			fx += data.text.measure(ui.FONT_SIZE_XS, vwap_str).x + 8
		}

		twap := services.dom_store_twap(data.dom)
		if twap > 0 {
			twap_str := fmt.bprintf(tbuf[:], "TWAP:%.1f", twap)
			ui.push_text(buf, {fx, fy}, twap_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
			fx += data.text.measure(ui.FONT_SIZE_XS, twap_str).x + 8
		}

		if data.dom.trade_count > 0 {
			tc_str := fmt.bprintf(tbuf[:], "N:%d", data.dom.trade_count)
			ui.push_text(buf, {fx, fy}, tc_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		}
	}

	ui.push(buf, ui.Cmd_Clip_Pop{})
}

// Heatmap colors: bid (green) and ask (red) with intensity-based alpha.
@(private = "file")
dom_heatmap_green :: proc(t: f32) -> ui.Color {
	// Low: dark green, high: bright green.
	return {0.05 + t * 0.1, 0.2 + t * 0.4, 0.05 + t * 0.05, 0.15 + t * 0.35}
}

@(private = "file")
dom_heatmap_red :: proc(t: f32) -> ui.Color {
	// Low: dark red, high: bright red.
	return {0.2 + t * 0.4, 0.05 + t * 0.1, 0.05 + t * 0.05, 0.15 + t * 0.35}
}

@(private = "file")
dom_auto_decimals :: proc(price_group: f64) -> int {
	if price_group >= 100 do return 0
	if price_group >= 1 do return 1
	if price_group >= 0.1 do return 2
	if price_group >= 0.01 do return 3
	return 4
}

@(private = "file")
dom_format_price :: proc(buf_arr: []u8, price: f64, decimals: int) -> string {
	switch decimals {
	case 0:  return fmt.bprintf(buf_arr, "%.0f", price)
	case 1:  return fmt.bprintf(buf_arr, "%.1f", price)
	case 3:  return fmt.bprintf(buf_arr, "%.3f", price)
	case 4:  return fmt.bprintf(buf_arr, "%.4f", price)
	case:    return fmt.bprintf(buf_arr, "%.2f", price)
	}
}

@(private = "file")
dom_format_size :: proc(buf_arr: []u8, size: f64) -> string {
	if size >= 1000 {
		return fmt.bprintf(buf_arr, "%.0f", size)
	} else if size >= 100 {
		return fmt.bprintf(buf_arr, "%.1f", size)
	} else if size >= 1 {
		return fmt.bprintf(buf_arr, "%.2f", size)
	}
	return fmt.bprintf(buf_arr, "%.3f", size)
}
