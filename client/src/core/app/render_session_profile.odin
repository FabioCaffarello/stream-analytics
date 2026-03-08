package app

// S49/S61: Session & Profile Engine — widget render procs.
// S61: Renders from pre-resolved Cell_View_Model. Widget procs receive
// ^Cmd_Buffer + resolved store pointers — zero App_State coupling.

import "core:fmt"
import "core:math"
import "mr:services"
import "mr:ui"

// S61: Entry point from Cell_View_Model dispatch (no App_State dependency).
render_session_profile_cell_vm :: proc(cmd_buf: ^ui.Command_Buffer, vm: Cell_View_Model, cell_vp: ui.Rect) {
	if cmd_buf == nil do return
	if cell_vp.size.x <= 0 || cell_vp.size.y <= 0 do return

	ui.push(cmd_buf, ui.Cmd_Rect_Filled{rect = cell_vp, color = ui.with_alpha(ui.COL_SURFACE_1, 0.92)})

	#partial switch vm.widget_kind {
	case .Session_VPVR:
		render_session_vpvr(cmd_buf, vm.stores.session_vpvr, cell_vp)
	case .TPO:
		render_tpo_profile(cmd_buf, vm.stores.tpo, cell_vp)
	case:
	}
}

// --- Session VPVR ---

@(private = "file")
render_session_vpvr :: proc(cmd_buf: ^ui.Command_Buffer, store: ^services.Session_VPVR_Store, vp: ui.Rect) {
	if store == nil || store.count == 0 {
		ui.push_text(cmd_buf,
			{vp.pos.x + 6, vp.pos.y + 14},
			"Session VPVR: no data",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}

	// Session label header.
	label := services.get_session_vpvr_label(store)
	if len(label) > 0 {
		ui.push_text(cmd_buf,
			{vp.pos.x + 6, vp.pos.y + 12},
			label, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	}

	bar_y_start := vp.pos.y + 18
	bar_area := ui.Rect{
		pos  = {vp.pos.x + 4, bar_y_start},
		size = {vp.size.x - 8, vp.size.y - 22},
	}
	if bar_area.size.y <= 0 do return

	max_vol := store.max_volume
	if max_vol <= 0 do max_vol = 1

	row_h := bar_area.size.y / f32(store.count)
	bar_max_w := bar_area.size.x * 0.85

	for i in 0 ..< store.count {
		bucket := store.buckets[i]
		row := store.count - 1 - i
		y := bar_area.pos.y + f32(row) * row_h

		total := bucket.buy_volume + bucket.sell_volume
		total_w := f32(total / max_vol) * bar_max_w

		buy_w := f32(bucket.buy_volume / max_vol) * bar_max_w
		sell_w := total_w - buy_w

		bar_x := bar_area.pos.x + bar_area.size.x - total_w
		buy_dom := total > 0 ? clamp(f32(bucket.buy_volume / total), 0, 1) : f32(0.5)
		sell_alpha := f32(0.5) + (1 - buy_dom) * f32(0.35)
		buy_alpha := f32(0.5) + buy_dom * f32(0.35)

		if sell_w > 0.5 {
			ui.push(cmd_buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {bar_x, y}, size = {sell_w, math.max(row_h - 1, 1)}},
				color = ui.with_alpha(ui.COL_ORDERBOOK_RED, sell_alpha),
			})
		}
		if buy_w > 0.5 {
			ui.push(cmd_buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {bar_x + sell_w, y}, size = {buy_w, math.max(row_h - 1, 1)}},
				color = ui.with_alpha(ui.COL_ORDERBOOK_GREEN, buy_alpha),
			})
		}

		// POC highlight.
		if i == store.poc_index {
			ui.push(cmd_buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {bar_x - 2, y}, size = {total_w + 4, math.max(row_h, 2)}},
				color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.3),
			})
			// POC line across full width.
			poc_y := y + row_h * 0.5
			ui.push(cmd_buf, ui.Cmd_Line{
				from = {bar_area.pos.x, poc_y},
				to   = {ui.rect_right(bar_area), poc_y},
				color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.6),
				thickness = 1,
			})
		}
	}

	// VAH/VAL dashed lines.
	if store.vah_price > 0 && store.val_price > 0 && store.count > 1 {
		min_p := store.buckets[0].price
		max_p := store.buckets[store.count - 1].price
		price_range := max_p - min_p
		if price_range > 0 {
			vah_frac := f32((store.vah_price - min_p) / price_range)
			val_frac := f32((store.val_price - min_p) / price_range)
			vah_y := bar_area.pos.y + bar_area.size.y * (1 - vah_frac)
			val_y := bar_area.pos.y + bar_area.size.y * (1 - val_frac)

			ui.push(cmd_buf, ui.Cmd_Line{
				from = {bar_area.pos.x, vah_y}, to = {ui.rect_right(bar_area), vah_y},
				color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.4), thickness = 1,
			})
			ui.push(cmd_buf, ui.Cmd_Line{
				from = {bar_area.pos.x, val_y}, to = {ui.rect_right(bar_area), val_y},
				color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.4), thickness = 1,
			})
		}
	}
}

// --- TPO Profile ---

@(private = "file")
render_tpo_profile :: proc(cmd_buf: ^ui.Command_Buffer, store: ^services.TPO_Store, vp: ui.Rect) {
	if store == nil || store.level_count == 0 || store.period_count == 0 {
		ui.push_text(cmd_buf,
			{vp.pos.x + 6, vp.pos.y + 14},
			"TPO: no data",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}

	// Session label header.
	label := services.get_tpo_label(store)
	if len(label) > 0 {
		ui.push_text(cmd_buf,
			{vp.pos.x + 6, vp.pos.y + 12},
			label, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	}

	grid_y := vp.pos.y + 18
	grid_area := ui.Rect{
		pos  = {vp.pos.x + 4, grid_y},
		size = {vp.size.x - 8, vp.size.y - 22},
	}
	if grid_area.size.y <= 0 do return

	row_h := grid_area.size.y / f32(store.level_count)
	letter_w := f32(10) // fixed width per TPO letter block

	// Find max letter columns for layout.
	max_letters := 0
	for i in 0 ..< store.level_count {
		if store.levels[i].count > max_letters do max_letters = store.levels[i].count
	}
	if max_letters == 0 do max_letters = 1

	// Scale letter_w to fit available width.
	avail_w := grid_area.size.x
	if f32(max_letters) * letter_w > avail_w {
		letter_w = avail_w / f32(max_letters)
	}

	letter_buf: [1]u8

	for li in 0 ..< store.level_count {
		lv := store.levels[li]
		row := store.level_count - 1 - li
		y := grid_area.pos.y + f32(row) * row_h

		is_poc := (li == store.poc_level_idx)

		// POC row background.
		if is_poc {
			ui.push(cmd_buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {grid_area.pos.x, y}, size = {grid_area.size.x, math.max(row_h, 2)}},
				color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.15),
			})
		}

		// Render each letter block.
		for ci in 0 ..< lv.count {
			if ci >= services.TPO_LETTERS_PER_LEVEL do break
			letter := lv.letters[ci]
			lx := grid_area.pos.x + f32(ci) * letter_w

			// Color by period: A=brightest, later periods progressively dimmer.
			period_idx := int(letter) - int('A')
			brightness := f32(1.0) - f32(period_idx) * f32(0.03)
			brightness = clamp(brightness, 0.3, 1.0)

			block_color := is_poc ? ui.with_alpha(ui.COL_YELLOW_ACCENT, brightness * 0.7) : ui.with_alpha(ui.COL_BLUE, brightness * 0.5)

			ui.push(cmd_buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {lx, y}, size = {math.max(letter_w - 1, 1), math.max(row_h - 1, 1)}},
				color = block_color,
			})

			// Render letter text if blocks are wide enough.
			if letter_w >= 8 && row_h >= 8 {
				letter_buf[0] = letter
				text_y := y + row_h * 0.5 + ui.FONT_SIZE_XS * 0.35
				ui.push_text(cmd_buf, {lx + 1, text_y},
					string(letter_buf[:]), ui.with_alpha(ui.COL_WHITE, 0.8), ui.FONT_SIZE_XS, .Mono)
			}
		}

		// Single-print marker: levels with only 1 letter.
		if lv.count == 1 {
			marker_x := grid_area.pos.x + letter_w + 2
			marker_y := y + row_h * 0.5
			ui.push(cmd_buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {marker_x, marker_y - 1}, size = {3, 3}},
				color = ui.COL_ACCENT_CYAN,
			})
		}
	}

	// IB bracket (Initial Balance — periods A+B range).
	if store.ib_high > 0 && store.ib_low > 0 && store.level_count > 1 {
		min_p := store.levels[0].price_low
		max_p := store.levels[store.level_count - 1].price_low
		price_range := max_p - min_p
		if price_range > 0 {
			ib_hi_frac := f32((store.ib_high - min_p) / price_range)
			ib_lo_frac := f32((store.ib_low - min_p) / price_range)
			ib_hi_y := grid_area.pos.y + grid_area.size.y * (1 - ib_hi_frac)
			ib_lo_y := grid_area.pos.y + grid_area.size.y * (1 - ib_lo_frac)
			bracket_x := ui.rect_right(grid_area) - 6

			// Vertical bracket line.
			ui.push(cmd_buf, ui.Cmd_Line{
				from = {bracket_x, ib_hi_y}, to = {bracket_x, ib_lo_y},
				color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.6), thickness = 1,
			})
			// Top tick.
			ui.push(cmd_buf, ui.Cmd_Line{
				from = {bracket_x - 3, ib_hi_y}, to = {bracket_x, ib_hi_y},
				color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.6), thickness = 1,
			})
			// Bottom tick.
			ui.push(cmd_buf, ui.Cmd_Line{
				from = {bracket_x - 3, ib_lo_y}, to = {bracket_x, ib_lo_y},
				color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.6), thickness = 1,
			})
			// IB label.
			ui.push_text(cmd_buf, {bracket_x - 16, (ib_hi_y + ib_lo_y) * 0.5 + ui.FONT_SIZE_XS * 0.35},
				"IB", ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Mono)
		}
	}

	// VAH/VAL lines.
	if store.vah_price > 0 && store.val_price > 0 && store.level_count > 1 {
		min_p := store.levels[0].price_low
		max_p := store.levels[store.level_count - 1].price_low
		price_range := max_p - min_p
		if price_range > 0 {
			vah_y := grid_area.pos.y + grid_area.size.y * (1 - f32((store.vah_price - min_p) / price_range))
			val_y := grid_area.pos.y + grid_area.size.y * (1 - f32((store.val_price - min_p) / price_range))
			ui.push(cmd_buf, ui.Cmd_Line{
				from = {grid_area.pos.x, vah_y}, to = {ui.rect_right(grid_area), vah_y},
				color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.4), thickness = 1,
			})
			ui.push(cmd_buf, ui.Cmd_Line{
				from = {grid_area.pos.x, val_y}, to = {ui.rect_right(grid_area), val_y},
				color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.4), thickness = 1,
			})
		}
	}
}
