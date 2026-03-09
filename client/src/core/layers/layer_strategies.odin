package layers

import "core:fmt"
import "mr:services"
import "mr:ui"

@(private = "file")
price_to_y :: proc(rect: ui.Rect, min_price, max_price, price: f64) -> f32 {
	if max_price <= min_price do return rect.pos.y + rect.size.y * 0.5
	t := f32((price - min_price) / (max_price - min_price))
	t = clamp(t, 0, 1)
	return rect.pos.y + rect.size.y * (1.0 - t)
}

@(private = "file")
rect_right :: proc(r: ui.Rect) -> f32 {
	return r.pos.x + r.size.x
}

@(private = "file")
rect_bottom :: proc(r: ui.Rect) -> f32 {
	return r.pos.y + r.size.y
}

@(private = "file")
heatmap_color :: proc(intensity: f32) -> ui.Color {
	base := ui.viridis_gradient(clamp(intensity, 0, 1))
	return ui.with_alpha(base, 0.12 + 0.45 * clamp(intensity, 0, 1))
}

@(private = "file")
signal_severity_color :: proc(entry: services.Signal_Entry) -> ui.Color {
	e := entry
	sev := services.signal_entry_severity_string(&e)
	if sev == "high" || sev == "critical" do return ui.COL_RED
	if sev == "medium" do return ui.COL_WARNING
	if sev == "low" do return ui.COL_ACCENT_CYAN
	return ui.COL_TEXT_SECONDARY
}

@(private = "file")
layer_diag_state :: proc(store: ^Market_Store, stream: ^Market_Stream, has_data: bool) -> Layer_Widget_State {
	if store == nil || stream == nil do return .Loading
	if !has_data do return .Empty
	if stream.evictions > 0 do return .Degraded
	if store.last_now_ms > 0 && stream.last_unix > 0 {
		age_ms := store.last_now_ms - stream.last_unix * 1000
		if age_ms > 3_000 do return .Stale
	}
	return .Live
}

@(private = "file")
price_candles_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	if !ctx.capabilities.has_candles do return
	store := &ctx.stream.candles
	if store.count <= 0 do return

	// S86: Only draw candle bars when the cell is a candle-type widget.
	// Stats cells include Price_Candles for the stats text overlay only.
	render_bars := ctx.active_bundle == u32(Layer_Bundle.Bundle_Candles)

	visible := min(store.count, 140)
	start := max(store.count - visible, 0)
	min_price := f64(0)
	max_price := f64(0)
	for i in start ..< store.count {
		c := services.get_candle(store, i)
		if i == start {
			min_price = c.low
			max_price = c.high
		}
		if c.low < min_price do min_price = c.low
		if c.high > max_price do max_price = c.high
	}
	if max_price <= min_price {
		max_price = min_price + 1.0
	}

	if render_bars {
		slot_w := ctx.viewport.size.x / f32(max(visible, 1))
		body_w := max(slot_w * 0.65, 1)

		for i in 0 ..< visible {
			idx := start + i
			c := services.get_candle(store, idx)
			x_center := ctx.viewport.pos.x + (f32(i) + 0.5) * slot_w
			y_open := price_to_y(ctx.viewport, min_price, max_price, c.open)
			y_close := price_to_y(ctx.viewport, min_price, max_price, c.close)
			y_high := price_to_y(ctx.viewport, min_price, max_price, c.high)
			y_low := price_to_y(ctx.viewport, min_price, max_price, c.low)

			up := c.close >= c.open
			col := up ? ui.COL_GREEN : ui.COL_RED

			layer_outputs_push_line(out, 20, Render_Line{
				from = {x_center, y_high},
				to = {x_center, y_low},
				color = ui.with_alpha(col, 0.8),
				thickness = 1,
			})

			top := min(y_open, y_close)
			bot := max(y_open, y_close)
			h := max(bot - top, 1)
			layer_outputs_push_bar(out, 21, Render_Bar{
				rect = ui.Rect{pos = {x_center - body_w * 0.5, top}, size = {body_w, h}},
				color = ui.with_alpha(col, 0.55),
			})
		}
	}

	latest := services.get_candle_newest(store, 0)
	latest_buf: [64]u8
	latest_str := fmt.bprintf(latest_buf[:], "Last %.2f", latest.close)
	layer_outputs_push_text_badge(out, 26, text_badge_make(
		{ctx.viewport.pos.x + 6, ctx.viewport.pos.y + 14},
		latest_str,
		ui.COL_TEXT_PRIMARY,
		ui.FONT_SIZE_XS,
	))

	if ctx.stream.stats.count > 0 {
		st := services.get_stats(&ctx.stream.stats, 0)
		stats_buf: [128]u8
		window_s := st.window_ms / 1000
		if window_s < 0 do window_s = 0
		stats_str := fmt.bprintf(
			stats_buf[:],
			"Stats M %.2f F %.4f%% L %.2f/%.2f W %ds",
			st.mark_price,
			st.funding * 100,
			st.liq_buy,
			st.liq_sell,
			window_s,
		)
		layer_outputs_push_text_badge(out, 27, text_badge_make(
			{ctx.viewport.pos.x + 6, ctx.viewport.pos.y + 28},
			stats_str,
			ui.COL_TEXT_SECONDARY,
			ui.FONT_SIZE_XS,
		))

		quality_buf: [80]u8
		quality_str := fmt.bprintf(quality_buf[:], "Q 0x%x Ti %d", st.quality_flags, st.ts_ingest_ms)
		quality_color := ui.COL_TEXT_MUTED
		if st.quality_flags != 0 {
			quality_color = ui.COL_WARNING
		}
		layer_outputs_push_text_badge(out, 27, text_badge_make(
			{ctx.viewport.pos.x + 6, ctx.viewport.pos.y + 40},
			quality_str,
			quality_color,
			ui.FONT_SIZE_XS,
		))
	}

	if sig, ok := services.signal_store_latest_for_subject(&ctx.stream.signals, ctx.subject_id); ok {
		regime := "unknown"
		if sig.regime_len > 0 {
			regime = string(sig.regime[:int(sig.regime_len)])
		}
		regime_buf: [80]u8
		regime_str := fmt.bprintf(regime_buf[:], "Reg %s %.2f", regime, sig.regime_strength)
		layer_outputs_push_text_badge(out, 27, text_badge_make(
			{ctx.viewport.pos.x + 6, ctx.viewport.pos.y + 52},
			regime_str,
			ui.COL_ACCENT_CYAN,
			ui.FONT_SIZE_XS,
		))
	}
}

@(private = "file")
price_candles_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .Price_Candles
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && (stream.candles.count > 0 || stream.stats.count > 0)
	out.state = layer_diag_state(store, stream, out.has_data)
	if stream == nil do return
	out.entries = stream.stats.count
	out.max_entries = services.STATS_CAP
	out.evicted_total = stream.stats_drops
	out.last_seq = stream.last_seq
	out.last_unix = stream.last_unix
	out.parse_total = stream.stats_frames
	out.fallback_total = stream.stats_fallbacks
	out.drop_total = stream.stats_drops
	out.drop_capacity_total = stream.stats_drops
}

price_candles_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .Price_Candles,
		name        = "Price/Candles",
		// S86: Only match on Price_Candles bit — the Bundle_Candles and Bundle_Stats
		// composites already include bit 0 (Price_Candles), so this matches correctly
		// without leaking through shared Evidence/Signal bits.
		bundle_mask = u32(Layer_Bundle.Price_Candles),
		z_order     = layer_z_order_for_id(.Price_Candles),
		init        = layer_noop_init,
		on_event    = layer_noop_on_event,
		on_snapshot = layer_noop_on_snapshot,
		render      = price_candles_render,
		reset       = layer_noop_reset,
		diagnostics = price_candles_diagnostics,
	}
}

@(private = "file")
trades_tape_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	if !ctx.capabilities.has_trades do return
	store := &ctx.stream.trades
	if store.count <= 0 do return

	// S121: Pre-scan visible trades for size classification thresholds.
	vis_cap :: 18
	rows := min(store.count, vis_cap)
	max_qty := f64(0)
	sum_qty := f64(0)
	valid_count := 0
	for i in 0 ..< rows {
		t := services.get_trade(store, i)
		if t.price == 0 && t.qty == 0 do continue
		if t.qty > max_qty do max_qty = t.qty
		sum_qty += t.qty
		valid_count += 1
	}
	if max_qty <= 0 do max_qty = 1
	avg_qty := valid_count > 0 ? sum_qty / f64(valid_count) : max_qty

	// S121: Dust threshold — skip trades < 5% of avg (adapts per instrument).
	dust_thresh := avg_qty * 0.05

	// S121: Size classification thresholds.
	large_thresh := avg_qty * 3.0
	whale_thresh := avg_qty * 10.0

	// Render visible rows (skipping dust).
	rendered := 0
	row_h := max(ctx.viewport.size.y / f32(max(valid_count, 1)), 14)
	right_edge := rect_right(ctx.viewport)

	for i in 0 ..< rows {
		t := services.get_trade(store, i)
		if t.price == 0 && t.qty == 0 do continue
		if t.qty < dust_thresh do continue

		y := ctx.viewport.pos.y + f32(rendered) * row_h
		if y + row_h > rect_bottom(ctx.viewport) do break
		rendered += 1

		frac := f32(t.qty / max_qty)
		frac = clamp(frac, 0.05, 1.0)
		w := (ctx.viewport.size.x - 4) * frac
		col := t.side == .Buy ? ui.COL_GREEN : ui.COL_RED

		// S121: Size-based alpha: small→0.20, medium→0.35, large→0.55, whale→0.70.
		bar_alpha := f32(0.35)
		if t.qty >= whale_thresh {
			bar_alpha = 0.70
		} else if t.qty >= large_thresh {
			bar_alpha = 0.55
		} else if t.qty < avg_qty * 0.5 {
			bar_alpha = 0.20
		}

		x := ctx.viewport.pos.x + 2
		if t.side == .Sell {
			x = right_edge - 2 - w
		}
		layer_outputs_push_bar(out, 40, Render_Bar{
			rect = ui.Rect{pos = {x, y + 1}, size = {w, row_h - 2}},
			color = ui.with_alpha(col, bar_alpha),
		})

		// S121: Whale accent — 1px yellow top border for whale trades.
		if t.qty >= whale_thresh {
			layer_outputs_push_line(out, 42, Render_Line{
				from = {x, y + 1},
				to = {x + w, y + 1},
				color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.60),
				thickness = 1,
			})
		}

		// S121: Split into price (left) + qty (right) for column alignment.
		price_buf: [48]u8
		price_str := fmt.bprintf(price_buf[:], "%.2f", t.price)
		layer_outputs_push_text_badge(out, 41, text_badge_make(
			{ctx.viewport.pos.x + 6, y + row_h * 0.7},
			price_str,
			ui.COL_TEXT_PRIMARY,
			ui.FONT_SIZE_XS,
		))

		qty_buf: [48]u8
		qty_str := fmt.bprintf(qty_buf[:], "%.4f", t.qty)
		layer_outputs_push_text_badge(out, 41, text_badge_make(
			{ctx.viewport.pos.x + ctx.viewport.size.x * 0.45, y + row_h * 0.7},
			qty_str,
			ui.COL_TEXT_SECONDARY,
			ui.FONT_SIZE_XS,
		))

		// S121: Age column (right edge).
		if ctx.now_ms > 0 && t.unix > 0 {
			age_s := (ctx.now_ms / 1000) - t.unix
			if age_s >= 0 {
				age_buf: [16]u8
				age_str: string
				if age_s < 60 {
					age_str = fmt.bprintf(age_buf[:], "%ds", age_s)
				} else if age_s < 3600 {
					age_str = fmt.bprintf(age_buf[:], "%dm", age_s / 60)
				} else {
					age_str = fmt.bprintf(age_buf[:], "%dh", age_s / 3600)
				}
				layer_outputs_push_text_badge(out, 41, text_badge_make(
					{right_edge - 30, y + row_h * 0.7},
					age_str,
					ui.COL_TEXT_MUTED,
					ui.FONT_SIZE_XS,
				))
			}
		}
	}
}

@(private = "file")
trades_tape_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .Trades_Tape
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && stream.trades.count > 0
	out.state = layer_diag_state(store, stream, out.has_data)
	if stream == nil do return
	out.entries = stream.trades.count
	out.max_entries = services.TRADES_CAP
	out.evicted_total = stream.trades_drops + stream.tape_drops
	out.last_seq = stream.last_seq
	out.last_unix = stream.last_unix
	out.parse_total = stream.trades_frames + stream.tape_frames
	out.fallback_total = stream.tape_fallbacks
	out.drop_total = stream.trades_drops + stream.tape_drops
	out.drop_capacity_total = stream.trades_drops + stream.tape_drops
}

trades_tape_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .Trades_Tape,
		name        = "Trades Tape",
		// S86: Match on Trades_Tape bit only — Bundle_Trades/DOM/Counter already include bit 1.
		bundle_mask = u32(Layer_Bundle.Trades_Tape),
		z_order     = layer_z_order_for_id(.Trades_Tape),
		init        = layer_noop_init,
		on_event    = layer_noop_on_event,
		on_snapshot = layer_noop_on_snapshot,
		render      = trades_tape_render,
		reset       = layer_noop_reset,
		diagnostics = trades_tape_diagnostics,
	}
}

@(private = "file")
orderbook_dom_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	if !ctx.capabilities.has_orderbook do return
	ob := &ctx.stream.orderbook
	rows := min(max(ob.ask_count, ob.bid_count), 18)
	if rows <= 0 do return

	mid_x := ctx.viewport.pos.x + ctx.viewport.size.x * 0.5
	half_w := ctx.viewport.size.x * 0.5 - 6
	// S121: Reserve top 24px for mid-price + spread header.
	header_h := f32(24)
	body_y := ctx.viewport.pos.y + header_h
	body_h := ctx.viewport.size.y - header_h
	row_h := max(body_h / f32(max(rows, 1)), 10)

	// Pre-scan: max size and cumulative totals per side.
	max_size := f64(0)
	cum_ask_total := f64(0)
	cum_bid_total := f64(0)
	avg_size := f64(0)
	level_count := 0
	for i in 0 ..< rows {
		if i < ob.ask_count {
			if ob.ask_sizes[i] > max_size do max_size = ob.ask_sizes[i]
			cum_ask_total += ob.ask_sizes[i]
			avg_size += ob.ask_sizes[i]
			level_count += 1
		}
		if i < ob.bid_count {
			if ob.bid_sizes[i] > max_size do max_size = ob.bid_sizes[i]
			cum_bid_total += ob.bid_sizes[i]
			avg_size += ob.bid_sizes[i]
			level_count += 1
		}
	}
	if max_size <= 0 do max_size = 1
	if level_count > 0 do avg_size /= f64(level_count)
	// S121: Whale wall threshold = 5x average size.
	whale_wall := avg_size * 5.0

	// S121: Mid-price hero badge at top center.
	mid := services.mid_price(ob)
	mid_buf: [48]u8
	mid_str := fmt.bprintf(mid_buf[:], "%.2f", mid)
	layer_outputs_push_text_badge(out, 33, text_badge_make(
		{mid_x - 24, ctx.viewport.pos.y + 14},
		mid_str,
		ui.COL_TEXT_PRIMARY,
		ui.FONT_SIZE_SM,
	))

	// S121: Spread in absolute + basis points.
	sp := services.spread(ob)
	spread_buf: [64]u8
	spread_str: string
	if mid > 0 {
		bps := sp / mid * 10000
		spread_str = fmt.bprintf(spread_buf[:], "Spd %.2f (%.1f bps)", sp, bps)
	} else {
		spread_str = fmt.bprintf(spread_buf[:], "Spd %.2f", sp)
	}
	layer_outputs_push_text_badge(out, 33, text_badge_make(
		{ctx.viewport.pos.x + 6, ctx.viewport.pos.y + 14},
		spread_str,
		ui.COL_TEXT_MUTED,
		ui.FONT_SIZE_XS,
	))

	// Center divider line.
	layer_outputs_push_line(out, 30, Render_Line{
		from = {mid_x, body_y},
		to = {mid_x, rect_bottom(ctx.viewport)},
		color = ui.with_alpha(ui.COL_WHITE, 0.12),
		thickness = 1,
	})

	// S121: Render levels with cumulative depth shadow + price labels + whale highlights.
	cum_ask := f64(0)
	cum_bid := f64(0)

	for i in 0 ..< rows {
		y := body_y + f32(i) * row_h

		// Ask side (right of center).
		if i < ob.ask_count {
			cum_ask += ob.ask_sizes[i]
			// Cumulative depth shadow bar.
			if cum_ask_total > 0 {
				cum_frac := f32(cum_ask / cum_ask_total)
				cum_w := half_w * clamp(cum_frac, 0.02, 1)
				layer_outputs_push_bar(out, 30, Render_Bar{
					rect = ui.Rect{pos = {mid_x + 2, y + 1}, size = {cum_w, row_h - 2}},
					color = ui.with_alpha(ui.COL_RED, 0.10),
				})
			}
			// Level bar.
			ask_frac := f32(ob.ask_sizes[i] / max_size)
			ask_w := half_w * clamp(ask_frac, 0.05, 1)
			// S121: Whale wall highlight (brighter + yellow accent).
			ask_alpha := f32(0.35)
			if ob.ask_sizes[i] >= whale_wall {
				ask_alpha = 0.55
			}
			layer_outputs_push_bar(out, 31, Render_Bar{
				rect = ui.Rect{pos = {mid_x + 2, y + 1}, size = {ask_w, row_h - 2}},
				color = ui.with_alpha(ui.COL_RED, ask_alpha),
			})
			// S121: Whale wall yellow accent.
			if ob.ask_sizes[i] >= whale_wall {
				layer_outputs_push_line(out, 32, Render_Line{
					from = {mid_x + 2, y + 1},
					to = {mid_x + 2 + ask_w, y + 1},
					color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.45),
					thickness = 1,
				})
			}
			// S121: Price label.
			ap_buf: [32]u8
			ap_str := fmt.bprintf(ap_buf[:], "%.2f", ob.ask_prices[i])
			layer_outputs_push_text_badge(out, 32, text_badge_make(
				{mid_x + half_w - 36, y + row_h * 0.7},
				ap_str,
				ui.COL_TEXT_MUTED,
				ui.FONT_SIZE_XS,
			))
		}

		// Bid side (left of center).
		if i < ob.bid_count {
			cum_bid += ob.bid_sizes[i]
			// Cumulative depth shadow bar.
			if cum_bid_total > 0 {
				cum_frac := f32(cum_bid / cum_bid_total)
				cum_w := half_w * clamp(cum_frac, 0.02, 1)
				layer_outputs_push_bar(out, 30, Render_Bar{
					rect = ui.Rect{pos = {mid_x - 2 - cum_w, y + 1}, size = {cum_w, row_h - 2}},
					color = ui.with_alpha(ui.COL_GREEN, 0.10),
				})
			}
			// Level bar.
			bid_frac := f32(ob.bid_sizes[i] / max_size)
			bid_w := half_w * clamp(bid_frac, 0.05, 1)
			bid_alpha := f32(0.35)
			if ob.bid_sizes[i] >= whale_wall {
				bid_alpha = 0.55
			}
			layer_outputs_push_bar(out, 31, Render_Bar{
				rect = ui.Rect{pos = {mid_x - 2 - bid_w, y + 1}, size = {bid_w, row_h - 2}},
				color = ui.with_alpha(ui.COL_GREEN, bid_alpha),
			})
			if ob.bid_sizes[i] >= whale_wall {
				layer_outputs_push_line(out, 32, Render_Line{
					from = {mid_x - 2 - bid_w, y + 1},
					to = {mid_x - 2, y + 1},
					color = ui.with_alpha(ui.COL_YELLOW_ACCENT, 0.45),
					thickness = 1,
				})
			}
			// S121: Price label.
			bp_buf: [32]u8
			bp_str := fmt.bprintf(bp_buf[:], "%.2f", ob.bid_prices[i])
			layer_outputs_push_text_badge(out, 32, text_badge_make(
				{ctx.viewport.pos.x + 4, y + row_h * 0.7},
				bp_str,
				ui.COL_TEXT_MUTED,
				ui.FONT_SIZE_XS,
			))
		}
	}
}

@(private = "file")
orderbook_dom_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .OrderBook_DOM
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && (stream.orderbook.ask_count > 0 || stream.orderbook.bid_count > 0)
	out.state = layer_diag_state(store, stream, out.has_data)
	if stream == nil do return
	out.entries = stream.orderbook.ask_count + stream.orderbook.bid_count
	out.max_entries = services.OB_DEPTH_CAP * 2
	out.evicted_total = stream.orderbook_drops
	out.last_seq = stream.last_seq
	out.last_unix = stream.last_unix
	out.parse_total = stream.orderbook_frames
	out.fallback_total = stream.orderbook_fallbacks
	out.drop_total = stream.orderbook_drops
	out.drop_capacity_total = stream.orderbook_drops
}

orderbook_dom_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .OrderBook_DOM,
		name        = "OrderBook/DOM",
		// S86: Match on OrderBook_DOM bit only — Bundle_Orderbook/DOM already include bit 2.
		bundle_mask = u32(Layer_Bundle.OrderBook_DOM),
		z_order     = layer_z_order_for_id(.OrderBook_DOM),
		init        = layer_noop_init,
		on_event    = layer_noop_on_event,
		on_snapshot = layer_noop_on_snapshot,
		render      = orderbook_dom_render,
		reset       = layer_noop_reset,
		diagnostics = orderbook_dom_diagnostics,
	}
}

@(private = "file")
vpvr_heatmap_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	if !ctx.capabilities.has_heatmap && !ctx.capabilities.has_vpvr do return
	stream := ctx.stream

	if ctx.capabilities.has_heatmap && stream.heatmap.count > 0 {
		snap := services.get_heatmap_snapshot(&stream.heatmap, stream.heatmap.count - 1)
		if snap != nil {
			min_p := snap.min_price
			max_p := snap.max_price
			if max_p <= min_p {
				max_p = min_p + 1
			}
			max_size := max(snap.max_size, 1)
			levels := min(snap.level_count, 120)
			for i in 0 ..< levels {
				level := snap.levels[i]
				y := price_to_y(ctx.viewport, min_p, max_p, level.price)
				intensity := f32(level.size / max_size)
				h := max(ctx.viewport.size.y / f32(max(levels, 1)), 2)
				layer_outputs_push_heatmap_cell(out, 10, Render_Heatmap_Cell{
					rect = ui.Rect{pos = {ctx.viewport.pos.x, y - h * 0.5}, size = {ctx.viewport.size.x, h}},
					intensity = intensity,
					color = heatmap_color(intensity),
				})
			}
		}
	}

	if ctx.capabilities.has_vpvr && stream.vpvr.count > 0 {
		right_w := ctx.viewport.size.x * 0.25
		base_x := rect_right(ctx.viewport) - right_w
		max_vol := max(stream.vpvr.max_volume, 1)
		for i in 0 ..< stream.vpvr.count {
			b := services.get_vpvr_bucket(&stream.vpvr, i)
			frac := f32((b.buy_volume + b.sell_volume) / max_vol)
			w := right_w * clamp(frac, 0.03, 1)
			y := price_to_y(ctx.viewport, stream.vpvr.min_price, stream.vpvr.max_price, b.price)
			h := max(ctx.viewport.size.y / f32(max(stream.vpvr.count, 1)), 1.5)
			layer_outputs_push_bar(out, 12, Render_Bar{
				rect = ui.Rect{pos = {base_x + (right_w - w), y - h * 0.5}, size = {w, h}},
				color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.28),
			})
		}
	}
}

@(private = "file")
vpvr_heatmap_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .VPVR_Heatmap
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && (stream.heatmap.count > 0 || stream.vpvr.count > 0)
	out.state = layer_diag_state(store, stream, out.has_data)
	if stream == nil do return
	out.entries = stream.heatmap.count + stream.vpvr.count
	out.max_entries = services.HEATMAP_SNAP_CAP + services.VPVR_BUCKET_CAP
	out.evicted_total = stream.evictions
	out.last_seq = stream.last_seq
	out.last_unix = stream.last_unix
	out.parse_total = stream.event_count
	out.drop_total = stream.evictions
	out.drop_capacity_total = stream.evictions
}

vpvr_heatmap_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .VPVR_Heatmap,
		name        = "VPVR/Heatmap",
		// S86: Match on VPVR_Heatmap bit only — Bundle_Candles/Heatmap/VPVR already include bit 3.
		bundle_mask = u32(Layer_Bundle.VPVR_Heatmap),
		z_order     = layer_z_order_for_id(.VPVR_Heatmap),
		init        = layer_noop_init,
		on_event    = layer_noop_on_event,
		on_snapshot = layer_noop_on_snapshot,
		render      = vpvr_heatmap_render,
		reset       = layer_noop_reset,
		diagnostics = vpvr_heatmap_diagnostics,
	}
}

@(private = "file")
evidence_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	if !ctx.capabilities.has_evidence do return
	rows := min(ctx.stream.evidence_count, 6)
	if rows <= 0 do return
	y := ctx.viewport.pos.y + 14
	for i in 0 ..< rows {
		e, ok := market_stream_evidence_get_newest(ctx.stream, i)
		if !ok do continue
		line_buf: [128]u8
		kind := fixed_text_string(e.kind[:], e.kind_len)
		line := fmt.bprintf(line_buf[:], "E %s %.2f", kind, e.confidence)
		layer_outputs_push_text_badge(out, 50, text_badge_make(
			{ctx.viewport.pos.x + 6, y},
			line,
			ui.COL_WARNING,
			ui.FONT_SIZE_XS,
		))
		y += 12
	}
}

@(private = "file")
evidence_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .Evidence
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && stream.evidence_count > 0
	out.state = layer_diag_state(store, stream, out.has_data)
	if stream == nil do return
	out.entries = stream.evidence_count
	out.max_entries = EVIDENCE_RING_CAP
	out.evicted_total = stream.evidence_drops
	out.last_seq = stream.last_seq
	out.last_unix = stream.last_unix
	out.parse_total = stream.evidence_frames
	out.fallback_total = stream.evidence_fallbacks
	out.drop_total = stream.evidence_drops
	out.drop_capacity_total = stream.evidence_drops
}

evidence_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .Evidence,
		name        = "Evidence",
		// S86: Match on Evidence bit only — all cell bundles that want evidence already include bit 4.
		bundle_mask = u32(Layer_Bundle.Evidence),
		z_order     = layer_z_order_for_id(.Evidence),
		init        = layer_noop_init,
		on_event    = layer_noop_on_event,
		on_snapshot = layer_noop_on_snapshot,
		render      = evidence_render,
		reset       = layer_noop_reset,
		diagnostics = evidence_diagnostics,
	}
}

@(private = "file")
signal_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	if !ctx.capabilities.has_signal do return
	entries: [8]services.Signal_Entry
	n := services.signal_store_recent_for_subject(&ctx.stream.signals, ctx.subject_id, entries[:])
	if n <= 0 do return

	y := rect_bottom(ctx.viewport) - 6
	for i in 0 ..< n {
		e := entries[i]
		line_buf: [160]u8
		kind := services.signal_entry_kind_string(&e)
		reason := services.signal_entry_reason_string(&e)
		if len(reason) == 0 do reason = "n/a"
		line := fmt.bprintf(line_buf[:], "S %s %.2f why=%s", kind, e.confidence, reason)
		if ctx.signal_evidence_link_enabled && ctx.stream.last_linked_evidence_seq > 0 {
			link_buf: [160]u8
			line = fmt.bprintf(link_buf[:], "%s evidence=E#%d", line, ctx.stream.last_linked_evidence_seq)
		}
		y -= 13
		if y < ctx.viewport.pos.y + 8 do break
		layer_outputs_push_text_badge(out, 60, text_badge_make(
			{ctx.viewport.pos.x + 6, y},
			line,
			signal_severity_color(e),
			ui.FONT_SIZE_XS,
		))
	}
}

@(private = "file")
signal_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .Signal
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && stream.signals.kind_count > 0
	out.state = layer_diag_state(store, stream, out.has_data)
	if stream == nil do return
	signal_entries := 0
	for ki in 0 ..< stream.signals.kind_count {
		signal_entries += stream.signals.kinds[ki].count
	}
	out.entries = signal_entries
	out.max_entries = services.SIGNAL_KIND_CAP * services.SIGNAL_PER_KIND_CAP
	out.evicted_total = stream.signals.overwritten_total + stream.signals.evicted_kind_total
	out.last_seq = stream.last_seq
	out.last_unix = stream.last_unix
	out.parse_total = stream.signal_frames
	out.fallback_total = stream.signal_fallbacks
	out.drop_total = stream.signal_drops
	out.drop_capacity_total = stream.signal_drops
	out.signal_link_total = stream.signal_evidence_links
	out.signal_link_evidence_seq = stream.last_linked_evidence_seq
}

signal_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .Signal,
		name        = "Signal",
		// S86: Match on Signal bit only — all cell bundles that want signals already include bit 5.
		bundle_mask = u32(Layer_Bundle.Signal),
		z_order     = layer_z_order_for_id(.Signal),
		init        = layer_noop_init,
		on_event    = layer_noop_on_event,
		on_snapshot = layer_noop_on_snapshot,
		render      = signal_render,
		reset       = layer_noop_reset,
		diagnostics = signal_diagnostics,
	}
}

// ═══════════════════════════════════════════════════════════════
// Analytics layer — renders OI, Delta Volume, CVD, Bar Stats
// as text badges within the viewport. Supports filtered mode
// (single kind for Analytics cells) and full mode (all kinds
// for Candle cells).
// ═══════════════════════════════════════════════════════════════

@(private = "file")
analytics_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	if !ctx.capabilities.has_analytics do return
	store := &ctx.stream.analytics
	if store.count <= 0 do return

	y := ctx.viewport.pos.y + 14
	x := ctx.viewport.pos.x + 6

	// When filtered, render only the requested analytics kind (for Analytics widget cells).
	if ctx.analytics_filter {
		analytics_render_kind(ctx, out, store, ctx.analytics_kind, &y, x)
		return
	}

	// Unfiltered: render all analytics kinds that have data (for Candle bundle).
	analytics_render_kind(ctx, out, store, .Open_Interest, &y, x)
	analytics_render_kind(ctx, out, store, .Delta_Volume, &y, x)
	analytics_render_kind(ctx, out, store, .CVD, &y, x)
	analytics_render_kind(ctx, out, store, .Bar_Stats, &y, x)
}

@(private = "file")
analytics_render_kind :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs, store: ^services.Analytics_Store, kind: services.Analytics_Kind, y: ^f32, x: f32) {
	entry, ok := services.get_analytics_latest(store, kind)
	if !ok do return

	switch kind {
	case .Open_Interest:
		oi_val := entry.values[0]
		delta := entry.values[1]
		delta_pct := entry.values[2]

		val_buf: [64]u8
		val_str := fmt.bprintf(val_buf[:], "OI %.0f", oi_val)
		layer_outputs_push_text_badge(out, 25, text_badge_make(
			{x, y^}, val_str, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS,
		))
		y^ += 14

		delta_color := delta >= 0 ? ui.COL_GREEN : ui.COL_RED
		delta_sign := delta >= 0 ? "+" : ""
		delta_buf: [80]u8
		delta_str := fmt.bprintf(delta_buf[:], "%s%.0f (%s%.2f%%)", delta_sign, delta, delta_sign, delta_pct * 100)
		layer_outputs_push_text_badge(out, 25, text_badge_make(
			{x, y^}, delta_str, delta_color, ui.FONT_SIZE_XS,
		))
		y^ += 14

		// Confidence dot as text badge.
		conf_label: string
		conf_color: ui.Color
		switch entry.confidence {
		case 1:  conf_label = "H"; conf_color = ui.Color{0.0, 0.8, 0.0, 0.9}
		case 2:  conf_label = "M"; conf_color = ui.Color{0.8, 0.8, 0.0, 0.9}
		case 3:  conf_label = "L"; conf_color = ui.Color{0.4, 0.4, 0.4, 0.7}
		case:    conf_label = "?"; conf_color = ui.Color{0.3, 0.3, 0.3, 0.5}
		}
		layer_outputs_push_text_badge(out, 25, text_badge_make(
			{x, y^}, conf_label, conf_color, ui.FONT_SIZE_XS,
		))

		// Cadence badge.
		cadence_ms := entry.cadence_hint_ms
		if cadence_ms > 0 {
			cadence_buf: [16]u8
			cadence_str: string
			if cadence_ms < 1000 {
				cadence_str = fmt.bprintf(cadence_buf[:], "~%dms", cadence_ms)
			} else if cadence_ms < 60000 {
				cadence_str = fmt.bprintf(cadence_buf[:], "~%ds", cadence_ms / 1000)
			} else {
				cadence_str = fmt.bprintf(cadence_buf[:], "~%dm", cadence_ms / 60000)
			}
			layer_outputs_push_text_badge(out, 25, text_badge_make(
				{x + 16, y^}, cadence_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS,
			))

			// Stale indicator.
			most_recent := services.get_analytics(store, 0)
			if most_recent.ts_ms > 0 && entry.ts_ms > 0 {
				age_ms := most_recent.ts_ms - entry.ts_ms
				if age_ms > cadence_ms * 3 {
					layer_outputs_push_text_badge(out, 25, text_badge_make(
						{x + 64, y^}, "STALE", ui.COL_WARNING, ui.FONT_SIZE_XS,
					))
				}
			}
		}
		y^ += 14

	case .Delta_Volume:
		buy_vol := entry.values[0]
		sell_vol := entry.values[1]
		delta_vol := entry.values[2]

		delta_color := delta_vol >= 0 ? ui.COL_GREEN : ui.COL_RED
		val_buf: [80]u8
		val_str := fmt.bprintf(val_buf[:], "DV %.2f", delta_vol)
		layer_outputs_push_text_badge(out, 25, text_badge_make(
			{x, y^}, val_str, delta_color, ui.FONT_SIZE_XS,
		))
		y^ += 14

		bs_buf: [80]u8
		bs_str := fmt.bprintf(bs_buf[:], "B %.2f  S %.2f", buy_vol, sell_vol)
		layer_outputs_push_text_badge(out, 25, text_badge_make(
			{x, y^}, bs_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS,
		))
		y^ += 14

		// Buy/sell ratio bar.
		total := buy_vol + sell_vol
		if total > 0 {
			bar_w := ctx.viewport.size.x - 16
			buy_frac := f32(buy_vol / total)
			layer_outputs_push_bar(out, 25, Render_Bar{
				rect = ui.Rect{pos = {x, y^}, size = {bar_w * buy_frac, 6}},
				color = ui.with_alpha(ui.COL_GREEN, 0.5),
			})
			layer_outputs_push_bar(out, 25, Render_Bar{
				rect = ui.Rect{pos = {x + bar_w * buy_frac, y^}, size = {bar_w * (1 - buy_frac), 6}},
				color = ui.with_alpha(ui.COL_RED, 0.5),
			})
			y^ += 10
		}

	case .CVD:
		delta_vol := entry.values[0]
		cvd := entry.values[1]

		cvd_color := cvd >= 0 ? ui.COL_GREEN : ui.COL_RED
		val_buf: [64]u8
		val_str := fmt.bprintf(val_buf[:], "CVD %.2f", cvd)
		layer_outputs_push_text_badge(out, 25, text_badge_make(
			{x, y^}, val_str, cvd_color, ui.FONT_SIZE_XS,
		))
		y^ += 14

		dv_buf: [64]u8
		dv_str := fmt.bprintf(dv_buf[:], "Win delta %.2f", delta_vol)
		layer_outputs_push_text_badge(out, 25, text_badge_make(
			{x, y^}, dv_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS,
		))
		y^ += 14

	case .Bar_Stats:
		trade_count := entry.values[0]
		buy_count := entry.values[1]
		sell_count := entry.values[2]
		buy_vol := entry.values[4]
		sell_vol := entry.values[5]
		vwap := entry.values[6]
		imbalance := entry.values[7]
		is_burst := (entry.flags & 1) != 0

		tc_buf: [80]u8
		tc_str := fmt.bprintf(tc_buf[:], "Trades %.0f (B:%.0f S:%.0f)", trade_count, buy_count, sell_count)
		layer_outputs_push_text_badge(out, 25, text_badge_make(
			{x, y^}, tc_str, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS,
		))
		y^ += 14

		vwap_buf: [48]u8
		vwap_str := fmt.bprintf(vwap_buf[:], "VWAP %.2f", vwap)
		layer_outputs_push_text_badge(out, 25, text_badge_make(
			{x, y^}, vwap_str, ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS,
		))
		y^ += 14

		imb_color := imbalance >= 0 ? ui.COL_GREEN : ui.COL_RED
		imb_buf: [48]u8
		imb_str := fmt.bprintf(imb_buf[:], "Imb %.2f%%", imbalance * 100)
		layer_outputs_push_text_badge(out, 25, text_badge_make(
			{x, y^}, imb_str, imb_color, ui.FONT_SIZE_XS,
		))
		y^ += 14

		// Buy/sell ratio bar.
		total := buy_vol + sell_vol
		if total > 0 {
			bar_w := ctx.viewport.size.x - 16
			buy_frac := f32(buy_vol / total)
			layer_outputs_push_bar(out, 25, Render_Bar{
				rect = ui.Rect{pos = {x, y^}, size = {bar_w * buy_frac, 6}},
				color = ui.with_alpha(ui.COL_GREEN, 0.5),
			})
			layer_outputs_push_bar(out, 25, Render_Bar{
				rect = ui.Rect{pos = {x + bar_w * buy_frac, y^}, size = {bar_w * (1 - buy_frac), 6}},
				color = ui.with_alpha(ui.COL_RED, 0.5),
			})
			y^ += 10
		}

		if is_burst {
			layer_outputs_push_text_badge(out, 25, text_badge_make(
				{x, y^}, "BURST", ui.COL_WARNING, ui.FONT_SIZE_XS,
			))
			y^ += 14
		}
	}
}

@(private = "file")
analytics_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .Analytics
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && stream.analytics.count > 0
	out.state = layer_diag_state(store, stream, out.has_data)
	if stream == nil do return
	out.entries = stream.analytics.count
	out.max_entries = services.ANALYTICS_STORE_CAP
	out.last_seq = stream.last_seq
	out.last_unix = stream.last_unix
}

// ═══════════════════════════════════════════════════════════════
// S87: Stats Panel layer — dedicated stats rendering for Stats cells.
// Renders mark price, funding rate, liquidation levels, quality flags.
// Separated from Price_Candles so Stats cells don't trigger candle
// range scans or render "Last X.XX" price badges.
// ═══════════════════════════════════════════════════════════════

@(private = "file")
stats_panel_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	if !ctx.capabilities.has_stats do return
	if ctx.stream.stats.count <= 0 do return

	st := services.get_stats(&ctx.stream.stats, 0)
	x := ctx.viewport.pos.x + 6
	bar_w := ctx.viewport.size.x - 16

	// S121: Mark price as hero number (larger font, prominent).
	y := ctx.viewport.pos.y + 22
	mark_buf: [64]u8
	mark_str := fmt.bprintf(mark_buf[:], "%.2f", st.mark_price)
	layer_outputs_push_text_badge(out, 22, text_badge_make(
		{x, y}, mark_str, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_LG,
	))
	y += 24

	// S121: "Mark Price" label (muted, small).
	layer_outputs_push_text_badge(out, 22, text_badge_make(
		{x, y}, "Mark Price", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS,
	))
	y += 16

	// S121: Funding rate — larger, directional color.
	fund_color := st.funding >= 0 ? ui.COL_GREEN : ui.COL_RED
	fund_sign := st.funding >= 0 ? "+" : ""
	fund_buf: [64]u8
	fund_str := fmt.bprintf(fund_buf[:], "Funding %s%.4f%%", fund_sign, st.funding * 100)
	layer_outputs_push_text_badge(out, 22, text_badge_make(
		{x, y}, fund_str, fund_color, ui.FONT_SIZE_SM,
	))
	y += 18

	// S121: Spread in bps (cross-store from orderbook if available).
	if ctx.capabilities.has_orderbook {
		ob := &ctx.stream.orderbook
		sp := services.spread(ob)
		mid := services.mid_price(ob)
		if sp > 0 && mid > 0 {
			bps := sp / mid * 10000
			sp_buf: [64]u8
			sp_str := fmt.bprintf(sp_buf[:], "Spread %.2f (%.1f bps)", sp, bps)
			layer_outputs_push_text_badge(out, 22, text_badge_make(
				{x, y}, sp_str, ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS,
			))
			y += 14
		}
	}

	// S121: Liquidation as compact bar + text overlay.
	liq_total := st.liq_buy + st.liq_sell
	if liq_total > 0 {
		buy_frac := f32(st.liq_buy / liq_total)
		layer_outputs_push_bar(out, 22, Render_Bar{
			rect = ui.Rect{pos = {x, y}, size = {bar_w * buy_frac, 8}},
			color = ui.with_alpha(ui.COL_GREEN, 0.40),
		})
		layer_outputs_push_bar(out, 22, Render_Bar{
			rect = ui.Rect{pos = {x + bar_w * buy_frac, y}, size = {bar_w * (1 - buy_frac), 8}},
			color = ui.with_alpha(ui.COL_RED, 0.40),
		})
		y += 12
	}
	liq_buf: [80]u8
	liq_str := fmt.bprintf(liq_buf[:], "Liq B %.0f / S %.0f", st.liq_buy, st.liq_sell)
	layer_outputs_push_text_badge(out, 22, text_badge_make(
		{x, y}, liq_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS,
	))
	y += 14

	// Window duration.
	window_s := st.window_ms / 1000
	if window_s < 0 do window_s = 0
	win_buf: [48]u8
	win_str := fmt.bprintf(win_buf[:], "Window %ds", window_s)
	layer_outputs_push_text_badge(out, 22, text_badge_make(
		{x, y}, win_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS,
	))

	// S121: Quality flags — only show when non-zero (reduce noise).
	if st.quality_flags != 0 {
		y += 14
		quality_buf: [48]u8
		quality_str := fmt.bprintf(quality_buf[:], "Q 0x%x", st.quality_flags)
		layer_outputs_push_text_badge(out, 22, text_badge_make(
			{x, y}, quality_str, ui.COL_WARNING, ui.FONT_SIZE_XS,
		))
	}
}

@(private = "file")
stats_panel_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .Stats_Panel
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && stream.stats.count > 0
	out.state = layer_diag_state(store, stream, out.has_data)
	if stream == nil do return
	out.entries = stream.stats.count
	out.max_entries = services.STATS_CAP
}

stats_panel_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .Stats_Panel,
		name        = "Stats Panel",
		bundle_mask = u32(Layer_Bundle.Stats_Panel),
		z_order     = layer_z_order_for_id(.Stats_Panel),
		init        = layer_noop_init,
		on_event    = layer_noop_on_event,
		on_snapshot = layer_noop_on_snapshot,
		render      = stats_panel_render,
		reset       = layer_noop_reset,
		diagnostics = stats_panel_diagnostics,
	}
}

// ═══════════════════════════════════════════════════════════════
// S87: Trade Counter layer — dedicated counter rendering for Counter cells.
// Renders aggregate trade count, buy/sell ratio bar, volume summary.
// Separated from Trades_Tape so Counter cells don't render individual
// trade rows.
// ═══════════════════════════════════════════════════════════════

@(private = "file")
trade_counter_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	if !ctx.capabilities.has_candles do return
	store := &ctx.stream.candles
	if store.count <= 0 do return

	latest := services.get_candle_newest(store, 0)
	y := ctx.viewport.pos.y + 14
	x := ctx.viewport.pos.x + 6
	bar_w := ctx.viewport.size.x - 16

	// S121: Trade count — larger font for primary metric.
	tc_buf: [64]u8
	tc_str := fmt.bprintf(tc_buf[:], "Trades %d", latest.trade_count)
	layer_outputs_push_text_badge(out, 23, text_badge_make(
		{x, y}, tc_str, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_SM,
	))
	y += 18

	// Volume summary.
	total_vol := latest.buy_vol + latest.sell_vol
	vol_buf: [80]u8
	vol_str := fmt.bprintf(vol_buf[:], "Vol %.4f (B %.4f / S %.4f)", total_vol, latest.buy_vol, latest.sell_vol)
	layer_outputs_push_text_badge(out, 23, text_badge_make(
		{x, y}, vol_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS,
	))
	y += 14

	// S121: Net delta — buy minus sell, directional color.
	net_delta := latest.buy_vol - latest.sell_vol
	delta_color := net_delta >= 0 ? ui.COL_GREEN : ui.COL_RED
	delta_sign := net_delta >= 0 ? "+" : ""
	delta_buf: [64]u8
	delta_str := fmt.bprintf(delta_buf[:], "Delta %s%.4f", delta_sign, net_delta)
	layer_outputs_push_text_badge(out, 23, text_badge_make(
		{x, y}, delta_str, delta_color, ui.FONT_SIZE_SM,
	))
	y += 16

	// S121: Buy/sell ratio bar — taller (12px) for better visibility.
	if total_vol > 0 {
		buy_frac := f32(latest.buy_vol / total_vol)
		layer_outputs_push_bar(out, 23, Render_Bar{
			rect = ui.Rect{pos = {x, y}, size = {bar_w * buy_frac, 12}},
			color = ui.with_alpha(ui.COL_GREEN, 0.5),
		})
		layer_outputs_push_bar(out, 23, Render_Bar{
			rect = ui.Rect{pos = {x + bar_w * buy_frac, y}, size = {bar_w * (1 - buy_frac), 12}},
			color = ui.with_alpha(ui.COL_RED, 0.5),
		})
		y += 16

		// Ratio label.
		ratio_buf: [48]u8
		ratio_str := fmt.bprintf(ratio_buf[:], "B/S %.1f%%", f64(buy_frac) * 100)
		ratio_color := buy_frac >= 0.5 ? ui.COL_GREEN : ui.COL_RED
		layer_outputs_push_text_badge(out, 23, text_badge_make(
			{x, y}, ratio_str, ratio_color, ui.FONT_SIZE_XS,
		))
		y += 14
	}

	// S121: Volume rate (per second) from candle window.
	window_ms := latest.window_end_ts - latest.window_start_ts
	if window_ms > 0 && total_vol > 0 {
		window_s := f64(window_ms) / 1000.0
		rate := total_vol / window_s
		rate_buf: [48]u8
		rate_str := fmt.bprintf(rate_buf[:], "Rate %.2f/s", rate)
		layer_outputs_push_text_badge(out, 23, text_badge_make(
			{x, y}, rate_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS,
		))
		y += 14
	}

	// S121: Rolling 5-bar summary (if we have enough candles).
	roll_n := min(store.count, 5)
	if roll_n >= 2 {
		roll_buy := f64(0)
		roll_sell := f64(0)
		roll_trades := i64(0)
		for ri in 0 ..< roll_n {
			c := services.get_candle_newest(store, ri)
			roll_buy += c.buy_vol
			roll_sell += c.sell_vol
			roll_trades += c.trade_count
		}
		roll_buf: [80]u8
		roll_str := fmt.bprintf(roll_buf[:], "%d-bar: Vol %.2f  Trades %d", roll_n, roll_buy + roll_sell, roll_trades)
		layer_outputs_push_text_badge(out, 23, text_badge_make(
			{x, y}, roll_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS,
		))
		y += 14
	}

	// Last close price reference.
	price_buf: [48]u8
	price_str := fmt.bprintf(price_buf[:], "Last %.2f", latest.close)
	layer_outputs_push_text_badge(out, 23, text_badge_make(
		{x, y}, price_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS,
	))

	// Stats supplement if available.
	if ctx.capabilities.has_stats && ctx.stream.stats.count > 0 {
		st := services.get_stats(&ctx.stream.stats, 0)
		y += 14
		fund_buf: [48]u8
		fund_str := fmt.bprintf(fund_buf[:], "F %.4f%%", st.funding * 100)
		layer_outputs_push_text_badge(out, 23, text_badge_make(
			{x, y}, fund_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS,
		))
	}
}

@(private = "file")
trade_counter_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .Trade_Counter
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && stream.candles.count > 0
	out.state = layer_diag_state(store, stream, out.has_data)
	if stream == nil do return
	out.entries = stream.candles.count
	out.max_entries = services.CANDLE_CAP
}

trade_counter_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .Trade_Counter,
		name        = "Trade Counter",
		bundle_mask = u32(Layer_Bundle.Trade_Counter),
		z_order     = layer_z_order_for_id(.Trade_Counter),
		init        = layer_noop_init,
		on_event    = layer_noop_on_event,
		on_snapshot = layer_noop_on_snapshot,
		render      = trade_counter_render,
		reset       = layer_noop_reset,
		diagnostics = trade_counter_diagnostics,
	}
}

// ═══════════════════════════════════════════════════════════════
// S94: Analytics subplots — graphical CVD line / Delta Vol bars / OI line
// rendered in dedicated subplot viewports below the main candle chart.
// Called directly from layer_canvas, not through the registry.
// ═══════════════════════════════════════════════════════════════

SUBPLOT_COLLECT_CAP :: 48

// CVD subplot: line chart of cumulative volume delta.
subplot_cvd_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs, subplot_vp: ui.Rect) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	store := &ctx.stream.analytics
	if store.count <= 0 do return

	entries: [SUBPLOT_COLLECT_CAP]services.Analytics_Entry
	n := services.analytics_collect_by_kind(store, .CVD, entries[:])
	if n < 2 do return

	// Find min/max CVD for y-axis scaling.
	min_val := entries[0].values[1]
	max_val := entries[0].values[1]
	for i in 1 ..< n {
		v := entries[i].values[1]
		if v < min_val do min_val = v
		if v > max_val do max_val = v
	}
	if max_val <= min_val {
		max_val = min_val + 1.0
	}

	// Subplot background + divider.
	subplot_push_bg(out, subplot_vp)

	// Zero line.
	if min_val < 0 && max_val > 0 {
		zero_y := subplot_val_to_y(subplot_vp, min_val, max_val, 0)
		layer_outputs_push_line(out, 24, Render_Line{
			from = {subplot_vp.pos.x, zero_y},
			to   = {rect_right(subplot_vp), zero_y},
			color = ui.with_alpha(ui.COL_WHITE, 0.08),
			thickness = 1,
		})
	}

	// Draw CVD line segments.
	slot_w := subplot_vp.size.x / f32(max(n - 1, 1))
	for i in 1 ..< n {
		x0 := subplot_vp.pos.x + f32(i - 1) * slot_w
		x1 := subplot_vp.pos.x + f32(i) * slot_w
		y0 := subplot_val_to_y(subplot_vp, min_val, max_val, entries[i - 1].values[1])
		y1 := subplot_val_to_y(subplot_vp, min_val, max_val, entries[i].values[1])
		col := entries[i].values[1] >= 0 ? ui.COL_GREEN : ui.COL_RED
		layer_outputs_push_line(out, 25, Render_Line{
			from = {x0, y0}, to = {x1, y1},
			color = ui.with_alpha(col, 0.85),
			thickness = 1.5,
		})
	}

	// Label.
	latest_cvd := entries[n - 1].values[1]
	label_buf: [32]u8
	label := fmt.bprintf(label_buf[:], "CVD %.1f", latest_cvd)
	label_col := latest_cvd >= 0 ? ui.COL_GREEN : ui.COL_RED
	layer_outputs_push_text_badge(out, 26, text_badge_make(
		{subplot_vp.pos.x + 4, subplot_vp.pos.y + 10},
		label, label_col, ui.FONT_SIZE_XS,
	))
}

// Delta Volume subplot: vertical bars of per-window delta volume.
subplot_delta_vol_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs, subplot_vp: ui.Rect) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	store := &ctx.stream.analytics
	if store.count <= 0 do return

	entries: [SUBPLOT_COLLECT_CAP]services.Analytics_Entry
	n := services.analytics_collect_by_kind(store, .Delta_Volume, entries[:])
	if n == 0 do return

	// Find max absolute delta for y-axis scaling.
	max_abs := f64(0)
	for i in 0 ..< n {
		v := entries[i].values[2]
		abs_v := v >= 0 ? v : -v
		if abs_v > max_abs do max_abs = abs_v
	}
	if max_abs <= 0 do max_abs = 1.0

	// Subplot background + divider.
	subplot_push_bg(out, subplot_vp)

	// Zero line at center.
	mid_y := subplot_vp.pos.y + subplot_vp.size.y * 0.5
	layer_outputs_push_line(out, 24, Render_Line{
		from = {subplot_vp.pos.x, mid_y},
		to   = {rect_right(subplot_vp), mid_y},
		color = ui.with_alpha(ui.COL_WHITE, 0.08),
		thickness = 1,
	})

	// Draw delta volume bars.
	slot_w := subplot_vp.size.x / f32(max(n, 1))
	bar_w := max(slot_w * 0.7, 1)
	half_h := subplot_vp.size.y * 0.5

	for i in 0 ..< n {
		delta := entries[i].values[2]
		frac := f32(delta / max_abs)
		frac = clamp(frac, -1, 1)
		bar_h := half_h * (frac >= 0 ? frac : -frac)
		if bar_h < 1 do bar_h = 1
		x_center := subplot_vp.pos.x + (f32(i) + 0.5) * slot_w
		col := delta >= 0 ? ui.COL_GREEN : ui.COL_RED
		bar_y := delta >= 0 ? mid_y - bar_h : mid_y
		layer_outputs_push_bar(out, 25, Render_Bar{
			rect = ui.Rect{pos = {x_center - bar_w * 0.5, bar_y}, size = {bar_w, bar_h}},
			color = ui.with_alpha(col, 0.6),
		})
	}

	// Label.
	if n > 0 {
		latest_dv := entries[n - 1].values[2]
		label_buf: [32]u8
		label := fmt.bprintf(label_buf[:], "DV %.1f", latest_dv)
		label_col := latest_dv >= 0 ? ui.COL_GREEN : ui.COL_RED
		layer_outputs_push_text_badge(out, 26, text_badge_make(
			{subplot_vp.pos.x + 4, subplot_vp.pos.y + 10},
			label, label_col, ui.FONT_SIZE_XS,
		))
	}
}

// OI subplot: line chart of open interest.
subplot_oi_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs, subplot_vp: ui.Rect) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	store := &ctx.stream.analytics
	if store.count <= 0 do return

	entries: [SUBPLOT_COLLECT_CAP]services.Analytics_Entry
	n := services.analytics_collect_by_kind(store, .Open_Interest, entries[:])
	if n < 2 do return

	// Find min/max OI for y-axis scaling.
	min_val := entries[0].values[0]
	max_val := entries[0].values[0]
	for i in 1 ..< n {
		v := entries[i].values[0]
		if v < min_val do min_val = v
		if v > max_val do max_val = v
	}
	if max_val <= min_val {
		max_val = min_val + 1.0
	}

	// Subplot background + divider.
	subplot_push_bg(out, subplot_vp)

	// Draw OI line segments.
	slot_w := subplot_vp.size.x / f32(max(n - 1, 1))
	for i in 1 ..< n {
		x0 := subplot_vp.pos.x + f32(i - 1) * slot_w
		x1 := subplot_vp.pos.x + f32(i) * slot_w
		y0 := subplot_val_to_y(subplot_vp, min_val, max_val, entries[i - 1].values[0])
		y1 := subplot_val_to_y(subplot_vp, min_val, max_val, entries[i].values[0])
		layer_outputs_push_line(out, 25, Render_Line{
			from = {x0, y0}, to = {x1, y1},
			color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.85),
			thickness = 1.5,
		})
	}

	// Label.
	latest_oi := entries[n - 1].values[0]
	label_buf: [32]u8
	label := fmt.bprintf(label_buf[:], "OI %.0f", latest_oi)
	layer_outputs_push_text_badge(out, 26, text_badge_make(
		{subplot_vp.pos.x + 4, subplot_vp.pos.y + 10},
		label, ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS,
	))
}

// Helper: draw subplot background + top divider line.
@(private = "file")
subplot_push_bg :: proc(out: ^Layer_Outputs, vp: ui.Rect) {
	layer_outputs_push_bar(out, 23, Render_Bar{
		rect = vp,
		color = ui.with_alpha(ui.COL_SURFACE_1, 0.6),
	})
	layer_outputs_push_line(out, 24, Render_Line{
		from = {vp.pos.x, vp.pos.y},
		to   = {rect_right(vp), vp.pos.y},
		color = ui.COL_DIVIDER,
		thickness = 1,
	})
}

// Helper: map a value to y-coordinate within a subplot viewport.
@(private = "file")
subplot_val_to_y :: proc(vp: ui.Rect, min_val, max_val, val: f64) -> f32 {
	if max_val <= min_val do return vp.pos.y + vp.size.y * 0.5
	// Inset 4px top/bottom for padding.
	pad := f32(4)
	usable_h := vp.size.y - pad * 2
	t := f32((val - min_val) / (max_val - min_val))
	t = clamp(t, 0, 1)
	return vp.pos.y + pad + usable_h * (1.0 - t)
}

analytics_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .Analytics,
		name        = "Analytics",
		// S86: Match on Analytics bit only — Bundle_Candles and Bundle_Analytics already include bit 6.
		bundle_mask = u32(Layer_Bundle.Analytics),
		z_order     = layer_z_order_for_id(.Analytics),
		init        = layer_noop_init,
		on_event    = layer_noop_on_event,
		on_snapshot = layer_noop_on_snapshot,
		render      = analytics_render,
		reset       = layer_noop_reset,
		diagnostics = analytics_diagnostics,
	}
}
