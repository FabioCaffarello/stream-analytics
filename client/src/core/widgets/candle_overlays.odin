package widgets

// Candlestick chart overlays: heatmap, VPVR, overlay source badges.

import "core:fmt"
import "core:math"
import "mr:services"
import "mr:ui"

Overlay_Source_State :: enum u8 {
	None,
	Synthetic,
	Live,
}

overlay_source_state :: proc(is_live, is_synth: bool) -> Overlay_Source_State {
	if is_live do return .Live
	if is_synth do return .Synthetic
	return .None
}

overlay_source_badge :: proc(
	buf: ^ui.Command_Buffer,
	rect: ui.Rect,
	label: string,
	state: Overlay_Source_State,
	measure: proc(font_size: f32, text: string) -> ui.Vec2,
) -> f32 {
	status_text: string
	dot_color: ui.Color
	text_color: ui.Color
	switch state {
	case .Live:
		status_text = "LIVE"
		dot_color = ui.COL_GREEN
		text_color = ui.COL_GREEN
	case .Synthetic:
		status_text = "SYN"
		dot_color = ui.COL_YELLOW_ACCENT
		text_color = ui.COL_YELLOW_ACCENT
	case .None:
		status_text = "--"
		dot_color = ui.with_alpha(ui.COL_WHITE, 0.35)
		text_color = ui.COL_TEXT_MUTED
	}

	tag_buf: [24]u8
	tag := fmt.bprintf(tag_buf[:], "%s:%s", label, status_text)
	badge_w := ui.status_badge_width(tag, measure, ui.FONT_SIZE_XS)
	badge_h := min(rect.size.y - 2, f32(14))
	badge_y := rect.pos.y + (rect.size.y - badge_h) * 0.5
	ui.status_badge(buf, ui.Rect{pos = {rect.pos.x, badge_y}, size = {badge_w, badge_h}},
		tag, dot_color, text_color, measure, ui.FONT_SIZE_XS)
	return badge_w
}

draw_candle_underlays :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	if ctx.show_vpvr {
		draw_candle_vpvr_overlay(buf, Candle_VPVR_Overlay_Data{
			store       = data.vpvr_store,
			inner       = ctx.inner,
			chart_w     = ctx.chart_w,
			price_lo    = ctx.price_lo,
			price_hi    = ctx.price_hi,
			price_range = ctx.price_range,
			price_h     = ctx.price_h,
		})
	}
}

draw_candle_overlays :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	if ctx.show_heatmap {
		draw_candle_heatmap_overlay(buf, Candle_Heatmap_Overlay_Data{
			store           = data.heatmap_store,
			candle_store    = ctx.store,
			inner           = ctx.inner,
			price_lo        = ctx.price_lo,
			price_hi        = ctx.price_hi,
			price_range     = ctx.price_range,
			price_h         = ctx.price_h,
			slot_w          = ctx.slot_w,
			start_idx       = ctx.start_idx,
			end_idx         = ctx.end_idx,
			slot_offset     = ctx.slot_offset,
			timeframe_ms    = data.timeframe_ms,
			min_visible_pct = ctx.heatmap_profile.min_visible_pct,
			min_intensity   = ctx.heatmap_profile.min_intensity,
			min_alpha       = ctx.heatmap_profile.min_alpha,
			max_alpha       = ctx.heatmap_profile.max_alpha,
			measure         = data.text.measure,
		})
	}
}

Candle_Heatmap_Overlay_Data :: struct {
	store:        ^services.Heatmap_Store,
	candle_store: ^services.Candle_Store,
	inner:        ui.Rect,
	price_lo:     f64,
	price_hi:     f64,
	price_range:  f64,
	price_h:      f32,
	slot_w:       f32,
	start_idx:    int,
	end_idx:      int,
	slot_offset:  int,
	timeframe_ms: i64,
	min_visible_pct: f64,
	min_intensity:   f32,
	min_alpha:       f32,
	max_alpha:       f32,
	measure:      proc(font_size: f32, text: string) -> ui.Vec2,
}

@(private = "file")
draw_candle_heatmap_overlay :: proc(buf: ^ui.Command_Buffer, data: Candle_Heatmap_Overlay_Data) {
	if data.store == nil || data.store.count <= 0 do return
	if data.candle_store == nil || data.candle_store.count <= 0 do return
	if data.start_idx < 0 || data.end_idx <= data.start_idx do return
	if data.slot_w <= 0 || data.price_h <= 0 do return
	visible_slots := min(max((data.end_idx - data.start_idx) + data.slot_offset, 0), CANDLE_MAX_VISIBLE_SLOTS)
	if visible_slots <= 0 do return

	// Resolve timeframe in ms.
	tf_ms := data.timeframe_ms
	if tf_ms <= 0 && data.end_idx - data.start_idx >= 2 {
		c0 := services.get_candle(data.candle_store, data.start_idx)
		c1 := services.get_candle(data.candle_store, data.start_idx + 1)
		delta := c1.window_start_ts - c0.window_start_ts
		if delta > 0 do tf_ms = delta
	}
	if tf_ms <= 0 do return

	first_candle_ts := services.get_candle(data.candle_store, data.start_idx).window_start_ts

	// Build slot→snapshot mapping with O(1) slot computation per snapshot.
	slot_snapshot_idx: [CANDLE_MAX_VISIBLE_SLOTS]int
	slot_snapshot_unix: [CANDLE_MAX_VISIBLE_SLOTS]i64
	for i in 0 ..< visible_slots {
		slot_snapshot_idx[i] = -1
		slot_snapshot_unix[i] = -1
	}

	for i in 0 ..< data.store.count {
		s := services.get_heatmap_snapshot(data.store, i)
		if s == nil || s.unix <= 0 do continue

		// FIX 1: Use window_start_ms directly when available; fall back to
		// reverse-engineering from window_end_ts for synthetic/old data.
		aligned_ts: i64
		if s.window_start_ms > 0 {
			aligned_ts = s.window_start_ms
		} else {
			snap_end_ms := s.unix * 1000
			aligned_ts = ((snap_end_ms - 1) / tf_ms) * tf_ms
		}

		// Direct slot computation from first visible candle.
		candle_offset := int((aligned_ts - first_candle_ts) / tf_ms)
		slot := candle_offset + data.slot_offset
		if slot < 0 || slot >= visible_slots do continue

		// Keep latest snapshot per slot.
		if s.unix >= slot_snapshot_unix[slot] {
			slot_snapshot_idx[slot] = i
			slot_snapshot_unix[slot] = s.unix
		}
	}

	// FIX 2: Compute sparse flag over the entire visible window to prevent
	// per-snapshot flickering between cbrt/sqrt when adjacent snapshots
	// straddle the threshold.
	total_visible_levels := 0
	visible_snap_count := 0
	for slot in 0 ..< visible_slots {
		idx := slot_snapshot_idx[slot]
		if idx < 0 do continue
		snap := services.get_heatmap_snapshot(data.store, idx)
		if snap == nil do continue
		total_visible_levels += snap.level_count
		visible_snap_count += 1
	}
	avg_levels := visible_snap_count > 0 ? total_visible_levels / visible_snap_count : 0
	sparse := avg_levels < 8

	// Stable intensity normalization using global max (no per-frame flicker).
	norm_hi := data.store.global_max_size
	if norm_hi <= 0 do return

	// Visible-window max keeps contrast usable even if a stale outlier inflated global_max.
	visible_max := f64(0)
	for slot in 0 ..< visible_slots {
		idx := slot_snapshot_idx[slot]
		if idx < 0 do continue
		snap := services.get_heatmap_snapshot(data.store, idx)
		if snap == nil || snap.level_count <= 0 do continue
		for l in 0 ..< snap.level_count {
			level := snap.levels[l]
			if level.size <= 0 do continue
			if level.price < data.price_lo || level.price > data.price_hi do continue
			if level.size > visible_max do visible_max = level.size
		}
	}
	if visible_max > 0 {
		// IMPROVE 6: Adaptive normalization spread — tighter when dense (many
		// snapshots per visible window) to keep 5s heatmaps visible.
		spread_mult := visible_snap_count > 40 ? 2.0 : 4.0
		clamped_hi := visible_max * spread_mult
		if clamped_hi < norm_hi do norm_hi = clamped_hi
		if norm_hi < visible_max do norm_hi = visible_max
	}

	min_visible_pct := clamp(data.min_visible_pct, f64(0), f64(0.95))
	range_min := norm_hi * min_visible_pct
	if visible_max > 0 && range_min >= visible_max {
		range_min = visible_max * 0.5
	}

	// FIX 4: Clamp cell_w to slot_w to prevent cells exceeding slot boundary.
	cell_w := clamp(data.slot_w - 0.5, 1, data.slot_w)
	min_intensity := clamp(data.min_intensity, 0, 1)
	min_alpha := clamp(data.min_alpha, 0, 1)
	max_alpha := clamp(data.max_alpha, min_alpha, 1)

	for slot in 0 ..< visible_slots {
		idx := slot_snapshot_idx[slot]
		if idx < 0 do continue
		snap := services.get_heatmap_snapshot(data.store, idx)
		if snap == nil || snap.level_count <= 0 do continue
		x := data.inner.pos.x + f32(slot) * data.slot_w

		// Always use price_group from snapshot; skip if missing.
		price_step := snap.price_group
		if price_step <= 0 do continue
		cell_h := f32((price_step / data.price_range) * f64(data.price_h))
		cell_h = clamp(cell_h, 1, data.price_h)

		for l in 0 ..< snap.level_count {
			level := snap.levels[l]
			if level.size <= 0 do continue
			if level.price < data.price_lo || level.price > data.price_hi do continue

			intensity := heatmap_remap01(level.size, range_min, norm_hi)
			if intensity < min_intensity do continue
			// Perceptual lift: cbrt for sparse data, sqrt for dense.
			intensity_vis: f32
			if sparse {
				intensity_vis = clamp(f32(math.pow(f64(intensity), 1.0 / 3.0)), 0, 1)
			} else {
				intensity_vis = clamp(f32(math.sqrt(f64(intensity))), 0, 1)
			}

			center_y := data.inner.pos.y + f32((data.price_hi - level.price) / data.price_range) * data.price_h
			y := center_y - cell_h * 0.5
			if y < data.inner.pos.y do y = data.inner.pos.y
			max_y := data.inner.pos.y + data.price_h
			if y >= max_y do continue
			h := min(cell_h, max_y - y)
			if h <= 0 do continue

			col := candle_heatmap_gradient(intensity_vis)
			alpha := min_alpha + intensity_vis * (max_alpha - min_alpha)
			col = ui.with_alpha(col, alpha)

			// FIX 3: Large-cell visual softening — trigger on aspect ratio
			// (height >> width) instead of comparing price-domain to time-domain.
			if h > cell_w * 2.5 && h > 4 {
				edge_alpha := alpha * 0.3
				third := h / 3.0
				ui.push(buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {x, y}, size = {cell_w, third}},
					color = ui.with_alpha(col, edge_alpha),
				})
				ui.push(buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {x, y + third}, size = {cell_w, third}},
					color = ui.with_alpha(col, alpha),
				})
				ui.push(buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {x, y + 2 * third}, size = {cell_w, h - 2 * third}},
					color = ui.with_alpha(col, edge_alpha),
				})
			} else {
				ui.push(buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {x, y}, size = {cell_w, h}},
					color = col,
				})
			}

			// IMPROVE 5: Text labels on large heatmap cells (MM-inspired).
			if data.measure != nil && cell_w >= 40 && h >= 14 {
				size_buf: [12]u8
				size_str := format_compact_size(level.size, size_buf[:])
				if len(size_str) > 0 {
					text_color := intensity_vis > 0.55 ? ui.COL_BLACK : ui.COL_WHITE
					text_color = ui.with_alpha(text_color, alpha * 0.85)
					text_sz := data.measure(ui.FONT_SIZE_XS, size_str)
					text_x := x + (cell_w - text_sz.x) * 0.5
					text_y := y + (h - text_sz.y) * 0.5 + ui.FONT_SIZE_XS * 0.35
					ui.push_text(buf, {text_x, text_y}, size_str, text_color, ui.FONT_SIZE_XS, .Mono)
				}
			}
		}
	}
}

// Format a size value compactly for heatmap cell labels.
// 0.5 → "0.5", 1.2 → "1.2", 123.4 → "123", 1500 → "1.5K", 2500000 → "2.5M"
@(private = "file")
format_compact_size :: proc(val: f64, buf: []u8) -> string {
	if len(buf) < 8 || val <= 0 do return ""
	v: f64
	suffix: u8
	if val >= 1_000_000 {
		v = val / 1_000_000
		suffix = 'M'
	} else if val >= 1000 {
		v = val / 1000
		suffix = 'K'
	} else {
		v = val
		suffix = 0
	}
	whole := int(v)
	frac := int((v - f64(whole)) * 10 + 0.5) % 10
	if frac < 0 do frac = 0
	off := 0
	if whole >= 1000 { buf[off] = '0' + u8((whole / 1000) % 10); off += 1 }
	if whole >= 100  { buf[off] = '0' + u8((whole / 100) % 10); off += 1 }
	if whole >= 10   { buf[off] = '0' + u8((whole / 10) % 10); off += 1 }
	buf[off] = '0' + u8(whole % 10); off += 1
	if frac > 0 && whole < 100 {
		buf[off] = '.'; off += 1
		buf[off] = '0' + u8(frac); off += 1
	}
	if suffix != 0 {
		buf[off] = suffix; off += 1
	}
	return string(buf[:off])
}

Candle_VPVR_Overlay_Data :: struct {
	store:       ^services.VPVR_Store,
	inner:       ui.Rect,
	chart_w:     f32,
	price_lo:    f64,
	price_hi:    f64,
	price_range: f64,
	price_h:     f32,
}

@(private = "file")
draw_candle_vpvr_overlay :: proc(buf: ^ui.Command_Buffer, data: Candle_VPVR_Overlay_Data) {
	if data.store == nil || data.store.count <= 0 do return
	if data.price_range <= 0 || data.price_h <= 0 || data.chart_w <= 0 do return

	overlay_w := data.chart_w * VPVR_OVERLAY_PCT
	if overlay_w < 24 do return
	right_x := data.inner.pos.x + data.chart_w

	visible_max_volume := f64(0)
	for i in 0 ..< data.store.count {
		b := services.get_vpvr_bucket(data.store, i)
		if b.price < data.price_lo || b.price > data.price_hi do continue
		visible_max_volume = math.max(visible_max_volume, b.buy_volume + b.sell_volume)
	}
	if visible_max_volume <= 0 do return

	price_step := data.store.price_group
	if price_step <= 0 && data.store.count > 1 {
		b0 := services.get_vpvr_bucket(data.store, 0)
		b1 := services.get_vpvr_bucket(data.store, 1)
		price_step = math.abs(b1.price - b0.price)
	}
	if price_step <= 0 {
		price_step = data.price_range / f64(max(data.store.count, 1))
	}
	cell_h := f32((price_step / data.price_range) * f64(data.price_h))
	cell_h = clamp(cell_h, 1, data.price_h)

	for i in 0 ..< data.store.count {
		b := services.get_vpvr_bucket(data.store, i)
		if b.price < data.price_lo || b.price > data.price_hi do continue
		total := b.buy_volume + b.sell_volume
		if total <= 0 do continue

		vol_t := clamp(f32(total / visible_max_volume), 0, 1)
		if vol_t <= 0 do continue
		total_w := overlay_w * vol_t
		if total_w <= 0.5 do continue

		sell_w := total_w * clamp(f32(b.sell_volume / total), 0, 1)
		buy_w := total_w - sell_w
		x0 := right_x - total_w

		center_y := data.inner.pos.y + f32((data.price_hi - b.price) / data.price_range) * data.price_h
		y := center_y - cell_h * 0.5
		if y < data.inner.pos.y do y = data.inner.pos.y
		max_y := data.inner.pos.y + data.price_h
		if y >= max_y do continue
		h := min(cell_h, max_y - y)
		if h <= 0 do continue

		if sell_w > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x0, y}, size = {sell_w, h}},
				color = ui.with_alpha(ui.COL_ORDERBOOK_RED, 0.28),
			})
		}
		if buy_w > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x0 + sell_w, y}, size = {buy_w, h}},
				color = ui.with_alpha(ui.COL_ORDERBOOK_GREEN, 0.28),
			})
		}
	}

	if data.store.poc_index >= 0 && data.store.poc_index < data.store.count {
		poc := services.get_vpvr_bucket(data.store, data.store.poc_index)
		if poc.price >= data.price_lo && poc.price <= data.price_hi {
			poc_y := data.inner.pos.y + f32((data.price_hi - poc.price) / data.price_range) * data.price_h
			ui.push(buf, ui.Cmd_Line{
				from      = {right_x - overlay_w, poc_y},
				to        = {right_x, poc_y},
				color     = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.6),
				thickness = 1,
			})
		}
	}
}

candle_heatmap_gradient :: proc(t: f32) -> ui.Color {
	return ui.viridis_gradient(t)
}

heatmap_remap01 :: proc(v, lo, hi: f64) -> f32 {
	if hi <= lo {
		if v >= hi do return 1
		return 0
	}
	return clamp(f32((v - lo) / (hi - lo)), 0, 1)
}
