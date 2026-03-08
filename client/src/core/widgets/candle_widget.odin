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
	{min_visible_pct = 0.10, min_intensity = 0.02, min_alpha = 0.16, max_alpha = 0.55},
	{min_visible_pct = 0.18, min_intensity = 0.04, min_alpha = 0.22, max_alpha = 0.68},
	{min_visible_pct = 0.26, min_intensity = 0.06, min_alpha = 0.28, max_alpha = 0.80},
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
	active:              bool,   // true when mouse is over chart area
	mouse_pos:           ui.Vec2,
	hovered_idx:         int,    // candle index under cursor (-1 = none)
	price_at_y:          f64,    // price value at cursor Y
	last_yaxis_click_ms: i64,    // for double-click Y-axis auto-fit
}

Indicator_Render_Probe :: struct {
	rsi_enabled:           bool,
	macd_enabled:          bool,
	funding_enabled:       bool,
	liq_enabled:           bool,
	trade_counter_enabled: bool,
	cvd_enabled:           bool,  // S81
	delta_vol_enabled:     bool,  // S81
	rsi_rendered:          bool,
	macd_rendered:         bool,
	funding_rendered:      bool,
	liq_rendered:          bool,
	trade_counter_rendered: bool,
	cvd_rendered:          bool,  // S81
	delta_vol_rendered:    bool,  // S81
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
	signal_store:  ^services.Signal_Store,
	signal_subject_id: u64,
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

	// Empty state: show context-specific reason.
	if store == nil || store.count == 0 {
		msg: string
		#partial switch data.stream_state {
		case .Offline:
			msg = "Subscribing..."
		case .Desync:
			msg = "Resyncing stream..."
		case .Live, .Lag:
			if len(data.tf_label) > 0 {
				empty_buf: [48]u8
				msg = fmt.bprintf(empty_buf[:], "Waiting for %s candle data...", data.tf_label)
			} else {
				msg = "Waiting for candle data..."
			}
		}
		if len(msg) == 0 do msg = "Waiting for candle data..."
		msg_w := data.text.measure(ui.FONT_SIZE_SM, msg).x
		ui.push_text(buf,
			{inner.pos.x + (inner.size.x - msg_w) * 0.5, inner.pos.y + inner.size.y * 0.5},
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
	draw_candle_underlays(buf, data, &ctx)
	draw_signal_overlay(buf, Signal_Overlay_Data{
		store      = data.signal_store,
		subject_id = data.signal_subject_id,
		rect       = ui.rect_xywh(ctx.inner.pos.x + 6, ctx.inner.pos.y + 6, max(ctx.chart_w * 0.4, f32(120)), 18),
		text       = data.text,
	})
	sig_panel_w := min(f32(196), max(ctx.chart_w * 0.34, f32(132)))
	sig_panel_rect := ui.rect_xywh(ctx.inner.pos.x + ctx.chart_w - sig_panel_w - 6, ctx.inner.pos.y + 6, sig_panel_w, 84)
	signal_panel(buf, Signal_Panel_Data{
		store      = data.signal_store,
		subject_id = data.signal_subject_id,
		viewport   = sig_panel_rect,
		text       = data.text,
		max_rows   = 3,
	})

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
		layers[layer_count] = {render = bbands_layer_render, display_name = "BB", kind = .BBands, visible = true, is_overlay = true, data = &bb_ld}
		layer_count += 1
	}
	if data.show_ma {
		ma_ld.lines = MA_DEFAULT_LINES
		if data.ma_periods[0] > 0 do ma_ld.lines[0].period = data.ma_periods[0]
		if data.ma_periods[1] > 0 do ma_ld.lines[1].period = data.ma_periods[1]
		if data.ma_periods[2] > 0 do ma_ld.lines[2].period = data.ma_periods[2]
		layers[layer_count] = {render = ma_layer_render, display_name = "MA", kind = .MA, visible = true, is_overlay = true, data = &ma_ld}
		layer_count += 1
	}
	if data.show_vwap {
		vw_ld.config = VWAP_DEFAULT
		layers[layer_count] = {render = vwap_layer_render, display_name = "VW", kind = .VWAP, visible = true, is_overlay = true, data = &vw_ld}
		layer_count += 1
	}
	// --- Sub-plot layers ---
	if data.show_rsi {
		rsi_ld.config = RSI_DEFAULT
		rsi_ld.config.visible = true
		if data.rsi_period > 0 do rsi_ld.config.period = data.rsi_period
		layers[layer_count] = {render = rsi_layer_render, display_name = "RS", kind = .RSI, visible = true, is_overlay = false, data = &rsi_ld}
		layer_count += 1
	}
	if data.show_macd {
		macd_ld.config = MACD_DEFAULT
		macd_ld.config.visible = true
		if data.macd_fast > 0 do macd_ld.config.fast_period = data.macd_fast
		if data.macd_slow > 0 do macd_ld.config.slow_period = data.macd_slow
		if data.macd_signal > 0 do macd_ld.config.signal_period = data.macd_signal
		layers[layer_count] = {render = macd_layer_render, display_name = "MC", kind = .MACD, visible = true, is_overlay = false, data = &macd_ld}
		layer_count += 1
	}
	if data.show_funding {
		fund_ld.config = FUNDING_DEFAULT
		fund_ld.config.visible = true
		layers[layer_count] = {render = funding_layer_render, display_name = "FN", kind = .Funding, visible = true, is_overlay = false, data = &fund_ld}
		layer_count += 1
	}
	if data.show_liq {
		liq_ld.config = LIQ_DEFAULT
		liq_ld.config.visible = true
		layers[layer_count] = {render = liq_layer_render, display_name = "LQ", kind = .Liq, visible = true, is_overlay = false, data = &liq_ld}
		layer_count += 1
	}
	if data.show_trade_counter {
		tc_ld.config = TRADE_COUNTER_DEFAULT
		tc_ld.config.visible = true
		layers[layer_count] = {render = tc_layer_render, display_name = "TC", kind = .Trade_Counter, visible = true, is_overlay = false, data = &tc_ld}
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
	draw_candle_overlays(buf, data, &ctx)
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
			// Update indicator probe for diagnostics (dispatch on layer kind enum).
			if data.indicator_probe != nil {
				lk := layer.kind
				if lk == .RSI    do data.indicator_probe.rsi_rendered = true
				if lk == .MACD   do data.indicator_probe.macd_rendered = true
				if lk == .Funding do data.indicator_probe.funding_rendered = true
				if lk == .Liq    do data.indicator_probe.liq_rendered = true
				if lk == .Trade_Counter do data.indicator_probe.trade_counter_rendered = true
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
