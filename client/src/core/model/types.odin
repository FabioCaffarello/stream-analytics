package model

// Domain types migrated from MarketMonkey (pure, no platform imports).

import "core:strings"

Timeframe :: enum i64 {
	SEC         = 1,
	FIVE_SEC    = 5,
	MIN         = 60,
	FIVE_MIN    = 300,
	FIFTEEN_MIN = 900,
	THIRTY_MIN  = 1800,
	HOURLY      = 3600,
	DAILY       = 86400,
	WEEKLY      = 604800,
	MONTHLY     = 2629800,
}

timeframe_to_string :: proc(tf: Timeframe) -> string {
	switch tf {
	case .SEC:         return "1s"
	case .FIVE_SEC:    return "5s"
	case .MIN:         return "1m"
	case .FIVE_MIN:    return "5m"
	case .FIFTEEN_MIN: return "15m"
	case .THIRTY_MIN:  return "30m"
	case .HOURLY:      return "1h"
	case .DAILY:       return "1d"
	case .WEEKLY:      return "1w"
	case .MONTHLY:     return "1M"
	}
	return ""
}

StreamType :: enum i32 {
	Trades,
	Orderbook,
	Heatmaps,
	Heatmap,
	Candles,
	Volumes,
	Stats,
	Liquidations,
	ServerConfig,
}

Pair :: struct {
	exchange: string `json:"exchange"`,
	symbol:   string `json:"symbol"`,
}

pair_clone :: proc(pair: Pair, allocator := context.allocator) -> Pair {
	return Pair{
		exchange = strings.clone(pair.exchange, allocator),
		symbol   = strings.clone(pair.symbol, allocator),
	}
}

pair_delete :: proc(pair: Pair, allocator := context.allocator) {
	delete(pair.exchange, allocator)
	delete(pair.symbol, allocator)
}

// --- Payload (WS envelope) ---

Payload :: struct {
	pair:      Pair       `json:"Pair"`,
	stream:    StreamType `json:"Stream"`,
	timeframe: i64        `json:"Timeframe"`,
	data:      string     `json:"Data"`,
}

// --- Stream ---

Stream :: struct {
	pair:      Pair,
	stream:    StreamType,
	timeframe: i64,
}

stream_clone :: proc(stream: Stream, allocator := context.allocator) -> Stream {
	return Stream{
		pair      = pair_clone(stream.pair, allocator),
		timeframe = stream.timeframe,
		stream    = stream.stream,
	}
}

// --- Trade ---

Trade :: struct {
	pair:   Pair,
	price:  f64,
	qty:    f64,
	is_buy: bool `json:"isBuy"`,
	unix:   i64,
}

trade_clone :: proc(trade: ^Trade, allocator := context.allocator) -> Trade {
	assert(trade != nil, "trying to clone nil trade pointer")
	return Trade{
		price  = trade.price,
		qty    = trade.qty,
		is_buy = trade.is_buy,
		unix   = trade.unix,
		pair   = pair_clone(trade.pair, allocator),
	}
}

trade_delete :: proc(trade: Trade, allocator := context.allocator) {
	pair_delete(trade.pair, allocator)
}

// --- Candle ---

Candle :: struct {
	unix:   i64,
	open:   f64,
	close:  f64,
	high:   f64,
	low:    f64,
	v_buy:  f64 `json:"vbuy"`,
	v_sell: f64 `json:"vsell"`,
	t_buy:  f64 `json:"tbuy"`,
	t_sell: f64 `json:"tsell"`,
	final:  bool,
}

Candles :: struct {
	pair:      Pair,
	timeframe: i64,
	values:    []Candle,
}

// --- Orderbook ---

Orderbook :: struct {
	pair:       Pair,
	unix:       i64,
	ask_prices: []f64 `json:"askPrices"`,
	ask_sizes:  []f64 `json:"askSizes"`,
	bid_prices: []f64 `json:"bidPrices"`,
	bid_sizes:  []f64 `json:"bidSizes"`,
	last_price: f64   `json:"lastPrice"`,
}

orderbook_clone :: proc(ob: ^Orderbook, allocator := context.allocator) -> Orderbook {
	book: Orderbook
	book.unix       = ob.unix
	book.last_price = ob.last_price
	book.pair       = pair_clone(ob.pair, allocator)
	book.ask_prices = make([]f64, len(ob.ask_prices), allocator)
	book.ask_sizes  = make([]f64, len(ob.ask_sizes), allocator)
	book.bid_prices = make([]f64, len(ob.bid_prices), allocator)
	book.bid_sizes  = make([]f64, len(ob.bid_sizes), allocator)
	copy(book.ask_prices, ob.ask_prices)
	copy(book.ask_sizes, ob.ask_sizes)
	copy(book.bid_prices, ob.bid_prices)
	copy(book.bid_sizes, ob.bid_sizes)
	return book
}

orderbook_delete :: proc(ob: Orderbook, allocator := context.allocator) {
	delete(ob.ask_prices, allocator)
	delete(ob.ask_sizes, allocator)
	delete(ob.bid_prices, allocator)
	delete(ob.bid_sizes, allocator)
	pair_delete(ob.pair, allocator)
}

// --- Volume ---

Volume :: struct {
	unix:        i64,
	timeframe:   i64,
	prices:      []f64,
	buys:        []f64,
	sells:       []f64,
	price_group: f64  `json:"priceGroup"`,
	final:       bool,
}

volume_clone :: proc(v: Volume, allocator := context.allocator) -> Volume {
	vol: Volume
	vol.unix        = v.unix
	vol.timeframe   = v.timeframe
	vol.price_group = v.price_group
	vol.final       = v.final
	vol.prices = make([]f64, len(v.prices), allocator)
	vol.buys   = make([]f64, len(v.buys), allocator)
	vol.sells  = make([]f64, len(v.sells), allocator)
	copy(vol.prices, v.prices)
	copy(vol.buys, v.buys)
	copy(vol.sells, v.sells)
	return vol
}

volume_delete :: proc(v: Volume, allocator := context.allocator) {
	delete(v.prices, allocator)
	delete(v.buys, allocator)
	delete(v.sells, allocator)
}

Volumes :: struct {
	pair:      Pair,
	timeframe: i64,
	values:    []Volume,
}

volumes_clone_values :: proc(v: ^Volumes, allocator := context.allocator) -> []Volume {
	volumes := make([]Volume, len(v.values), allocator)
	for i in 0 ..< len(v.values) {
		volumes[i] = volume_clone(v.values[i], allocator)
	}
	return volumes
}

// --- Heatmap ---

Heatmap :: struct {
	unix:        i64,
	price_group: f64 `json:"priceGroup"`,
	min_price:   f64 `json:"minPrice"`,
	max_price:   f64 `json:"maxPrice"`,
	max_size:    f64 `json:"maxSize"`,
	min_size:    f64 `json:"minSize"`,
	prices:      []f64,
	sizes:       []f64,
}

heatmap_clone :: proc(heatmap: Heatmap, allocator := context.allocator) -> Heatmap {
	h: Heatmap
	h.unix        = heatmap.unix
	h.price_group = heatmap.price_group
	h.min_price   = heatmap.min_price
	h.max_price   = heatmap.max_price
	h.max_size    = heatmap.max_size
	h.min_size    = heatmap.min_size
	h.prices = make([]f64, len(heatmap.prices), allocator)
	h.sizes  = make([]f64, len(heatmap.sizes), allocator)
	copy(h.prices, heatmap.prices)
	copy(h.sizes, heatmap.sizes)
	return h
}

heatmap_delete :: proc(heatmap: Heatmap, allocator := context.allocator) {
	delete(heatmap.sizes, allocator)
	delete(heatmap.prices, allocator)
}

Heatmaps :: struct {
	pair:      Pair,
	timeframe: i64,
	values:    []Heatmap,
}

heatmaps_values_clone :: proc(h: ^Heatmaps, allocator := context.allocator) -> []Heatmap {
	heatmaps := make([]Heatmap, len(h.values), allocator)
	for i in 0 ..< len(h.values) {
		heatmaps[i] = heatmap_clone(h.values[i], allocator)
	}
	return heatmaps
}

// --- Stats ---

Stat :: struct {
	unix:       i64,
	liq_v_sell: f64  `json:"liqVsell"`,
	liq_v_buy:  f64  `json:"liqVbuy"`,
	mark_price: f64  `json:"markPrice"`,
	funding:    f64  `json:"funding"`,
	tbuy:       i64  `json:"tbuy"`,
	tsell:      i64  `json:"tsell"`,
	final:      bool,
}

Stats :: struct {
	pair:      Pair,
	timeframe: i64,
	values:    []Stat,
}
