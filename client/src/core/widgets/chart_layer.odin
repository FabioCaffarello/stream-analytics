package widgets

// Chart layer abstraction — extensible overlay/underlay system for candle chart.
// Each layer implements render + optional settings panel via vtable.
// PRD-0007 M3: vtable dispatch replaces direct indicator calls.

import "mr:services"
import "mr:ui"

MAX_CHART_LAYERS :: 12

Chart_Layer_Vtable :: struct {
	render:          proc(layer: rawptr, buf: ^ui.Command_Buffer, ctx: ^Chart_Layer_Context),
	render_settings: proc(layer: rawptr, buf: ^ui.Command_Buffer, rect: ui.Rect, pointer: ui.Pointer_Input),
}

Chart_Layer :: struct {
	using vtable: Chart_Layer_Vtable,
	display_name: string,
	kind:         Chart_Layer_Kind,
	visible:      bool,
	is_overlay:   bool,    // true = price-space overlay, false = sub-plot
	data:         rawptr,
}

// Layer kind enum for layer identification.
Chart_Layer_Kind :: enum u8 {
	MA,
	BBands,
	VWAP,
	RSI,
	MACD,
	Funding,
	Liq,
	Trade_Counter,
}

LAYER_SHORT_LABELS :: [8]string{"MA", "BB", "VW", "RS", "MC", "FN", "LQ", "TC"}

// Shared rendering context passed to all chart layers.
Chart_Layer_Context :: struct {
	chart_rect:   ui.Rect,
	price_min:    f64,
	price_max:    f64,
	price_range:  f64,
	candle_start: int,
	candle_count: int,
	slot_offset:  int,
	slot_width:   f32,
	price_height: f32,
	candles:      ^services.Candle_Store,
	timeframe_ms: i64,
	// Extended fields for sub-plot and stats-based indicators.
	sub_rect:     ui.Rect,                                       // allocated sub-plot rectangle (sub-plots only)
	stats_store:  ^services.Stats_Store,                         // stats data (funding, liq)
	measure:      proc(size: f32, text: string) -> ui.Vec2,      // text measurement
}

// Convert a price value to a Y pixel coordinate within the chart area.
chart_price_to_y :: proc(ctx: ^Chart_Layer_Context, price: f64) -> f32 {
	if ctx.price_range <= 0 do return ctx.chart_rect.pos.y
	return ctx.chart_rect.pos.y + f32((ctx.price_max - price) / ctx.price_range) * ctx.price_height
}

// Convert a candle index to its center X pixel coordinate.
chart_index_to_x :: proc(ctx: ^Chart_Layer_Context, idx: int) -> f32 {
	slot := (idx - ctx.candle_start) + ctx.slot_offset
	return ctx.chart_rect.pos.x + f32(slot) * ctx.slot_width + ctx.slot_width * 0.5
}

// Build a Chart_Layer_Context from the existing Candle_Render_Context.
chart_layer_context_from_candle :: proc(ctx: ^Candle_Render_Context) -> Chart_Layer_Context {
	return Chart_Layer_Context{
		chart_rect   = ctx.inner,
		price_min    = ctx.price_lo,
		price_max    = ctx.price_hi,
		price_range  = ctx.price_range,
		candle_start = ctx.start_idx,
		candle_count = ctx.end_idx - ctx.start_idx,
		slot_offset  = ctx.slot_offset,
		slot_width   = ctx.slot_w,
		price_height = ctx.price_h,
		candles      = ctx.store,
		timeframe_ms = ctx.timeframe_ms,
	}
}

// Binary search: find the candle slot index closest to a given timestamp (ms).
// Returns a fractional slot index for smooth positioning.
stats_ts_to_slot :: proc(ctx: ^Chart_Layer_Context, ts_ms: i64) -> f32 {
	if ctx.candles == nil || ctx.candle_count <= 0 do return 0
	start := ctx.candle_start
	count := ctx.candle_count
	// Binary search for the candle whose window contains ts_ms.
	lo := 0
	hi := count - 1
	for lo <= hi {
		mid := lo + (hi - lo) / 2
		c := services.get_candle(ctx.candles, start + mid)
		if ts_ms < c.window_start_ts {
			hi = mid - 1
		} else if ts_ms > c.window_end_ts {
			lo = mid + 1
		} else {
			// Timestamp falls within this candle's window.
			return f32(mid)
		}
	}
	// Clamp to range.
	return f32(clamp(lo, 0, count - 1))
}

// Convert a stats timestamp to an X pixel using binary search on candle slots.
stats_ts_to_x :: proc(ctx: ^Chart_Layer_Context, ts_ms: i64) -> f32 {
	slot_f := stats_ts_to_slot(ctx, ts_ms)
	slot := f32(ctx.slot_offset) + slot_f
	return ctx.chart_rect.pos.x + slot * ctx.slot_width + ctx.slot_width * 0.5
}

// ═══════════════════════════════════════════════════════════════
// Crosshair readout — compute indicator values at a specific candle index.
// ═══════════════════════════════════════════════════════════════

// Compute EMA value at candle index `idx` from the store.
readout_ema_at :: proc(store: ^services.Candle_Store, period: int, idx: int) -> f64 {
	if store == nil || period <= 0 || idx < 0 do return 0
	count := min(idx + 1, store.count)
	if count < period do return 0
	mult := 2.0 / f64(period + 1)
	ema := services.get_candle(store, 0).close
	for i in 1 ..< count {
		ema = services.get_candle(store, i).close * mult + ema * (1.0 - mult)
	}
	return ema
}

// Compute RSI value at candle index `idx`.
readout_rsi_at :: proc(store: ^services.Candle_Store, period: int, idx: int) -> f64 {
	if store == nil || period <= 0 || idx < period do return -1
	count := min(idx + 1, store.count)
	if count <= period do return -1
	avg_gain := f64(0)
	avg_loss := f64(0)
	for i in 1 ..= period {
		diff := services.get_candle(store, i).close - services.get_candle(store, i - 1).close
		if diff >= 0 do avg_gain += diff
		else do avg_loss -= diff
	}
	avg_gain /= f64(period)
	avg_loss /= f64(period)
	for i in period + 1 ..< count {
		diff := services.get_candle(store, i).close - services.get_candle(store, i - 1).close
		if diff >= 0 {
			avg_gain = (avg_gain * f64(period - 1) + diff) / f64(period)
			avg_loss = (avg_loss * f64(period - 1)) / f64(period)
		} else {
			avg_gain = (avg_gain * f64(period - 1)) / f64(period)
			avg_loss = (avg_loss * f64(period - 1) - diff) / f64(period)
		}
	}
	if avg_loss == 0 do return 100.0
	rs := avg_gain / avg_loss
	return 100.0 - 100.0 / (1.0 + rs)
}

// Compute MACD line value at candle index `idx`.
readout_macd_at :: proc(store: ^services.Candle_Store, fast, slow: int, idx: int) -> f64 {
	if store == nil || fast <= 0 || slow <= 0 || idx < slow do return 0
	fast_ema := readout_ema_at(store, fast, idx)
	slow_ema := readout_ema_at(store, slow, idx)
	return fast_ema - slow_ema
}

// ═══════════════════════════════════════════════════════════════
// Layer wrapper structs — each holds config, dispatches to render_* procs.
// ═══════════════════════════════════════════════════════════════

// --- MA overlay ---
MA_Layer_Data :: struct {
	lines: [3]MA_Line,
}
ma_layer_render :: proc(layer: rawptr, buf: ^ui.Command_Buffer, ctx: ^Chart_Layer_Context) {
	d := (^MA_Layer_Data)(layer)
	lines := d.lines
	render_ma_lines(buf, ctx, lines[:])
}

// --- BBands overlay ---
BBands_Layer_Data :: struct {
	config: BBands_Config,
}
bbands_layer_render :: proc(layer: rawptr, buf: ^ui.Command_Buffer, ctx: ^Chart_Layer_Context) {
	d := (^BBands_Layer_Data)(layer)
	render_bbands(buf, ctx, d.config)
}

// --- VWAP overlay ---
VWAP_Layer_Data :: struct {
	config: VWAP_Config,
}
vwap_layer_render :: proc(layer: rawptr, buf: ^ui.Command_Buffer, ctx: ^Chart_Layer_Context) {
	d := (^VWAP_Layer_Data)(layer)
	render_vwap(buf, ctx, d.config)
}

// --- RSI sub-plot ---
RSI_Layer_Data :: struct {
	config: RSI_Config,
}
rsi_layer_render :: proc(layer: rawptr, buf: ^ui.Command_Buffer, ctx: ^Chart_Layer_Context) {
	d := (^RSI_Layer_Data)(layer)
	render_rsi(buf, ctx.sub_rect, ctx, d.config, ctx.measure)
}

// --- MACD sub-plot ---
MACD_Layer_Data :: struct {
	config: MACD_Config,
}
macd_layer_render :: proc(layer: rawptr, buf: ^ui.Command_Buffer, ctx: ^Chart_Layer_Context) {
	d := (^MACD_Layer_Data)(layer)
	render_macd(buf, ctx.sub_rect, ctx, d.config, ctx.measure)
}

// --- Funding sub-plot ---
Funding_Layer_Data :: struct {
	config: Funding_Config,
}
funding_layer_render :: proc(layer: rawptr, buf: ^ui.Command_Buffer, ctx: ^Chart_Layer_Context) {
	d := (^Funding_Layer_Data)(layer)
	render_funding(buf, ctx.sub_rect, ctx, ctx.stats_store, d.config, ctx.measure)
}

// --- Liq sub-plot ---
Liq_Layer_Data :: struct {
	config: Liq_Config,
}
liq_layer_render :: proc(layer: rawptr, buf: ^ui.Command_Buffer, ctx: ^Chart_Layer_Context) {
	d := (^Liq_Layer_Data)(layer)
	render_liq(buf, ctx.sub_rect, ctx, ctx.stats_store, d.config, ctx.measure)
}

// --- Trade Counter sub-plot ---
TC_Layer_Data :: struct {
	config: Trade_Counter_Config,
}
tc_layer_render :: proc(layer: rawptr, buf: ^ui.Command_Buffer, ctx: ^Chart_Layer_Context) {
	d := (^TC_Layer_Data)(layer)
	render_trade_counter(buf, ctx.sub_rect, ctx, d.config, ctx.measure)
}
