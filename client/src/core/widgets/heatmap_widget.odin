package widgets

// Heatmap widget — grid of colored rects showing order book depth over time.
// 3-stop gradient: purple → teal → yellow (low → medium → high intensity).
// Pure RCL: uses Cmd_Rect_Filled only (full Canvas2D support).

import "core:fmt"
import "core:math"
import "mr:ports"
import "mr:services"
import "mr:ui"

Heatmap_Widget_Data :: struct {
	store:    ^services.Heatmap_Store,
	viewport: ui.Rect,
	text:     ports.Text_Port,
}

heatmap_widget :: proc(buf: ^ui.Command_Buffer, data: Heatmap_Widget_Data) {
	store := data.store
	if store == nil || store.count == 0 do return

	// Panel.
	inner := ui.panel(buf, data.viewport, ui.Panel_Config{
		title        = "Heatmap",
		title_height = data.text.line_height(ui.FONT_SIZE_SM),
		bg_color     = ui.COL_PANEL_BG,
		pad          = 4,
	}, data.text.measure, ui.FONT_SIZE_SM)

	ui.push(buf, ui.Cmd_Clip_Push{rect = data.viewport})

	// Reserve space for axis labels.
	label_w  := data.text.measure(ui.FONT_SIZE_SM, "00000").x + 4
	legend_h := data.text.line_height(ui.FONT_SIZE_SM) + 8

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
	for s in 0 ..< snap_count {
		snap := services.get_heatmap_snapshot(store, s)
		if snap == nil do continue

		x := grid.pos.x + f32(s) * cell_w

		for l in 0 ..< snap.level_count {
			intensity := f32(snap.levels[l].size / global_max)
			intensity = clamp(intensity, 0, 1)
			color := heatmap_gradient(intensity)

			// Price levels: lowest at bottom, highest at top.
			row := max_levels - 1 - l
			y := grid.pos.y + f32(row) * cell_h

			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x, y}, size = {math.max(cell_w - 0.5, 1), math.max(cell_h - 0.5, 1)}},
				color = color,
			})
		}
	}

	// Y-axis labels (price levels from first snapshot).
	first_snap := services.get_heatmap_snapshot(store, 0)
	if first_snap != nil {
		tbuf: [32]u8
		label_step := max(max_levels / 6, 1)
		for l := 0; l < max_levels; l += label_step {
			row := max_levels - 1 - l
			y := grid.pos.y + f32(row) * cell_h + cell_h * 0.5 + data.text.line_height(ui.FONT_SIZE_SM) * 0.5
			price_str := fmt.bprintf(tbuf[:], "%.0f", first_snap.levels[l].price)
			ui.push_text(buf, {inner.pos.x, y}, price_str,
				ui.with_alpha(ui.COL_WHITE, 0.6), ui.FONT_SIZE_SM, .Mono)
		}
	}

	// Legend bar at bottom.
	legend_y := grid.pos.y + grid.size.y + 4
	legend_w := grid.size.x * 0.4
	legend_x := grid.pos.x + (grid.size.x - legend_w) * 0.5
	legend_bar_h := legend_h - 4
	LEGEND_STEPS :: 20

	for i in 0 ..< LEGEND_STEPS {
		t := f32(i) / f32(LEGEND_STEPS - 1)
		color := heatmap_gradient(t)
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

// 3-stop gradient: purple (0.0) → teal (0.5) → yellow (1.0).
@(private = "file")
heatmap_gradient :: proc(t: f32) -> ui.Color {
	t := clamp(t, 0, 1)

	if t < 0.5 {
		// Purple → Teal.
		f := t * 2 // 0..1
		return {
			lerp(f32(0.4), f32(0.0), f),  // R
			lerp(f32(0.0), f32(0.7), f),  // G
			lerp(f32(0.8), f32(0.7), f),  // B
			1.0,
		}
	}
	// Teal → Yellow.
	f := (t - 0.5) * 2 // 0..1
	return {
		lerp(f32(0.0), f32(0.98), f), // R
		lerp(f32(0.7), f32(1.0), f),  // G
		lerp(f32(0.7), f32(0.4), f),  // B
		1.0,
	}
}

@(private = "file")
lerp :: proc(a, b, t: f32) -> f32 {
	return a + (b - a) * t
}
