package widgets

// Scrollable trade list — renders recent trades as a clipped, scrollable table.
// Uses ui primitives (panel, scroll_area, table) for layout.

import "core:fmt"
import "core:math"
import "mr:ports"
import "mr:services"
import "mr:ui"

Trades_Widget_Data :: struct {
	store:    ^services.Trades_Store,
	viewport: ui.Rect,
	text:     ports.Text_Port,
	scroll_y: ^f32, // persistent scroll offset (mutated by input)
	input:    ports.Input_State,
}

trades_widget :: proc(buf: ^ui.Command_Buffer, data: Trades_Widget_Data) {
	vp := data.viewport
	row_h := data.text.line_height(ui.FONT_SIZE_BASE) + 2

	// Panel with title.
	inner := ui.panel(buf, vp, ui.Panel_Config{
		title        = "Trades",
		title_height = data.text.line_height(ui.FONT_SIZE_BASE),
		bg_color     = ui.COL_PANEL_BG,
		pad          = 4,
	}, data.text.measure, ui.FONT_SIZE_BASE)

	// Column widths.
	col_time_w  := data.text.measure(ui.FONT_SIZE_BASE, "00:00:00").x + 12
	col_side_w  := data.text.measure(ui.FONT_SIZE_BASE, "SELL").x + 12
	col_price_w := data.text.measure(ui.FONT_SIZE_BASE, "00000.00").x + 12
	col_qty_w   := inner.size.x - col_time_w - col_side_w - col_price_w
	col_widths  := [?]f32{col_time_w, col_side_w, col_price_w, col_qty_w}

	// Header row.
	hdr_tbl := ui.table_begin(inner, col_widths[:], row_h)
	ui.table_next_row(&hdr_tbl)
	hdr_color := ui.with_alpha(ui.COL_WHITE, 0.5)

	headers := [?]string{"Time", "Side", "Price", "Qty"}
	for i in 0 ..< 4 {
		pos := ui.table_cell_text_pos(&hdr_tbl, i)
		ui.push_text(buf, pos, headers[i], hdr_color, ui.FONT_SIZE_BASE, .Mono)
	}

	// Body area (below header).
	body := inner
	body.pos.y  = hdr_tbl.y_cursor
	body.size.y = inner.size.y - (hdr_tbl.y_cursor - inner.pos.y)

	// Scrollable area.
	content_h := f32(data.store.count) * row_h
	scroll_state := ui.Scroll_State{offset_y = data.scroll_y^}
	visible, scroll_offset := ui.scroll_area_begin(buf, body, content_h, &scroll_state,
		data.input.mouse.pos, data.input.mouse.scroll.y, row_h)
	data.scroll_y^ = scroll_state.offset_y

	// Visible rows.
	start_idx := int(scroll_offset / row_h)
	visible_rows := int(visible.size.y / row_h) + 2

	tbuf: [64]u8
	tbl := ui.table_begin(ui.Rect{
		pos  = {visible.pos.x, visible.pos.y - math.mod(scroll_offset, row_h)},
		size = visible.size,
	}, col_widths[:], row_h)

	for i in 0 ..< visible_rows {
		idx := start_idx + i
		if idx >= data.store.count do break

		trade := services.get_trade(data.store, idx)
		ui.table_next_row(&tbl)

		side_color := trade.side == .Buy ? ui.COL_GREEN : ui.COL_RED
		text_color := ui.with_alpha(ui.COL_WHITE, 0.85)

		// Time.
		hours   := (trade.unix / 3600) % 24
		minutes := (trade.unix / 60) % 60
		seconds := trade.unix % 60
		time_str := fmt.bprintf(tbuf[:], "%02d:%02d:%02d", hours, minutes, seconds)
		ui.push_text(buf, ui.table_cell_text_pos(&tbl, 0), time_str, text_color, ui.FONT_SIZE_BASE, .Mono)

		// Side.
		side_str := trade.side == .Buy ? "BUY" : "SELL"
		ui.push_text(buf, ui.table_cell_text_pos(&tbl, 1), side_str, side_color, ui.FONT_SIZE_BASE, .Mono)

		// Price.
		price_str := fmt.bprintf(tbuf[:], "%.2f", trade.price)
		ui.push_text(buf, ui.table_cell_text_pos(&tbl, 2), price_str, side_color, ui.FONT_SIZE_BASE, .Mono)

		// Qty.
		qty_str := fmt.bprintf(tbuf[:], "%.4f", trade.qty)
		ui.push_text(buf, ui.table_cell_text_pos(&tbl, 3), qty_str, text_color, ui.FONT_SIZE_BASE, .Mono)
	}

	ui.scroll_area_end(buf)
}
