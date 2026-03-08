package layers

import "core:testing"
import "mr:ports"
import "mr:services"
import "mr:ui"

@(private = "file")
make_ctx :: proc(store: ^Market_Store, subject_id: u64, viewport: ui.Rect) -> Layer_Context {
	stream := market_store_stream_for_subject(store, subject_id)
	return Layer_Context{
		store = store,
		stream = stream,
		subject_id = subject_id,
		now_ms = 1_700_000_000_000,
		frame_seq = 1,
		viewport = viewport,
		capabilities = layer_capabilities_from_stream(stream),
		signal_evidence_link_enabled = true,
	}
}

@(private = "file")
apply_event :: proc(store: ^Market_Store, evt: ports.MD_Event) {
	e := evt
	_ = market_store_apply_event(store, &e)
}

@(private = "file")
fixed24 :: proc(s: string) -> [24]u8 {
	out: [24]u8
	n := min(len(s), len(out))
	for i in 0 ..< n {
		out[i] = s[i]
	}
	return out
}

@(private = "file")
fixed12 :: proc(s: string) -> [12]u8 {
	out: [12]u8
	n := min(len(s), len(out))
	for i in 0 ..< n {
		out[i] = s[i]
	}
	return out
}

@(private = "file")
fixed96 :: proc(s: string) -> [96]u8 {
	out: [96]u8
	n := min(len(s), len(out))
	for i in 0 ..< n {
		out[i] = s[i]
	}
	return out
}

@(test)
test_price_candles_layer_renders_expected_primitive_count :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(111)
	for i in 0 ..< 3 {
		apply_event(store, ports.MD_Event{
			source = {subject_id = sid, channel = .Candles, seq = i64(i + 1)},
			kind = .Candle,
			unix = i64(100 + i),
			data = {candle = ports.MD_Candle_Event{
				open = 100 + f64(i),
				high = 101 + f64(i),
				low = 99 + f64(i),
				close = 100.5 + f64(i),
				volume = 1,
				buy_vol = 0.5,
				sell_vol = 0.5,
				trade_count = 1,
				window_start_ts = i64(i) * 60_000,
				window_end_ts = i64(i + 1) * 60_000,
				is_closed = true,
			}},
		})
	}
	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {300, 180}})
	ctx.active_bundle = u32(Layer_Bundle.Bundle_Candles) // S86: must set active_bundle for bar rendering
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	price_candles_layer_strategy().render(&ctx, out)
	testing.expect_value(t, out.count, 7)
	testing.expect_value(t, out.items[0].kind, Render_Primitive_Kind.Line)
	testing.expect_value(t, out.items[1].kind, Render_Primitive_Kind.Bar)
}

@(test)
test_trades_tape_layer_renders_expected_rows :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(222)
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 1},
		kind = .Trade,
		unix = 100,
		data = {trade = {price = 101.5, qty = 1.25, is_buy = true, unix = 100}},
	})
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 2},
		kind = .Trade,
		unix = 101,
		data = {trade = {price = 101.0, qty = 0.75, is_buy = false, unix = 101}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {260, 160}})
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	trades_tape_layer_strategy().render(&ctx, out)
	testing.expect_value(t, out.count, 4)
}

@(test)
test_orderbook_dom_layer_emits_depth_and_spread_badge :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(333)
	asks := [?]f64{101.0, 101.5}
	asizes := [?]f64{1.0, 0.5}
	bids := [?]f64{100.5, 100.0}
	bsizes := [?]f64{1.2, 0.6}
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Orderbook, seq = 1},
		kind = .Orderbook_Snapshot,
		unix = 120,
		data = {ob = {
			ask_prices = raw_data(asks[:]),
			ask_sizes = raw_data(asizes[:]),
			bid_prices = raw_data(bids[:]),
			bid_sizes = raw_data(bsizes[:]),
			ask_count = len(asks),
			bid_count = len(bids),
			is_snapshot = true,
			last_price = 100.75,
			unix = 120,
		}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {280, 180}})
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	orderbook_dom_layer_strategy().render(&ctx, out)
	testing.expect(t, out.count >= 4)
}

@(private = "file")
g_stub_events: [16]ports.MD_Event
@(private = "file")
g_stub_count: int
@(private = "file")
g_stub_polled: bool

@(private = "file")
stub_poll :: proc(events_buf: []ports.MD_Event) -> int {
	if g_stub_polled do return 0
	n := min(len(events_buf), g_stub_count)
	for i in 0 ..< n {
		events_buf[i] = g_stub_events[i]
	}
	g_stub_polled = true
	return n
}

@(private = "file")
stub_now_ms :: proc() -> i64 {
	return 1_710_000_000_000
}

@(test)
test_datasource_integration_three_layers_bounded_state :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	ds := new(Data_Source)
	defer free(ds)
	reg := new(Layer_Registry)
	defer free(reg)
	layer_registry_init(reg, store)
	layer_registry_set_enabled(reg, .OrderBook_DOM, false)
	layer_registry_set_enabled(reg, .VPVR_Heatmap, false)
	layer_registry_set_enabled(reg, .Evidence, false)

	sid := u64(901)
	g_stub_events = {}
	g_stub_count = 0
	g_stub_polled = false

	g_stub_events[g_stub_count] = ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 1},
		kind = .Trade,
		unix = 100,
		data = {trade = {price = 200.0, qty = 1.0, is_buy = true, unix = 100}},
	}
	g_stub_count += 1
	g_stub_events[g_stub_count] = ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 2},
		kind = .Candle,
		unix = 101,
		data = {candle = {
			open = 199.5, high = 201.0, low = 198.8, close = 200.2,
			volume = 3.4, buy_vol = 2.0, sell_vol = 1.4,
			trade_count = 3, window_start_ts = 0, window_end_ts = 60_000, is_closed = true,
		}},
	}
	g_stub_count += 1
	g_stub_events[g_stub_count] = ports.MD_Event{
		source = {subject_id = sid, channel = .Signals, seq = 3},
		kind = .Signal,
		unix = 102,
		data = {signal = {
			kind = fixed24("trend"), kind_len = 5,
			severity = fixed12("low"), severity_len = 3,
			confidence = 0.7,
			reason = fixed96("momentum"), reason_len = 8,
			regime = fixed24("bull"), regime_len = 4,
			regime_strength = 0.5,
			unix = 102,
		}},
	}
	g_stub_count += 1

	md := ports.Marketdata_Port{poll = stub_poll, now_ms = stub_now_ms}
	result := data_source_poll_and_apply(ds, md, store)
	testing.expect_value(t, result.processed, g_stub_count)
	testing.expect(t, store.stream_count <= MARKET_STREAM_CAP)

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {300, 180}})
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	bundle := u32(Layer_Bundle.Bundle_Candles | Layer_Bundle.Bundle_Trades)
	layer_registry_render_bundle(reg, bundle, &ctx, out)
	testing.expect(t, out.count > 0)
	testing.expect(t, out.count <= LAYER_OUTPUT_CAP)
}

@(test)
test_layer_capabilities_gate_blocks_render_when_disabled_in_ctx :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(444)
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 1},
		kind = .Trade,
		unix = 100,
		data = {trade = {price = 101.0, qty = 1.0, is_buy = true, unix = 100}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {220, 120}})
	ctx.capabilities.has_trades = false
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	trades_tape_layer_strategy().render(&ctx, out)
	testing.expect_value(t, out.count, 0)
}

@(test)
test_analytics_layer_renders_oi_and_dv :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(666)
	// Push OI event.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 1},
		kind = .Open_Interest,
		unix = 100,
		data = {open_interest = ports.MD_Open_Interest_Event{
			open_interest = 50000, delta = 100, delta_pct = 0.002,
			window_start_ts = 0, window_end_ts = 60_000, unix = 100,
		}},
	})
	// Push DV event.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 2},
		kind = .Delta_Volume,
		unix = 101,
		data = {delta_volume = ports.MD_Delta_Volume_Event{
			buy_volume = 25.5, sell_volume = 20.3, delta_volume = 5.2,
			window_start_ts = 0, window_end_ts = 60_000, unix = 101,
		}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {300, 180}})
	testing.expect_value(t, ctx.capabilities.has_analytics, true)

	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	analytics_layer_strategy().render(&ctx, out)
	// Unfiltered: should render both OI and DV text badges + bars.
	testing.expect(t, out.count >= 4, "analytics should emit at least 4 primitives for OI+DV")

	// Filtered mode: only OI.
	layer_outputs_reset(out)
	ctx.analytics_filter = true
	ctx.analytics_kind = .Open_Interest
	analytics_layer_strategy().render(&ctx, out)
	oi_count := out.count

	layer_outputs_reset(out)
	ctx.analytics_kind = .Delta_Volume
	analytics_layer_strategy().render(&ctx, out)
	dv_count := out.count

	testing.expect(t, oi_count > 0, "filtered OI should emit primitives")
	testing.expect(t, dv_count > 0, "filtered DV should emit primitives")
}

@(test)
test_analytics_layer_no_data_emits_nothing :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(777)
	// Create stream but no analytics events.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 1},
		kind = .Candle,
		unix = 100,
		data = {candle = {open = 1, high = 2, low = 0.5, close = 1.5, volume = 1, buy_vol = 0.5, sell_vol = 0.5, trade_count = 1, window_start_ts = 0, window_end_ts = 60_000, is_closed = true}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {300, 180}})
	testing.expect_value(t, ctx.capabilities.has_analytics, false)

	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	analytics_layer_strategy().render(&ctx, out)
	testing.expect_value(t, out.count, 0)
}

@(test)
test_market_store_reduces_analytics_events :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(888)

	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 1},
		kind = .Open_Interest,
		unix = 100,
		data = {open_interest = ports.MD_Open_Interest_Event{
			open_interest = 42000, delta = -500, delta_pct = -0.012,
			window_start_ts = 0, window_end_ts = 60_000, unix = 100,
		}},
	})

	stream := market_store_stream_for_subject(store, sid)
	testing.expect(t, stream != nil, "stream should be allocated")
	testing.expect_value(t, stream.analytics.count, 1)

	entry := services.get_analytics(&stream.analytics, 0)
	testing.expect_value(t, entry.kind, services.Analytics_Kind.Open_Interest)
	testing.expect_value(t, entry.values[0], 42000.0)
	testing.expect_value(t, entry.values[1], -500.0)
}

@(test)
test_layer_registry_collect_diagnostics_reports_state :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	reg := new(Layer_Registry)
	defer free(reg)
	layer_registry_init(reg, store)

	sid := u64(555)
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 1},
		kind = .Candle,
		unix = 100,
		data = {candle = {
			open = 1, high = 2, low = 0.5, close = 1.5,
			volume = 1, buy_vol = 0.5, sell_vol = 0.5,
			trade_count = 1, window_start_ts = 0, window_end_ts = 60_000, is_closed = true,
		}},
	})
	market_store_set_active_subject(store, sid)

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {220, 120}})
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	layer_registry_render_bundle(reg, u32(Layer_Bundle.Bundle_Candles), &ctx, out)

	diags: [LAYER_REGISTRY_CAP]Layer_Diagnostics
	n := layer_registry_collect_diagnostics(reg, store, diags[:])
	testing.expect(t, n > 0)
	found_price := false
	for i in 0 ..< n {
		testing.expect(t, diags[i].entries >= 0, "entries should be non-negative")
		testing.expect(t, diags[i].max_entries >= 0, "max_entries should be non-negative")
		testing.expect(t, diags[i].entries <= diags[i].max_entries, "entries should be bounded by max_entries")
		testing.expect(t, diags[i].render_p95_us >= 0, "render_p95_us should be non-negative")
		testing.expect(t, diags[i].render_p99_us >= 0, "render_p99_us should be non-negative")
		if diags[i].id == .Price_Candles {
			found_price = true
			testing.expect_value(t, diags[i].enabled, true)
			testing.expect_value(t, diags[i].has_data, true)
			testing.expect(t, diags[i].render_invocations > 0)
		}
	}
	testing.expect_value(t, found_price, true)
}

// ═══════════════════════════════════════════════════════════════
// S87: Stats Panel + Trade Counter layer tests
// ═══════════════════════════════════════════════════════════════

@(test)
test_stats_panel_renders_stats_text_not_candles :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(1001)
	// Push stats event (no candle data).
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Stats, seq = 1},
		kind = .Stats,
		unix = 100,
		data = {stats = {
			mark_price = 42500.0, funding = 0.0001,
			tbuy = 41000.0, tsell = 44000.0,
			window_ms = 60_000, quality_flags = 0,
		}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {280, 160}})
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	stats_panel_layer_strategy().render(&ctx, out)
	// Should emit: Mark, Funding, Liq, Window, Quality = 5 text badges.
	testing.expect_value(t, out.count, 5)
	// All should be text badges.
	for i in 0 ..< out.count {
		testing.expect_value(t, out.items[i].kind, Render_Primitive_Kind.Text_Badge)
	}
}

@(test)
test_stats_panel_no_stats_emits_nothing :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(1002)
	// Push only candle data — no stats.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 1},
		kind = .Candle,
		unix = 100,
		data = {candle = {open = 1, high = 2, low = 0.5, close = 1.5, volume = 1, buy_vol = 0.5, sell_vol = 0.5, trade_count = 1, window_start_ts = 0, window_end_ts = 60_000, is_closed = true}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {280, 160}})
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	stats_panel_layer_strategy().render(&ctx, out)
	testing.expect_value(t, out.count, 0)
}

@(test)
test_trade_counter_renders_aggregates_not_tape :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(1003)
	// Push candle with trade counts.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 1},
		kind = .Candle,
		unix = 100,
		data = {candle = {
			open = 100, high = 105, low = 98, close = 103,
			volume = 50.0, buy_vol = 30.0, sell_vol = 20.0,
			trade_count = 142,
			window_start_ts = 0, window_end_ts = 60_000, is_closed = true,
		}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {280, 160}})
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	trade_counter_layer_strategy().render(&ctx, out)
	// Should emit: Trades count, Vol summary, 2 bars (buy/sell), ratio label, Last price = 6.
	testing.expect(t, out.count >= 5, "counter should emit at least 5 primitives")
	// First should be text badge (trade count).
	testing.expect_value(t, out.items[0].kind, Render_Primitive_Kind.Text_Badge)
}

@(test)
test_trade_counter_no_candles_emits_nothing :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(1004)
	// Push only trade events — no candle aggregates.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 1},
		kind = .Trade,
		unix = 100,
		data = {trade = {price = 101.0, qty = 1.0, is_buy = true, unix = 100}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {280, 160}})
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	trade_counter_layer_strategy().render(&ctx, out)
	testing.expect_value(t, out.count, 0)
}

@(test)
test_bundle_stats_does_not_trigger_price_candles_layer :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	reg := new(Layer_Registry)
	defer free(reg)
	layer_registry_init(reg, store)

	sid := u64(1005)
	// Push both candle and stats data.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 1},
		kind = .Candle,
		unix = 100,
		data = {candle = {open = 100, high = 102, low = 99, close = 101, volume = 10, buy_vol = 6, sell_vol = 4, trade_count = 20, window_start_ts = 0, window_end_ts = 60_000, is_closed = true}},
	})
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Stats, seq = 2},
		kind = .Stats,
		unix = 101,
		data = {stats = {mark_price = 101.0, funding = 0.0002, tbuy = 98.0, tsell = 104.0, window_ms = 60_000, quality_flags = 0}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {280, 160}})
	ctx.active_bundle = u32(Layer_Bundle.Bundle_Stats)
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	layer_registry_render_bundle(reg, u32(Layer_Bundle.Bundle_Stats), &ctx, out)

	// Should have output from Stats_Panel + Signal, but NOT from Price_Candles.
	// Verify no Line or Bar primitives (candle wicks/bodies) — stats panel emits only text badges.
	has_candle_primitive := false
	for i in 0 ..< out.count {
		p := out.items[i]
		if p.kind == .Line && p.z == 20 do has_candle_primitive = true  // z=20 is Price_Candles z-order
		if p.kind == .Bar && p.z == 21 do has_candle_primitive = true
	}
	testing.expect(t, !has_candle_primitive, "Bundle_Stats must not trigger Price_Candles layer")
	testing.expect(t, out.count > 0, "Bundle_Stats should produce output from Stats_Panel")
}

@(test)
test_bundle_counter_does_not_trigger_trades_tape_layer :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	reg := new(Layer_Registry)
	defer free(reg)
	layer_registry_init(reg, store)

	sid := u64(1006)
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 1},
		kind = .Candle,
		unix = 100,
		data = {candle = {open = 100, high = 102, low = 99, close = 101, volume = 10, buy_vol = 6, sell_vol = 4, trade_count = 20, window_start_ts = 0, window_end_ts = 60_000, is_closed = true}},
	})
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 2},
		kind = .Trade,
		unix = 101,
		data = {trade = {price = 101.5, qty = 1.25, is_buy = true, unix = 101}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {280, 160}})
	ctx.active_bundle = u32(Layer_Bundle.Bundle_Counter)
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	layer_registry_render_bundle(reg, u32(Layer_Bundle.Bundle_Counter), &ctx, out)

	// Should have output from Trade_Counter + Signal, but NOT from Trades_Tape.
	has_tape_primitive := false
	for i in 0 ..< out.count {
		p := out.items[i]
		if p.z == 40 || p.z == 41 do has_tape_primitive = true  // z=40,41 are Trades_Tape z-orders
	}
	testing.expect(t, !has_tape_primitive, "Bundle_Counter must not trigger Trades_Tape layer")
	testing.expect(t, out.count > 0, "Bundle_Counter should produce output from Trade_Counter")
}

// ═══════════════════════════════════════════════════════════════
// S94: Analytics subplot tests
// ═══════════════════════════════════════════════════════════════

@(test)
test_subplot_cvd_renders_line_segments :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(2001)

	// Push 5 CVD events to get enough data for line segments.
	for i in 0 ..< 5 {
		apply_event(store, ports.MD_Event{
			source = {subject_id = sid, channel = .Candles, seq = i64(i + 1)},
			kind = .CVD,
			unix = i64(100 + i),
			data = {cvd = ports.MD_CVD_Event{
				delta_volume = f64(i) * 2.0,
				cvd = f64(i) * 10.0,
				window_start_ts = i64(i) * 60_000,
				window_end_ts = i64(i + 1) * 60_000,
				unix = i64(100 + i),
			}},
		})
	}

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {300, 200}})
	subplot_vp := ui.Rect{pos = {0, 150}, size = {300, 50}}
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	subplot_cvd_render(&ctx, out, subplot_vp)

	// Should emit: bg bar + divider line + zero line + 4 line segments + label = 8 primitives.
	testing.expect(t, out.count >= 6, "CVD subplot should emit at least 6 primitives for 5 entries")

	// Verify at least one line segment exists (z=25 for CVD lines).
	has_cvd_line := false
	for i in 0 ..< out.count {
		if out.items[i].kind == .Line && out.items[i].z == 25 do has_cvd_line = true
	}
	testing.expect(t, has_cvd_line, "CVD subplot should emit line segments at z=25")

	// Verify label text badge exists.
	has_label := false
	for i in 0 ..< out.count {
		if out.items[i].kind == .Text_Badge && out.items[i].z == 26 do has_label = true
	}
	testing.expect(t, has_label, "CVD subplot should emit label badge at z=26")
}

@(test)
test_subplot_delta_vol_renders_bars :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(2002)

	// Push 4 Delta Volume events.
	for i in 0 ..< 4 {
		delta := f64(i) * 3.0 - 4.0  // mix of positive and negative
		apply_event(store, ports.MD_Event{
			source = {subject_id = sid, channel = .Candles, seq = i64(i + 1)},
			kind = .Delta_Volume,
			unix = i64(100 + i),
			data = {delta_volume = ports.MD_Delta_Volume_Event{
				buy_volume = 10.0 + f64(i),
				sell_volume = 8.0 + f64(i),
				delta_volume = delta,
				window_start_ts = i64(i) * 60_000,
				window_end_ts = i64(i + 1) * 60_000,
				unix = i64(100 + i),
			}},
		})
	}

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {300, 200}})
	subplot_vp := ui.Rect{pos = {0, 150}, size = {300, 50}}
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	subplot_delta_vol_render(&ctx, out, subplot_vp)

	// Should emit: bg + divider + zero line + 4 bars + label = 8 primitives.
	testing.expect(t, out.count >= 6, "DV subplot should emit at least 6 primitives for 4 entries")

	// Verify delta vol bars at z=25.
	bar_count := 0
	for i in 0 ..< out.count {
		if out.items[i].kind == .Bar && out.items[i].z == 25 do bar_count += 1
	}
	testing.expect_value(t, bar_count, 4)
}

@(test)
test_subplot_oi_renders_line_segments :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(2003)

	// Push 3 OI events.
	for i in 0 ..< 3 {
		apply_event(store, ports.MD_Event{
			source = {subject_id = sid, channel = .Candles, seq = i64(i + 1)},
			kind = .Open_Interest,
			unix = i64(100 + i),
			data = {open_interest = ports.MD_Open_Interest_Event{
				open_interest = 50000.0 + f64(i) * 100.0,
				delta = f64(i) * 10.0,
				delta_pct = 0.001,
				window_start_ts = i64(i) * 60_000,
				window_end_ts = i64(i + 1) * 60_000,
				unix = i64(100 + i),
			}},
		})
	}

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {300, 200}})
	subplot_vp := ui.Rect{pos = {0, 150}, size = {300, 50}}
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	subplot_oi_render(&ctx, out, subplot_vp)

	// Should emit: bg + divider + 2 line segments + label = 5 primitives.
	testing.expect(t, out.count >= 4, "OI subplot should emit at least 4 primitives for 3 entries")

	// Verify OI line at z=25.
	has_oi_line := false
	for i in 0 ..< out.count {
		if out.items[i].kind == .Line && out.items[i].z == 25 do has_oi_line = true
	}
	testing.expect(t, has_oi_line, "OI subplot should emit line segments at z=25")
}

@(test)
test_subplot_cvd_no_data_emits_nothing :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(2004)

	// Push only candle data — no CVD events.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 1},
		kind = .Candle,
		unix = 100,
		data = {candle = {open = 1, high = 2, low = 0.5, close = 1.5, volume = 1, buy_vol = 0.5, sell_vol = 0.5, trade_count = 1, window_start_ts = 0, window_end_ts = 60_000, is_closed = true}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {300, 200}})
	subplot_vp := ui.Rect{pos = {0, 150}, size = {300, 50}}
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	subplot_cvd_render(&ctx, out, subplot_vp)
	testing.expect_value(t, out.count, 0)
}

@(test)
test_subplot_cvd_single_entry_needs_two :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(2005)

	// Push only 1 CVD entry — need at least 2 for line segments.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Candles, seq = 1},
		kind = .CVD,
		unix = 100,
		data = {cvd = ports.MD_CVD_Event{
			delta_volume = 5.0, cvd = 10.0,
			window_start_ts = 0, window_end_ts = 60_000, unix = 100,
		}},
	})

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {300, 200}})
	subplot_vp := ui.Rect{pos = {0, 150}, size = {300, 50}}
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	subplot_cvd_render(&ctx, out, subplot_vp)
	// Single entry: not enough for a line chart.
	testing.expect_value(t, out.count, 0)
}

@(test)
test_subplot_flags_count :: proc(t: ^testing.T) {
	testing.expect_value(t, subplot_flags_count(Subplot_Flags{}), 0)
	testing.expect_value(t, subplot_flags_count(Subplot_Flags{show_cvd = true}), 1)
	testing.expect_value(t, subplot_flags_count(Subplot_Flags{show_cvd = true, show_delta_vol = true}), 2)
	testing.expect_value(t, subplot_flags_count(Subplot_Flags{show_cvd = true, show_delta_vol = true, show_oi = true}), 3)
}

@(test)
test_analytics_collect_by_kind :: proc(t: ^testing.T) {
	store: services.Analytics_Store
	// Push mixed entries: OI, DV, CVD, OI, DV.
	services.push_analytics(&store, services.Analytics_Entry{kind = .Open_Interest, ts_ms = 1, values = {100, 0, 0, 0, 0, 0, 0, 0}})
	services.push_analytics(&store, services.Analytics_Entry{kind = .Delta_Volume, ts_ms = 2, values = {10, 8, 2, 0, 0, 0, 0, 0}})
	services.push_analytics(&store, services.Analytics_Entry{kind = .CVD, ts_ms = 3, values = {5, 15, 0, 0, 0, 0, 0, 0}})
	services.push_analytics(&store, services.Analytics_Entry{kind = .Open_Interest, ts_ms = 4, values = {200, 0, 0, 0, 0, 0, 0, 0}})
	services.push_analytics(&store, services.Analytics_Entry{kind = .Delta_Volume, ts_ms = 5, values = {12, 9, 3, 0, 0, 0, 0, 0}})

	// Collect OI entries — should get 2 in oldest-first order.
	oi_entries: [8]services.Analytics_Entry
	n := services.analytics_collect_by_kind(&store, .Open_Interest, oi_entries[:])
	testing.expect_value(t, n, 2)
	testing.expect_value(t, oi_entries[0].values[0], 100.0)  // oldest first
	testing.expect_value(t, oi_entries[1].values[0], 200.0)

	// Collect CVD — should get 1.
	cvd_entries: [8]services.Analytics_Entry
	n2 := services.analytics_collect_by_kind(&store, .CVD, cvd_entries[:])
	testing.expect_value(t, n2, 1)
	testing.expect_value(t, cvd_entries[0].values[1], 15.0)

	// Collect Bar_Stats — should get 0.
	bs_entries: [8]services.Analytics_Entry
	n3 := services.analytics_collect_by_kind(&store, .Bar_Stats, bs_entries[:])
	testing.expect_value(t, n3, 0)
}
