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
		if diags[i].id == .Price_Candles {
			found_price = true
			testing.expect_value(t, diags[i].enabled, true)
			testing.expect_value(t, diags[i].has_data, true)
			testing.expect(t, diags[i].render_invocations > 0)
		}
	}
	testing.expect_value(t, found_price, true)
}
