package layers

import "core:fmt"
import "core:math"
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
price_candles_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs) {
	if ctx == nil || out == nil || ctx.stream == nil do return
	if !ctx.capabilities.has_candles do return
	store := &ctx.stream.candles
	if store.count <= 0 do return

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

	latest := services.get_candle_newest(store, 0)
	latest_buf: [64]u8
	latest_str := fmt.bprintf(latest_buf[:], "Last %.2f", latest.close)
	layer_outputs_push_text_badge(out, 26, text_badge_make(
		{ctx.viewport.pos.x + 6, ctx.viewport.pos.y + 14},
		latest_str,
		ui.COL_TEXT_PRIMARY,
		ui.FONT_SIZE_XS,
	))
}

@(private = "file")
price_candles_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .Price_Candles
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && stream.candles.count > 0
}

price_candles_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .Price_Candles,
		name        = "Price/Candles",
		bundle_mask = u32(Layer_Bundle.Price_Candles | Layer_Bundle.Bundle_Candles | Layer_Bundle.Bundle_Stats),
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

	rows := min(store.count, 18)
	row_h := max(ctx.viewport.size.y / f32(max(rows, 1)), 12)
	max_qty := f64(0)
	for i in 0 ..< rows {
		t := services.get_trade(store, i)
		if t.qty > max_qty do max_qty = t.qty
	}
	if max_qty <= 0 do max_qty = 1

	for i in 0 ..< rows {
		t := services.get_trade(store, i)
		y := ctx.viewport.pos.y + f32(i) * row_h
		frac := f32(t.qty / max_qty)
		frac = clamp(frac, 0.05, 1.0)
		w := (ctx.viewport.size.x - 4) * frac
		col := t.side == .Buy ? ui.COL_GREEN : ui.COL_RED

		x := ctx.viewport.pos.x + 2
		if t.side == .Sell {
			x = rect_right(ctx.viewport) - 2 - w
		}
		layer_outputs_push_bar(out, 40, Render_Bar{
			rect = ui.Rect{pos = {x, y + 1}, size = {w, row_h - 2}},
			color = ui.with_alpha(col, 0.35),
		})

		line_buf: [80]u8
		line := fmt.bprintf(line_buf[:], "%.2f x %.4f", t.price, t.qty)
		layer_outputs_push_text_badge(out, 41, text_badge_make(
			{ctx.viewport.pos.x + 6, y + row_h * 0.7},
			line,
			ui.COL_TEXT_SECONDARY,
			ui.FONT_SIZE_XS,
		))
	}
}

@(private = "file")
trades_tape_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .Trades_Tape
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && stream.trades.count > 0
}

trades_tape_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .Trades_Tape,
		name        = "Trades Tape",
		bundle_mask = u32(Layer_Bundle.Trades_Tape | Layer_Bundle.Bundle_Trades | Layer_Bundle.Bundle_DOM | Layer_Bundle.Bundle_Counter),
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
	row_h := max(ctx.viewport.size.y / f32(max(rows, 1)), 10)
	max_size := f64(0)
	for i in 0 ..< rows {
		if i < ob.ask_count && ob.ask_sizes[i] > max_size do max_size = ob.ask_sizes[i]
		if i < ob.bid_count && ob.bid_sizes[i] > max_size do max_size = ob.bid_sizes[i]
	}
	if max_size <= 0 do max_size = 1

	layer_outputs_push_line(out, 30, Render_Line{
		from = {mid_x, ctx.viewport.pos.y},
		to = {mid_x, rect_bottom(ctx.viewport)},
		color = ui.with_alpha(ui.COL_WHITE, 0.12),
		thickness = 1,
	})

	for i in 0 ..< rows {
		y := ctx.viewport.pos.y + f32(i) * row_h
		if i < ob.ask_count {
			ask_frac := f32(ob.ask_sizes[i] / max_size)
			ask_w := (ctx.viewport.size.x * 0.5 - 6) * clamp(ask_frac, 0.05, 1)
			layer_outputs_push_bar(out, 31, Render_Bar{
				rect = ui.Rect{pos = {mid_x + 2, y + 1}, size = {ask_w, row_h - 2}},
				color = ui.with_alpha(ui.COL_RED, 0.35),
			})
		}
		if i < ob.bid_count {
			bid_frac := f32(ob.bid_sizes[i] / max_size)
			bid_w := (ctx.viewport.size.x * 0.5 - 6) * clamp(bid_frac, 0.05, 1)
			layer_outputs_push_bar(out, 31, Render_Bar{
				rect = ui.Rect{pos = {mid_x - 2 - bid_w, y + 1}, size = {bid_w, row_h - 2}},
				color = ui.with_alpha(ui.COL_GREEN, 0.35),
			})
		}
	}

	spread := services.spread(ob)
	spread_buf: [64]u8
	spread_str := fmt.bprintf(spread_buf[:], "Spread %.2f", spread)
	layer_outputs_push_text_badge(out, 33, text_badge_make(
		{ctx.viewport.pos.x + 6, ctx.viewport.pos.y + 12},
		spread_str,
		ui.COL_TEXT_PRIMARY,
		ui.FONT_SIZE_XS,
	))
}

@(private = "file")
orderbook_dom_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	if out == nil do return
	out.id = .OrderBook_DOM
	if store == nil do return
	stream := market_store_active_stream(store)
	out.has_data = stream != nil && (stream.orderbook.ask_count > 0 || stream.orderbook.bid_count > 0)
}

orderbook_dom_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .OrderBook_DOM,
		name        = "OrderBook/DOM",
		bundle_mask = u32(Layer_Bundle.OrderBook_DOM | Layer_Bundle.Bundle_Orderbook | Layer_Bundle.Bundle_DOM),
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
}

vpvr_heatmap_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .VPVR_Heatmap,
		name        = "VPVR/Heatmap",
		bundle_mask = u32(Layer_Bundle.VPVR_Heatmap | Layer_Bundle.Bundle_Candles | Layer_Bundle.Bundle_Heatmap | Layer_Bundle.Bundle_VPVR),
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
}

evidence_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .Evidence,
		name        = "Evidence",
		bundle_mask = u32(Layer_Bundle.Evidence | Layer_Bundle.Bundle_Candles | Layer_Bundle.Bundle_Trades | Layer_Bundle.Bundle_Orderbook | Layer_Bundle.Bundle_DOM | Layer_Bundle.Bundle_Heatmap | Layer_Bundle.Bundle_VPVR),
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
		line := fmt.bprintf(line_buf[:], "S %s %.2f %s", kind, e.confidence, reason)
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
}

signal_layer_strategy :: proc() -> Layer_Strategy {
	return Layer_Strategy{
		id          = .Signal,
		name        = "Signal",
		bundle_mask = u32(Layer_Bundle.Signal | Layer_Bundle.Bundle_Candles | Layer_Bundle.Bundle_Trades | Layer_Bundle.Bundle_Orderbook | Layer_Bundle.Bundle_DOM | Layer_Bundle.Bundle_Heatmap | Layer_Bundle.Bundle_VPVR | Layer_Bundle.Bundle_Stats | Layer_Bundle.Bundle_Counter),
		z_order     = layer_z_order_for_id(.Signal),
		init        = layer_noop_init,
		on_event    = layer_noop_on_event,
		on_snapshot = layer_noop_on_snapshot,
		render      = signal_render,
		reset       = layer_noop_reset,
		diagnostics = signal_diagnostics,
	}
}
