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
	// Panel v2 controls.
	group_options:  []string,     // formatted grouping labels (e.g. "0.1", "1", "10")
	group_idx:      ^int,         // pointer to selected grouping index in app state
	pointer:        ui.Pointer_Input,
}

orderbook_widget :: proc(buf: ^ui.Command_Buffer, data: Orderbook_Widget_Data) {
	store := data.store
	if store == nil || (store.ask_count == 0 && store.bid_count == 0) {
		vp := data.viewport
		inner, _ := ui.panel_v2(buf, vp, ui.Panel_V2_Config{
			title        = "Orderbook",
			title_height = data.text.line_height(ui.FONT_SIZE_SM),
			bg_color     = ui.COL_PANEL_BG,
			pad          = 4,
		}, data.text.measure, ui.FONT_SIZE_SM)
		msg :: "Waiting for orderbook..."
		ui.push_text(buf,
			{inner.pos.x + inner.size.x * 0.5 - 80, inner.pos.y + inner.size.y * 0.5},
			msg, ui.with_alpha(ui.COL_WHITE, 0.3), ui.FONT_SIZE_SM)
		return
	}

	row_h := data.text.line_height(ui.FONT_SIZE_SM) + 2
	price_group := data.price_group > 0 ? data.price_group : 10.0
	max_rows := data.max_rows > 0 ? data.max_rows : OB_MAX_ROWS

	// Panel with header controls.
	inner, ctrl_rect := ui.panel_v2(buf, data.viewport, ui.Panel_V2_Config{
		title        = "Orderbook",
		title_height = data.text.line_height(ui.FONT_SIZE_SM),
		bg_color     = ui.COL_PANEL_BG,
		pad          = 4,
	}, data.text.measure, ui.FONT_SIZE_SM)

	// Grouping segmented control in header.
	if len(data.group_options) > 0 && data.group_idx != nil && ctrl_rect.size.x > 40 {
		seg_w := min(ctrl_rect.size.x, f32(len(data.group_options)) * 32)
		seg_rect := ui.Rect{
			pos  = {ui.rect_right(ctrl_rect) - seg_w, ctrl_rect.pos.y + (ctrl_rect.size.y - 14) * 0.5},
			size = {seg_w, 14},
		}
		res := ui.segmented_control(buf, seg_rect, data.group_options, data.group_idx^,
			data.pointer, data.text.measure, ui.FONT_SIZE_XS, .Mono)
		if res.changed {
			data.group_idx^ = res.index
		}
	}

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

	// Auto-detect price precision from the grouping.
	price_decimals := ob_auto_decimals(price_group)

	// --- Ask rows (bottom-to-top: best ask nearest center) ---
	ask_tbl := ui.table_begin(ui.rect_xywh(inner.pos.x, body_y, inner.size.x, f32(ask_n) * row_h), col_widths[:], row_h)
	for i in 0 ..< ask_n {
		row_idx := ask_n - 1 - i
		level := agg_asks[row_idx]
		row := ui.table_next_row(&ask_tbl)

		// Fill bar: gradient intensity by cumulative depth.
		bar_w := f32(level.sum / max_sum) * inner.size.x
		bar_alpha := f32(0.08) + f32(0.15) * f32(level.sum / max_sum)
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = row.pos, size = {bar_w, row_h - 1}},
			color = ui.with_alpha(ui.COL_RED, bar_alpha),
		})

		// Best ask highlight (nearest center = last rendered ask row).
		if row_idx == 0 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {row.pos.x, row.pos.y + row_h - 2}, size = {inner.size.x, 1}},
				color = ui.with_alpha(ui.COL_RED, 0.3),
			})
		}

		price_str := ob_format_price(tbuf[:], level.price, price_decimals)
		ui.push_text(buf, ui.table_cell_text_pos(&ask_tbl, 0), price_str, ui.COL_RED, ui.FONT_SIZE_SM, .Mono)
		size_str := fmt.bprintf(tbuf[:], "%.4f", level.size)
		ui.push_text(buf, ui.table_cell_text_pos(&ask_tbl, 1), size_str, ui.with_alpha(ui.COL_WHITE, 0.85), ui.FONT_SIZE_SM, .Mono)
		sum_str := fmt.bprintf(tbuf[:], "%.3f", level.sum)
		ui.push_text(buf, ui.table_cell_text_pos(&ask_tbl, 2), sum_str, ui.with_alpha(ui.COL_WHITE, 0.6), ui.FONT_SIZE_SM, .Mono)
	}

	// --- Center: last_price + spread + imbalance ---
	center_h := data.text.line_height(ui.FONT_SIZE_LG) + 8
	center_y := ask_tbl.y_cursor
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {inner.pos.x, center_y}, size = {inner.size.x, center_h}},
		color = ui.with_alpha(ui.COL_PRIMARY, 0.5),
	})

	price_str := ob_format_price(tbuf[:], store.last_price, price_decimals)
	price_text_y := center_y + data.text.line_height(ui.FONT_SIZE_LG) + 2
	ui.push_text(buf, {inner.pos.x + 4, price_text_y}, price_str, ui.COL_WHITE, ui.FONT_SIZE_LG, .Mono)

	// Best bid/ask labels below the center price.
	ba := services.best_ask(store)
	bb := services.best_bid(store)
	if ba > 0 && bb > 0 {
		ba_str := ob_format_price(tbuf[:], ba, price_decimals)
		ba_label_w := data.text.measure(ui.FONT_SIZE_XS, ba_str).x
		ask_label_y := center_y + center_h - data.text.line_height(ui.FONT_SIZE_XS) - 5
		ui.push_text(buf, {ui.rect_right(inner) - ba_label_w - 4, ask_label_y}, ba_str,
			ui.with_alpha(ui.COL_RED, 0.65), ui.FONT_SIZE_XS, .Mono)
		bb_str := ob_format_price(tbuf[:], bb, price_decimals)
		ui.push_text(buf, {inner.pos.x + 4, ask_label_y}, bb_str,
			ui.with_alpha(ui.COL_GREEN, 0.65), ui.FONT_SIZE_XS, .Mono)
	}

	sp := services.spread(store)
	mid := services.mid_price(store)
	spread_pct := mid > 0 ? sp / mid * 100.0 : 0
	spread_str := fmt.bprintf(tbuf[:], "%.2f (%.3f%%)", sp, spread_pct)
	spread_w := data.text.measure(ui.FONT_SIZE_SM, spread_str).x
	spread_color := spread_pct < 0.05 ? ui.COL_GREEN : (spread_pct < 0.2 ? ui.COL_YELLOW_ACCENT : ui.COL_RED)
	ui.push_text(buf, {ui.rect_right(inner) - spread_w - 4, price_text_y}, spread_str,
		ui.with_alpha(spread_color, 0.8), ui.FONT_SIZE_SM, .Mono)

	// Imbalance bar: bid vs ask volume ratio in center row.
	bid_total := bid_n > 0 ? agg_bids[bid_n - 1].sum : f64(0)
	ask_total := ask_n > 0 ? agg_asks[ask_n - 1].sum : f64(0)
	imb_total := bid_total + ask_total
	if imb_total > 0 {
		imb_h := f32(3)
		imb_y := center_y + center_h - imb_h - 1
		bid_pct := f32(bid_total / imb_total)
		bid_bar_w := inner.size.x * bid_pct
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {inner.pos.x, imb_y}, size = {bid_bar_w, imb_h}},
			color = ui.with_alpha(ui.COL_GREEN, 0.6),
		})
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {inner.pos.x + bid_bar_w, imb_y}, size = {inner.size.x - bid_bar_w, imb_h}},
			color = ui.with_alpha(ui.COL_RED, 0.6),
		})
	}

	// --- Bid rows (top-to-bottom: best bid nearest center) ---
	bid_top := center_y + center_h
	bid_tbl := ui.table_begin(ui.rect_xywh(inner.pos.x, bid_top, inner.size.x, f32(bid_n) * row_h), col_widths[:], row_h)
	for i in 0 ..< bid_n {
		level := agg_bids[i]
		row := ui.table_next_row(&bid_tbl)

		// Fill bar: gradient intensity by cumulative depth.
		bar_w := f32(level.sum / max_sum) * inner.size.x
		bar_alpha := f32(0.08) + f32(0.15) * f32(level.sum / max_sum)
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = row.pos, size = {bar_w, row_h - 1}},
			color = ui.with_alpha(ui.COL_GREEN, bar_alpha),
		})

		// Best bid highlight (nearest center = first bid row).
		if i == 0 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = row.pos, size = {inner.size.x, 1}},
				color = ui.with_alpha(ui.COL_GREEN, 0.3),
			})
		}

		price_str := ob_format_price(tbuf[:], level.price, price_decimals)
		ui.push_text(buf, ui.table_cell_text_pos(&bid_tbl, 0), price_str, ui.COL_GREEN, ui.FONT_SIZE_SM, .Mono)
		size_str := fmt.bprintf(tbuf[:], "%.4f", level.size)
		ui.push_text(buf, ui.table_cell_text_pos(&bid_tbl, 1), size_str, ui.with_alpha(ui.COL_WHITE, 0.85), ui.FONT_SIZE_SM, .Mono)
		sum_str := fmt.bprintf(tbuf[:], "%.3f", level.sum)
		ui.push_text(buf, ui.table_cell_text_pos(&bid_tbl, 2), sum_str, ui.with_alpha(ui.COL_WHITE, 0.6), ui.FONT_SIZE_SM, .Mono)
	}

	// --- Mini depth chart below bids ---
	depth_h := f32(32)
	depth_top := bid_tbl.y_cursor + 2
	if depth_top + depth_h < ui.rect_bottom(data.viewport) && ask_n > 0 && bid_n > 0 {
		depth_w := inner.size.x
		// Draw cumulative ask curve (left-to-right, red).
		for i in 0 ..< ask_n {
			t := f32(agg_asks[i].sum / max_sum)
			x := inner.pos.x + f32(i) / f32(max(ask_n - 1, 1)) * depth_w
			h := t * depth_h
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x, depth_top + depth_h - h}, size = {depth_w / f32(ask_n), h}},
				color = ui.with_alpha(ui.COL_RED, 0.15),
			})
		}
		// Draw cumulative bid curve (left-to-right, green).
		for i in 0 ..< bid_n {
			t := f32(agg_bids[i].sum / max_sum)
			x := inner.pos.x + f32(i) / f32(max(bid_n - 1, 1)) * depth_w
			h := t * depth_h
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x, depth_top + depth_h - h}, size = {depth_w / f32(bid_n), h}},
				color = ui.with_alpha(ui.COL_GREEN, 0.15),
			})
		}
	}

	ui.push(buf, ui.Cmd_Clip_Pop{})
}

// Auto-detect decimal places from price grouping.
@(private = "file")
ob_auto_decimals :: proc(price_group: f64) -> int {
	if price_group >= 100 do return 0
	if price_group >= 1 do return 1
	if price_group >= 0.1 do return 2
	if price_group >= 0.01 do return 3
	return 4
}

// Format price with the appropriate number of decimals.
@(private = "file")
ob_format_price :: proc(buf: []u8, price: f64, decimals: int) -> string {
	switch decimals {
	case 0:  return fmt.bprintf(buf, "%.0f", price)
	case 1:  return fmt.bprintf(buf, "%.1f", price)
	case 3:  return fmt.bprintf(buf, "%.3f", price)
	case 4:  return fmt.bprintf(buf, "%.4f", price)
	case:    return fmt.bprintf(buf, "%.2f", price)
	}
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
