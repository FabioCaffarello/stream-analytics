package main

// Native entry point — backend-agnostic.
//
// Imports: backend (windowing/GL), mr:app (core logic).
// Does NOT import vendor:glfw, vendor:OpenGL, or deps:imgui.
// To switch backends: change make_glfw_backend() to make_sdl2_backend().

import "core:os"
import "core:fmt"
import "core:strconv"
import "core:strings"
import "core:time"
import "backend"
import "mr:app"
import "mr:ports"

Venue_Symbol :: struct {
	venue, symbol: string,
}

// Current UI has a single set of stores/widgets for one active stream.
// Subscribe to a single instrument to avoid mixing markets in the same store and to keep
// delivery load below slow-client thresholds until multi-symbol partitioning is implemented.
DEFAULT_SUBS :: []Venue_Symbol{
	{"binance", "BTCUSDT:SPOT"},
}

SOAK_MULTI_SUBS :: []Venue_Symbol{
	{"binance", "BTCUSDT:SPOT"},
	{"binance", "ETHUSDT:SPOT"},
	{"binance", "SOLUSDT:SPOT"},
}

@(private = "file")
parse_positive_int_flag :: proc(arg: string, prefix: string, fallback: int) -> int {
	if !strings.has_prefix(arg, prefix) do return fallback
	v, ok := strconv.parse_int(arg[len(prefix):])
	if !ok do return fallback
	if v <= 0 do return fallback
	return int(v)
}

@(private = "file")
parse_string_flag :: proc(arg: string, prefix: string, fallback: string) -> string {
	if !strings.has_prefix(arg, prefix) do return fallback
	v := arg[len(prefix):]
	if len(v) == 0 do return fallback
	return v
}

@(private = "file")
subscribe_default_channels :: proc(md_port: ports.Marketdata_Port, subs: []Venue_Symbol) {
	for vs in subs {
		md_port.subscribe(vs.venue, vs.symbol, .Trades)
		md_port.subscribe(vs.venue, vs.symbol, .Tape)
		md_port.subscribe(vs.venue, vs.symbol, .Orderbook)
		md_port.subscribe(vs.venue, vs.symbol, .Stats)
		md_port.subscribe(vs.venue, vs.symbol, .Heatmaps)
		md_port.subscribe(vs.venue, vs.symbol, .VPVR)
		md_port.subscribe(vs.venue, vs.symbol, .Candles)
	}
}

main :: proc() {
	// 1. Parse flags.
	use_sdl2 := false
	offline := false
	soak_multi := false
	ws_url := "ws://127.0.0.1:8080/ws"
	api_key := "prod_key_1"
	soak_seconds := 0
	soak_log_ms := 0
	for i in 0 ..< len(os.args) {
		arg := os.args[i]
		if arg == "--sdl2"    do use_sdl2 = true
		if arg == "--offline" do offline = true
		if arg == "--soak-multi" do soak_multi = true
		ws_url = parse_string_flag(arg, "--ws-url=", ws_url)
		api_key = parse_string_flag(arg, "--api-key=", api_key)
		soak_seconds = parse_positive_int_flag(arg, "--soak-seconds=", soak_seconds)
		soak_log_ms = parse_positive_int_flag(arg, "--soak-log-ms=", soak_log_ms)
	}

	// 2. Backend init.
	be := use_sdl2 ? backend.make_sdl2_backend() : backend.make_glfw_backend()
	if !be.init("Stream Analytics", 800, 600) do return
	defer be.shutdown()

	// 3. Ports.
	font_port := make_font_port()
	text_port := make_text_port()
	md_port := offline ? stub_marketdata_port() : make_marketdata_native(ws_url, api_key)
	settings_port := make_settings_port()

	// 4. PRD-0009: Normal subscriptions are now driven by cell bindings (reconcile_subscriptions).
	// Only soak mode still uses explicit subscribe_default_channels for testing.
	if !offline && (soak_multi || soak_seconds > 0) {
		subs := soak_multi ? SOAK_MULTI_SUBS : DEFAULT_SUBS
		subscribe_default_channels(md_port, subs)
		if soak_seconds > 0 || soak_log_ms > 0 {
			fmt.printf("[soak] subscriptions=%d mode=%s log_ms=%d duration_s=%d\n",
				len(subs) * 6, soak_multi ? "multi" : "default", soak_log_ms, soak_seconds)
		}
	}

	// 5. App init.
	state := new(app.App_State)
	app.init(state, text_port, md_port, font_port, settings_port, offline)
	app.set_runtime_connection_defaults(state, ws_url, api_key)
	defer app.shutdown(state)

	start_tick := time.tick_now()
	last_soak_log_tick: time.Tick
	has_last_soak_log_tick := false

	// 6. Main loop (backend-agnostic).
	for !be.should_close() {
		be.poll_events()
		be.begin_frame()

		input := be.collect_input()
		buf := app.update(state, input)
		w, h := be.framebuffer_size()
		render_commands(buf, f32(w), f32(h))

		be.end_frame()
		be.swap()

		if soak_log_ms > 0 || soak_seconds > 0 {
			now_tick := time.tick_now()

			if soak_log_ms > 0 {
				should_log := !has_last_soak_log_tick
				if !should_log {
					should_log = time.tick_since(last_soak_log_tick) >= time.Duration(soak_log_ms) * time.Millisecond
				}
				if should_log {
					last_soak_log_tick = now_tick
					has_last_soak_log_tick = true
					probe := app.runtime_probe(state)
					if probe.has_md_metrics {
						now_ms := time.now()._nsec / 1_000_000
						last_age_ms := i64(0)
						if probe.md_metrics.last_msg_ts_ms > 0 {
							last_age_ms = max(now_ms - probe.md_metrics.last_msg_ts_ms, 0)
						}
						fmt.printf("[soak] t_ms=%d frame=%d conn=%v health=%v streams=%d active=%x ev=%d fix=%d pend_restore=%v subs=%d q=%d qmax=%d cb=%d drop=%d d+%d p=%d rc=%d rc+%d mps=%.1f bps=%d rtt=%d lag=%d last=%d pm=%d pr=%d pb=%d w[t=%d ob=%d/%d st=%d hm=%d vp=%d c=%d]\n",
							i64(time.tick_since(start_tick)/time.Millisecond),
							probe.frame, probe.conn_status, probe.candle_health,
							probe.stream_count, probe.active_subject_id,
							probe.stream_evictions, probe.stream_repairs, probe.pending_restore,
							probe.md_metrics.active_subs, probe.md_metrics.trade_backlog, probe.md_qmax_recent,
							probe.md_metrics.candle_backlog,
							probe.md_metrics.drop_count, probe.md_drop_delta_recent,
							probe.md_metrics.latest_pending, probe.md_metrics.reconnect_count, probe.md_rc_delta_recent,
							probe.md_metrics.msg_rate, i64(probe.md_metrics.bytes_rate), probe.md_metrics.rtt_ms, probe.md_metrics.lag_ms, last_age_ms,
							probe.md_metrics.parsed_msgs_total, probe.md_metrics.parse_arena_resets, probe.md_metrics.parsed_bytes_total,
							probe.w_trades_count, probe.w_orderbook_asks, probe.w_orderbook_bids,
							probe.w_stats_count, probe.w_heatmap_snaps, probe.w_vpvr_levels, probe.w_candle_count,
						)
					} else {
						fmt.printf("[soak] t_ms=%d frame=%d conn=%v health=%v streams=%d active=%x ev=%d fix=%d pend_restore=%v w[t=%d ob=%d/%d st=%d hm=%d vp=%d c=%d]\n",
							i64(time.tick_since(start_tick)/time.Millisecond),
							probe.frame, probe.conn_status, probe.candle_health,
							probe.stream_count, probe.active_subject_id,
							probe.stream_evictions, probe.stream_repairs, probe.pending_restore,
							probe.w_trades_count, probe.w_orderbook_asks, probe.w_orderbook_bids,
							probe.w_stats_count, probe.w_heatmap_snaps, probe.w_vpvr_levels, probe.w_candle_count,
						)
					}
				}
			}

			if soak_seconds > 0 && time.tick_since(start_tick) >= time.Duration(soak_seconds) * time.Second {
				fmt.printf("[soak] done duration_s=%d\n", soak_seconds)
				break
			}
		}

		free_all(context.temp_allocator)
	}
}
