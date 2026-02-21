package widgets

// Orderbook widget — displays aggregated bid/ask depth with fill bars.
// Pure RCL: no platform imports. Uses ui primitives for panel/table layout.

import "core:fmt"
import "core:math"
import "mr:ports"
import "mr:services"
import "mr:ui"

OB_MAX_ROWS :: 25
OB_AGG_CAP  :: 128

Aggregated_Level :: struct {
	price, size, sum: f64,
}

Orderbook_Widget_Data :: struct {
	store:       ^services.Orderbook_Store,
	viewport:    ui.Rect,
	text:        ports.Text_Port,
	scroll_y:    ^f32,
	input:       ports.Input_State,
	price_group: f64,
	max_rows:    int,
}

orderbook_widget :: proc(buf: ^ui.Command_Buffer, data: Orderbook_Widget_Data) {
	store := data.store
	if store == nil || (store.ask_count == 0 && store.bid_count == 0) do return

	row_h := data.text.line_height(ui.FONT_SIZE_SM) + 2
	price_group := data.price_group > 0 ? data.price_group : 10.0
	max_rows := data.max_rows > 0 ? data.max_rows : OB_MAX_ROWS

	// Panel.
	inner := ui.panel(buf, data.viewport, ui.Panel_Config{
		title        = "Orderbook",
		title_height = data.text.line_height(ui.FONT_SIZE_SM),
		bg_color     = ui.COL_PANEL_BG,
		pad          = 4,
	}, data.text.measure, ui.FONT_SIZE_SM)

	ui.push(buf, ui.Cmd_Clip_Push{rect = data.viewport})

	// Aggregate levels by price_group.
	agg_asks: [OB_AGG_CAP]Aggregated_Level
	agg_bids: [OB_AGG_CAP]Aggregated_Level
	ask_n := aggregate_levels(store.ask_prices[:store.ask_count], store.ask_sizes[:store.ask_count], price_group, &agg_asks, max_rows)
	bid_n := aggregate_levels(store.bid_prices[:store.bid_count], store.bid_sizes[:store.bid_count], price_group, &agg_bids, max_rows)

	cumsum(&agg_asks, ask_n)
	cumsum(&agg_bids, bid_n)

	max_sum: f64 = 1
	if ask_n > 0 do max_sum = math.max(max_sum, agg_asks[ask_n - 1].sum)
	if bid_n > 0 do max_sum = math.max(max_sum, agg_bids[bid_n - 1].sum)

	// Column widths.
	col_price_w := data.text.measure(ui.FONT_SIZE_SM, "00000.00").x + 8
	col_size_w  := data.text.measure(ui.FONT_SIZE_SM, "00.0000").x + 8
	col_sum_w   := inner.size.x - col_price_w - col_size_w
	col_widths  := [?]f32{col_price_w, col_size_w, col_sum_w}

	// Header.
	hdr_tbl := ui.table_begin(inner, col_widths[:], row_h)
	ui.table_next_row(&hdr_tbl)
	hdr_color := ui.with_alpha(ui.COL_WHITE, 0.5)
	hdr_labels := [?]string{"Price", "Size", "Sum"}
	for i in 0 ..< 3 {
		ui.push_text(buf, ui.table_cell_text_pos(&hdr_tbl, i), hdr_labels[i], hdr_color, ui.FONT_SIZE_SM, .Mono)
	}

	tbuf: [64]u8
	body_y := hdr_tbl.y_cursor

	// --- Ask rows (bottom-to-top: best ask nearest center) ---
	ask_tbl := ui.table_begin(ui.rect_xywh(inner.pos.x, body_y, inner.size.x, f32(ask_n) * row_h), col_widths[:], row_h)
	for i in 0 ..< ask_n {
		row_idx := ask_n - 1 - i
		level := agg_asks[row_idx]
		row := ui.table_next_row(&ask_tbl)

		// Fill bar.
		bar_w := f32(level.sum / max_sum) * inner.size.x
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = row.pos, size = {bar_w, row_h - 1}},
			color = ui.COL_ORDERBOOK_RED,
		})

		price_str := fmt.bprintf(tbuf[:], "%.2f", level.price)
		ui.push_text(buf, ui.table_cell_text_pos(&ask_tbl, 0), price_str, ui.COL_RED, ui.FONT_SIZE_SM, .Mono)
		size_str := fmt.bprintf(tbuf[:], "%.4f", level.size)
		ui.push_text(buf, ui.table_cell_text_pos(&ask_tbl, 1), size_str, ui.with_alpha(ui.COL_WHITE, 0.85), ui.FONT_SIZE_SM, .Mono)
		sum_str := fmt.bprintf(tbuf[:], "%.3f", level.sum)
		ui.push_text(buf, ui.table_cell_text_pos(&ask_tbl, 2), sum_str, ui.with_alpha(ui.COL_WHITE, 0.6), ui.FONT_SIZE_SM, .Mono)
	}

	// --- Center: last_price + spread ---
	center_h := data.text.line_height(ui.FONT_SIZE_LG) + 8
	center_y := ask_tbl.y_cursor
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {inner.pos.x, center_y}, size = {inner.size.x, center_h}},
		color = ui.with_alpha(ui.COL_PRIMARY, 0.5),
	})

	price_str := fmt.bprintf(tbuf[:], "%.2f", store.last_price)
	price_text_y := center_y + data.text.line_height(ui.FONT_SIZE_LG) + 2
	ui.push_text(buf, {inner.pos.x + 4, price_text_y}, price_str, ui.COL_WHITE, ui.FONT_SIZE_LG, .Mono)

	sp := services.spread(store)
	spread_str := fmt.bprintf(tbuf[:], "Spread: %.2f", sp)
	spread_w := data.text.measure(ui.FONT_SIZE_SM, spread_str).x
	ui.push_text(buf, {ui.rect_right(inner) - spread_w - 4, price_text_y}, spread_str,
		ui.with_alpha(ui.COL_WHITE, 0.6), ui.FONT_SIZE_SM, .Mono)

	// --- Bid rows (top-to-bottom: best bid nearest center) ---
	bid_top := center_y + center_h
	bid_tbl := ui.table_begin(ui.rect_xywh(inner.pos.x, bid_top, inner.size.x, f32(bid_n) * row_h), col_widths[:], row_h)
	for i in 0 ..< bid_n {
		level := agg_bids[i]
		row := ui.table_next_row(&bid_tbl)

		bar_w := f32(level.sum / max_sum) * inner.size.x
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = row.pos, size = {bar_w, row_h - 1}},
			color = ui.COL_ORDERBOOK_GREEN,
		})

		price_str := fmt.bprintf(tbuf[:], "%.2f", level.price)
		ui.push_text(buf, ui.table_cell_text_pos(&bid_tbl, 0), price_str, ui.COL_GREEN, ui.FONT_SIZE_SM, .Mono)
		size_str := fmt.bprintf(tbuf[:], "%.4f", level.size)
		ui.push_text(buf, ui.table_cell_text_pos(&bid_tbl, 1), size_str, ui.with_alpha(ui.COL_WHITE, 0.85), ui.FONT_SIZE_SM, .Mono)
		sum_str := fmt.bprintf(tbuf[:], "%.3f", level.sum)
		ui.push_text(buf, ui.table_cell_text_pos(&bid_tbl, 2), sum_str, ui.with_alpha(ui.COL_WHITE, 0.6), ui.FONT_SIZE_SM, .Mono)
	}

	ui.push(buf, ui.Cmd_Clip_Pop{})
}

// --- Aggregation helpers (stack-only, no allocation) ---

@(private = "file")
aggregate_levels :: proc(
	prices, sizes: []f64,
	price_group: f64,
	out: ^[OB_AGG_CAP]Aggregated_Level,
	max_rows: int,
) -> int {
	n := 0
	cap := min(max_rows, OB_AGG_CAP)

	for i in 0 ..< len(prices) {
		if n >= cap do break
		bucket := math.floor(prices[i] / price_group) * price_group

		found := false
		for j in 0 ..< n {
			if out[j].price == bucket {
				out[j].size += sizes[i]
				found = true
				break
			}
		}
		if !found {
			out[n] = Aggregated_Level{price = bucket, size = sizes[i]}
			n += 1
		}
	}

	// Insertion sort by price ascending.
	for i in 1 ..< n {
		key := out[i]
		j := i - 1
		for j >= 0 && out[j].price > key.price {
			out[j + 1] = out[j]
			j -= 1
		}
		out[j + 1] = key
	}

	return n
}

@(private = "file")
cumsum :: proc(levels: ^[OB_AGG_CAP]Aggregated_Level, n: int) {
	running: f64 = 0
	for i in 0 ..< n {
		running += levels[i].size
		levels[i].sum = running
	}
}
