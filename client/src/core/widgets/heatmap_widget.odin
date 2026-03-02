package widgets

// Heatmap widget — grid of colored rects showing order book depth over time.
// Uses Viridis 5-stop colormap (dark navy → bright yellow).
// Text labels on cells when large enough. Pure RCL.

import "core:fmt"
import "core:math"
import "mr:ports"
import "mr:services"
import "mr:ui"

Heatmap_Widget_Data :: struct {
	store:    ^services.Heatmap_Store,
	viewport: ui.Rect,
	text:     ports.Text_Port,
	input:    ports.Input_State,
	pointer:  ui.Pointer_Input,
}

heatmap_widget :: proc(buf: ^ui.Command_Buffer, data: Heatmap_Widget_Data) {
	store := data.store
	if store == nil || store.count == 0 {
		vp := data.viewport
		inner, _ := ui.panel_v2(buf, vp, ui.Panel_V2_Config{
			title        = "Heatmap",
			title_height = data.text.line_height(ui.FONT_SIZE_SM),
			bg_color     = ui.COL_PANEL_BG,
			pad          = 4,
		}, data.text.measure, ui.FONT_SIZE_SM)
		msg :: "Waiting for heatmap..."
		ui.push_text(buf,
			{inner.pos.x + inner.size.x * 0.5 - 60, inner.pos.y + inner.size.y * 0.5},
			msg, ui.with_alpha(ui.COL_WHITE, 0.3), ui.FONT_SIZE_SM)
		return
	}

	// Panel.
	inner, _ := ui.panel_v2(buf, data.viewport, ui.Panel_V2_Config{
		title        = "Heatmap",
		title_height = data.text.line_height(ui.FONT_SIZE_SM),
		bg_color     = ui.COL_PANEL_BG,
		pad          = 4,
	}, data.text.measure, ui.FONT_SIZE_SM)

	ui.push(buf, ui.Cmd_Clip_Push{rect = data.viewport})

	// Reserve space for axis labels.
	label_w  := data.text.measure(ui.FONT_SIZE_SM, "00000").x + 4
	time_axis_h := data.text.line_height(ui.FONT_SIZE_XS) + 4
	legend_h := time_axis_h + data.text.line_height(ui.FONT_SIZE_SM) + 8

	grid := ui.Rect{
		pos  = {inner.pos.x + label_w, inner.pos.y},
		size = {inner.size.x - label_w, inner.size.y - legend_h},
	}

	// Grid dimensions.
	snap_count := store.count
	if snap_count == 0 {
		ui.push(buf, ui.Cmd_Clip_Pop{})
		return
	}

	// Find the max level count across visible snapshots.
	max_levels := 0
	for i in 0 ..< snap_count {
		snap := services.get_heatmap_snapshot(store, i)
		if snap != nil {
			max_levels = max(max_levels, snap.level_count)
		}
	}
	if max_levels == 0 {
		ui.push(buf, ui.Cmd_Clip_Pop{})
		return
	}

	cell_w := grid.size.x / f32(snap_count)
	cell_h := grid.size.y / f32(max_levels)
	global_max := store.global_max_size
	if global_max <= 0 do global_max = 1

	// Draw cells.
	actual_cell_w := math.max(cell_w - 0.5, 1)
	actual_cell_h := math.max(cell_h - 0.5, 1)
	show_text := actual_cell_w > 50 && actual_cell_h > 15

	for s in 0 ..< snap_count {
		snap := services.get_heatmap_snapshot(store, s)
		if snap == nil do continue

		x := grid.pos.x + f32(s) * cell_w

		for l in 0 ..< snap.level_count {
			intensity := f32(snap.levels[l].size / global_max)
			intensity = clamp(intensity, 0, 1)
			color := ui.viridis_gradient(intensity)

			// Price levels: lowest at bottom, highest at top.
			row := max_levels - 1 - l
			y := grid.pos.y + f32(row) * cell_h

			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x, y}, size = {actual_cell_w, actual_cell_h}},
				color = color,
			})

			// Text label on cells when large enough.
			if show_text && snap.levels[l].size > 0 {
				size_buf: [12]u8
				size_str := format_compact_size(size_buf[:], snap.levels[l].size)
				text_color := intensity < 0.5 ? ui.COL_WHITE : ui.COL_BLACK
				text_x := x + actual_cell_w * 0.5 - data.text.measure(ui.FONT_SIZE_XS, size_str).x * 0.5
				text_y := y + actual_cell_h * 0.5 + ui.FONT_SIZE_XS * 0.35
				ui.push_text(buf, {text_x, text_y}, size_str,
					ui.with_alpha(text_color, 0.85), ui.FONT_SIZE_XS, .Mono)
			}
		}
	}

	// Y-axis labels (price levels from first snapshot).
	first_snap := services.get_heatmap_snapshot(store, 0)
	tbuf: [32]u8
	if first_snap != nil {
		label_step := max(max_levels / 6, 1)
		for l := 0; l < max_levels; l += label_step {
			row := max_levels - 1 - l
			y := grid.pos.y + f32(row) * cell_h + cell_h * 0.5 + data.text.line_height(ui.FONT_SIZE_SM) * 0.5
			price_str := ui.format_price(tbuf[:], first_snap.levels[l].price, ui.auto_price_decimals(first_snap.levels[l].price))
			ui.push_text(buf, {inner.pos.x, y}, price_str,
				ui.with_alpha(ui.COL_WHITE, 0.6), ui.FONT_SIZE_SM, .Mono)
		}
	}

	// X-axis time labels (HH:MM below grid).
	time_label_count := min(5, snap_count)
	time_step := snap_count / max(time_label_count, 1)
	if time_step < 1 do time_step = 1
	time_label_y := grid.pos.y + grid.size.y + data.text.line_height(ui.FONT_SIZE_XS)
	for ti := 0; ti < snap_count; ti += time_step {
		snap := services.get_heatmap_snapshot(store, ti)
		if snap == nil do continue
		hours := (snap.unix / 3600) % 24
		mins := (snap.unix / 60) % 60
		time_str := fmt.bprintf(tbuf[:], "%02d:%02d", hours, mins)
		tx := grid.pos.x + f32(ti) * cell_w + cell_w * 0.5 - data.text.measure(ui.FONT_SIZE_XS, time_str).x * 0.5
		ui.push_text(buf, {tx, time_label_y}, time_str,
			ui.with_alpha(ui.COL_WHITE, 0.4), ui.FONT_SIZE_XS, .Mono)
	}

	// Hover tooltip: show price, size, time at hovered cell.
	mx := data.input.mouse.pos.x
	my := data.input.mouse.pos.y
	if mx >= grid.pos.x && mx < ui.rect_right(grid) && my >= grid.pos.y && my < ui.rect_bottom(grid) {
		hover_col := int((mx - grid.pos.x) / cell_w)
		hover_row := int((my - grid.pos.y) / cell_h)
		if hover_col >= 0 && hover_col < snap_count {
			hsnap := services.get_heatmap_snapshot(store, hover_col)
			if hsnap != nil {
				hover_level := max_levels - 1 - hover_row
				if hover_level >= 0 && hover_level < hsnap.level_count {
					hl := hsnap.levels[hover_level]
					hours := (hsnap.unix / 3600) % 24
					mins := (hsnap.unix / 60) % 60
					hm_tip_pbuf: [16]u8
					tip: ui.Tooltip_Data
					hm_tip_sbuf: [16]u8
					hm_tip_tbuf: [8]u8
					tip.lines[0] = {label = "Price: ", value = ui.format_price(hm_tip_pbuf[:], hl.price, ui.auto_price_decimals(hl.price)), color = ui.COL_TEXT_PRIMARY}
					tip.lines[1] = {label = "Size:  ", value = fmt.bprintf(hm_tip_sbuf[:], "%.2f", hl.size), color = ui.COL_TEXT_PRIMARY}
					tip.lines[2] = {label = "Time:  ", value = fmt.bprintf(hm_tip_tbuf[:], "%02d:%02d", hours, mins), color = ui.COL_TEXT_SECONDARY}
					tip.count = 3
					ui.draw_tooltip(buf, {mx, my}, tip, data.text.measure, data.viewport)
				}
			}
		}
	}

	// Legend bar at bottom (below time labels).
	legend_y := grid.pos.y + grid.size.y + time_axis_h + 2
	legend_w := grid.size.x * 0.4
	legend_x := grid.pos.x + (grid.size.x - legend_w) * 0.5
	legend_bar_h := legend_h - 4
	LEGEND_STEPS :: 20

	for i in 0 ..< LEGEND_STEPS {
		t := f32(i) / f32(LEGEND_STEPS - 1)
		color := ui.viridis_gradient(t)
		step_w := legend_w / f32(LEGEND_STEPS)
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {legend_x + f32(i) * step_w, legend_y}, size = {step_w, legend_bar_h}},
			color = color,
		})
	}

	ui.push_text(buf, {legend_x - data.text.measure(ui.FONT_SIZE_SM, "Low").x - 4, legend_y + legend_bar_h},
		"Low", ui.with_alpha(ui.COL_WHITE, 0.5), ui.FONT_SIZE_SM, .Mono)
	ui.push_text(buf, {legend_x + legend_w + 4, legend_y + legend_bar_h},
		"High", ui.with_alpha(ui.COL_WHITE, 0.5), ui.FONT_SIZE_SM, .Mono)

	ui.push(buf, ui.Cmd_Clip_Pop{})
}

// Format a size value as compact string: "1.2M", "340K", "12.5".
@(private = "file")
format_compact_size :: proc(buf: []u8, size: f64) -> string {
	if size >= 1_000_000 {
		return fmt.bprintf(buf, "%.1fM", size / 1_000_000)
	}
	if size >= 1_000 {
		return fmt.bprintf(buf, "%.0fK", size / 1_000)
	}
	if size >= 1 {
		return fmt.bprintf(buf, "%.1f", size)
	}
	return fmt.bprintf(buf, "%.2f", size)
}
