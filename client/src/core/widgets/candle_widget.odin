package widgets

// Candlestick chart widget — pure RCL rendering.
// Emits candle bodies (Cmd_Rect_Filled), wicks (Cmd_Line), volume bars,
// price/time axis labels, and current price line.

import "core:fmt"
import "core:math"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:ui"

// --- Configuration ---

CANDLE_MIN_ZOOM     :: f32(10)   // minimum candles visible
CANDLE_MAX_ZOOM     :: f32(500)  // maximum candles visible
CANDLE_DEFAULT_ZOOM :: f32(60)   // initial candles visible
CANDLE_MAX_VISIBLE_SLOTS :: int(500)
CANDLE_BODY_PCT     :: f32(0.7)  // body width as fraction of slot width
CANDLE_BODY_LINE_ONLY_MIN_W :: f32(3.0)
VOLUME_HEIGHT_PCT   :: f32(0.18) // volume bars take 18% of viewport height
PRICE_MARGIN_PCT    :: f64(0.15) // 15% buffer above/below price range
Y_AXIS_WIDTH        :: f32(70)   // reserved right margin for price labels
X_AXIS_HEIGHT       :: f32(20)   // reserved bottom margin for time labels
VPVR_OVERLAY_PCT    :: f32(0.20) // max VPVR overlay width relative to chart width
HEATMAP_INTENSITY_OPTIONS :: [3]string{"L", "M", "H"}

Heatmap_Intensity_Profile :: struct {
	min_visible_pct: f64,
	min_intensity:   f32,
	min_alpha:       f32,
	max_alpha:       f32,
}

HEATMAP_INTENSITY_PROFILES :: [3]Heatmap_Intensity_Profile{
	{min_visible_pct = 0.16, min_intensity = 0.03, min_alpha = 0.08, max_alpha = 0.40},
	{min_visible_pct = 0.24, min_intensity = 0.05, min_alpha = 0.10, max_alpha = 0.50},
	{min_visible_pct = 0.34, min_intensity = 0.07, min_alpha = 0.12, max_alpha = 0.58},
}

// --- Chart type ---

Chart_Type :: enum u8 {
	Candlesticks,
	Line,
	Heiken_Ashi,
	Footprint,
	Footprint_Delta,
}

CHART_TYPE_LABELS :: [5]string{"Candle", "Line", "HA", "FP", "FPΔ"}

// --- Crosshair state ---

Crosshair_State :: struct {
	active:       bool,   // true when mouse is over chart area
	mouse_pos:    ui.Vec2,
	hovered_idx:  int,    // candle index under cursor (-1 = none)
	price_at_y:   f64,    // price value at cursor Y
}

Indicator_Render_Probe :: struct {
	rsi_enabled:           bool,
	macd_enabled:          bool,
	funding_enabled:       bool,
	liq_enabled:           bool,
	trade_counter_enabled: bool,
	rsi_rendered:          bool,
	macd_rendered:         bool,
	funding_rendered:      bool,
	liq_rendered:          bool,
	trade_counter_rendered: bool,
}

// --- Widget data ---

Candle_Widget_Data :: struct {
	store:         ^services.Candle_Store,
	heatmap_store: ^services.Heatmap_Store,
	vpvr_store:    ^services.VPVR_Store,
	viewport:      ui.Rect,
	text:          ports.Text_Port,
	input:         ports.Input_State,
	scroll_x:      ^f32,
	zoom_level:    ^f32,
	health_label:  string,
	health_detail: string,
	health_color:  ui.Color,
	tf_label:      string,
	stream_id:     string,
	stream_state:  streams.Stream_State,
	heatmap_live:  bool,
	heatmap_synth: bool,
	vpvr_live:     bool,
	vpvr_synth:    bool,
	show_volume:   ^bool,
	show_heatmap_overlay: ^bool,
	show_vpvr_overlay:    ^bool,
	heatmap_intensity_idx: ^int,
	crosshair:     ^Crosshair_State,
	chart_type:    ^Chart_Type,
	// Indicator toggles.
	show_ma:       bool,
	show_bbands:   bool,
	show_vwap:     bool,
	show_rsi:      bool,
	show_macd:     bool,
	show_funding:       bool,
	show_liq:           bool,
	show_trade_counter: bool,
	// Indicator parameters.
	ma_periods:    [3]int,
	bb_period:     int,
	bb_sigma:      f64,
	rsi_period:    int,
	macd_fast:     int,
	macd_slow:     int,
	macd_signal:   int,
	stats_store:   ^services.Stats_Store,
	draw_tools:    ^Draw_Tools_State,
	footprint_store: ^services.Footprint_Store,
	pointer:       ui.Pointer_Input,
	now_ms:        i64,         // current wall-clock ms (0 = no countdown)
	timeframe_ms:  i64,         // active candle TF in ms (e.g. 60_000)
	// Crosshair sync: price level from another chart's crosshair.
	sync_price:    f64,         // price from synced crosshair (0 = no sync)
	sync_active:   bool,        // true if sync crosshair should be drawn
	indicator_probe: ^Indicator_Render_Probe,
	// Subplot resize (PRD-0007 M1).
	sub_main_split: ^f32,       // nil or pointer to user override ratio for subplot area
	sub_ratios:     ^[5]f32,    // nil or pointer to per-subplot custom ratios
	sub_resize_idx: ^int,       // nil or pointer to active separator drag index (-1 = none)
}

Candle_Render_Context :: struct {
	store:        ^services.Candle_Store,
	inner:        ui.Rect,
	chart_w:      f32,
	chart_h:      f32,
	price_h:      f32,
	vol_h:        f32,
	slot_w:       f32,
	body_w:       f32,
	price_lo:     f64,
	price_hi:     f64,
	price_range:  f64,
	vol_max:      f64,
	start_idx:    int,
	end_idx:      int,
	actual_visible: int,
	slot_offset:  int,
	zoom:         f32,
	vol_avg:      f64,
	show_vol:     bool,
	show_heatmap: bool,
	show_vpvr:    bool,
	heatmap_profile: Heatmap_Intensity_Profile,
	chart_type:      Chart_Type,
	footprint_store: ^services.Footprint_Store,
	timeframe_ms:    i64,
	// Raw extremes (before margin buffer) for high/low labels.
	raw_high:     f64,
	raw_low:      f64,
	raw_high_idx: int,
	raw_low_idx:  int,
}

Overlay_Source_State :: enum u8 {
	None,
	Synthetic,
	Live,
}

@(private = "file")
overlay_source_state :: proc(is_live, is_synth: bool) -> Overlay_Source_State {
	if is_live do return .Live
	if is_synth do return .Synthetic
	return .None
}

@(private = "file")
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

// --- Main draw procedure ---

candle_widget :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data) {
	store := data.store
	vp := data.viewport

	// Panel header with layer controls.
	title_buf: [96]u8
	tf := data.tf_label if len(data.tf_label) > 0 else "1m"
	title := fmt.bprintf(title_buf[:], "Candles (%s)", tf)
	if len(data.stream_id) > 0 {
		title = fmt.bprintf(title_buf[:], "Candles (%s) %s", tf, data.stream_id)
	}

	inner, ctrl_rect := ui.panel_v2(buf, vp, ui.Panel_V2_Config{
		title        = title,
		title_height = data.text.line_height(ui.FONT_SIZE_SM),
		bg_color     = ui.COL_PANEL_BG,
		pad          = 4,
	}, data.text.measure, ui.FONT_SIZE_SM)

	// Health badge in header control area.
	hdr_x := ctrl_rect.pos.x
	if len(data.health_label) > 0 && ctrl_rect.size.x > 40 {
		badge_w := ui.status_badge_width(data.health_label, data.text.measure, ui.FONT_SIZE_XS)
		badge_h := min(ctrl_rect.size.y - 2, f32(14))
		badge_y := ctrl_rect.pos.y + (ctrl_rect.size.y - badge_h) * 0.5
		ui.status_badge(buf, ui.Rect{pos = {hdr_x, badge_y}, size = {badge_w, badge_h}},
			data.health_label, data.health_color, data.health_color,
			data.text.measure, ui.FONT_SIZE_XS)
		hdr_x += badge_w + 6
	}

	// Source badges: signal whether overlay data is live or synthetic fallback.
	if ctrl_rect.size.x > 260 {
		hm_state := overlay_source_state(data.heatmap_live, data.heatmap_synth)
		hm_badge_w := overlay_source_badge(buf, ui.Rect{
			pos  = {hdr_x, ctrl_rect.pos.y},
			size = ctrl_rect.size,
		}, "HM", hm_state, data.text.measure)
		hdr_x += hm_badge_w + 4

		vp_state := overlay_source_state(data.vpvr_live, data.vpvr_synth)
		vp_badge_w := overlay_source_badge(buf, ui.Rect{
			pos  = {hdr_x, ctrl_rect.pos.y},
			size = ctrl_rect.size,
		}, "VP", vp_state, data.text.measure)
		hdr_x += vp_badge_w + 6
	}

	// Layer pills (PRD-0007 M3/M5): compact toggles with hover settings tooltip.
	if hdr_x < ui.rect_right(ctrl_rect) - 200 {
		PILL_LABELS :: [8]string{"MA", "BB", "VW", "RS", "MC", "FN", "LQ", "TC"}
		pill_labels := PILL_LABELS
		pill_active := [8]bool{
			data.show_ma, data.show_bbands, data.show_vwap,
			data.show_rsi, data.show_macd,
			data.show_funding, data.show_liq, data.show_trade_counter,
		}
		hovered_pill := -1
		hovered_pill_rect: ui.Rect
		for pi in 0 ..< 8 {
			pw := data.text.measure(ui.FONT_SIZE_XS, pill_labels[pi]).x + 8
			if hdr_x + pw > ui.rect_right(ctrl_rect) - 200 do break
			pill_h := min(ctrl_rect.size.y - 2, f32(12))
			pill_y := ctrl_rect.pos.y + (ctrl_rect.size.y - pill_h) * 0.5
			pill_rect := ui.Rect{pos = {hdr_x, pill_y}, size = {pw, pill_h}}
			active := pill_active[pi]
			hovered := ui.rect_contains(pill_rect, data.pointer.pos)
			pill_bg := active ? ui.with_alpha(ui.COL_BLUE, hovered ? 0.35 : 0.25) : ui.with_alpha(ui.COL_SURFACE_2, hovered ? 0.7 : 0.5)
			pill_fg := active ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_MUTED
			ui.push(buf, ui.Cmd_Rect_Filled{rect = pill_rect, color = pill_bg})
			ui.push_text(buf, {pill_rect.pos.x + 4, pill_y + pill_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				pill_labels[pi], pill_fg, ui.FONT_SIZE_XS, .Mono)
			if hovered { hovered_pill = pi; hovered_pill_rect = pill_rect }
			hdr_x += pw + 2
		}
		hdr_x += 4

		// Hover tooltip: show current settings for hovered indicator pill.
		if hovered_pill >= 0 {
			tip_buf: [64]u8
			tip_str := ""
			switch hovered_pill {
			case 0: // MA
				tip_str = fmt.bprintf(tip_buf[:], "MA: EMA(%d) EMA(%d) SMA(%d) [M]",
					data.ma_periods[0], data.ma_periods[1], data.ma_periods[2])
			case 1: // BBands
				tip_str = fmt.bprintf(tip_buf[:], "BBands: SMA(%d) %.1f sigma [B]", data.bb_period, data.bb_sigma)
			case 2: // VWAP
				tip_str = fmt.bprintf(tip_buf[:], "VWAP: daily anchor [V]")
			case 3: // RSI
				tip_str = fmt.bprintf(tip_buf[:], "RSI: period=%d [R]", data.rsi_period)
			case 4: // MACD
				tip_str = fmt.bprintf(tip_buf[:], "MACD: %d/%d/%d [I]", data.macd_fast, data.macd_slow, data.macd_signal)
			case 5: // Funding
				tip_str = fmt.bprintf(tip_buf[:], "Funding Rate [H]")
			case 6: // Liq
				tip_str = fmt.bprintf(tip_buf[:], "Liquidation Volume [J]")
			case 7: // Trade Counter
				tip_str = fmt.bprintf(tip_buf[:], "Trade Counter [K]")
			}
			if len(tip_str) > 0 {
				tw := data.text.measure(ui.FONT_SIZE_XS, tip_str).x + 10
				th := f32(16)
				tx := hovered_pill_rect.pos.x
				ty := ui.rect_bottom(hovered_pill_rect) + 2
				ui.push(buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {tx - 2, ty}, size = {tw, th}},
					color = ui.with_alpha(ui.COL_SURFACE_1, 0.92),
				})
				ui.push_text(buf, {tx + 3, ty + th * 0.5 + ui.FONT_SIZE_XS * 0.35},
					tip_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
			}
		}
	}

	hdr_right := ui.rect_right(ctrl_rect)
	ctrl_gap := f32(4)
	ctrl_h := min(ctrl_rect.size.y - 2, f32(14))
	ctrl_y := ctrl_rect.pos.y + (ctrl_rect.size.y - ctrl_h) * 0.5

	if data.show_volume != nil && hdr_right-hdr_x >= 40 {
		vol_w := f32(36)
		vol_rect := ui.Rect{pos = {hdr_right - vol_w, ctrl_y}, size = {vol_w, ctrl_h}}
		vol_res := ui.toggle(buf, vol_rect, "Vol", data.show_volume^, data.pointer, data.text.measure, ui.FONT_SIZE_XS)
		if vol_res.changed {
			data.show_volume^ = vol_res.value
		}
		hdr_right = vol_rect.pos.x - ctrl_gap
	}

	if data.show_vpvr_overlay != nil && hdr_right-hdr_x >= 40 {
		vp_w := f32(34)
		vp_rect := ui.Rect{pos = {hdr_right - vp_w, ctrl_y}, size = {vp_w, ctrl_h}}
		vp_res := ui.toggle(buf, vp_rect, "VP", data.show_vpvr_overlay^, data.pointer, data.text.measure, ui.FONT_SIZE_XS)
		if vp_res.changed {
			data.show_vpvr_overlay^ = vp_res.value
		}
		hdr_right = vp_rect.pos.x - ctrl_gap
	}

	if data.show_heatmap_overlay != nil && hdr_right-hdr_x >= 40 {
		hm_w := f32(34)
		hm_rect := ui.Rect{pos = {hdr_right - hm_w, ctrl_y}, size = {hm_w, ctrl_h}}
		hm_res := ui.toggle(buf, hm_rect, "HM", data.show_heatmap_overlay^, data.pointer, data.text.measure, ui.FONT_SIZE_XS)
		if hm_res.changed {
			data.show_heatmap_overlay^ = hm_res.value
		}
		hdr_right = hm_rect.pos.x - ctrl_gap
	}

	// Chart type selector.
	if data.chart_type != nil && hdr_right-hdr_x >= 130 {
		ct_labels := CHART_TYPE_LABELS
		ct_w := f32(130)
		ct_rect := ui.Rect{pos = {hdr_right - ct_w, ctrl_y}, size = {ct_w, ctrl_h}}
		ct_res := ui.segmented_control(buf, ct_rect, ct_labels[:], int(data.chart_type^),
			data.pointer, data.text.measure, ui.FONT_SIZE_XS, .Mono)
		if ct_res.changed {
			data.chart_type^ = Chart_Type(ct_res.index)
		}
		hdr_right = ct_rect.pos.x - ctrl_gap
	}

	if data.heatmap_intensity_idx != nil &&
		data.show_heatmap_overlay != nil &&
		data.show_heatmap_overlay^ &&
		hdr_right-hdr_x >= 80 {
		idx := clamp(data.heatmap_intensity_idx^, 0, len(HEATMAP_INTENSITY_OPTIONS) - 1)
		data.heatmap_intensity_idx^ = idx
		seg_w := f32(70)
		seg_rect := ui.Rect{pos = {hdr_right - seg_w, ctrl_y}, size = {seg_w, ctrl_h}}
		heatmap_options := HEATMAP_INTENSITY_OPTIONS
		seg_res := ui.segmented_control(buf, seg_rect, heatmap_options[:], idx,
			data.pointer, data.text.measure, ui.FONT_SIZE_XS, .Mono)
		if seg_res.changed {
			data.heatmap_intensity_idx^ = seg_res.index
		}
	}

	ui.push(buf, ui.Cmd_Clip_Push{rect = inner})

	// Empty state.
	if store == nil || store.count == 0 {
		msg :: "Waiting for candle data..."
		ui.push_text(buf,
			{inner.pos.x + inner.size.x * 0.5 - 80, inner.pos.y + inner.size.y * 0.5},
			msg, ui.with_alpha(ui.COL_WHITE, 0.3), ui.FONT_SIZE_SM)
		ui.push(buf, ui.Cmd_Clip_Pop{})
		return
	}

	// Auto-fit mode (zoom_level <= 0): fit chart to available candles.
	// Mouse-wheel zoom sets zoom_level > 0, exiting auto-fit.
	// TF/stream changes reset zoom_level to 0, re-entering auto-fit.
	auto_fit := data.zoom_level^ <= 0

	chart_w := inner.size.x - Y_AXIS_WIDTH
	chart_h := inner.size.y - X_AXIS_HEIGHT
	if chart_w <= 0 || chart_h <= 0 {
		ui.push(buf, ui.Cmd_Clip_Pop{})
		return
	}

	price_h := chart_h * (1.0 - VOLUME_HEIGHT_PCT)
	vol_h := chart_h * VOLUME_HEIGHT_PCT

	zoom: f32
	if auto_fit {
		AUTOFIT_MIN :: f32(5)
		zoom = store.count > 0 ? clamp(f32(store.count), AUTOFIT_MIN, CANDLE_MAX_ZOOM) : CANDLE_DEFAULT_ZOOM
	} else {
		zoom = clamp(data.zoom_level^, CANDLE_MIN_ZOOM, CANDLE_MAX_ZOOM)
		data.zoom_level^ = zoom
	}

	visible_count := int(zoom)
	slot_w := chart_w / zoom
	body_w := max(slot_w * CANDLE_BODY_PCT, 1)

	// Scroll: 0 = newest candle at right edge.
	scroll := int(data.scroll_x^)
	max_scroll := max(store.count - visible_count, 0)
	scroll = clamp(scroll, 0, max_scroll)
	data.scroll_x^ = f32(scroll)

	// Determine visible range (indices into store, 0 = oldest).
	end_idx := store.count - scroll       // exclusive, newest visible + 1
	start_idx := max(end_idx - visible_count, 0)
	actual_visible := end_idx - start_idx

	// Find price range and max volume in visible window.
	price_lo := math.F64_MAX
	price_hi := -math.F64_MAX
	raw_high_idx := start_idx
	raw_low_idx := start_idx
	vol_max := f64(0)
	vol_sum := f64(0)
	vol_count := 0
	for i in start_idx ..< end_idx {
		c := services.get_candle(store, i)
		if c.low < price_lo {
			price_lo = c.low
			raw_low_idx = i
		}
		if c.high > price_hi {
			price_hi = c.high
			raw_high_idx = i
		}
		if c.volume > vol_max do vol_max = c.volume
		if c.volume > 0 {
			vol_sum += c.volume
			vol_count += 1
		}
	}
	vol_avg := vol_count > 0 ? vol_sum / f64(vol_count) : f64(1)
	raw_high := price_hi
	raw_low := price_lo
	if price_hi <= price_lo {
		price_hi = price_lo + 1
	}

	// Add buffer to price range.
	price_range := price_hi - price_lo
	price_lo -= price_range * PRICE_MARGIN_PCT
	price_hi += price_range * PRICE_MARGIN_PCT
	price_range = price_hi - price_lo
	if price_range <= 0 do price_range = 1

	// Resolve volume visibility.
	show_vol := data.show_volume == nil || data.show_volume^
	eff_vol_pct := show_vol ? VOLUME_HEIGHT_PCT : f32(0)
	price_h = chart_h * (1.0 - eff_vol_pct)
	vol_h = show_vol ? chart_h * VOLUME_HEIGHT_PCT : 0

	slot_offset := visible_count - actual_visible

	show_heatmap := data.show_heatmap_overlay == nil || data.show_heatmap_overlay^
	show_vpvr := data.show_vpvr_overlay == nil || data.show_vpvr_overlay^
	heatmap_profiles := HEATMAP_INTENSITY_PROFILES
	heatmap_profile := heatmap_profiles[1]
	if data.heatmap_intensity_idx != nil {
		idx := clamp(data.heatmap_intensity_idx^, 0, len(heatmap_profiles) - 1)
		data.heatmap_intensity_idx^ = idx
		heatmap_profile = heatmap_profiles[idx]
	}

	// Chart type.
	eff_chart_type := Chart_Type.Candlesticks
	if data.chart_type != nil {
		eff_chart_type = data.chart_type^
	}

	ctx := Candle_Render_Context{
		store          = store,
		inner          = inner,
		chart_w        = chart_w,
		chart_h        = chart_h,
		price_h        = price_h,
		vol_h          = vol_h,
		slot_w         = slot_w,
		body_w         = body_w,
		price_lo       = price_lo,
		price_hi       = price_hi,
		price_range    = price_range,
		vol_max        = vol_max,
		start_idx      = start_idx,
		end_idx        = end_idx,
		actual_visible = actual_visible,
		slot_offset    = slot_offset,
		zoom           = zoom,
		vol_avg        = vol_avg,
		show_vol       = show_vol,
		show_heatmap   = show_heatmap,
		show_vpvr      = show_vpvr,
		heatmap_profile  = heatmap_profile,
		chart_type       = eff_chart_type,
		footprint_store  = data.footprint_store,
		timeframe_ms     = data.timeframe_ms,
		raw_high         = raw_high,
		raw_low          = raw_low,
		raw_high_idx     = raw_high_idx,
		raw_low_idx      = raw_low_idx,
	}

	// Probe emitted for web/native runtime diagnostics.
	if data.indicator_probe != nil {
		data.indicator_probe^ = Indicator_Render_Probe{
			rsi_enabled            = data.show_rsi,
			macd_enabled           = data.show_macd,
			funding_enabled        = data.show_funding,
			liq_enabled            = data.show_liq,
			trade_counter_enabled  = data.show_trade_counter,
		}
	}

	// Determine sub-indicator area allocation.
	sub_count := 0
	if data.show_rsi do sub_count += 1
	if data.show_macd do sub_count += 1
	if data.show_funding do sub_count += 1
	if data.show_liq do sub_count += 1
	if data.show_trade_counter do sub_count += 1

	// User override or auto 15% per subplot.
	sub_indicator_pct := f32(0)
	if sub_count > 0 {
		if data.sub_main_split != nil && data.sub_main_split^ > 0 {
			sub_indicator_pct = clamp(data.sub_main_split^, 0.10, 0.75)
		} else {
			sub_indicator_pct = min(f32(sub_count) * 0.15, 0.55)
		}
	}

	// If sub-indicators are active, reduce the main chart area.
	if sub_indicator_pct > 0 {
		total_chart := ctx.price_h + ctx.vol_h
		sub_h := total_chart * sub_indicator_pct
		main_h := total_chart - sub_h
		if ctx.show_vol {
			ctx.price_h = main_h * (1.0 - VOLUME_HEIGHT_PCT)
			ctx.vol_h = main_h * VOLUME_HEIGHT_PCT
		} else {
			ctx.price_h = main_h
		}
	}

	// Layer pipeline aligned with MarketMonkey: guides -> overlays -> indicators -> primary series -> annotations.
	draw_candle_price_grid(buf, &ctx)
	draw_candle_day_boundaries(buf, &ctx)
	draw_candle_overlays(buf, data, &ctx)

	// Build layer stack and dispatch overlays via vtable (PRD-0007 M3).
	layers: [MAX_CHART_LAYERS]Chart_Layer
	layer_count := 0

	// Layer data storage (stack-allocated).
	ma_ld: MA_Layer_Data
	bb_ld: BBands_Layer_Data
	vw_ld: VWAP_Layer_Data
	rsi_ld: RSI_Layer_Data
	macd_ld: MACD_Layer_Data
	fund_ld: Funding_Layer_Data
	liq_ld: Liq_Layer_Data
	tc_ld: TC_Layer_Data

	// --- Overlay layers (price-space) ---
	if data.show_bbands {
		bb_ld.config = BBANDS_DEFAULT
		if data.bb_period > 0 do bb_ld.config.period = data.bb_period
		if data.bb_sigma > 0 do bb_ld.config.std_mult = data.bb_sigma
		layers[layer_count] = {render = bbands_layer_render, display_name = "BB", visible = true, is_overlay = true, data = &bb_ld}
		layer_count += 1
	}
	if data.show_ma {
		ma_ld.lines = MA_DEFAULT_LINES
		if data.ma_periods[0] > 0 do ma_ld.lines[0].period = data.ma_periods[0]
		if data.ma_periods[1] > 0 do ma_ld.lines[1].period = data.ma_periods[1]
		if data.ma_periods[2] > 0 do ma_ld.lines[2].period = data.ma_periods[2]
		layers[layer_count] = {render = ma_layer_render, display_name = "MA", visible = true, is_overlay = true, data = &ma_ld}
		layer_count += 1
	}
	if data.show_vwap {
		vw_ld.config = VWAP_DEFAULT
		layers[layer_count] = {render = vwap_layer_render, display_name = "VW", visible = true, is_overlay = true, data = &vw_ld}
		layer_count += 1
	}
	// --- Sub-plot layers ---
	if data.show_rsi {
		rsi_ld.config = RSI_DEFAULT
		rsi_ld.config.visible = true
		if data.rsi_period > 0 do rsi_ld.config.period = data.rsi_period
		layers[layer_count] = {render = rsi_layer_render, display_name = "RS", visible = true, is_overlay = false, data = &rsi_ld}
		layer_count += 1
	}
	if data.show_macd {
		macd_ld.config = MACD_DEFAULT
		macd_ld.config.visible = true
		if data.macd_fast > 0 do macd_ld.config.fast_period = data.macd_fast
		if data.macd_slow > 0 do macd_ld.config.slow_period = data.macd_slow
		if data.macd_signal > 0 do macd_ld.config.signal_period = data.macd_signal
		layers[layer_count] = {render = macd_layer_render, display_name = "MC", visible = true, is_overlay = false, data = &macd_ld}
		layer_count += 1
	}
	if data.show_funding {
		fund_ld.config = FUNDING_DEFAULT
		fund_ld.config.visible = true
		layers[layer_count] = {render = funding_layer_render, display_name = "FN", visible = true, is_overlay = false, data = &fund_ld}
		layer_count += 1
	}
	if data.show_liq {
		liq_ld.config = LIQ_DEFAULT
		liq_ld.config.visible = true
		layers[layer_count] = {render = liq_layer_render, display_name = "LQ", visible = true, is_overlay = false, data = &liq_ld}
		layer_count += 1
	}
	if data.show_trade_counter {
		tc_ld.config = TRADE_COUNTER_DEFAULT
		tc_ld.config.visible = true
		layers[layer_count] = {render = tc_layer_render, display_name = "TC", visible = true, is_overlay = false, data = &tc_ld}
		layer_count += 1
	}

	// Dispatch overlay layers (price-space).
	{
		layer_ctx := chart_layer_context_from_candle(&ctx)
		layer_ctx.stats_store = data.stats_store
		layer_ctx.measure = data.text.measure
		for li in 0 ..< layer_count {
			if layers[li].is_overlay && layers[li].visible && layers[li].render != nil {
				layers[li].render(layers[li].data, buf, &layer_ctx)
			}
		}
	}

	draw_candle_bars(buf, &ctx)
	draw_candle_volume_separator(buf, &ctx)
	draw_candle_current_price(buf, data, &ctx)
	draw_candle_high_low_labels(buf, data, &ctx)

	// Draw tools (horizontal lines, etc.)
	if data.draw_tools != nil {
		layer_ctx := chart_layer_context_from_candle(&ctx)
		render_draw_tools(buf, &layer_ctx, data.draw_tools, data.pointer, data.now_ms, data.text.measure, data.input.modifiers.shift)
	}

	draw_candle_crosshair(buf, data, &ctx)
	draw_candle_sync_crosshair(buf, data, &ctx)
	draw_candle_ohlcv_bar(buf, data, &ctx)
	draw_candle_time_axis_labels(buf, &ctx)
	draw_candle_count_indicator(buf, &ctx)

	// RSI / MACD sub-plots below main chart.
	if sub_indicator_pct > 0 && sub_count > 0 {
		layer_ctx := chart_layer_context_from_candle(&ctx)
		layer_ctx.stats_store = data.stats_store
		layer_ctx.measure = data.text.measure
		total_chart := ctx.inner.size.y - X_AXIS_HEIGHT
		sub_total_h := total_chart * sub_indicator_pct
		sub_y := ctx.inner.pos.y + total_chart - sub_total_h

		// Compute per-subplot heights from custom ratios or equal division.
		sub_heights: [5]f32
		has_custom_ratios := false
		if data.sub_ratios != nil {
			ratio_sum := f32(0)
			for si in 0 ..< sub_count {
				ratio_sum += data.sub_ratios[si]
			}
			if ratio_sum > 0 {
				has_custom_ratios = true
				for si in 0 ..< sub_count {
					sub_heights[si] = sub_total_h * (data.sub_ratios[si] / ratio_sum)
				}
			}
		}
		if !has_custom_ratios {
			per_sub_h := sub_total_h / f32(sub_count)
			for si in 0 ..< sub_count {
				sub_heights[si] = per_sub_h
			}
		}

		// Main↔subplot separator drag (first separator between main chart and subplots).
		SUB_SEP_HIT :: f32(4)
		main_sub_border := sub_y
		sep_hit := ui.Rect{pos = {ctx.inner.pos.x, main_sub_border - SUB_SEP_HIT * 0.5}, size = {ctx.chart_w, SUB_SEP_HIT}}
		if data.sub_resize_idx != nil {
			if data.sub_resize_idx^ == -2 {
				// Dragging main↔sub separator.
				if data.pointer.left_down {
					new_sub_y := data.pointer.pos.y
					new_sub_pct := (ctx.inner.pos.y + total_chart - new_sub_y) / total_chart
					new_sub_pct = clamp(new_sub_pct, 0.10, 0.75)
					if data.sub_main_split != nil {
						data.sub_main_split^ = new_sub_pct
					}
				} else {
					data.sub_resize_idx^ = -1
				}
			} else if data.sub_resize_idx^ >= 0 && data.sub_resize_idx^ < sub_count - 1 {
				// Dragging between sub-plots.
				si := data.sub_resize_idx^
				if data.pointer.left_down && data.sub_ratios != nil {
					// Find Y of the separator between si and si+1.
					sep_y_base := sub_y
					for s in 0 ..< si {
						sep_y_base += sub_heights[s]
					}
					new_top_h := data.pointer.pos.y - sep_y_base
					old_pair_h := sub_heights[si] + sub_heights[si + 1]
					new_bot_h := old_pair_h - new_top_h
					SUB_MIN_H :: f32(20)
					if new_top_h >= SUB_MIN_H && new_bot_h >= SUB_MIN_H {
						// Compute ratio proportional to the pair.
						r_sum := data.sub_ratios[si] + data.sub_ratios[si + 1]
						if r_sum <= 0 do r_sum = 2.0
						data.sub_ratios[si]     = r_sum * (new_top_h / old_pair_h)
						data.sub_ratios[si + 1] = r_sum * (new_bot_h / old_pair_h)
					}
				} else {
					data.sub_resize_idx^ = -1
				}
			} else if data.sub_resize_idx^ == -1 {
				// Not dragging — detect hover on main↔sub separator.
				if ui.rect_contains(sep_hit, data.pointer.pos) {
					ui.push(buf, ui.Cmd_Rect_Filled{
						rect = {pos = {ctx.inner.pos.x, main_sub_border - 1}, size = {ctx.chart_w, 2}},
						color = ui.with_alpha(ui.COL_BLUE, 0.35),
					})
					if data.pointer.left_pressed {
						data.sub_resize_idx^ = -2  // -2 = dragging main↔sub
					}
				}
			}
		}

		// Render sub-plots via vtable dispatch and inter-subplot separators.
		cur_sub_y := sub_y
		sub_idx := 0

		// Build sub-plot rects from the layer stack (non-overlay layers).
		sub_rects: [5]ui.Rect
		sub_layer_indices: [5]int  // index into layers[]
		for li in 0 ..< layer_count {
			if layers[li].is_overlay do continue
			if sub_idx >= sub_count do break
			h := sub_heights[sub_idx]
			sub_rects[sub_idx] = ui.rect_xywh(ctx.inner.pos.x, cur_sub_y, ctx.chart_w, h)
			sub_layer_indices[sub_idx] = li
			cur_sub_y += h
			sub_idx += 1
		}

		// Dispatch each sub-plot layer via vtable.
		for si in 0 ..< sub_idx {
			li := sub_layer_indices[si]
			layer := &layers[li]
			if layer.render != nil {
				layer_ctx.sub_rect = sub_rects[si]
				layer.render(layer.data, buf, &layer_ctx)
			}
			// Update indicator probe for diagnostics.
			if data.indicator_probe != nil {
				lname := layer.display_name
				if lname == "RS" do data.indicator_probe.rsi_rendered = true
				if lname == "MC" do data.indicator_probe.macd_rendered = true
				if lname == "FN" do data.indicator_probe.funding_rendered = true
				if lname == "LQ" do data.indicator_probe.liq_rendered = true
				if lname == "TC" do data.indicator_probe.trade_counter_rendered = true
			}

			// Inter-subplot separator hover detection.
			if si < sub_idx - 1 && data.sub_resize_idx != nil && data.sub_resize_idx^ == -1 {
				sep_bottom := ui.rect_bottom(sub_rects[si])
				sub_sep_hit := ui.Rect{pos = {ctx.inner.pos.x, sep_bottom - SUB_SEP_HIT * 0.5}, size = {ctx.chart_w, SUB_SEP_HIT}}
				if ui.rect_contains(sub_sep_hit, data.pointer.pos) {
					ui.push(buf, ui.Cmd_Rect_Filled{
						rect = {pos = {ctx.inner.pos.x, sep_bottom - 1}, size = {ctx.chart_w, 2}},
						color = ui.with_alpha(ui.COL_BLUE, 0.30),
					})
					if data.pointer.left_pressed {
						data.sub_resize_idx^ = si
					}
				}
			}
		}

		// Double-click on main↔sub separator resets to auto.
		if data.sub_resize_idx != nil && data.sub_main_split != nil {
			if ui.rect_contains(sep_hit, data.pointer.pos) && data.pointer.left_pressed {
				// Detect double-click as two presses within ~20 frames isn't easy in immediate mode,
				// so we use right-click as reset instead for simplicity.
			}
		}
	}

	apply_candle_zoom_input(data, &ctx)

	// ">>" scroll-to-latest button (visible when scrolled away from newest candle).
	if data.scroll_x^ > 1 {
		stl_w := f32(28)
		stl_h := f32(18)
		stl_x := inner.pos.x + chart_w - stl_w - 4
		stl_y := inner.pos.y + ctx.price_h - stl_h - 4
		stl_rect := ui.rect_xywh(stl_x, stl_y, stl_w, stl_h)
		stl_hovered := ui.rect_contains(stl_rect, data.pointer.pos)
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = stl_rect,
			color = ui.with_alpha(ui.COL_BLUE, stl_hovered ? 0.5 : 0.3),
		})
		ui.push_text(buf,
			{stl_x + 5, stl_y + stl_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			">>", ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)
		if stl_hovered && data.pointer.left_pressed {
			data.scroll_x^ = 0
		}
	}

	ui.push(buf, ui.Cmd_Clip_Pop{})
}

@(private = "file")
draw_candle_price_grid :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	if ctx.price_range <= 0 do return
	step := compute_round_grid_step(ctx.price_range)
	if step <= 0 do return
	first := math.ceil(ctx.price_lo / step) * step
	count := 0
	for gp := first; gp < ctx.price_hi && count < 12; gp += step {
		count += 1
		t := f32((ctx.price_hi - gp) / ctx.price_range)
		gy := ctx.inner.pos.y + t * ctx.price_h
		ui.push(buf, ui.Cmd_Line{
			from      = {ctx.inner.pos.x, gy},
			to        = {ctx.inner.pos.x + ctx.chart_w, gy},
			color     = ui.with_alpha(ui.COL_WHITE, 0.06),
			thickness = 1,
		})
		grid_pbuf: [16]u8
		price_str := ui.format_price(grid_pbuf[:], gp, ui.auto_price_decimals(gp))
		ui.push_text(buf, {ctx.inner.pos.x + ctx.chart_w + 4, gy - 5}, price_str,
			ui.with_alpha(ui.COL_WHITE, 0.5), ui.FONT_SIZE_XS, .Mono)
	}
}

// Compute a "round" grid step for ~5-6 grid lines.
// Returns steps like 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25, 50, 100, 250, 500, 1000, 5000.
@(private = "file")
compute_round_grid_step :: proc(price_range: f64) -> f64 {
	if price_range <= 0 do return 1
	target := price_range / 6
	mag := math.pow(10.0, math.floor(math.log10(target)))
	if mag <= 0 do mag = 1
	normalized := target / mag
	if normalized < 1.5 do return mag
	if normalized < 3.5 do return mag * 2.5
	if normalized < 7.5 do return mag * 5
	return mag * 10
}

@(private = "file")
draw_candle_day_boundaries :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	if ctx.actual_visible <= 0 do return
	MS_PER_DAY :: i64(86_400_000)
	prev_day := i64(-1)
	for i in ctx.start_idx ..< ctx.end_idx {
		c := services.get_candle(ctx.store, i)
		day := c.window_start_ts / MS_PER_DAY
		if prev_day >= 0 && day != prev_day {
			slot := (i - ctx.start_idx) + ctx.slot_offset
			x := ctx.inner.pos.x + f32(slot) * ctx.slot_w
			// Dashed vertical line at day boundary.
			dash_len := f32(4)
			gap_len := f32(3)
			y := ctx.inner.pos.y
			for y < ctx.inner.pos.y + ctx.price_h {
				y_end := min(y + dash_len, ctx.inner.pos.y + ctx.price_h)
				ui.push(buf, ui.Cmd_Line{
					from      = {x, y},
					to        = {x, y_end},
					color     = ui.with_alpha(ui.COL_WHITE, 0.15),
					thickness = 1,
				})
				y += dash_len + gap_len
			}
			// Date label (MM/DD) below chart area.
			m, d := unix_ms_to_mday(c.window_start_ts)
			date_buf: [8]u8
			date_str := fmt.bprintf(date_buf[:], "%02d/%02d", m, d)
			ui.push_text(buf, {x + 2, ctx.inner.pos.y + ctx.chart_h + 14}, date_str,
				ui.with_alpha(ui.COL_WHITE, 0.5), ui.FONT_SIZE_XS, .Mono)
		}
		prev_day = day
	}
}

// Convert unix milliseconds to (month, day) using civil_from_days algorithm.
@(private = "file")
unix_ms_to_mday :: proc(ts_ms: i64) -> (month: int, day: int) {
	z := ts_ms / 86_400_000 + 719468
	era: i64
	if z >= 0 {
		era = z / 146097
	} else {
		era = (z - 146096) / 146097
	}
	doe := z - era * 146097
	yoe := (doe - doe / 1461 + doe / 36524 - doe / 146097) / 365
	doy := doe - (365 * yoe + yoe / 4 - yoe / 100)
	mp := (5 * doy + 2) / 153
	day = int(doy - (153 * mp + 2) / 5 + 1)
	if mp < 10 {
		month = int(mp + 3)
	} else {
		month = int(mp - 9)
	}
	return
}

@(private = "file")
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
		})
	}

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

@(private = "file")
draw_candle_bars :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	switch ctx.chart_type {
	case .Line:
		draw_candle_bars_line(buf, ctx)
	case .Heiken_Ashi:
		draw_candle_bars_ha(buf, ctx)
	case .Candlesticks:
		draw_candle_bars_ohlc(buf, ctx)
	case .Footprint:
		draw_candle_bars_footprint(buf, ctx, ctx.footprint_store)
	case .Footprint_Delta:
		draw_candle_bars_footprint_delta(buf, ctx, ctx.footprint_store)
	}
}

@(private = "file")
draw_candle_bars_ohlc :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	line_only_mode := ctx.body_w < CANDLE_BODY_LINE_ONLY_MIN_W
	for i in ctx.start_idx ..< ctx.end_idx {
		c := services.get_candle(ctx.store, i)
		slot := (i - ctx.start_idx) + ctx.slot_offset
		cx := ctx.inner.pos.x + f32(slot) * ctx.slot_w + ctx.slot_w * 0.5

		bullish := c.close >= c.open
		color := bullish ? ui.COL_GREEN : ui.COL_RED

		y_high  := ctx.inner.pos.y + f32((ctx.price_hi - c.high) / ctx.price_range) * ctx.price_h
		y_low   := ctx.inner.pos.y + f32((ctx.price_hi - c.low) / ctx.price_range) * ctx.price_h
		y_open  := ctx.inner.pos.y + f32((ctx.price_hi - c.open) / ctx.price_range) * ctx.price_h
		y_close := ctx.inner.pos.y + f32((ctx.price_hi - c.close) / ctx.price_range) * ctx.price_h

		body_top := min(y_open, y_close)
		body_bot := max(y_open, y_close)
		body_height := max(body_bot - body_top, 1)

		ui.push(buf, ui.Cmd_Line{
			from      = {cx, y_high},
			to        = {cx, y_low},
			color     = color,
			thickness = 1,
		})

		if line_only_mode {
			ui.push(buf, ui.Cmd_Line{
				from      = {cx, body_top},
				to        = {cx, body_top + body_height},
				color     = color,
				thickness = 2,
			})
		} else {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {cx - ctx.body_w * 0.5, body_top}, size = {ctx.body_w, body_height}},
				color = color,
			})
		}

		draw_candle_volume_bar(buf, ctx, i, cx)
	}
}

@(private = "file")
draw_candle_bars_line :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	prev_x := f32(0)
	prev_y := f32(0)
	has_prev := false

	for i in ctx.start_idx ..< ctx.end_idx {
		c := services.get_candle(ctx.store, i)
		slot := (i - ctx.start_idx) + ctx.slot_offset
		cx := ctx.inner.pos.x + f32(slot) * ctx.slot_w + ctx.slot_w * 0.5
		cy := ctx.inner.pos.y + f32((ctx.price_hi - c.close) / ctx.price_range) * ctx.price_h

		if has_prev {
			ui.push(buf, ui.Cmd_Line{
				from      = {prev_x, prev_y},
				to        = {cx, cy},
				color     = ui.COL_ACCENT_CYAN,
				thickness = 1,
			})
		}

		// Filled gradient area below line.
		if has_prev {
			fill_top := min(prev_y, cy)
			fill_bot := ctx.inner.pos.y + ctx.price_h
			if fill_bot > fill_top {
				ui.push(buf, ui.Cmd_Rect_Filled{
					rect  = {pos = {prev_x, fill_top}, size = {cx - prev_x, fill_bot - fill_top}},
					color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.05),
				})
			}
		}

		prev_x = cx
		prev_y = cy
		has_prev = true

		draw_candle_volume_bar(buf, ctx, i, cx)
	}
}

@(private = "file")
draw_candle_bars_ha :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	// Heiken Ashi formula:
	// HA_Close = (O + H + L + C) / 4
	// HA_Open  = (prev_HA_Open + prev_HA_Close) / 2
	// HA_High  = max(H, HA_Open, HA_Close)
	// HA_Low   = min(L, HA_Open, HA_Close)
	line_only_mode := ctx.body_w < CANDLE_BODY_LINE_ONLY_MIN_W

	ha_open := f64(0)
	ha_close := f64(0)
	ha_initialized := false

	// Start one candle early if possible to initialize HA.
	render_start := max(ctx.start_idx - 1, 0)

	for i in render_start ..< ctx.end_idx {
		if i >= ctx.store.count do break
		c := services.get_candle(ctx.store, i)

		new_ha_close := (c.open + c.high + c.low + c.close) / 4.0
		new_ha_open: f64
		if !ha_initialized {
			new_ha_open = (c.open + c.close) / 2.0
			ha_initialized = true
		} else {
			new_ha_open = (ha_open + ha_close) / 2.0
		}
		ha_high := max(c.high, max(new_ha_open, new_ha_close))
		ha_low := min(c.low, min(new_ha_open, new_ha_close))

		ha_open = new_ha_open
		ha_close = new_ha_close

		if i < ctx.start_idx do continue // skip pre-render candle

		slot := (i - ctx.start_idx) + ctx.slot_offset
		cx := ctx.inner.pos.x + f32(slot) * ctx.slot_w + ctx.slot_w * 0.5

		bullish := ha_close >= ha_open
		color := bullish ? ui.COL_GREEN : ui.COL_RED

		y_high  := ctx.inner.pos.y + f32((ctx.price_hi - ha_high) / ctx.price_range) * ctx.price_h
		y_low   := ctx.inner.pos.y + f32((ctx.price_hi - ha_low) / ctx.price_range) * ctx.price_h
		y_open  := ctx.inner.pos.y + f32((ctx.price_hi - ha_open) / ctx.price_range) * ctx.price_h
		y_close := ctx.inner.pos.y + f32((ctx.price_hi - ha_close) / ctx.price_range) * ctx.price_h

		body_top := min(y_open, y_close)
		body_bot := max(y_open, y_close)
		body_height := max(body_bot - body_top, 1)

		ui.push(buf, ui.Cmd_Line{
			from      = {cx, y_high},
			to        = {cx, y_low},
			color     = color,
			thickness = 1,
		})

		if line_only_mode {
			ui.push(buf, ui.Cmd_Line{
				from      = {cx, body_top},
				to        = {cx, body_top + body_height},
				color     = color,
				thickness = 2,
			})
		} else {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {cx - ctx.body_w * 0.5, body_top}, size = {ctx.body_w, body_height}},
				color = color,
			})
		}

		draw_candle_volume_bar(buf, ctx, i, cx)
	}
}

// Shared volume bar rendering for all chart types.
// Stacked buy/sell split when buy_vol/sell_vol are available.
@(private = "file")
draw_candle_volume_bar :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context, i: int, cx: f32) {
	if !ctx.show_vol || ctx.vol_max <= 0 do return
	c := services.get_candle(ctx.store, i)
	if c.volume <= 0 do return
	vol_pct := f32(c.volume / ctx.vol_max)
	bar_h := vol_pct * ctx.vol_h
	vol_y := ctx.inner.pos.y + ctx.price_h + ctx.vol_h - bar_h
	// Gradient alpha: scale from 0.15 (below avg) to 0.55 (3x avg or more).
	vol_ratio := clamp(f32(c.volume / max(ctx.vol_avg, 1)), 0, 3)
	vol_alpha := f32(0.15) + vol_ratio * (f32(0.55) - f32(0.15)) / f32(3)
	bar_x := cx - ctx.body_w * 0.5

	// Stacked buy/sell when data is available.
	total_bs := c.buy_vol + c.sell_vol
	if total_bs > 0 {
		buy_frac := f32(c.buy_vol / total_bs)
		buy_h := bar_h * buy_frac
		sell_h := bar_h - buy_h
		// Sell (red) on top, buy (green) on bottom.
		if sell_h > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {bar_x, vol_y}, size = {ctx.body_w, sell_h}},
				color = ui.with_alpha(ui.COL_RED, vol_alpha),
			})
		}
		if buy_h > 0.5 {
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {bar_x, vol_y + sell_h}, size = {ctx.body_w, buy_h}},
				color = ui.with_alpha(ui.COL_GREEN, vol_alpha),
			})
		}
		return
	}

	// Fallback: single color based on candle direction.
	bullish := c.close >= c.open
	vol_color := bullish ? ui.with_alpha(ui.COL_GREEN, vol_alpha) : ui.with_alpha(ui.COL_RED, vol_alpha)
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {bar_x, vol_y}, size = {ctx.body_w, bar_h}},
		color = vol_color,
	})
}

@(private = "file")
draw_candle_volume_separator :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	if !ctx.show_vol do return
	sep_y := ctx.inner.pos.y + ctx.price_h
	ui.push(buf, ui.Cmd_Line{
		from      = {ctx.inner.pos.x, sep_y},
		to        = {ctx.inner.pos.x + ctx.chart_w, sep_y},
		color     = ui.with_alpha(ui.COL_WHITE, 0.1),
		thickness = 1,
	})

	// Volume average line — dashed horizontal at vol_avg level.
	if ctx.vol_avg > 0 && ctx.vol_max > 0 && ctx.vol_h > 4 {
		avg_ratio := f32(ctx.vol_avg / ctx.vol_max)
		if avg_ratio > 0.01 && avg_ratio < 0.99 {
			avg_y := sep_y + ctx.vol_h * (1.0 - avg_ratio)
			dash := f32(4)
			gap := f32(4)
			x := ctx.inner.pos.x
			for x < ctx.inner.pos.x + ctx.chart_w {
				x_end := min(x + dash, ctx.inner.pos.x + ctx.chart_w)
				ui.push(buf, ui.Cmd_Line{
					from      = {x, avg_y},
					to        = {x_end, avg_y},
					color     = ui.with_alpha(ui.COL_WHITE, 0.18),
					thickness = 1,
				})
				x += dash + gap
			}
		}
	}
}

@(private = "file")
draw_candle_current_price :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	if ctx.end_idx <= 0 do return

	latest := services.get_candle(ctx.store, ctx.end_idx - 1)
	curr_y := ctx.inner.pos.y + f32((ctx.price_hi - latest.close) / ctx.price_range) * ctx.price_h
	if curr_y < ctx.inner.pos.y || curr_y > ctx.inner.pos.y + ctx.price_h do return

	dash_len := f32(6)
	gap_len := f32(4)
	x := ctx.inner.pos.x
	for x < ctx.inner.pos.x + ctx.chart_w {
		x_end := min(x + dash_len, ctx.inner.pos.x + ctx.chart_w)
		ui.push(buf, ui.Cmd_Line{
			from      = {x, curr_y},
			to        = {x_end, curr_y},
			color     = ui.COL_YELLOW_ACCENT,
			thickness = 1,
		})
		x += dash_len + gap_len
	}

	curr_pbuf: [16]u8
	price_str := ui.format_price(curr_pbuf[:], latest.close, ui.auto_price_decimals(latest.close))
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {ctx.inner.pos.x + ctx.chart_w + 1, curr_y - 8}, size = {Y_AXIS_WIDTH - 2, 16}},
		color = ui.COL_YELLOW_ACCENT,
	})
	ui.push_text(buf, {ctx.inner.pos.x + ctx.chart_w + 4, curr_y + 4}, price_str,
		ui.COL_BLACK, ui.FONT_SIZE_XS, .Mono)

	if data.now_ms <= 0 || data.timeframe_ms <= 0 do return

	next_close_ms := latest.window_start_ts + data.timeframe_ms
	remaining_ms := next_close_ms - data.now_ms
	if remaining_ms < 0 do remaining_ms = 0
	remaining_sec := remaining_ms / 1000
	cd_min := remaining_sec / 60
	cd_sec := remaining_sec % 60
	cd_buf: [8]u8
	cd_str := fmt.bprintf(cd_buf[:], "%02d:%02d", cd_min, cd_sec)
	cd_y := curr_y + 12
	if cd_y + 16 > ctx.inner.pos.y + ctx.price_h do return

	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {ctx.inner.pos.x + ctx.chart_w + 1, cd_y - 2}, size = {Y_AXIS_WIDTH - 2, 14}},
		color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.7),
	})
	ui.push_text(buf, {ctx.inner.pos.x + ctx.chart_w + 4, cd_y + 8}, cd_str,
		ui.COL_BLACK, ui.FONT_SIZE_XS, .Mono)
}

@(private = "file")
draw_candle_high_low_labels :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	if ctx.end_idx <= ctx.start_idx || ctx.price_range <= 0 do return
	if ctx.raw_high <= ctx.raw_low do return

	// Skip if latest close is at the extreme (current price label already covers it).
	latest := services.get_candle(ctx.store, ctx.end_idx - 1)
	high_matches_close := math.abs(ctx.raw_high - latest.close) < ctx.price_range * 0.005
	low_matches_close := math.abs(ctx.raw_low - latest.close) < ctx.price_range * 0.005

	label_x := ctx.inner.pos.x + ctx.chart_w + 4

	if !high_matches_close {
		high_y := ctx.inner.pos.y + f32((ctx.price_hi - ctx.raw_high) / ctx.price_range) * ctx.price_h
		if high_y >= ctx.inner.pos.y && high_y <= ctx.inner.pos.y + ctx.price_h {
			// Dashed leader line from candle to right margin.
			high_slot := (ctx.raw_high_idx - ctx.start_idx) + ctx.slot_offset
			cx := ctx.inner.pos.x + f32(high_slot) * ctx.slot_w + ctx.slot_w * 0.5
			dash := f32(4)
			gap := f32(3)
			x := cx
			for x < ctx.inner.pos.x + ctx.chart_w {
				x_end := min(x + dash, ctx.inner.pos.x + ctx.chart_w)
				ui.push(buf, ui.Cmd_Line{
					from      = {x, high_y},
					to        = {x_end, high_y},
					color     = ui.with_alpha(ui.COL_GREEN, 0.35),
					thickness = 1,
				})
				x += dash + gap
			}
			// Price label in right margin.
			pbuf: [16]u8
			price_str := ui.format_price(pbuf[:], ctx.raw_high, ui.auto_price_decimals(ctx.raw_high))
			ui.push_text(buf, {label_x, high_y - 5}, price_str,
				ui.with_alpha(ui.COL_GREEN, 0.7), ui.FONT_SIZE_XS, .Mono)
			// Small "H" marker.
			ui.push_text(buf, {label_x - 10, high_y - 5}, "H",
				ui.with_alpha(ui.COL_GREEN, 0.5), ui.FONT_SIZE_XS, .Mono)
		}
	}

	if !low_matches_close {
		low_y := ctx.inner.pos.y + f32((ctx.price_hi - ctx.raw_low) / ctx.price_range) * ctx.price_h
		if low_y >= ctx.inner.pos.y && low_y <= ctx.inner.pos.y + ctx.price_h {
			low_slot := (ctx.raw_low_idx - ctx.start_idx) + ctx.slot_offset
			cx := ctx.inner.pos.x + f32(low_slot) * ctx.slot_w + ctx.slot_w * 0.5
			dash := f32(4)
			gap := f32(3)
			x := cx
			for x < ctx.inner.pos.x + ctx.chart_w {
				x_end := min(x + dash, ctx.inner.pos.x + ctx.chart_w)
				ui.push(buf, ui.Cmd_Line{
					from      = {x, low_y},
					to        = {x_end, low_y},
					color     = ui.with_alpha(ui.COL_RED, 0.35),
					thickness = 1,
				})
				x += dash + gap
			}
			pbuf: [16]u8
			price_str := ui.format_price(pbuf[:], ctx.raw_low, ui.auto_price_decimals(ctx.raw_low))
			ui.push_text(buf, {label_x, low_y - 5}, price_str,
				ui.with_alpha(ui.COL_RED, 0.7), ui.FONT_SIZE_XS, .Mono)
			ui.push_text(buf, {label_x - 10, low_y - 5}, "L",
				ui.with_alpha(ui.COL_RED, 0.5), ui.FONT_SIZE_XS, .Mono)
		}
	}
}

@(private = "file")
draw_candle_crosshair :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	state := data.crosshair
	if state == nil do return

	mx := data.input.mouse.pos.x
	my := data.input.mouse.pos.y
	chart_left := ctx.inner.pos.x
	chart_right := ctx.inner.pos.x + ctx.chart_w
	chart_top := ctx.inner.pos.y
	chart_bot := ctx.inner.pos.y + ctx.price_h

	in_chart := mx >= chart_left && mx <= chart_right && my >= chart_top && my <= chart_bot
	state.active = in_chart
	state.mouse_pos = {mx, my}
	state.hovered_idx = -1

	if !in_chart do return

	// Price at cursor Y.
	y_pct := f64(my - chart_top) / f64(ctx.price_h)
	state.price_at_y = ctx.price_hi - y_pct * ctx.price_range

	// Find hovered candle slot.
	rel_x := mx - chart_left
	slot := int(rel_x / ctx.slot_w) - ctx.slot_offset
	candle_idx := ctx.start_idx + slot
	if candle_idx >= ctx.start_idx && candle_idx < ctx.end_idx {
		state.hovered_idx = candle_idx
	}

	// Vertical crosshair line (dashed).
	dash_len := f32(4)
	gap_len := f32(3)
	y := chart_top
	for y < chart_bot {
		y_end := min(y + dash_len, chart_bot)
		ui.push(buf, ui.Cmd_Line{
			from      = {mx, y},
			to        = {mx, y_end},
			color     = ui.COL_CROSS_HAIR,
			thickness = 1,
		})
		y += dash_len + gap_len
	}

	// Horizontal crosshair line (dashed).
	x := chart_left
	for x < chart_right {
		x_end := min(x + dash_len, chart_right)
		ui.push(buf, ui.Cmd_Line{
			from      = {x, my},
			to        = {x_end, my},
			color     = ui.COL_CROSS_HAIR,
			thickness = 1,
		})
		x += dash_len + gap_len
	}

	// Y-axis price label at crosshair level.
	cross_pbuf: [16]u8
	price_str := ui.format_price(cross_pbuf[:], state.price_at_y, ui.auto_price_decimals(state.price_at_y))
	label_w := data.text.measure(ui.FONT_SIZE_XS, price_str).x + 8
	label_h := f32(14)
	label_x := chart_right + 1
	label_y := my - label_h * 0.5
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {label_x, label_y}, size = {label_w, label_h}},
		color = ui.with_alpha(ui.COL_CROSS_HAIR, 0.8),
	})
	ui.push_text(buf, {label_x + 4, label_y + label_h - 3}, price_str,
		ui.COL_BLACK, ui.FONT_SIZE_XS, .Mono)

	// X-axis time label at crosshair position.
	if state.hovered_idx >= 0 && state.hovered_idx < ctx.store.count {
		c := services.get_candle(ctx.store, state.hovered_idx)
		ts_sec := c.window_start_ts / 1000
		hours := (ts_sec / 3600) % 24
		mins := (ts_sec / 60) % 60
		cross_tbuf: [12]u8
		time_str: string
		if ctx.timeframe_ms > 0 && ctx.timeframe_ms < 60_000 {
			secs := ts_sec % 60
			time_str = fmt.bprintf(cross_tbuf[:], "%02d:%02d:%02d", hours, mins, secs)
		} else {
			time_str = fmt.bprintf(cross_tbuf[:], "%02d:%02d", hours, mins)
		}
		time_w := data.text.measure(ui.FONT_SIZE_XS, time_str).x + 8
		time_h := f32(14)
		time_x := mx - time_w * 0.5
		time_y := chart_bot + 2
		ui.push(buf, ui.Cmd_Rect_Filled{
			rect  = {pos = {time_x, time_y}, size = {time_w, time_h}},
			color = ui.with_alpha(ui.COL_CROSS_HAIR, 0.8),
		})
		ui.push_text(buf, {time_x + 4, time_y + time_h - 3}, time_str,
			ui.COL_BLACK, ui.FONT_SIZE_XS, .Mono)
	}

	// OHLCV tooltip for hovered candle.
	if state.hovered_idx >= 0 && state.hovered_idx < ctx.store.count {
		c := services.get_candle(ctx.store, state.hovered_idx)
		decs := ui.auto_price_decimals(c.close)
		tip_o: [16]u8
		tip_h: [16]u8
		tip_l: [16]u8
		tip_c: [16]u8
		tip: ui.Tooltip_Data
		tip.lines[0] = {label = "O: ", value = ui.format_price(tip_o[:], c.open, decs),  color = ui.COL_TEXT_PRIMARY}
		tip.lines[1] = {label = "H: ", value = ui.format_price(tip_h[:], c.high, decs),  color = ui.COL_TEXT_PRIMARY}
		tip.lines[2] = {label = "L: ", value = ui.format_price(tip_l[:], c.low, decs),   color = ui.COL_TEXT_PRIMARY}
		tip.lines[3] = {label = "C: ", value = ui.format_price(tip_c[:], c.close, decs), color = c.close >= c.open ? ui.COL_GREEN : ui.COL_RED}
		tip_v: [16]u8
		tip.lines[4] = {label = "V: ", value = fmt.bprintf(tip_v[:], "%.1f", c.volume), color = ui.COL_TEXT_SECONDARY}
		tip.count = 5
		ui.draw_tooltip(buf, {mx, my}, tip, data.text.measure, ctx.inner)
	}
}

// Sync crosshair: dimmed horizontal line from another chart's crosshair price.
@(private = "file")
draw_candle_sync_crosshair :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	if !data.sync_active || data.sync_price <= 0 do return
	// Don't draw sync line if local crosshair is active (avoid clutter).
	if data.crosshair != nil && data.crosshair.active do return
	if ctx.price_range <= 0 do return

	y_pct := f32((ctx.price_hi - data.sync_price) / ctx.price_range)
	sync_y := ctx.inner.pos.y + y_pct * ctx.price_h
	if sync_y < ctx.inner.pos.y || sync_y > ctx.inner.pos.y + ctx.price_h do return

	// Dimmed dashed horizontal line.
	dash_len := f32(6)
	gap_len := f32(4)
	chart_right := ctx.inner.pos.x + ctx.chart_w
	x := ctx.inner.pos.x
	for x < chart_right {
		x_end := min(x + dash_len, chart_right)
		ui.push(buf, ui.Cmd_Line{
			from      = {x, sync_y},
			to        = {x_end, sync_y},
			color     = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.35),
			thickness = 1,
		})
		x += dash_len + gap_len
	}

	// Small price label on the right.
	cross_pbuf: [16]u8
	price_str := ui.format_price(cross_pbuf[:], data.sync_price, ui.auto_price_decimals(data.sync_price))
	label_w := data.text.measure(ui.FONT_SIZE_XS, price_str).x + 6
	label_h := f32(12)
	label_x := chart_right + 1
	label_y := sync_y - label_h * 0.5
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {label_x, label_y}, size = {label_w, label_h}},
		color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.25),
	})
	ui.push_text(buf, {label_x + 3, label_y + label_h - 2}, price_str,
		ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Mono)
}

// Persistent OHLCV data bar at chart top-left (shows hovered or latest candle).
@(private = "file")
draw_candle_ohlcv_bar :: proc(buf: ^ui.Command_Buffer, data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	if ctx.store == nil || ctx.store.count <= 0 do return

	// Use hovered candle if crosshair active, otherwise latest.
	idx := ctx.end_idx - 1
	if data.crosshair != nil && data.crosshair.active && data.crosshair.hovered_idx >= 0 {
		idx = data.crosshair.hovered_idx
	}
	if idx < 0 || idx >= ctx.store.count do return

	c := services.get_candle(ctx.store, idx)
	decs := ui.auto_price_decimals(c.close)
	bullish := c.close >= c.open
	close_color := bullish ? ui.COL_GREEN : ui.COL_RED

	x := ctx.inner.pos.x + 4
	y := ctx.inner.pos.y + 14

	// Semi-transparent backdrop for readability.
	ui.push(buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {ctx.inner.pos.x, ctx.inner.pos.y}, size = {ctx.chart_w, 20}},
		color = ui.with_alpha(ui.COL_SURFACE_0, 0.7),
	})

	o_buf: [16]u8
	o_str := ui.format_price(o_buf[:], c.open, decs)
	ui.push_text(buf, {x, y}, "O", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, "O").x + 2
	ui.push_text(buf, {x, y}, o_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, o_str).x + 6

	h_buf: [16]u8
	h_str := ui.format_price(h_buf[:], c.high, decs)
	ui.push_text(buf, {x, y}, "H", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, "H").x + 2
	ui.push_text(buf, {x, y}, h_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, h_str).x + 6

	l_buf: [16]u8
	l_str := ui.format_price(l_buf[:], c.low, decs)
	ui.push_text(buf, {x, y}, "L", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, "L").x + 2
	ui.push_text(buf, {x, y}, l_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, l_str).x + 6

	c_buf: [16]u8
	c_str := ui.format_price(c_buf[:], c.close, decs)
	ui.push_text(buf, {x, y}, "C", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, "C").x + 2
	ui.push_text(buf, {x, y}, c_str, close_color, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, c_str).x + 6

	v_buf: [16]u8
	v_str := fmt.bprintf(v_buf[:], "%.1f", c.volume)
	ui.push_text(buf, {x, y}, "V", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, "V").x + 2
	ui.push_text(buf, {x, y}, v_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	x += data.text.measure(ui.FONT_SIZE_XS, v_str).x + 6

	// Cross-indicator readout: append active indicator values when crosshair hovers a candle.
	is_hover := data.crosshair != nil && data.crosshair.active && data.crosshair.hovered_idx >= 0
	if is_hover && idx < ctx.store.count {
		ind_color := ui.COL_TEXT_SECONDARY

		// MA (EMA9).
		if data.show_ma && data.ma_periods[0] > 0 {
			ema_val := readout_ema_at(ctx.store, data.ma_periods[0], idx)
			if ema_val > 0 {
				ema_buf: [24]u8
				ema_str := fmt.bprintf(ema_buf[:], "EMA%d:", data.ma_periods[0])
				ui.push_text(buf, {x, y}, ema_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
				x += data.text.measure(ui.FONT_SIZE_XS, ema_str).x + 2
				ev_buf: [16]u8
				ev_str := ui.format_price(ev_buf[:], ema_val, decs)
				ui.push_text(buf, {x, y}, ev_str, {0.98, 0.85, 0.2, 0.9}, ui.FONT_SIZE_XS, .Mono)
				x += data.text.measure(ui.FONT_SIZE_XS, ev_str).x + 6
			}
		}

		// RSI.
		if data.show_rsi && data.rsi_period > 0 {
			rsi_val := readout_rsi_at(ctx.store, data.rsi_period, idx)
			if rsi_val >= 0 {
				ui.push_text(buf, {x, y}, "RSI:", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
				x += data.text.measure(ui.FONT_SIZE_XS, "RSI:").x + 2
				rsi_buf: [8]u8
				rsi_str := fmt.bprintf(rsi_buf[:], "%.1f", rsi_val)
				rsi_color := rsi_val >= 70 ? ui.COL_RED : (rsi_val <= 30 ? ui.COL_GREEN : ind_color)
				ui.push_text(buf, {x, y}, rsi_str, rsi_color, ui.FONT_SIZE_XS, .Mono)
				x += data.text.measure(ui.FONT_SIZE_XS, rsi_str).x + 6
			}
		}

		// MACD.
		if data.show_macd && data.macd_fast > 0 && data.macd_slow > 0 {
			macd_val := readout_macd_at(ctx.store, data.macd_fast, data.macd_slow, idx)
			ui.push_text(buf, {x, y}, "MACD:", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			x += data.text.measure(ui.FONT_SIZE_XS, "MACD:").x + 2
			sign := macd_val >= 0 ? "+" : ""
			macd_buf: [16]u8
			macd_str := fmt.bprintf(macd_buf[:], "%s%.1f", sign, macd_val)
			macd_color := macd_val >= 0 ? ui.COL_GREEN : ui.COL_RED
			ui.push_text(buf, {x, y}, macd_str, macd_color, ui.FONT_SIZE_XS, .Mono)
			x += data.text.measure(ui.FONT_SIZE_XS, macd_str).x + 6
		}
	}
}

@(private = "file")
draw_candle_time_axis_labels :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	if ctx.actual_visible <= 0 do return

	label_count := min(5, ctx.actual_visible)
	step := ctx.actual_visible / label_count
	if step < 1 do step = 1

	sub_minute := ctx.timeframe_ms > 0 && ctx.timeframe_ms < 60_000
	for j := 0; j < ctx.actual_visible; j += step {
		idx := ctx.start_idx + j
		if idx >= ctx.end_idx do break
		c := services.get_candle(ctx.store, idx)
		ts_sec := c.window_start_ts / 1000
		hours := (ts_sec / 3600) % 24
		mins := (ts_sec / 60) % 60
		axis_tbuf: [12]u8
		time_str: string
		if sub_minute {
			secs := ts_sec % 60
			time_str = fmt.bprintf(axis_tbuf[:], "%02d:%02d:%02d", hours, mins, secs)
		} else {
			time_str = fmt.bprintf(axis_tbuf[:], "%02d:%02d", hours, mins)
		}
		slot := j + ctx.slot_offset
		lx := ctx.inner.pos.x + f32(slot) * ctx.slot_w + ctx.slot_w * 0.5 - 12
		ly := ctx.inner.pos.y + ctx.chart_h + 14
		ui.push_text(buf, {lx, ly}, time_str,
			ui.with_alpha(ui.COL_WHITE, 0.4), ui.FONT_SIZE_XS, .Mono)
	}
}

@(private = "file")
draw_candle_count_indicator :: proc(buf: ^ui.Command_Buffer, ctx: ^Candle_Render_Context) {
	cnt_buf: [16]u8
	count_str := fmt.bprintf(cnt_buf[:], "%d candles", ctx.store.count)
	ui.push_text(buf, {ctx.inner.pos.x + ctx.chart_w - 80, ctx.inner.pos.y + 4}, count_str,
		ui.with_alpha(ui.COL_WHITE, 0.35), ui.FONT_SIZE_XS, .Mono)
}

@(private = "file")
apply_candle_zoom_input :: proc(data: Candle_Widget_Data, ctx: ^Candle_Render_Context) {
	mx := data.input.mouse.pos.x
	my := data.input.mouse.pos.y
	mouse_in_chart := mx >= ctx.inner.pos.x && mx <= ctx.inner.pos.x + ctx.inner.size.x &&
		my >= ctx.inner.pos.y && my <= ctx.inner.pos.y + ctx.inner.size.y
	if !mouse_in_chart do return

	visible_count := max(int(ctx.zoom), 1)
	max_scroll := f32(max(ctx.store.count - visible_count, 0))
	mouse_x_ratio := clamp((mx - ctx.inner.pos.x) / max(ctx.chart_w, 1), f32(0), f32(0.999))
	anchor_idx := ctx.start_idx
	if ctx.actual_visible > 0 {
		anchor_rel := clamp(int(mouse_x_ratio * f32(ctx.actual_visible)), 0, ctx.actual_visible - 1)
		anchor_idx = ctx.start_idx + anchor_rel
	}

	// Pan horizontally by dragging the chart with left mouse button.
	if data.input.mouse.buttons[.Left] {
		drag_slots := data.input.mouse.delta.x / max(ctx.slot_w, 1)
		if drag_slots != 0 {
			data.scroll_x^ = clamp(data.scroll_x^ + drag_slots, 0, max_scroll)
		}
	}

	// Shift + wheel (or horizontal wheel) also pans in candle slots.
	pan_wheel := f32(0)
	if data.input.modifiers.shift {
		pan_wheel = -data.input.mouse.scroll.y
	} else if data.input.mouse.scroll.x != 0 {
		pan_wheel = data.input.mouse.scroll.x
	}
	if pan_wheel != 0 {
		data.scroll_x^ = clamp(data.scroll_x^ + pan_wheel * 3, 0, max_scroll)
	}

	// Vertical wheel keeps zoom behavior (unless Shift is held for pan).
	wheel := data.input.mouse.scroll.y
	if data.input.modifiers.shift do wheel = 0
	if wheel == 0 do return
	zoom_delta := -wheel * ctx.zoom * 0.1
	data.zoom_level^ = clamp(ctx.zoom + zoom_delta, CANDLE_MIN_ZOOM, CANDLE_MAX_ZOOM)

	next_visible := max(int(data.zoom_level^), 1)
	next_start_max := max(ctx.store.count - next_visible, 0)
	next_start := anchor_idx - int(mouse_x_ratio * f32(next_visible))
	next_start = clamp(next_start, 0, next_start_max)
	next_end := next_start + next_visible
	data.scroll_x^ = f32(max(ctx.store.count - next_end, 0))
	next_max_scroll := f32(max(ctx.store.count - next_visible, 0))
	data.scroll_x^ = clamp(data.scroll_x^, 0, next_max_scroll)
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

		// Heatmap unix is window_end_ts in seconds. Align to candle window_start_ts.
		snap_end_ms := s.unix * 1000
		aligned_ts := ((snap_end_ms - 1) / tf_ms) * tf_ms

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

	// Stable intensity normalization using global max (no per-frame flicker).
	global_max := data.store.global_max_size
	if global_max <= 0 do return

	min_visible_pct := clamp(data.min_visible_pct, f64(0), f64(0.95))
	range_min := global_max * min_visible_pct

	cell_w := max(data.slot_w - 0.5, 1)
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

			intensity := heatmap_remap01(level.size, range_min, global_max)
			if intensity < min_intensity do continue

			center_y := data.inner.pos.y + f32((data.price_hi - level.price) / data.price_range) * data.price_h
			y := center_y - cell_h * 0.5
			if y < data.inner.pos.y do y = data.inner.pos.y
			max_y := data.inner.pos.y + data.price_h
			if y >= max_y do continue
			h := min(cell_h, max_y - y)
			if h <= 0 do continue

			col := candle_heatmap_gradient(intensity)
			alpha := min_alpha + intensity * (max_alpha - min_alpha)
			col = ui.with_alpha(col, alpha)
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect  = {pos = {x, y}, size = {cell_w, h}},
				color = col,
			})
		}
	}
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

@(private = "file")
candle_heatmap_gradient :: proc(t: f32) -> ui.Color {
	return ui.viridis_gradient(t)
}

@(private = "file")
heatmap_remap01 :: proc(v, lo, hi: f64) -> f32 {
	if hi <= lo {
		if v >= hi do return 1
		return 0
	}
	return clamp(f32((v - lo) / (hi - lo)), 0, 1)
}
