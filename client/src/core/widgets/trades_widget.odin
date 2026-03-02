package widgets

// Scrollable trade list — renders recent trades as a clipped, scrollable table.
// Uses ui primitives (panel, scroll_area, table) for layout.

import "core:fmt"
import "core:math"
import "mr:ports"
import "mr:services"
import "mr:ui"

TRADE_FILTER_OPTIONS :: [4]string{"All", ">0.1", ">1", ">10"}
TRADE_FILTER_THRESHOLDS :: [4]f64{0, 0.1, 1.0, 10.0}

Trades_Widget_Data :: struct {
	store:      ^services.Trades_Store,
	viewport:   ui.Rect,
	text:       ports.Text_Port,
	scroll_y:   ^f32,
	input:      ports.Input_State,
	filter_idx: ^int,
	pointer:    ui.Pointer_Input,
	now_ms:     i64,             // current wall-clock ms for elapsed time
}

trades_widget :: proc(buf: ^ui.Command_Buffer, data: Trades_Widget_Data) {
	vp := data.viewport
	row_h := data.text.line_height(ui.FONT_SIZE_BASE) + 2

	// Panel with header controls.
	inner, ctrl_rect := ui.panel_v2(buf, vp, ui.Panel_V2_Config{
		title        = "Trades",
		title_height = data.text.line_height(ui.FONT_SIZE_BASE),
		bg_color     = ui.COL_PANEL_BG,
		pad          = 4,
	}, data.text.measure, ui.FONT_SIZE_BASE)

	// Trade count badge (left of filter control).
	if data.store != nil && data.store.count > 0 {
		tc_buf: [8]u8
		tc_str := fmt.bprintf(tc_buf[:], "%d", data.store.count)
		tc_w := data.text.measure(ui.FONT_SIZE_XS, tc_str).x + 8
		tc_h := f32(12)
		tc_x := ctrl_rect.pos.x
		tc_y := ctrl_rect.pos.y + (ctrl_rect.size.y - tc_h) * 0.5
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {tc_x, tc_y}, size = {tc_w, tc_h}},
			color = ui.with_alpha(ui.COL_WHITE, 0.08),
		})
		ui.push_text(buf, {tc_x + 4, tc_y + tc_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			tc_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}

	// Filter segmented control in header.
	if data.filter_idx != nil && ctrl_rect.size.x > 60 {
		opts := TRADE_FILTER_OPTIONS
		seg_w := min(ctrl_rect.size.x, f32(len(opts)) * 30)
		seg_rect := ui.Rect{
			pos  = {ui.rect_right(ctrl_rect) - seg_w, ctrl_rect.pos.y + (ctrl_rect.size.y - 14) * 0.5},
			size = {seg_w, 14},
		}
		res := ui.segmented_control(buf, seg_rect, opts[:], data.filter_idx^,
			data.pointer, data.text.measure, ui.FONT_SIZE_XS, .Mono)
		if res.changed {
			data.filter_idx^ = res.index
		}
	}

	// Determine filter threshold.
	filter_threshold := f64(0)
	thresholds := TRADE_FILTER_THRESHOLDS
	if data.filter_idx != nil && data.filter_idx^ >= 0 && data.filter_idx^ < len(thresholds) {
		filter_threshold = thresholds[data.filter_idx^]
	}

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

	// Body area (below header), with room for volume summary bar at bottom.
	summary_h := f32(18)
	body := inner
	body.pos.y  = hdr_tbl.y_cursor
	body.size.y = inner.size.y - (hdr_tbl.y_cursor - inner.pos.y) - summary_h

	// Scrollable area — count qualifying trades when filter is active.
	content_count := data.store.count
	if filter_threshold > 0 {
		content_count = 0
		for qi in 0 ..< data.store.count {
			if services.get_trade(data.store, qi).qty >= filter_threshold do content_count += 1
		}
	}
	content_h := f32(content_count) * row_h
	scroll_state := ui.Scroll_State{offset_y = data.scroll_y^}
	visible, scroll_offset := ui.scroll_area_begin(buf, body, content_h, &scroll_state,
		data.input.mouse.pos, data.input.mouse.scroll.y, row_h)
	data.scroll_y^ = scroll_state.offset_y

	// Find starting store index — skip filtered trades to reach the right offset.
	visible_rows := int(visible.size.y / row_h) + 2
	skip_count := int(scroll_offset / row_h)
	start_store_idx := 0
	skipped := 0
	for start_store_idx < data.store.count && skipped < skip_count {
		if filter_threshold <= 0 || services.get_trade(data.store, start_store_idx).qty >= filter_threshold {
			skipped += 1
		}
		start_store_idx += 1
	}

	// Compute average trade qty for proportional bars.
	avg_qty := f64(1)
	if data.store.count > 0 {
		qty_sum := f64(0)
		for qi in 0 ..< data.store.count {
			qty_sum += services.get_trade(data.store, qi).qty
		}
		avg_qty = qty_sum / f64(data.store.count)
		if avg_qty <= 0 do avg_qty = 1
	}

	now_unix := data.now_ms / 1000 // convert ms to seconds for elapsed time

	tbuf: [64]u8
	tbl := ui.table_begin(ui.Rect{
		pos  = {visible.pos.x, visible.pos.y - math.mod(scroll_offset, row_h)},
		size = visible.size,
	}, col_widths[:], row_h)

	rendered := 0
	store_idx := start_store_idx
	for rendered < visible_rows && store_idx < data.store.count {
		trade := services.get_trade(data.store, store_idx)
		store_idx += 1
		if filter_threshold > 0 && trade.qty < filter_threshold do continue
		ui.table_next_row(&tbl)
		rendered += 1

		side_color := trade.side == .Buy ? ui.COL_GREEN : ui.COL_RED

		// Scale text alpha by relative trade size: small trades dim, large trades bright.
		size_ratio := clamp(f32(trade.qty / avg_qty), 0.3, 8.0)
		text_alpha := f32(0.50) + f32(0.35) * min(size_ratio / 3.0, 1.0)
		text_color := ui.with_alpha(ui.COL_WHITE, text_alpha)

		// Whale detection: trades >10x average get yellow accent + wide bar.
		is_whale := trade.qty >= avg_qty * 10

		// Large trade background tint (>5x avg): subtle row highlight.
		if size_ratio > 5 {
			tint_alpha := f32(0.04) * min((size_ratio - 5) / 10, 1.0)
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {tbl.x_origin, tbl.y_cursor - row_h}, size = {visible.size.x, row_h}},
				color = ui.with_alpha(side_color, tint_alpha),
			})
		}

		// Flash highlight for recent trades (first 3 visible rows).
		row_idx := rendered - 1
		if row_idx < 3 {
			flash_alpha := f32(0.12) * (1.0 - f32(row_idx) * 0.35)
			flash_color := trade.side == .Buy ? ui.COL_GREEN : ui.COL_RED
			flash_rect := ui.Rect{
				pos  = {tbl.x_origin, tbl.y_cursor - row_h},
				size = {visible.size.x, row_h},
			}
			ui.push(buf, ui.Cmd_Rect_Filled{rect = flash_rect, color = ui.with_alpha(flash_color, flash_alpha)})
		}

		// Proportional accent bar: width scales with trade size relative to average.
		// Whales get yellow side color + extra-wide bar.
		whale_side := is_whale ? ui.COL_YELLOW_ACCENT : side_color
		bar_ratio := clamp(f32(trade.qty / avg_qty), 0, 5) / 5
		bar_w := is_whale ? visible.size.x * 0.3 : max(bar_ratio * visible.size.x * 0.15, 2)
		accent_rect := ui.Rect{
			pos  = {tbl.x_origin, tbl.y_cursor - row_h},
			size = {bar_w, row_h},
		}
		ui.push(buf, ui.Cmd_Rect_Filled{rect = accent_rect, color = ui.with_alpha(whale_side, 0.25)})

		// Time: show elapsed ("3s", "2m") for trades within 5 min, else absolute.
		elapsed := now_unix - trade.unix
		time_str: string
		if now_unix > 0 && elapsed >= 0 && elapsed < 300 {
			if elapsed < 60 {
				time_str = fmt.bprintf(tbuf[:], "%ds ago", elapsed)
			} else {
				time_str = fmt.bprintf(tbuf[:], "%dm ago", elapsed / 60)
			}
		} else {
			hours   := (trade.unix / 3600) % 24
			minutes := (trade.unix / 60) % 60
			seconds := trade.unix % 60
			time_str = fmt.bprintf(tbuf[:], "%02d:%02d:%02d", hours, minutes, seconds)
		}
		ui.push_text(buf, ui.table_cell_text_pos(&tbl, 0), time_str, text_color, ui.FONT_SIZE_BASE, .Mono)

		// Side.
		side_str := trade.side == .Buy ? "BUY" : "SELL"
		ui.push_text(buf, ui.table_cell_text_pos(&tbl, 1), side_str, side_color, ui.FONT_SIZE_BASE, .Mono)

		// Price.
		price_str := ui.format_price(tbuf[:], trade.price, ui.auto_price_decimals(trade.price))
		ui.push_text(buf, ui.table_cell_text_pos(&tbl, 2), price_str, side_color, ui.FONT_SIZE_BASE, .Mono)

		// Qty.
		qty_str := fmt.bprintf(tbuf[:], "%.4f", trade.qty)
		ui.push_text(buf, ui.table_cell_text_pos(&tbl, 3), qty_str, text_color, ui.FONT_SIZE_BASE, .Mono)
	}

	ui.scroll_area_end(buf)

	// --- Volume summary bar at bottom ---
	if data.store != nil && data.store.count > 0 {
		buy_vol := f64(0)
		sell_vol := f64(0)
		for qi in 0 ..< data.store.count {
			t := services.get_trade(data.store, qi)
			if t.side == .Buy {
				buy_vol += t.qty
			} else {
				sell_vol += t.qty
			}
		}
		total := buy_vol + sell_vol
		if total > 0 {
			sum_y := ui.rect_bottom(body) + 2
			bar_w := inner.size.x
			bar_h := f32(6)
			buy_pct := f32(buy_vol / total)
			// Buy bar (green, left).
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {inner.pos.x, sum_y}, size = {bar_w * buy_pct, bar_h}},
				color = ui.with_alpha(ui.COL_GREEN, 0.6),
			})
			// Sell bar (red, right).
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {inner.pos.x + bar_w * buy_pct, sum_y}, size = {bar_w * (1 - buy_pct), bar_h}},
				color = ui.with_alpha(ui.COL_RED, 0.6),
			})
			// Labels.
			label_y := sum_y + bar_h + 1 + ui.FONT_SIZE_XS * 0.35
			buy_str := fmt.bprintf(tbuf[:], "B:%.2f", buy_vol)
			ui.push_text(buf, {inner.pos.x, label_y}, buy_str, ui.COL_GREEN, ui.FONT_SIZE_XS, .Mono)
			sell_str := fmt.bprintf(tbuf[:], "S:%.2f", sell_vol)
			sell_w := data.text.measure(ui.FONT_SIZE_XS, sell_str).x
			ui.push_text(buf, {ui.rect_right(inner) - sell_w, label_y}, sell_str, ui.COL_RED, ui.FONT_SIZE_XS, .Mono)
		}
	}
}
