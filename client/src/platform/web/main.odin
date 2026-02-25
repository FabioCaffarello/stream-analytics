package main

// WASM entry point — init in main(), per-frame in step() export.
// The odin.js animation loop calls step(dt, odin_ctx) on every frame.

import "base:runtime"
import "mr:app"
import "mr:ports"

Venue_Symbol :: struct {
	venue, symbol: string,
}

// All venues MR supports, BTC + ETH per venue.
DEFAULT_SUBS :: []Venue_Symbol{
	{"binance",     "BTCUSDT"}, {"binance",     "ETHUSDT"},
	{"bybit",       "BTCUSDT"}, {"bybit",       "ETHUSDT"},
	{"coinbase",    "BTCUSD"},  {"coinbase",    "ETHUSD"},
	{"hyperliquid", "BTC"},     {"hyperliquid", "ETH"},
	{"kraken",      "BTCUSD"},  {"kraken",      "ETHUSD"},
	{"krakenf",     "BTCUSDT"}, {"krakenf",     "ETHUSDT"},
}

// Default connection config (overridable via URL query params in future).
WS_URL  :: "ws://127.0.0.1:8080/ws"
API_KEY :: "prod_key_1"

g_state: app.App_State

main :: proc() {
	text_port := make_text_port()
	font_port := stub_font_port()
	settings_port := stub_settings_port()
	md_port := make_marketdata_web(WS_URL, API_KEY)

	// Subscribe all venues/channels.
	for vs in DEFAULT_SUBS {
		md_port.subscribe(vs.venue, vs.symbol, .Trades)
		md_port.subscribe(vs.venue, vs.symbol, .Orderbook)
		md_port.subscribe(vs.venue, vs.symbol, .Stats)
		md_port.subscribe(vs.venue, vs.symbol, .Heatmaps)
		md_port.subscribe(vs.venue, vs.symbol, .VPVR)
	}

	app.init(&g_state, text_port, md_port, font_port, settings_port, false)
}

@(export)
step :: proc "c" (dt: f32, odin_ctx: rawptr) -> bool {
	context = (^runtime.Context)(odin_ctx)^

	input: ports.Input_State
	buf := app.update(&g_state, input)
	render_commands(buf)

	free_all(context.temp_allocator)
	return true // continue animation loop
}
