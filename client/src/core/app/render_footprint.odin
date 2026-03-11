package app

// S157: Footprint Chart Renderer — per-candle volume distribution grid.
// Renders the most recent N candle windows as columns, with price levels as rows.
// Buy volume shown green (right side), sell volume shown red (left side).
// Delta at Price visualization with intensity proportional to volume.

import "core:fmt"
import "core:math"
import "mr:services"
import "mr:ui"

// S157: Entry point for footprint widget contract.
render_footprint_widget :: proc(
	cmd_buf: ^ui.Command_Buffer,
	store: ^services.Footprint_Store,
	vp: ui.Rect,
) {
	if cmd_buf == nil do return
	if vp.size.x <= 0 || vp.size.y <= 0 do return

	ui.push(cmd_buf, ui.Cmd_Rect_Filled{rect = vp, color = ui.with_alpha(ui.COL_SURFACE_1, 0.92)})

	if store == nil || store.count == 0 {
		ui.push_text(cmd_buf,
			{vp.pos.x + 6, vp.pos.y + 14},
			"Footprint: waiting for trades",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}

	// Collect visible candle windows (newest first, up to viewport capacity).
	MAX_VISIBLE_CANDLES :: 40
	visible_count := min(store.count, MAX_VISIBLE_CANDLES)

	// Header: candle count + total trade summary.
	hdr_buf: [64]u8
	hdr_label := fmt.bprintf(hdr_buf[:], "Footprint  %d candles", visible_count)
	ui.push_text(cmd_buf,
		{vp.pos.x + 6, vp.pos.y + 12},
		hdr_label, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

	// Grid area below header.
	HEADER_H :: f32(18)
	grid_area := ui.Rect{
		pos  = {vp.pos.x + 2, vp.pos.y + HEADER_H},
		size = {vp.size.x - 4, vp.size.y - HEADER_H - 2},
	}
	if grid_area.size.y <= 10 || grid_area.size.x <= 10 do return

	// Scan all visible entries to find global price range + max volume.
	price_min := math.F64_MAX
	price_max := -math.F64_MAX
	max_vol := f64(0)

	for vi in 0 ..< visible_count {
		raw_idx := (store.head - 1 - vi + services.FOOTPRINT_CANDLE_CAP) % services.FOOTPRINT_CANDLE_CAP
		entry := &store.entries[raw_idx]
		for li in 0 ..< entry.level_count {
			lv := &entry.levels[li]
			if lv.price < price_min do price_min = lv.price
			if lv.price > price_max do price_max = lv.price
			total := lv.buy_vol + lv.sell_vol
			if total > max_vol do max_vol = total
		}
	}

	if price_min >= price_max || max_vol <= 0 {
		// Single price level — show simple summary.
		ui.push_text(cmd_buf,
			{vp.pos.x + 6, vp.pos.y + 30},
			"Accumulating...",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}

	price_range := price_max - price_min

	// Column width per candle window.
	col_w := grid_area.size.x / f32(visible_count)
	col_w = math.min(col_w, f32(80)) // cap column width

	// Render columns (newest on right).
	for vi in 0 ..< visible_count {
		raw_idx := (store.head - 1 - vi + services.FOOTPRINT_CANDLE_CAP) % services.FOOTPRINT_CANDLE_CAP
		entry := &store.entries[raw_idx]
		col_idx := visible_count - 1 - vi // newest at right
		col_x := grid_area.pos.x + f32(col_idx) * col_w

		// Column separator.
		if col_idx > 0 {
			ui.push(cmd_buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {col_x, grid_area.pos.y}, size = {1, grid_area.size.y}},
				color = ui.with_alpha(ui.COL_BORDER_SUBTLE, 0.3),
			})
		}

		// Render each price level as a horizontal bar.
		for li in 0 ..< entry.level_count {
			lv := &entry.levels[li]
			// Y position based on price fraction (higher price = higher on screen).
			frac := f32((lv.price - price_min) / price_range)
			y := grid_area.pos.y + grid_area.size.y * (1 - frac)

			total := lv.buy_vol + lv.sell_vol
			intensity := f32(total / max_vol)
			bar_h := math.max(grid_area.size.y / f32(max(int(price_range / entry.price_group), 4)), f32(2))
			bar_h = math.min(bar_h, f32(16))

			half_w := (col_w - 2) * 0.5
			center_x := col_x + col_w * 0.5

			// Buy bar (green, right of center).
			buy_w := half_w * f32(lv.buy_vol / max_vol)
			if buy_w > 0.5 {
				alpha := f32(0.3) + intensity * f32(0.6)
				ui.push(cmd_buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {center_x, y - bar_h * 0.5}, size = {buy_w, math.max(bar_h - 1, 1)}},
					color = ui.with_alpha(ui.COL_ORDERBOOK_GREEN, alpha),
				})
			}

			// Sell bar (red, left of center).
			sell_w := half_w * f32(lv.sell_vol / max_vol)
			if sell_w > 0.5 {
				alpha := f32(0.3) + intensity * f32(0.6)
				ui.push(cmd_buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {center_x - sell_w, y - bar_h * 0.5}, size = {sell_w, math.max(bar_h - 1, 1)}},
					color = ui.with_alpha(ui.COL_ORDERBOOK_RED, alpha),
				})
			}
		}
	}

	// Delta summary for newest candle (bottom-right badge).
	newest_idx := (store.head - 1 + services.FOOTPRINT_CANDLE_CAP) % services.FOOTPRINT_CANDLE_CAP
	newest := &store.entries[newest_idx]
	total_buy, total_sell := f64(0), f64(0)
	for li in 0 ..< newest.level_count {
		total_buy += newest.levels[li].buy_vol
		total_sell += newest.levels[li].sell_vol
	}
	delta := total_buy - total_sell
	delta_buf: [32]u8
	delta_label := fmt.bprintf(delta_buf[:], "D:%.1f", delta)
	delta_color := delta >= 0 ? ui.COL_ORDERBOOK_GREEN : ui.COL_ORDERBOOK_RED
	badge_x := ui.rect_right(grid_area) - 60
	badge_y := ui.rect_bottom(grid_area) - 4
	ui.push_text(cmd_buf, {badge_x, badge_y}, delta_label, delta_color, ui.FONT_SIZE_XS, .Mono)
}
