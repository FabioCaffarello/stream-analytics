package main

// WASM entry point — init in main(), per-frame in step() export.
// The odin.js animation loop calls step(dt, odin_ctx) on every frame.

import "base:runtime"
import "core:strings"
import "mr:app"
import "mr:ports"

Venue_Symbol :: struct {
	venue, symbol: string,
}

// Current UI has a single set of stores/widgets (one book, one trades list, one candle chart).
// Subscribe to a single instrument to avoid mixing markets in the same store and to keep
// delivery load below slow-client thresholds until multi-symbol partitioning is implemented.
DEFAULT_SUBS :: []Venue_Symbol{
	{"binance", "BTCUSDT:SPOT"},
}

// Default connection config.
WS_URL  :: "ws://127.0.0.1:8080/ws"
API_KEY :: "prod_key_1"

g_state: app.App_State

query_param_or_into :: proc(name: string, fallback: string, backing: []u8) -> string {
	if len(backing) == 0 do return fallback
	n := url_query_param(
		raw_data(transmute([]u8)name), i32(len(name)),
		raw_data(backing), i32(len(backing)),
	)
	if n <= 0 do return fallback
	if n > i32(len(backing)) do n = i32(len(backing))
	val := strings.trim_space(string(backing[:int(n)]))
	if len(val) == 0 do return fallback
	return val
}

main :: proc() {
	text_port := make_text_port()
	font_port := stub_font_port()
	settings_port := stub_settings_port()
	md_port := make_marketdata_web(WS_URL, API_KEY)

	venue_buf: [32]u8
	symbol_buf: [64]u8
	market_type_buf: [32]u8
	venue := query_param_or_into("venue", DEFAULT_SUBS[0].venue, venue_buf[:])
	symbol := query_param_or_into("symbol", DEFAULT_SUBS[0].symbol, symbol_buf[:])
	market_type := query_param_or_into("market_type", "", market_type_buf[:])
	if len(market_type) > 0 && !strings.contains(symbol, ":") {
		symbol = strings.concatenate({symbol, ":", market_type})
	}

	// Single-symbol subscription (query params: ?venue=binance&symbol=SOLUSDT&market_type=SPOT).
	md_port.subscribe(venue, symbol, .Trades)
	md_port.subscribe(venue, symbol, .Orderbook)
	md_port.subscribe(venue, symbol, .Stats)
	md_port.subscribe(venue, symbol, .Candles)

	app.init(&g_state, text_port, md_port, font_port, settings_port, false)
}

@(private = "file")
decode_key_state :: proc(input: ^ports.Input_State) {
	bits := key_state()
	if bits == 0 do return
	if bits & (1 << 0) != 0 do input.keys.pressed += {.Up}
	if bits & (1 << 1) != 0 do input.keys.pressed += {.Down}
	if bits & (1 << 2) != 0 do input.keys.pressed += {.Left}
	if bits & (1 << 3) != 0 do input.keys.pressed += {.Right}
	if bits & (1 << 4) != 0 do input.keys.pressed += {.Enter}
	if bits & (1 << 5) != 0 do input.keys.pressed += {.Escape}
	if bits & (1 << 6) != 0 do input.keys.pressed += {.Tab}
	if bits & (1 << 7) != 0 do input.keys.pressed += {.Space}
	if bits & (1 << 8) != 0 do input.keys.pressed += {.Num_1}
	if bits & (1 << 9) != 0 do input.keys.pressed += {.Num_2}
	if bits & (1 << 10) != 0 do input.keys.pressed += {.Num_3}
	if bits & (1 << 11) != 0 do input.keys.pressed += {.Num_4}
	if bits & (1 << 12) != 0 do input.keys.pressed += {.Num_5}
	if bits & (1 << 13) != 0 do input.keys.pressed += {.Num_6}
}

@(export)
step :: proc "c" (dt: f32, odin_ctx: rawptr) -> bool {
	context = (^runtime.Context)(odin_ctx)^

	input: ports.Input_State
	input.viewport_size = canvas_viewport_size()
	decode_key_state(&input)
	buf, should_render := app.update_web(&g_state, input)
	if should_render {
		render_commands(buf)
	}

	free_all(context.temp_allocator)
	return true // continue animation loop
}
