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
	ctx.tf_ms = 60_000 // S140: 1m timeframe for time axis
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	price_candles_layer_strategy().render(&ctx, out)
	// S140: 3 candles * 2 (line+bar) + 1 text badge + time axis primitives.
	testing.expect(t, out.count >= 7, "candle layer should emit at least 7 primitives (3 candles + badge + time axis)")
	testing.expect_value(t, out.items[0].kind, Render_Primitive_Kind.Line)
	testing.expect_value(t, out.items[1].kind, Render_Primitive_Kind.Bar)
}

// S139: Chart viewport scroll — rendering respects scroll_offset.
@(test)
test_price_candles_viewport_scroll :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(139)
	for i in 0 ..< 10 {
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
	ctx.active_bundle = u32(Layer_Bundle.Bundle_Candles)
	// S139: Show only 5 candles, scrolled 3 from live edge.
	ctx.chart_viewport = Chart_Viewport{visible_count = 5, scroll_offset = 3}
	ctx.tf_ms = 60_000 // S140
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	price_candles_layer_strategy().render(&ctx, out)
	// S140: 5 candles * 2 (line+bar) + 1 text badge + time axis primitives.
	testing.expect(t, out.count >= 11, "scroll test should emit at least 11 primitives")
}

// S139: Chart viewport zoom — visible_count controls candle count.
@(test)
test_price_candles_viewport_zoom :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(1390)
	for i in 0 ..< 20 {
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
	ctx.active_bundle = u32(Layer_Bundle.Bundle_Candles)
	// S139: Zoom to show only 10 candles (no scroll).
	ctx.chart_viewport = Chart_Viewport{visible_count = 10, scroll_offset = 0}
	ctx.tf_ms = 60_000 // S140
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	price_candles_layer_strategy().render(&ctx, out)
	// S140: 10 candles * 2 (line+bar) + 1 text badge + time axis primitives.
	testing.expect(t, out.count >= 21, "zoom test should emit at least 21 primitives")
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
	// S121: Each trade now emits bar + price + qty + age = 4 primitives; 2 trades = 8.
	testing.expect(t, out.count >= 6, "trades tape should emit at least 6 primitives for 2 trades")
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
	// S121: Now emits mid-price + spread + divider + per-level (cumulative + bar + price) per side.
	testing.expect(t, out.count >= 10, "orderbook should emit at least 10 primitives with price labels and depth")
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
	// S121: Emits hero mark price + label + funding + liq bar(2) + liq text + window = 7+.
	// Quality flags suppressed when 0. Spread badge when orderbook available.
	testing.expect(t, out.count >= 6, "stats panel should emit at least 6 primitives")
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
	// S121: Trades, Vol, Delta, 2 bars, ratio, rate, rolling, Last price = 9+.
	testing.expect(t, out.count >= 7, "counter should emit at least 7 primitives")
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
	// S148-BUG-6: Now emits placeholder (bg + divider + label) when no CVD data.
	testing.expect(t, out.count > 0, "expected placeholder output for empty CVD subplot")
	// Should have exactly 3 primitives: bg bar, divider line, text label.
	testing.expect_value(t, out.count, 3)
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
	// S148-BUG-6: Single entry = placeholder (bg + divider + label), not line segments.
	testing.expect(t, out.count > 0, "expected placeholder for single CVD entry")
	// Should have exactly 3 primitives: bg bar, divider line, text label.
	testing.expect_value(t, out.count, 3)
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

// ═══════════════════════════════════════════════════════════════
// S140: Time Axis & Timestamp System tests
// ═══════════════════════════════════════════════════════════════

@(test)
test_time_axis_format_time_label_sub_minute :: proc(t: ^testing.T) {
	// 2024-03-10 14:30:45 UTC = 1710081045000 ms
	buf: [24]u8
	label := format_time_label(buf[:], 1710081045000, 1_000) // 1s TF
	testing.expect_value(t, label, "14:30:45")
}

@(test)
test_time_axis_format_time_label_minute :: proc(t: ^testing.T) {
	// 2024-03-10 14:30:00 UTC = 1710081000000 ms
	buf: [24]u8
	label := format_time_label(buf[:], 1710081000000, 60_000) // 1m TF
	testing.expect_value(t, label, "14:30")
}

@(test)
test_time_axis_format_time_label_hourly :: proc(t: ^testing.T) {
	// 2024-03-10 14:00:00 UTC = 1710079200000 ms
	buf: [24]u8
	label := format_time_label(buf[:], 1710079200000, 3_600_000) // 1h TF
	testing.expect_value(t, label, "14:00")
}

@(test)
test_time_axis_format_day_label :: proc(t: ^testing.T) {
	// 2024-03-10 00:00:00 UTC = 1710028800000 ms
	buf: [24]u8
	label := format_day_label(buf[:], 1710028800000)
	testing.expect_value(t, label, "10 Mar")
}

@(test)
test_time_axis_format_time_label_daily :: proc(t: ^testing.T) {
	// 2024-03-10 = daily TF → should use day format
	buf: [24]u8
	label := format_time_label(buf[:], 1710028800000, 86_400_000) // 1d TF
	testing.expect_value(t, label, "10 Mar")
}

@(test)
test_time_axis_unix_ms_to_date :: proc(t: ^testing.T) {
	// 2024-03-10 00:00:00 UTC = 1710028800000 ms
	d, m, y := unix_ms_to_date(1710028800000)
	testing.expect_value(t, d, 10)
	testing.expect_value(t, m, 3)
	testing.expect_value(t, y, 2024)
}

@(test)
test_time_axis_unix_ms_to_date_epoch :: proc(t: ^testing.T) {
	// Unix epoch: 1970-01-01
	d, m, y := unix_ms_to_date(0)
	testing.expect_value(t, d, 1)
	testing.expect_value(t, m, 1)
	testing.expect_value(t, y, 1970)
}

@(test)
test_time_axis_unix_ms_to_date_2026 :: proc(t: ^testing.T) {
	// 2026-03-09 00:00:00 UTC = 1773014400000 ms (20521 days from epoch)
	d, m, y := unix_ms_to_date(1773014400000)
	testing.expect_value(t, d, 9)
	testing.expect_value(t, m, 3)
	testing.expect_value(t, y, 2026)
}

@(test)
test_time_axis_snap_interval_1s :: proc(t: ^testing.T) {
	// For 1s TF, raw=3 → should snap to 5.
	result := time_axis_snap_interval(3, 1_000)
	testing.expect_value(t, result, 5)
}

@(test)
test_time_axis_snap_interval_1m :: proc(t: ^testing.T) {
	// For 1m TF, raw=7 → should snap to 10.
	result := time_axis_snap_interval(7, 60_000)
	testing.expect_value(t, result, 10)
}

@(test)
test_time_axis_snap_interval_1h :: proc(t: ^testing.T) {
	// For 1h TF, raw=3 → should snap to 4.
	result := time_axis_snap_interval(3, 3_600_000)
	testing.expect_value(t, result, 4)
}

@(test)
test_time_axis_snap_interval_raw_1 :: proc(t: ^testing.T) {
	// Raw=1 → always return 1.
	result := time_axis_snap_interval(1, 60_000)
	testing.expect_value(t, result, 1)
}

@(test)
test_time_axis_render_emits_primitives :: proc(t: ^testing.T) {
	store := new(services.Candle_Store)
	defer free(store)

	// Push 20 candles with valid timestamps (1m apart starting 2024-03-10 14:00).
	base_ts := i64(1710079200000) // 2024-03-10 14:00:00 UTC
	for i in 0 ..< 20 {
		services.push_candle(store, services.Candle_Entry{
			open = 100, high = 101, low = 99, close = 100.5,
			volume = 1, buy_vol = 0.5, sell_vol = 0.5,
			trade_count = 1,
			window_start_ts = base_ts + i64(i) * 60_000,
			window_end_ts = base_ts + i64(i + 1) * 60_000,
			is_closed = true,
		})
	}

	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)

	time_axis_render(out, Time_Axis_Params{
		axis_vp        = ui.Rect{pos = {0, 164}, size = {600, 16}},
		store          = store,
		start          = 0,
		actual_visible = 20,
		slot_w         = 30, // 600 / 20
		tf_ms          = 60_000,
		scroll_offset  = 0,
		chart_left     = 0,
	})

	// Should have at least some grid lines + labels + live edge.
	testing.expect(t, out.count >= 3, "time axis should emit at least 3 primitives")

	// Verify live edge line exists (green accent).
	has_live := false
	for i in 0 ..< out.count {
		p := out.items[i]
		if p.kind == .Line && p.z == 19 do has_live = true
	}
	testing.expect(t, has_live, "time axis should emit live edge indicator")
}

@(test)
test_time_axis_render_live_only_badge :: proc(t: ^testing.T) {
	store := new(services.Candle_Store)
	defer free(store)

	// Push only 3 candles (< 10 threshold).
	base_ts := i64(1710079200000)
	for i in 0 ..< 3 {
		services.push_candle(store, services.Candle_Entry{
			open = 100, high = 101, low = 99, close = 100.5,
			volume = 1, buy_vol = 0.5, sell_vol = 0.5,
			trade_count = 1,
			window_start_ts = base_ts + i64(i) * 60_000,
			window_end_ts = base_ts + i64(i + 1) * 60_000,
			is_closed = true,
		})
	}

	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)

	time_axis_render(out, Time_Axis_Params{
		axis_vp        = ui.Rect{pos = {0, 164}, size = {300, 16}},
		store          = store,
		start          = 0,
		actual_visible = 3,
		slot_w         = 100,
		tf_ms          = 60_000,
		scroll_offset  = 0,
		chart_left     = 0,
	})

	// Should include "LIVE ONLY" badge.
	has_live_only := false
	for i in 0 ..< out.count {
		p := out.items[i]
		if p.kind == .Text_Badge && p.z == 19 {
			badge := p.data.text
			s := text_badge_string(&badge)
			if s == "LIVE ONLY" do has_live_only = true
		}
	}
	testing.expect(t, has_live_only, "time axis should show LIVE ONLY badge for < 10 candles")
}

@(test)
test_time_axis_no_live_edge_when_scrolled :: proc(t: ^testing.T) {
	store := new(services.Candle_Store)
	defer free(store)

	base_ts := i64(1710079200000)
	for i in 0 ..< 20 {
		services.push_candle(store, services.Candle_Entry{
			open = 100, high = 101, low = 99, close = 100.5,
			volume = 1, buy_vol = 0.5, sell_vol = 0.5,
			trade_count = 1,
			window_start_ts = base_ts + i64(i) * 60_000,
			window_end_ts = base_ts + i64(i + 1) * 60_000,
			is_closed = true,
		})
	}

	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)

	time_axis_render(out, Time_Axis_Params{
		axis_vp        = ui.Rect{pos = {0, 164}, size = {600, 16}},
		store          = store,
		start          = 0,
		actual_visible = 15,
		slot_w         = 40,
		tf_ms          = 60_000,
		scroll_offset  = 5, // scrolled away from live edge
		chart_left     = 0,
	})

	// Should NOT have live edge (green) line at z=19 — green has alpha 0.20.
	// We check that there's no line at z=19 with green-ish color.
	has_live_edge := false
	for i in 0 ..< out.count {
		p := out.items[i]
		if p.kind == .Line && p.z == 19 {
			line := p.data.line
			if line.color.g > 0.5 && line.color.r < 0.3 do has_live_edge = true
		}
	}
	testing.expect(t, !has_live_edge, "time axis should NOT show live edge when scrolled")
}

@(test)
test_time_axis_month_abbrev :: proc(t: ^testing.T) {
	testing.expect_value(t, month_abbrev(1), "Jan")
	testing.expect_value(t, month_abbrev(6), "Jun")
	testing.expect_value(t, month_abbrev(12), "Dec")
	testing.expect_value(t, month_abbrev(0), "???")
	testing.expect_value(t, month_abbrev(13), "???")
}

// ═══════════════════════════════════════════════════════════════
// S141: Subplot Viewport Windowing tests
// ═══════════════════════════════════════════════════════════════

@(test)
test_subplot_viewport_window_auto :: proc(t: ^testing.T) {
	// Auto mode: zero chart_viewport returns full range.
	cv := Chart_Viewport{}
	s, c := subplot_viewport_window(20, cv)
	testing.expect(t, s == 0, "start should be 0")
	testing.expect(t, c == 20, "count should be 20")
}

@(test)
test_subplot_viewport_window_scrolled :: proc(t: ^testing.T) {
	cv := Chart_Viewport{visible_count = 10, scroll_offset = 5}
	s, c := subplot_viewport_window(30, cv)
	// end = 30 - 5 = 25, start = 25 - 10 = 15
	testing.expect(t, s == 15, "start should be 15")
	testing.expect(t, c == 10, "count should be 10")
}

@(test)
test_subplot_viewport_window_at_live_edge :: proc(t: ^testing.T) {
	cv := Chart_Viewport{visible_count = 10, scroll_offset = 0}
	s, c := subplot_viewport_window(30, cv)
	// end = 30 - 0 = 30, start = 30 - 10 = 20
	testing.expect(t, s == 20, "start should be 20")
	testing.expect(t, c == 10, "count should be 10")
}

@(test)
test_subplot_viewport_window_clamped :: proc(t: ^testing.T) {
	// Visible count exceeds total entries.
	cv := Chart_Viewport{visible_count = 50, scroll_offset = 0}
	s, c := subplot_viewport_window(10, cv)
	testing.expect(t, s == 0, "start should be 0")
	testing.expect(t, c == 10, "count should be 10")
}

@(test)
test_subplot_viewport_window_empty :: proc(t: ^testing.T) {
	cv := Chart_Viewport{visible_count = 10, scroll_offset = 0}
	s, c := subplot_viewport_window(0, cv)
	testing.expect(t, s == 0, "start should be 0")
	testing.expect(t, c == 0, "count should be 0")
}

@(test)
test_subplot_cvd_viewport_windowed_render :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(3001)

	// Push 10 CVD entries.
	for i in 0 ..< 10 {
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
	// S141: Apply viewport window — show 5 entries, scrolled 3 from live.
	ctx.chart_viewport = Chart_Viewport{visible_count = 5, scroll_offset = 3}
	subplot_vp := ui.Rect{pos = {0, 150}, size = {300, 50}}
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	subplot_cvd_render(&ctx, out, subplot_vp)

	// Should emit fewer primitives than full render (windowed to 5 entries).
	// bg + divider + zero_line + 4 line segments + label = 8.
	testing.expect(t, out.count >= 6, "windowed CVD should emit at least 6 primitives")
	testing.expect(t, out.count <= 15, "windowed CVD should not emit more than full range")
}

@(test)
test_subplot_delta_vol_viewport_windowed :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(3002)

	for i in 0 ..< 10 {
		apply_event(store, ports.MD_Event{
			source = {subject_id = sid, channel = .Candles, seq = i64(i + 1)},
			kind = .Delta_Volume,
			unix = i64(100 + i),
			data = {delta_volume = ports.MD_Delta_Volume_Event{
				buy_volume = 10 + f64(i), sell_volume = 8 + f64(i),
				delta_volume = f64(i) - 5,
				window_start_ts = i64(i) * 60_000,
				window_end_ts = i64(i + 1) * 60_000,
				unix = i64(100 + i),
			}},
		})
	}

	ctx := make_ctx(store, sid, ui.Rect{pos = {0, 0}, size = {300, 200}})
	ctx.chart_viewport = Chart_Viewport{visible_count = 4, scroll_offset = 2}
	subplot_vp := ui.Rect{pos = {0, 150}, size = {300, 50}}
	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)
	subplot_delta_vol_render(&ctx, out, subplot_vp)

	// 4 bars from windowed range.
	bar_count := 0
	for i in 0 ..< out.count {
		if out.items[i].kind == .Bar && out.items[i].z == 25 do bar_count += 1
	}
	testing.expect_value(t, bar_count, 4)
}

// ═══════════════════════════════════════════════════════════════
// S147: DataSource range_complete detection
// ═══════════════════════════════════════════════════════════════

@(test)
test_s147_data_source_range_complete_on_is_last :: proc(t: ^testing.T) {
	// When a Range_Candle_Batch with is_last=true is polled, the result must
	// have range_complete=true and range_oldest_ts set from the batch.
	ds := new(Data_Source)
	defer free(ds)
	store := new(Market_Store)
	defer free(store)

	polled := false
	poll_fn := proc(events_buf: []ports.MD_Event) -> int {
		events_buf[0] = ports.MD_Event{
			source = {subject_id = 42, channel = .Candles, seq = 1},
			kind = .Range_Candle_Batch,
			unix = 1000,
			data = {range_candles = ports.MD_Range_Candle_Batch{
				count = 2,
				is_last = true,
				candles = {},
			}},
		}
		events_buf[0].data.range_candles.candles[0] = {window_start_ts = 5000, window_end_ts = 6000, open = 1, high = 2, low = 0.5, close = 1.5, is_closed = true}
		events_buf[0].data.range_candles.candles[1] = {window_start_ts = 3000, window_end_ts = 4000, open = 1, high = 2, low = 0.5, close = 1.5, is_closed = true}
		return 1
	}
	md := ports.Marketdata_Port{poll = poll_fn}
	result := data_source_poll_and_apply(ds, md, store)
	testing.expect(t, result.range_complete, "range_complete should be true when is_last batch arrives")
	testing.expect_value(t, result.range_oldest_ts, i64(3000))
	testing.expect_value(t, result.processed, 1)
}

@(test)
test_s147_data_source_no_range_complete_without_is_last :: proc(t: ^testing.T) {
	// A Range_Candle_Batch without is_last should NOT set range_complete.
	ds := new(Data_Source)
	defer free(ds)
	store := new(Market_Store)
	defer free(store)

	poll_fn := proc(events_buf: []ports.MD_Event) -> int {
		events_buf[0] = ports.MD_Event{
			source = {subject_id = 42, channel = .Candles, seq = 1},
			kind = .Range_Candle_Batch,
			unix = 1000,
			data = {range_candles = ports.MD_Range_Candle_Batch{count = 1, is_last = false}},
		}
		events_buf[0].data.range_candles.candles[0] = {window_start_ts = 5000, open = 1, high = 2, low = 0.5, close = 1.5}
		return 1
	}
	md := ports.Marketdata_Port{poll = poll_fn}
	result := data_source_poll_and_apply(ds, md, store)
	testing.expect(t, !result.range_complete, "range_complete should be false without is_last")
}

@(test)
test_s147_data_source_range_complete_not_set_for_live_candle :: proc(t: ^testing.T) {
	// A regular Candle event must NOT set range_complete.
	ds := new(Data_Source)
	defer free(ds)
	store := new(Market_Store)
	defer free(store)

	poll_fn := proc(events_buf: []ports.MD_Event) -> int {
		events_buf[0] = ports.MD_Event{
			source = {subject_id = 42, channel = .Candles, seq = 1},
			kind = .Candle,
			unix = 1000,
			data = {candle = ports.MD_Candle_Event{window_start_ts = 5000, open = 1, high = 2, low = 0.5, close = 1.5}},
		}
		return 1
	}
	md := ports.Marketdata_Port{poll = poll_fn}
	result := data_source_poll_and_apply(ds, md, store)
	testing.expect(t, !result.range_complete, "range_complete should be false for regular Candle events")
}

// ═══════════════════════════════════════════════════════════════
// S147-BUG-06: Time axis spacing must be wider for sub-minute TFs.
// ═══════════════════════════════════════════════════════════════

@(test)
test_time_axis_5s_labels_no_overlap :: proc(t: ^testing.T) {
	// 5s TF, slot_w=4px (typical for ~500 candles on 1920px).
	// Old min_spacing=64: raw_interval=64/4=16 → snap to 30 → 120px gap (OK).
	// With narrow slot_w=7px (default 140 candles): raw_interval=96/7≈14 → snap to 30 → 210px gap.
	// This ensures "HH:MM:SS" labels (8 chars ≈ 64px) don't overlap.
	store := new(services.Candle_Store)
	defer free(store)
	base_ts := i64(1710079200000)
	for i in 0 ..< 140 {
		services.push_candle(store, services.Candle_Entry{
			open = 100, high = 101, low = 99, close = 100.5,
			volume = 1, buy_vol = 0.5, sell_vol = 0.5, trade_count = 1,
			window_start_ts = base_ts + i64(i) * 5_000,
			window_end_ts = base_ts + i64(i + 1) * 5_000,
			is_closed = true,
		})
	}

	out := new(Layer_Outputs)
	defer free(out)
	layer_outputs_reset(out)

	slot_w := f32(7) // 980px / 140 candles
	time_axis_render(out, Time_Axis_Params{
		axis_vp        = ui.Rect{pos = {0, 164}, size = {980, 16}},
		store          = store,
		start          = 0,
		actual_visible = 140,
		slot_w         = slot_w,
		tf_ms          = 5_000,
		scroll_offset  = 0,
		chart_left     = 0,
	})

	// Count text badge positions and verify they're at least 84px apart (96-margin).
	// 8-char labels at ~8px/char = 64px, so centers need ≥ 64px gap.
	prev_x := f32(-999)
	for i in 0 ..< out.count {
		p := out.items[i]
		if p.kind == .Text_Badge && p.z == 19 {
			badge := p.data.text
			x := badge.pos.x
			if prev_x > -900 {
				gap := x - prev_x
				testing.expect(t, gap >= 60,
					"time axis labels on 5s TF must be spaced >= 60px apart to prevent overlap")
			}
			prev_x = x
		}
	}
}

// ---------------------------------------------------------------------------
// S148: Trade reducer populates per-stream DOM store
// ---------------------------------------------------------------------------

@(test)
test_trade_reducer_populates_dom_store :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(148_001)

	// Push 3 trades at different prices.
	for i in 0 ..< 3 {
		apply_event(store, ports.MD_Event{
			source = {subject_id = sid, channel = .Trades, seq = i64(i + 1)},
			kind = .Trade,
			unix = i64(1000 + i),
			data = {trade = ports.MD_Trade_Event{
				price  = 100.0 + f64(i),
				qty    = 1.5,
				is_buy = i % 2 == 0,
				unix   = i64(1000 + i),
			}},
		})
	}

	stream := market_store_stream_for_subject(store, sid)
	testing.expect(t, stream != nil, "stream should exist after trades")
	testing.expect_value(t, stream.trades.count, 3)

	// DOM store should have accumulated the trades.
	testing.expect(t, stream.dom.trade_count == 3, "DOM store should have 3 trades")
	testing.expect(t, stream.dom.total_buy_vol > 0, "DOM store should have buy volume")
	testing.expect(t, stream.dom.total_sell_vol > 0, "DOM store should have sell volume")
	testing.expect(t, stream.dom.level_count > 0, "DOM store should have price levels")
	testing.expect(t, stream.dom.vwap_sum_v > 0, "DOM VWAP accumulator should be non-zero")
	testing.expect(t, stream.dom.recent_count == 3, "DOM recent fills should have 3 entries")
}

@(test)
test_dom_store_per_stream_isolation :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid_a := u64(148_002)
	sid_b := u64(148_003)

	// Push trade to stream A.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid_a, channel = .Trades, seq = 1},
		kind = .Trade,
		unix = 1000,
		data = {trade = ports.MD_Trade_Event{price = 50.0, qty = 2.0, is_buy = true, unix = 1000}},
	})

	// Push trade to stream B.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid_b, channel = .Trades, seq = 1},
		kind = .Trade,
		unix = 1001,
		data = {trade = ports.MD_Trade_Event{price = 200.0, qty = 3.0, is_buy = false, unix = 1001}},
	})

	sa := market_store_stream_for_subject(store, sid_a)
	sb := market_store_stream_for_subject(store, sid_b)
	testing.expect(t, sa != nil && sb != nil, "both streams should exist")

	// Verify isolation: stream A has only its trade.
	testing.expect(t, sa.dom.trade_count == 1, "stream A DOM should have 1 trade")
	testing.expect(t, sa.dom.total_buy_vol == 2.0, "stream A DOM buy vol should be 2.0")
	testing.expect(t, sa.dom.total_sell_vol == 0, "stream A DOM sell vol should be 0")

	// Stream B has only its trade.
	testing.expect(t, sb.dom.trade_count == 1, "stream B DOM should have 1 trade")
	testing.expect(t, sb.dom.total_buy_vol == 0, "stream B DOM buy vol should be 0")
	testing.expect(t, sb.dom.total_sell_vol == 3.0, "stream B DOM sell vol should be 3.0")
}

@(test)
test_footprint_store_in_market_stream :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(148_004)

	// S155: Without active_tf_ms, footprint should NOT accumulate.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 1},
		kind = .Trade,
		unix = 1000,
		data = {trade = ports.MD_Trade_Event{price = 100.0, qty = 1.0, is_buy = true, unix = 1000}},
	})

	stream := market_store_stream_for_subject(store, sid)
	testing.expect(t, stream != nil, "stream should exist")
	// No active_tf_ms → footprint not populated.
	testing.expect_value(t, stream.footprint.count, 0)

	// Manually push to verify the store works per-stream.
	services.footprint_store_push_trade(&stream.footprint, 100.0, 1.0, true, 60_000, 60_000, 1.0)
	testing.expect_value(t, stream.footprint.count, 1)
}

// S155: With active_tf_ms set, trade reducer populates footprint store.
@(test)
test_s155_footprint_accumulates_with_tf :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(155_001)
	store.active_tf_ms = 60_000 // 1m

	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 1},
		kind = .Trade,
		unix = 120_000, // 2 minutes in ms
		data = {trade = ports.MD_Trade_Event{price = 100.0, qty = 2.5, is_buy = true, unix = 120_000}},
	})

	stream := market_store_stream_for_subject(store, sid)
	testing.expect(t, stream != nil, "stream exists")
	testing.expect_value(t, stream.footprint.count, 1)

	// Second trade in same candle window.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 2},
		kind = .Trade,
		unix = 125_000,
		data = {trade = ports.MD_Trade_Event{price = 101.0, qty = 1.0, is_buy = false, unix = 125_000}},
	})
	testing.expect_value(t, stream.footprint.count, 1) // same window → same entry

	// Third trade in a new candle window.
	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 3},
		kind = .Trade,
		unix = 200_000,
		data = {trade = ports.MD_Trade_Event{price = 102.0, qty = 0.5, is_buy = true, unix = 200_000}},
	})
	testing.expect_value(t, stream.footprint.count, 2) // new window → new entry
}

// S155: Footprint accumulation skipped when active_tf_ms is 0.
@(test)
test_s155_footprint_skipped_without_tf :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(155_002)
	store.active_tf_ms = 0 // no TF

	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 1},
		kind = .Trade,
		unix = 60_000,
		data = {trade = ports.MD_Trade_Event{price = 100.0, qty = 1.0, is_buy = true, unix = 60_000}},
	})

	stream := market_store_stream_for_subject(store, sid)
	testing.expect(t, stream != nil, "stream exists")
	testing.expect_value(t, stream.footprint.count, 0) // no TF → no footprint
	// DOM should still accumulate (TF-independent).
	testing.expect(t, stream.dom.trade_count == 1, "DOM still accumulates without TF")
}

// S155: Footprint + DOM both accumulate from same trade.
@(test)
test_s155_trade_feeds_dom_and_footprint :: proc(t: ^testing.T) {
	store := new(Market_Store)
	defer free(store)
	sid := u64(155_003)
	store.active_tf_ms = 5_000 // 5s

	apply_event(store, ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 1},
		kind = .Trade,
		unix = 10_000,
		data = {trade = ports.MD_Trade_Event{price = 50.0, qty = 3.0, is_buy = true, unix = 10_000}},
	})

	stream := market_store_stream_for_subject(store, sid)
	testing.expect(t, stream != nil, "stream exists")
	// Both stores fed from same trade.
	testing.expect(t, stream.dom.trade_count == 1, "DOM has trade")
	testing.expect_value(t, stream.footprint.count, 1)
	// Trades store also has it.
	testing.expect_value(t, stream.trades.count, 1)
}
