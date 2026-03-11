package layers

import "core:fmt"
import "mr:services"
import "mr:ui"

// S140: Time axis rendering for candle charts.
//
// Renders timestamp labels along the bottom of the chart viewport,
// with vertical grid lines at label positions. Adapts label format
// and density based on the active timeframe and zoom level.
//
// Label policy:
//   - Sub-minute TFs (1s, 5s): "HH:MM:SS"
//   - Minute TFs (1m, 5m):     "HH:MM"
//   - Hourly+ TFs (15m..4h):   "HH:MM", day boundaries show "DD Mon"
//   - Daily TF (1d):           "DD Mon"
//
// Visual indicators:
//   - Live edge: green accent on rightmost candle when scroll_offset == 0
//   - "LIVE ONLY": badge when candle count is small and at live edge

// Height reserved at the bottom of the candle chart for time axis labels.
TIME_AXIS_H :: f32(16)

// Minimum pixel spacing between time labels to avoid overlap.
TIME_AXIS_MIN_SPACING :: f32(64)

// Time Axis parameters passed from the candle renderer.
Time_Axis_Params :: struct {
	axis_vp:        ui.Rect,              // bottom strip viewport
	store:          ^services.Candle_Store,
	start:          int,                  // first visible candle index (logical)
	actual_visible: int,                  // number of visible candles
	slot_w:         f32,                  // pixel width per candle slot
	tf_ms:          i64,                  // timeframe duration in ms
	scroll_offset:  int,                  // 0 = at live edge
	chart_left:     f32,                  // left x of chart area
}

// Render time axis labels and grid lines.
time_axis_render :: proc(out: ^Layer_Outputs, p: Time_Axis_Params) {
	if out == nil || p.store == nil do return
	if p.actual_visible <= 0 || p.slot_w <= 0 do return

	// S147-BUG-06: Sub-minute TFs use "HH:MM:SS" labels (8 chars ≈ 64px at FONT_SIZE_XS).
	// The default 64px min spacing causes overlap — increase to 96px for sub-minute.
	min_spacing := p.tf_ms < 60_000 ? f32(96) : TIME_AXIS_MIN_SPACING
	raw_interval := max(1, int(min_spacing / p.slot_w))
	label_interval := time_axis_snap_interval(raw_interval, p.tf_ms)

	// Render grid lines and labels.
	label_y := p.axis_vp.pos.y + TIME_AXIS_H * 0.75
	grid_top := p.axis_vp.pos.y

	prev_day := i64(-1) // track day boundaries

	for i in 0 ..< p.actual_visible {
		idx := p.start + i
		c := services.get_candle(p.store, idx)
		if c.window_start_ts <= 0 do continue

		// Check if this candle aligns with a label slot.
		is_label_slot := (i % label_interval) == 0

		// Check for day boundary (always show label at day transitions).
		curr_day := c.window_start_ts / 86_400_000
		is_day_boundary := prev_day >= 0 && curr_day != prev_day
		prev_day = curr_day

		if !is_label_slot && !is_day_boundary do continue

		x_center := p.chart_left + (f32(i) + 0.5) * p.slot_w

		// Vertical grid line (very subtle).
		grid_color := is_day_boundary ? ui.with_alpha(ui.COL_WHITE, 0.10) : ui.with_alpha(ui.COL_WHITE, 0.04)
		layer_outputs_push_line(out, 18, Render_Line{
			from      = {x_center, grid_top - 200}, // extend into chart area
			to        = {x_center, grid_top},
			color     = grid_color,
			thickness = 1,
		})

		// Format label.
		buf: [24]u8
		label: string
		if is_day_boundary && p.tf_ms < 86_400_000 {
			label = format_day_label(buf[:], c.window_start_ts)
		} else {
			label = format_time_label(buf[:], c.window_start_ts, p.tf_ms)
		}

		// Center the label under the candle.
		layer_outputs_push_text_badge(out, 19, text_badge_make(
			{x_center - f32(len(label)) * 3.2, label_y},
			label,
			is_day_boundary ? ui.COL_TEXT_SECONDARY : ui.COL_TEXT_MUTED,
			ui.FONT_SIZE_XS,
		))
	}

	// Live edge indicator.
	if p.scroll_offset == 0 && p.actual_visible > 0 {
		live_x := p.chart_left + (f32(p.actual_visible) - 0.5) * p.slot_w
		layer_outputs_push_line(out, 19, Render_Line{
			from      = {live_x, grid_top - 200},
			to        = {live_x, grid_top},
			color     = ui.with_alpha(ui.COL_GREEN, 0.20),
			thickness = 1.5,
		})
	}

	// "LIVE ONLY" badge when very few candles and at live edge.
	if p.scroll_offset == 0 && p.store.count < 10 && p.store.count > 0 {
		layer_outputs_push_text_badge(out, 19, text_badge_make(
			{p.axis_vp.pos.x + 4, label_y},
			"LIVE ONLY",
			ui.COL_YELLOW_ACCENT,
			ui.FONT_SIZE_XS,
		))
	}
}

// Snap a raw label interval to a "nice" number of candles that aligns
// with natural time boundaries for the given timeframe.
time_axis_snap_interval :: proc(raw: int, tf_ms: i64) -> int {
	if raw <= 1 do return 1

	// Look up nice interval steps for the active timeframe tier.
	nice: [5]int
	count: int
	switch {
	case tf_ms <= 1_000:     nice = {5, 10, 15, 30, 60}; count = 5
	case tf_ms <= 5_000:     nice = {2, 6, 12, 30, 60};  count = 5
	case tf_ms <= 60_000:    nice = {5, 10, 15, 30, 60};  count = 5
	case tf_ms <= 300_000:   nice = {2, 3, 6, 12, 24};    count = 5
	case tf_ms <= 900_000:   nice = {2, 4, 8, 16, 32};    count = 5
	case tf_ms <= 1_800_000: nice = {2, 4, 8, 12, 24};    count = 5
	case tf_ms <= 3_600_000: nice = {2, 4, 6, 12, 24};    count = 5
	case tf_ms <= 14_400_000: nice = {2, 3, 6, 12, 0};   count = 4
	case:                    nice = {2, 5, 7, 14, 30};     count = 5
	}

	for i in 0 ..< count {
		if nice[i] >= raw do return nice[i]
	}
	return nice[count - 1]
}

// Format a time label based on candle timestamp and timeframe.
// Sub-minute: "HH:MM:SS"
// Minute/hourly: "HH:MM"
// Daily: "DD Mon"
format_time_label :: proc(buf: []u8, unix_ms: i64, tf_ms: i64) -> string {
	total_s := unix_ms / 1000
	sec := int(total_s % 60)
	total_min := total_s / 60
	min := int(total_min % 60)
	total_hr := total_min / 60
	hr := int(total_hr % 24)

	if tf_ms >= 86_400_000 {
		return format_day_label(buf, unix_ms)
	}
	if tf_ms < 60_000 {
		return fmt.bprintf(buf, "%02d:%02d:%02d", hr, min, sec)
	}
	return fmt.bprintf(buf, "%02d:%02d", hr, min)
}

// Format a day boundary label: "DD Mon".
format_day_label :: proc(buf: []u8, unix_ms: i64) -> string {
	day, month, _ := unix_ms_to_date(unix_ms)
	mon := month_abbrev(month)
	return fmt.bprintf(buf, "%02d %s", day, mon)
}

// Convert Unix ms to (day, month, year) using civil date algorithm.
// Based on Howard Hinnant's chrono algorithm (days since epoch).
unix_ms_to_date :: proc(unix_ms: i64) -> (day, month, year: int) {
	z := int(unix_ms / 86_400_000) + 719468
	era := (z >= 0 ? z : z - 146096) / 146097
	doe := z - era * 146097
	yoe := (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365
	y := yoe + era * 400
	doy := doe - (365 * yoe + yoe / 4 - yoe / 100)
	mp := (5 * doy + 2) / 153
	d := doy - (153 * mp + 2) / 5 + 1
	m := mp + (mp < 10 ? 3 : -9)
	if m <= 2 do y += 1
	return d, m, y
}

// 3-letter month abbreviation.
month_abbrev :: proc(month: int) -> string {
	switch month {
	case 1:  return "Jan"
	case 2:  return "Feb"
	case 3:  return "Mar"
	case 4:  return "Apr"
	case 5:  return "May"
	case 6:  return "Jun"
	case 7:  return "Jul"
	case 8:  return "Aug"
	case 9:  return "Sep"
	case 10: return "Oct"
	case 11: return "Nov"
	case 12: return "Dec"
	}
	return "???"
}
