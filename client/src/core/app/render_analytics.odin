package app

// S48/S61: Orderflow Analytics Pack — widget render procs.
// S61: Renders from pre-resolved Cell_View_Model. Widget procs receive
// ^Cmd_Buffer + resolved store pointers — zero App_State coupling.

import "core:fmt"
import "core:math"
import "mr:services"
import "mr:ui"

// S61: Entry point from Cell_View_Model dispatch (no App_State dependency).
render_analytics_cell_vm :: proc(cmd_buf: ^ui.Command_Buffer, vm: Cell_View_Model, cell_vp: ui.Rect) {
	if cmd_buf == nil do return
	if cell_vp.size.x <= 0 || cell_vp.size.y <= 0 do return

	ui.push(cmd_buf, ui.Cmd_Rect_Filled{rect = cell_vp, color = ui.with_alpha(ui.COL_SURFACE_1, 0.92)})

	if vm.stores.analytics == nil {
		ui.push_text(cmd_buf,
			{cell_vp.pos.x + 6, cell_vp.pos.y + 14},
			"Waiting analytics...",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}

	switch vm.analytics_kind {
	case .Open_Interest:
		render_analytics_oi(cmd_buf, vm.stores.analytics, cell_vp, vm.show_history)
	case .Delta_Volume:
		render_analytics_delta_volume(cmd_buf, vm.stores.analytics, cell_vp, vm.show_history)
	case .CVD:
		render_analytics_cvd(cmd_buf, vm.stores.analytics, cell_vp, vm.show_history)
	case .Bar_Stats:
		render_analytics_bar_stats(cmd_buf, vm.stores.analytics, cell_vp)
	}
}

// --- Open Interest ---
// Latest OI value + delta + delta_pct, with sparkline history.
@(private = "file")
render_analytics_oi :: proc(cmd_buf: ^ui.Command_Buffer, store: ^services.Analytics_Store, vp: ui.Rect, show_hist: bool) {
	entry, ok := services.get_analytics_latest(store, .Open_Interest)
	if !ok {
		ui.push_text(cmd_buf,
			{vp.pos.x + 6, vp.pos.y + 14},
			"OI: no data",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}

	oi_val := entry.values[0]
	delta := entry.values[1]
	delta_pct := entry.values[2]

	// Main OI value.
	val_buf: [64]u8
	val_str := fmt.bprintf(val_buf[:], "OI %.0f", oi_val)
	ui.push_text(cmd_buf,
		{vp.pos.x + 8, vp.pos.y + 18},
		val_str, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_MD, .Bold)

	// Delta + delta_pct.
	delta_color := delta >= 0 ? ui.COL_GREEN : ui.COL_RED
	delta_sign := delta >= 0 ? "+" : ""
	delta_buf: [80]u8
	delta_str := fmt.bprintf(delta_buf[:], "%s%.0f (%s%.2f%%)", delta_sign, delta, delta_sign, delta_pct * 100)
	ui.push_text(cmd_buf,
		{vp.pos.x + 8, vp.pos.y + 36},
		delta_str, delta_color, ui.FONT_SIZE_XS, .Mono)

	// Sparkline history.
	if show_hist {
		hist_y := vp.pos.y + 50
		hist_h := vp.size.y - 54
		if hist_h > 10 {
			render_analytics_sparkline(cmd_buf, store, .Open_Interest, 0, vp.pos.x + 4, hist_y, vp.size.x - 8, hist_h, ui.COL_ACCENT_CYAN)
		}
	}
}

// --- Delta Volume ---
// Buy/sell bars with delta label, +-bar chart history.
@(private = "file")
render_analytics_delta_volume :: proc(cmd_buf: ^ui.Command_Buffer, store: ^services.Analytics_Store, vp: ui.Rect, show_hist: bool) {
	entry, ok := services.get_analytics_latest(store, .Delta_Volume)
	if !ok {
		ui.push_text(cmd_buf,
			{vp.pos.x + 6, vp.pos.y + 14},
			"DV: no data",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}

	buy_vol := entry.values[0]
	sell_vol := entry.values[1]
	delta_vol := entry.values[2]

	// Delta value.
	delta_color := delta_vol >= 0 ? ui.COL_GREEN : ui.COL_RED
	val_buf: [80]u8
	val_str := fmt.bprintf(val_buf[:], "Delta %.2f", delta_vol)
	ui.push_text(cmd_buf,
		{vp.pos.x + 8, vp.pos.y + 18},
		val_str, delta_color, ui.FONT_SIZE_MD, .Bold)

	// Buy / Sell volumes.
	bs_buf: [80]u8
	bs_str := fmt.bprintf(bs_buf[:], "Buy %.2f  Sell %.2f", buy_vol, sell_vol)
	ui.push_text(cmd_buf,
		{vp.pos.x + 8, vp.pos.y + 36},
		bs_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

	// Buy/Sell ratio bar.
	total := buy_vol + sell_vol
	if total > 0 {
		bar_y := vp.pos.y + 46
		bar_h := f32(8)
		bar_w := vp.size.x - 16
		buy_frac := f32(buy_vol / total)
		ui.push(cmd_buf, ui.Cmd_Rect_Filled{
			rect = ui.rect_xywh(vp.pos.x + 8, bar_y, bar_w * buy_frac, bar_h),
			color = ui.with_alpha(ui.COL_GREEN, 0.5),
		})
		ui.push(cmd_buf, ui.Cmd_Rect_Filled{
			rect = ui.rect_xywh(vp.pos.x + 8 + bar_w * buy_frac, bar_y, bar_w * (1 - buy_frac), bar_h),
			color = ui.with_alpha(ui.COL_RED, 0.5),
		})
	}

	// Delta history as +/- bars.
	if show_hist {
		hist_y := vp.pos.y + 60
		hist_h := vp.size.y - 64
		if hist_h > 10 {
			render_analytics_delta_bars(cmd_buf, store, .Delta_Volume, 2, vp.pos.x + 4, hist_y, vp.size.x - 8, hist_h)
		}
	}
}

// --- CVD (Cumulative Volume Delta) ---
// CVD value + history area, delta per window as secondary label.
@(private = "file")
render_analytics_cvd :: proc(cmd_buf: ^ui.Command_Buffer, store: ^services.Analytics_Store, vp: ui.Rect, show_hist: bool) {
	entry, ok := services.get_analytics_latest(store, .CVD)
	if !ok {
		ui.push_text(cmd_buf,
			{vp.pos.x + 6, vp.pos.y + 14},
			"CVD: no data",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}

	delta_vol := entry.values[0]
	cvd := entry.values[1]

	// CVD value.
	cvd_color := cvd >= 0 ? ui.COL_GREEN : ui.COL_RED
	val_buf: [64]u8
	val_str := fmt.bprintf(val_buf[:], "CVD %.2f", cvd)
	ui.push_text(cmd_buf,
		{vp.pos.x + 8, vp.pos.y + 18},
		val_str, cvd_color, ui.FONT_SIZE_MD, .Bold)

	// Delta this window.
	dv_buf: [64]u8
	dv_str := fmt.bprintf(dv_buf[:], "Window delta %.2f", delta_vol)
	ui.push_text(cmd_buf,
		{vp.pos.x + 8, vp.pos.y + 36},
		dv_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

	// CVD line history.
	if show_hist {
		hist_y := vp.pos.y + 50
		hist_h := vp.size.y - 54
		if hist_h > 10 {
			render_analytics_sparkline(cmd_buf, store, .CVD, 1, vp.pos.x + 4, hist_y, vp.size.x - 8, hist_h, ui.COL_ACCENT_CYAN)
		}
	}
}

// --- Bar Statistics ---
// Trade count, buy/sell ratio, VWAP, imbalance, burst flag.
@(private = "file")
render_analytics_bar_stats :: proc(cmd_buf: ^ui.Command_Buffer, store: ^services.Analytics_Store, vp: ui.Rect) {
	entry, ok := services.get_analytics_latest(store, .Bar_Stats)
	if !ok {
		ui.push_text(cmd_buf,
			{vp.pos.x + 6, vp.pos.y + 14},
			"Bar Stats: no data",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}

	trade_count := entry.values[0]
	buy_count := entry.values[1]
	sell_count := entry.values[2]
	total_vol := entry.values[3]
	buy_vol := entry.values[4]
	sell_vol := entry.values[5]
	vwap := entry.values[6]
	imbalance := entry.values[7]
	is_burst := (entry.flags & 1) != 0

	y := vp.pos.y + 14

	// Trade count + buy/sell.
	tc_buf: [80]u8
	tc_str := fmt.bprintf(tc_buf[:], "Trades %.0f  (B:%.0f S:%.0f)", trade_count, buy_count, sell_count)
	ui.push_text(cmd_buf, {vp.pos.x + 8, y}, tc_str, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)
	y += 16

	// Volume.
	vol_buf: [80]u8
	vol_str := fmt.bprintf(vol_buf[:], "Vol %.4f  (B:%.4f S:%.4f)", total_vol, buy_vol, sell_vol)
	ui.push_text(cmd_buf, {vp.pos.x + 8, y}, vol_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	y += 16

	// VWAP.
	vwap_buf: [48]u8
	vwap_str := fmt.bprintf(vwap_buf[:], "VWAP %.2f", vwap)
	ui.push_text(cmd_buf, {vp.pos.x + 8, y}, vwap_str, ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Mono)
	y += 16

	// Imbalance.
	imb_color := imbalance >= 0 ? ui.COL_GREEN : ui.COL_RED
	imb_buf: [48]u8
	imb_str := fmt.bprintf(imb_buf[:], "Imbalance %.2f%%", imbalance * 100)
	ui.push_text(cmd_buf, {vp.pos.x + 8, y}, imb_str, imb_color, ui.FONT_SIZE_XS, .Mono)
	y += 16

	// Buy/sell ratio bar.
	total := buy_vol + sell_vol
	if total > 0 {
		bar_w := vp.size.x - 16
		buy_frac := f32(buy_vol / total)
		ui.push(cmd_buf, ui.Cmd_Rect_Filled{
			rect = ui.rect_xywh(vp.pos.x + 8, y, bar_w * buy_frac, 8),
			color = ui.with_alpha(ui.COL_GREEN, 0.5),
		})
		ui.push(cmd_buf, ui.Cmd_Rect_Filled{
			rect = ui.rect_xywh(vp.pos.x + 8 + bar_w * buy_frac, y, bar_w * (1 - buy_frac), 8),
			color = ui.with_alpha(ui.COL_RED, 0.5),
		})
		y += 14
	}

	// Burst flag.
	if is_burst {
		ui.push_text(cmd_buf, {vp.pos.x + 8, y}, "BURST",
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Bold)
	}
}

// --- Sparkline: renders recent values of a specific analytics kind as a line chart ---
@(private = "file")
render_analytics_sparkline :: proc(
	cmd_buf: ^ui.Command_Buffer,
	store: ^services.Analytics_Store,
	kind: services.Analytics_Kind,
	slot_idx: int,
	x, y, w, h: f32,
	color: ui.Color,
) {
	// Collect values.
	vals: [64]f64
	count := 0
	for i := store.count - 1; i >= 0; i -= 1 {
		e := services.get_analytics(store, i)
		if e.kind == kind {
			vals[count] = e.values[slot_idx]
			count += 1
			if count >= 64 do break
		}
	}
	if count < 2 do return

	// Find min/max.
	min_v := vals[0]
	max_v := vals[0]
	for i in 1 ..< count {
		if vals[i] < min_v do min_v = vals[i]
		if vals[i] > max_v do max_v = vals[i]
	}
	if max_v <= min_v {
		max_v = min_v + 1
	}

	// Draw line segments (oldest to newest, left to right).
	step_x := w / f32(max(count - 1, 1))
	for i in 0 ..< count - 1 {
		// vals are stored newest-first, so reverse for left-to-right.
		v0 := vals[count - 1 - i]
		v1 := vals[count - 2 - i]
		t0 := f32((v0 - min_v) / (max_v - min_v))
		t1 := f32((v1 - min_v) / (max_v - min_v))
		x0 := x + f32(i) * step_x
		x1 := x + f32(i + 1) * step_x
		y0 := y + h * (1 - t0)
		y1 := y + h * (1 - t1)
		ui.push(cmd_buf, ui.Cmd_Line{
			from = {x0, y0}, to = {x1, y1},
			color = ui.with_alpha(color, 0.8), thickness = 1,
		})
	}
}

// --- Delta bars: renders recent delta values as +/- bars from center ---
@(private = "file")
render_analytics_delta_bars :: proc(
	cmd_buf: ^ui.Command_Buffer,
	store: ^services.Analytics_Store,
	kind: services.Analytics_Kind,
	slot_idx: int,
	x, y, w, h: f32,
) {
	// Collect values.
	vals: [64]f64
	count := 0
	for i := store.count - 1; i >= 0; i -= 1 {
		e := services.get_analytics(store, i)
		if e.kind == kind {
			vals[count] = e.values[slot_idx]
			count += 1
			if count >= 64 do break
		}
	}
	if count < 1 do return

	// Find max absolute value.
	max_abs := f64(0)
	for i in 0 ..< count {
		a := math.abs(vals[i])
		if a > max_abs do max_abs = a
	}
	if max_abs <= 0 do max_abs = 1

	mid_y := y + h * 0.5
	bar_w := max(w / f32(max(count, 1)) - 1, 1)

	// Draw center line.
	ui.push(cmd_buf, ui.Cmd_Line{
		from = {x, mid_y}, to = {x + w, mid_y},
		color = ui.with_alpha(ui.COL_WHITE, 0.1), thickness = 1,
	})

	for i in 0 ..< count {
		// Reverse: oldest on left.
		v := vals[count - 1 - i]
		frac := f32(v / max_abs) * 0.5  // half height for positive/negative
		bar_h := math.abs(frac) * h
		bx := x + f32(i) * (bar_w + 1)
		col := v >= 0 ? ui.COL_GREEN : ui.COL_RED
		if v >= 0 {
			ui.push(cmd_buf, ui.Cmd_Rect_Filled{
				rect = ui.rect_xywh(bx, mid_y - bar_h, bar_w, bar_h),
				color = ui.with_alpha(col, 0.5),
			})
		} else {
			ui.push(cmd_buf, ui.Cmd_Rect_Filled{
				rect = ui.rect_xywh(bx, mid_y, bar_w, bar_h),
				color = ui.with_alpha(col, 0.5),
			})
		}
	}
}
