package main

// WASM entry point — init in main(), per-frame in step() export.
// The JS runtime (runtime.js) animation loop calls step(dt, odin_ctx) on every frame.

import "base:runtime"
import "mr:app"
import "mr:ports"
import "mr:services"

// Default connection config.
WS_URL  :: "ws://127.0.0.1:8080/ws"
API_KEY :: ""

g_state: app.App_State
g_prev_mouse_pos: [2]f32
g_has_prev_mouse_pos: bool

main :: proc() {
	text_port := make_text_port()
	font_port := stub_font_port()
	settings_port := stub_settings_port()
	md_port := make_marketdata_web(WS_URL, API_KEY, false) // deferred: OFFLINE until user/auto_connect

	app.init(&g_state, text_port, md_port, font_port, settings_port, false)
	app.set_runtime_connection_defaults(&g_state, WS_URL, API_KEY)
}

@(private = "file")
mark_key_state :: proc(input: ^ports.Input_State, bits, pressed_bits, released_bits: u32, bit: u32, key: ports.Key) {
	mask := u32(1) << bit
	down := bits & mask != 0
	if down do input.keys.pressed += {key}
	if pressed_bits & mask != 0 do input.keys.just_pressed += {key}
	if released_bits & mask != 0 do input.keys.just_released += {key}
}

@(private = "file")
decode_input_state :: proc(input: ^ports.Input_State, dt: f32) {
	input.delta_time = dt

	input.mouse.pos = {mouse_x(), mouse_y()}
	if g_has_prev_mouse_pos {
		input.mouse.delta = {
			input.mouse.pos.x - g_prev_mouse_pos.x,
			input.mouse.pos.y - g_prev_mouse_pos.y,
		}
	}
	g_prev_mouse_pos = input.mouse.pos
	g_has_prev_mouse_pos = true

	mouse_bits := mouse_buttons()
	mouse_pressed_bits := mouse_pressed_buttons()
	mouse_released_bits := mouse_released_buttons()
	left_down := mouse_bits & (1 << 0) != 0
	right_down := mouse_bits & (1 << 1) != 0
	middle_down := mouse_bits & (1 << 2) != 0
	input.mouse.buttons[.Left] = left_down
	input.mouse.buttons[.Right] = right_down
	input.mouse.buttons[.Middle] = middle_down
	input.mouse.pressed[.Left] = mouse_pressed_bits & (1 << 0) != 0
	input.mouse.pressed[.Right] = mouse_pressed_bits & (1 << 1) != 0
	input.mouse.pressed[.Middle] = mouse_pressed_bits & (1 << 2) != 0
	input.mouse.released[.Left] = mouse_released_bits & (1 << 0) != 0
	input.mouse.released[.Right] = mouse_released_bits & (1 << 1) != 0
	input.mouse.released[.Middle] = mouse_released_bits & (1 << 2) != 0

	input.mouse.scroll = {mouse_scroll_x(), mouse_scroll_y()}

	mod_bits := modifier_state()
	input.modifiers.shift = mod_bits & (1 << 0) != 0
	input.modifiers.ctrl = mod_bits & (1 << 1) != 0
	input.modifiers.alt = mod_bits & (1 << 2) != 0
	input.modifiers.super = mod_bits & (1 << 3) != 0

	bits := key_state()
	key_pressed_bits := key_pressed_state()
	key_released_bits := key_released_state()
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 0, .Up)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 1, .Down)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 2, .Left)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 3, .Right)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 4, .Enter)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 5, .Escape)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 6, .Tab)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 7, .Space)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 8, .Num_1)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 9, .Num_2)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 10, .Num_3)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 11, .Num_4)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 12, .Num_5)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 13, .Num_6)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 14, .Num_7)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 15, .Num_8)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 16, .Num_9)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 17, .S)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 18, .Slash)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 19, .C)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 20, .G)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 21, .F)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 22, .M)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 23, .B)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 24, .V)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 25, .R)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 26, .I)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 27, .H)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 28, .J)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 29, .K)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 30, .Z)
	mark_key_state(input, bits, key_pressed_bits, key_released_bits, 31, .Delete)
	// S46: D key requires >32 bits; web snapshot via health panel button instead.
}

@(export)
step :: proc "c" (dt: f32, odin_ctx: rawptr) -> bool {
	context = (^runtime.Context)(odin_ctx)^

	input: ports.Input_State
	input.delta_time = dt
	input.viewport_size = canvas_viewport_size()
	decode_input_state(&input, dt)
	buf, should_render := app.update_web(&g_state, input)
	if should_render {
		render_commands(buf)
	}

	free_all(context.temp_allocator)
	return true // continue animation loop
}

@(export)
probe_widget_trades_count :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_trades_count)
}

@(export)
probe_widget_orderbook_asks :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_orderbook_asks)
}

@(export)
probe_widget_orderbook_bids :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_orderbook_bids)
}

@(export)
probe_widget_stats_count :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_count)
}

@(export)
probe_widget_stats_parse_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_parse_total)
}

@(export)
probe_widget_stats_fallback_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_fallback_total)
}

@(export)
probe_widget_stats_drop_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_drop_total)
}

@(export)
probe_widget_stats_render_p95_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_render_p95_us)
}

@(export)
probe_widget_stats_render_p99_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_render_p99_us)
}

@(export)
probe_widget_stats_render_budget_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_render_budget_us)
}

@(export)
probe_widget_stats_render_over_budget :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_render_over_budget)
}

@(export)
probe_widget_stats_drop_capacity_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_drop_capacity_total)
}

@(export)
probe_widget_stats_drop_render_overflow_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_drop_render_overflow_total)
}

@(export)
probe_widget_stats_entries :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_entries)
}

@(export)
probe_widget_stats_max_entries :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_max_entries)
}

@(export)
probe_widget_stats_evicted_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_evicted_total)
}

@(export)
probe_widget_stats_state :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_stats_state)
}

@(export)
probe_widget_heatmap_snaps :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_heatmap_snaps)
}

@(export)
probe_widget_vpvr_levels :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_vpvr_levels)
}

@(export)
probe_widget_candle_count :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_candle_count)
}

@(export)
probe_widget_candle_latest_close :: proc "c" () -> f64 {
	context = runtime.default_context()
	cs := app.active_candle_store(&g_state)
	if cs == nil || cs.count <= 0 do return 0
	c := services.get_candle_newest(cs, 0)
	return c.close
}

@(export)
probe_widget_candle_latest_end_ts :: proc "c" () -> f64 {
	context = runtime.default_context()
	cs := app.active_candle_store(&g_state)
	if cs == nil || cs.count <= 0 do return 0
	c := services.get_candle_newest(cs, 0)
	return f64(c.window_end_ts)
}

@(export)
probe_active_tf_index :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.active_tf_idx)
}

@(export)
probe_ui_actions_enqueued_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.ui_actions_enqueued_total)
}

@(export)
probe_stream_count :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.stream_count)
}

@(export)
probe_stream_evictions :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.stream_evictions)
}

@(export)
probe_layer_stream_entries :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.layer_stream_entries)
}

@(export)
probe_layer_stream_evictions :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.layer_stream_evictions)
}

@(export)
probe_has_active_stream :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.has_active_stream do return 1
	return 0
}

@(export)
probe_active_subject_lo32 :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.active_subject_id & u64(0xffff_ffff))
}

@(export)
probe_stream_switches_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.stream_switches_total)
}

@(export)
probe_timeframe_switches_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.timeframe_switches_total)
}

@(export)
probe_active_live_stats :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.active_live_stats do return 1
	return 0
}

@(export)
probe_active_live_heatmap :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.active_live_heatmap do return 1
	return 0
}

@(export)
probe_active_live_vpvr :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.active_live_vpvr do return 1
	return 0
}

@(export)
probe_active_synth_heatmap :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.active_synth_heatmap do return 1
	return 0
}

@(export)
probe_active_synth_vpvr :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.active_synth_vpvr do return 1
	return 0
}

@(export)
probe_active_live_candle :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.active_live_candle do return 1
	return 0
}

@(export)
probe_compare_mode :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.compare_mode do return 1
	return 0
}

@(export)
probe_compare_widget_idx :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.compare_widget_idx)
}

@(export)
probe_compare_count :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.compare_count)
}

@(export)
probe_indicator_rsi_enabled :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.ind_rsi_enabled do return 1
	return 0
}

@(export)
probe_indicator_macd_enabled :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.ind_macd_enabled do return 1
	return 0
}

@(export)
probe_indicator_funding_enabled :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.ind_funding_enabled do return 1
	return 0
}

@(export)
probe_indicator_liq_enabled :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.ind_liq_enabled do return 1
	return 0
}

@(export)
probe_indicator_trade_counter_enabled :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.ind_trade_counter_enabled do return 1
	return 0
}

@(export)
probe_indicator_rsi_rendered :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.ind_rsi_rendered do return 1
	return 0
}

@(export)
probe_indicator_macd_rendered :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.ind_macd_rendered do return 1
	return 0
}

@(export)
probe_indicator_funding_rendered :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.ind_funding_rendered do return 1
	return 0
}

@(export)
probe_indicator_liq_rendered :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.ind_liq_rendered do return 1
	return 0
}

@(export)
probe_indicator_trade_counter_rendered :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.ind_trade_counter_rendered do return 1
	return 0
}

@(export)
probe_md_hello_received :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.md_metrics.hello_received do return 1
	return 0
}

@(export)
probe_md_subscribe_ack_count :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.subscribe_ack_count)
}

@(export)
probe_md_seq_gap_count :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.seq_gap_count)
}

@(export)
probe_md_prev_seq_violations :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.prev_seq_violations)
}

@(export)
probe_md_backend_gap_missing_ts_server :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.backend_gap_missing_ts_server)
}

@(export)
probe_md_backend_gap_no_metrics :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.backend_gap_no_metrics)
}

@(export)
probe_md_backend_gap_seq_gap_recurring :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.backend_gap_seq_gap_recurring)
}

@(export)
probe_md_server_metrics_cadence_ms :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.server_metrics_cadence_ms)
}

@(export)
probe_md_resync_count :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.resync_count)
}

@(export)
probe_md_transport_mode :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.transport_mode)
}

@(export)
probe_md_alloc_estimate_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_alloc_estimate_total)
}

@(export)
probe_md_alloc_estimate_frame :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_alloc_estimate_frame)
}

@(export)
probe_md_trade_backlog :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.trade_backlog)
}

@(export)
probe_md_trade_backlog_cap :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.trade_backlog_cap)
}

@(export)
probe_md_candle_backlog :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.candle_backlog)
}

@(export)
probe_md_candle_backlog_cap :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.candle_backlog_cap)
}

@(export)
probe_md_signal_backlog :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.signal_backlog)
}

@(export)
probe_md_signal_backlog_cap :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.signal_backlog_cap)
}

@(export)
probe_md_parse_time_p95_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.parse_time_p95_us)
}

@(export)
probe_md_parse_time_p99_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.parse_time_p99_us)
}

@(export)
probe_md_apply_time_p95_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.apply_time_p95_us)
}

@(export)
probe_md_apply_time_p99_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.apply_time_p99_us)
}

@(export)
probe_md_batched_decode_time_p95_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.batched_decode_time_p95_us)
}

@(export)
probe_md_batched_decode_time_p99_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_metrics.batched_decode_time_p99_us)
}

@(export)
probe_md_canonical_stats_frames :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_canonical_stats_frames)
}

@(export)
probe_md_stats_fallback_frames :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_stats_fallback_frames)
}

@(export)
probe_md_canonical_evidence_frames :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_canonical_evidence_frames)
}

@(export)
probe_md_evidence_fallback_frames :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_evidence_fallback_frames)
}

@(export)
probe_md_canonical_signal_frames :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_canonical_signal_frames)
}

@(export)
probe_md_signal_fallback_frames :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.md_signal_fallback_frames)
}

@(export)
probe_widget_evidence_count :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_count)
}

@(export)
probe_widget_signal_count :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_count)
}

@(export)
probe_widget_signal_link_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_link_total)
}

@(export)
probe_widget_signal_link_evidence_seq :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_link_evidence_seq)
}

@(export)
probe_widget_dom_parse_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_parse_total)
}

@(export)
probe_widget_dom_fallback_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_fallback_total)
}

@(export)
probe_widget_dom_drop_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_drop_total)
}

@(export)
probe_widget_dom_render_p95_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_render_p95_us)
}

@(export)
probe_widget_dom_render_p99_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_render_p99_us)
}

@(export)
probe_widget_dom_render_budget_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_render_budget_us)
}

@(export)
probe_widget_dom_render_over_budget :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_render_over_budget)
}

@(export)
probe_widget_dom_drop_capacity_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_drop_capacity_total)
}

@(export)
probe_widget_dom_drop_render_overflow_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_drop_render_overflow_total)
}

@(export)
probe_widget_dom_entries :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_entries)
}

@(export)
probe_widget_dom_max_entries :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_max_entries)
}

@(export)
probe_widget_dom_evicted_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_dom_evicted_total)
}

@(export)
probe_widget_tape_parse_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_parse_total)
}

@(export)
probe_widget_tape_fallback_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_fallback_total)
}

@(export)
probe_widget_tape_drop_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_drop_total)
}

@(export)
probe_widget_tape_render_p95_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_render_p95_us)
}

@(export)
probe_widget_tape_render_p99_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_render_p99_us)
}

@(export)
probe_widget_tape_render_budget_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_render_budget_us)
}

@(export)
probe_widget_tape_render_over_budget :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_render_over_budget)
}

@(export)
probe_widget_tape_drop_capacity_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_drop_capacity_total)
}

@(export)
probe_widget_tape_drop_render_overflow_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_drop_render_overflow_total)
}

@(export)
probe_widget_tape_entries :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_entries)
}

@(export)
probe_widget_tape_max_entries :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_max_entries)
}

@(export)
probe_widget_tape_evicted_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_tape_evicted_total)
}

@(export)
probe_widget_evidence_parse_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_parse_total)
}

@(export)
probe_widget_evidence_fallback_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_fallback_total)
}

@(export)
probe_widget_evidence_drop_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_drop_total)
}

@(export)
probe_widget_evidence_render_p95_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_render_p95_us)
}

@(export)
probe_widget_evidence_render_p99_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_render_p99_us)
}

@(export)
probe_widget_evidence_render_budget_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_render_budget_us)
}

@(export)
probe_widget_evidence_render_over_budget :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_render_over_budget)
}

@(export)
probe_widget_evidence_drop_capacity_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_drop_capacity_total)
}

@(export)
probe_widget_evidence_drop_render_overflow_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_drop_render_overflow_total)
}

@(export)
probe_widget_evidence_entries :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_entries)
}

@(export)
probe_widget_evidence_max_entries :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_max_entries)
}

@(export)
probe_widget_evidence_evicted_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_evicted_total)
}

@(export)
probe_widget_signal_parse_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_parse_total)
}

@(export)
probe_widget_signal_fallback_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_fallback_total)
}

@(export)
probe_widget_signal_drop_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_drop_total)
}

@(export)
probe_widget_signal_render_p95_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_render_p95_us)
}

@(export)
probe_widget_signal_render_p99_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_render_p99_us)
}

@(export)
probe_widget_signal_render_budget_us :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_render_budget_us)
}

@(export)
probe_widget_signal_render_over_budget :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_render_over_budget)
}

@(export)
probe_widget_signal_drop_capacity_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_drop_capacity_total)
}

@(export)
probe_widget_signal_drop_render_overflow_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_drop_render_overflow_total)
}

@(export)
probe_widget_signal_entries :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_entries)
}

@(export)
probe_widget_signal_max_entries :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_max_entries)
}

@(export)
probe_widget_signal_evicted_total :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_evicted_total)
}

@(export)
probe_widget_evidence_state :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_evidence_state)
}

@(export)
probe_widget_signal_state :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.w_signal_state)
}

@(export)
probe_layout_version :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	return i32(p.layout_version)
}

@(export)
probe_layout_migrated :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.layout_migrated do return 1
	return 0
}

@(export)
probe_layout_link_enabled :: proc "c" () -> i32 {
	context = runtime.default_context()
	p := app.runtime_probe(&g_state)
	if p.layout_link_enabled do return 1
	return 0
}
