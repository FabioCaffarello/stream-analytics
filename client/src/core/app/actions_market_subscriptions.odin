package app

import "core:fmt"
import "core:strings"

@(private = "file")
subscribe_all_market_channels :: proc(state: ^App_State, venue, symbol: string) {
	if state == nil || state.marketdata.subscribe == nil do return
	state.marketdata.subscribe(venue, symbol, .Trades)
	state.marketdata.subscribe(venue, symbol, .Tape)
	state.marketdata.subscribe(venue, symbol, .Orderbook)
	state.marketdata.subscribe(venue, symbol, .Candles)
	state.marketdata.subscribe(venue, symbol, .Stats)
	state.marketdata.subscribe(venue, symbol, .Heatmaps)
	state.marketdata.subscribe(venue, symbol, .VPVR)
	state.marketdata.subscribe(venue, symbol, .Evidence)
	state.marketdata.subscribe(venue, symbol, .Signals)
}

@(private = "file")
unsubscribe_all_market_channels :: proc(state: ^App_State, venue, symbol: string) {
	if state == nil || state.marketdata.unsubscribe == nil do return
	state.marketdata.unsubscribe(venue, symbol, .Trades)
	state.marketdata.unsubscribe(venue, symbol, .Tape)
	state.marketdata.unsubscribe(venue, symbol, .Orderbook)
	state.marketdata.unsubscribe(venue, symbol, .Candles)
	state.marketdata.unsubscribe(venue, symbol, .Stats)
	state.marketdata.unsubscribe(venue, symbol, .Heatmaps)
	state.marketdata.unsubscribe(venue, symbol, .VPVR)
	state.marketdata.unsubscribe(venue, symbol, .Evidence)
	state.marketdata.unsubscribe(venue, symbol, .Signals)
}

apply_subscribe_market_action :: proc(state: ^App_State, market_entry_idx: int) {
	if state == nil do return
	mi := market_entry_idx
	if mi < 0 || mi >= state.stores.markets.count do return

	entry := state.stores.markets.entries[mi]
	venue := normalized_venue(entry.venue)
	symbol := entry.ticker
	symbol_buf: [80]u8
	if len(entry.market_type) > 0 && !strings.contains(symbol, ":") {
		symbol = fmt.bprintf(symbol_buf[:], "%s:%s", symbol, entry.market_type)
	}
	subscribe_all_market_channels(state, venue, symbol)
	sub_buf: [64]u8
	show_toast(state, fmt.bprintf(sub_buf[:], "%s:%s", venue, symbol))
}

apply_unsubscribe_market_action :: proc(state: ^App_State, market_entry_idx: int) {
	if state == nil do return
	mi := market_entry_idx
	if mi < 0 || mi >= state.stores.markets.count do return

	entry := state.stores.markets.entries[mi]
	venue := normalized_venue(entry.venue)
	symbol := entry.ticker
	symbol_buf: [80]u8
	if len(entry.market_type) > 0 && !strings.contains(symbol, ":") {
		symbol = fmt.bprintf(symbol_buf[:], "%s:%s", symbol, entry.market_type)
	}
	unsubscribe_all_market_channels(state, venue, symbol)
	sub_buf: [64]u8
	show_toast(state, fmt.bprintf(sub_buf[:], "Unsub %s:%s", venue, symbol))
}
