package main

// Native entry point — backend-agnostic.
//
// Imports: backend (windowing/GL), mr:app (core logic).
// Does NOT import vendor:glfw, vendor:OpenGL, or deps:imgui.
// To switch backends: change make_glfw_backend() to make_sdl2_backend().

import "core:os"
import "core:strings"
import "backend"
import "mr:app"

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

main :: proc() {
	// 1. Parse flags.
	use_sdl2 := false
	offline := false
	ws_url := "ws://127.0.0.1:8080/ws"
	api_key := "prod_key_1"
	for i in 0 ..< len(os.args) {
		arg := os.args[i]
		if arg == "--sdl2"    do use_sdl2 = true
		if arg == "--offline" do offline = true
		if strings.has_prefix(arg, "--ws-url=") {
			ws_url = arg[len("--ws-url="):]
		}
		if strings.has_prefix(arg, "--api-key=") {
			api_key = arg[len("--api-key="):]
		}
	}

	// 2. Backend init.
	be := use_sdl2 ? backend.make_sdl2_backend() : backend.make_glfw_backend()
	if !be.init("Market Raccoon", 800, 600) do return
	defer be.shutdown()

	// 3. Ports.
	font_port := make_font_port()
	text_port := make_text_port()
	md_port := offline ? stub_marketdata_port() : make_marketdata_native(ws_url, api_key)
	settings_port := make_settings_port()

	// 4. Subscribe to all venues/channels when connected.
	if !offline {
		for vs in DEFAULT_SUBS {
			md_port.subscribe(vs.venue, vs.symbol, .Trades)
			md_port.subscribe(vs.venue, vs.symbol, .Orderbook)
			md_port.subscribe(vs.venue, vs.symbol, .Stats)
			md_port.subscribe(vs.venue, vs.symbol, .Heatmaps)
			md_port.subscribe(vs.venue, vs.symbol, .VPVR)
		}
	}

	// 5. App init.
	state: app.App_State
	app.init(&state, text_port, md_port, font_port, settings_port, offline)
	defer app.shutdown(&state)

	// 6. Main loop (backend-agnostic).
	for !be.should_close() {
		be.poll_events()
		be.begin_frame()

		input := be.collect_input()
		buf := app.update(&state, input)
		w, h := be.framebuffer_size()
		render_commands(buf, f32(w), f32(h))

		be.end_frame()
		be.swap()

		free_all(context.temp_allocator)
	}
}
