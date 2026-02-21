package widgets

// VPVR (Volume Profile Visible Range) widget.
// Horizontal bars showing volume distribution by price level.
// Green = buy volume, Red = sell volume. POC highlighted.
// Pure RCL: uses Cmd_Rect_Filled + Cmd_Text + Cmd_Line only.

import "core:fmt"
import "core:math"
import "mr:ports"
import "mr:services"
import "mr:ui"

VALUE_AREA_PCT :: 0.70

VPVR_Widget_Data :: struct {
	store:    ^services.VPVR_Store,
	viewport: ui.Rect,
	text:     ports.Text_Port,
	input:    ports.Input_State,
}

vpvr_widget :: proc(buf: ^ui.Command_Buffer, data: VPVR_Widget_Data) {
	store := data.store
	if store == nil || store.count == 0 do return

	// Panel.
	inner := ui.panel(buf, data.viewport, ui.Panel_Config{
		title        = "VPVR",
		title_height = data.text.line_height(ui.FONT_SIZE_SM),
		bg_color     = ui.COL_PANEL_BG,
		pad          = 4,
	}, data.text.measure, ui.FONT_SIZE_SM)

	ui.push(buf, ui.Cmd_Clip_Push{rect = data.viewport})

	// Reserve space for price labels on the left.
	label_w := data.text.measure(ui.FONT_SIZE_SM, "00000.0").x + 8

	bar_area := ui.Rect{
		pos  = {inner.pos.x + label_w, inner.pos.y},
		size = {inner.size.x - label_w, inner.size.y},
	}

	max_vol := store.max_volume
	if max_vol <= 0 do max_vol = 1

	row_h := bar_area.size.y / f32(store.count)
	bar_max_w := bar_area.size.x * 0.85

	// Compute value area.
	vah_idx, val_idx := services.compute_value_area(store, VALUE_AREA_PCT)

	tbuf: [32]u8

	for i in 0 ..< store.count {
		bucket := services.get_vpvr_bucket(store, i)

		// Price levels: lowest at bottom, highest at top.
		row := store.count - 1 - i
		y := bar_area.pos.y + f32(row) * row_h

		total := bucket.buy_volume + bucket.sell_volume
		total_w := f32(total / max_vol) * bar_max_w

		// Buy bar (green, from right edge leftward).
		buy_w := f32(bucket.buy_volume / max_vol) * bar_max_w
		sell_w := total_w - buy_w

		// Draw sell bar first (leftmost), then buy bar.
		bar_x := bar_area.pos.x + bar_area.size.x - total_w

		if sell_w > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {bar_x, y}, size = {sell_w, math.max(row_h - 1, 1)}},
				color = ui.COL_ORDERBOOK_RED,
			})
		}
		if buy_w > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {bar_x + sell_w, y}, size = {buy_w, math.max(row_h - 1, 1)}},
				color = ui.COL_ORDERBOOK_GREEN,
			})
		}

		// POC highlight.
		if i == store.poc_index {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {bar_x - 2, y}, size = {total_w + 4, math.max(row_h, 2)}},
				color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.3),
			})
		}

		// Price label (every N rows to avoid overlap).
		label_step := max(store.count / 12, 1)
		if i % label_step == 0 {
			price_str := fmt.bprintf(tbuf[:], "%.1f", bucket.price)
			text_y := y + row_h * 0.5 + data.text.line_height(ui.FONT_SIZE_SM) * 0.4
			ui.push_text(buf, {inner.pos.x, text_y}, price_str,
				ui.with_alpha(ui.COL_WHITE, 0.6), ui.FONT_SIZE_SM, .Mono)
		}
	}

	// VAH line (Value Area High).
	if vah_idx >= 0 && vah_idx < store.count {
		vah_row := store.count - 1 - vah_idx
		vah_y := bar_area.pos.y + f32(vah_row) * row_h
		ui.push(buf, ui.Cmd_Line{
			from      = {bar_area.pos.x, vah_y},
			to        = {ui.rect_right(bar_area), vah_y},
			color     = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.5),
			thickness = 1,
		})
	}

	// VAL line (Value Area Low).
	if val_idx >= 0 && val_idx < store.count {
		val_row := store.count - 1 - val_idx
		val_y := bar_area.pos.y + f32(val_row) * row_h + row_h
		ui.push(buf, ui.Cmd_Line{
			from      = {bar_area.pos.x, val_y},
			to        = {ui.rect_right(bar_area), val_y},
			color     = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.5),
			thickness = 1,
		})
	}

	// POC label.
	poc_bucket := services.get_vpvr_bucket(store, store.poc_index)
	poc_str := fmt.bprintf(tbuf[:], "POC %.1f", poc_bucket.price)
	poc_row := store.count - 1 - store.poc_index
	poc_y := bar_area.pos.y + f32(poc_row) * row_h + row_h * 0.5 + data.text.line_height(ui.FONT_SIZE_SM) * 0.4
	poc_label_w := data.text.measure(ui.FONT_SIZE_SM, poc_str).x
	ui.push_text(buf, {ui.rect_right(bar_area) - poc_label_w - 4, poc_y}, poc_str,
		ui.COL_YELLOW_ACCENT, ui.FONT_SIZE_SM, .Mono)

	ui.push(buf, ui.Cmd_Clip_Pop{})
}
