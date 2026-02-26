package util

// Subject builder — maps (venue, symbol, channel) to MR canonical WS subjects.
// Format: "<stream_type>/<venue>/<symbol>/<timeframe>"

import "core:strings"
import "mr:ports"

@(private = "file")
subject_hash_append :: proc(h: ^u64, s: string) {
	for c in s {
		h^ ~= u64(u8(c))
		h^ *= u64(1099511628211)
	}
}

// Builds a subject string allocated on the heap (context.allocator).
// Caller owns the returned string; it survives temp_allocator resets.
build_subject :: proc(venue, symbol: string, channel: ports.MD_Channel) -> string {
	stream_type: string
	timeframe: string

	switch channel {
	case .Trades:
		stream_type = "marketdata.trade"
		timeframe = "raw"
	case .Orderbook:
		stream_type = "marketdata.bookdelta"
		timeframe = "raw"
	case .Stats:
		stream_type = "aggregation.stats"
		// Delivery route uses raw; window/timeframe is carried inside payload.
		timeframe = "raw"
	case .Heatmaps:
		stream_type = "insights.heatmap_snapshot"
		timeframe = "1m"
	case .VPVR:
		stream_type = "insights.volume_profile_snapshot"
		timeframe = "1m"
	case .Candles:
		stream_type = "aggregation.candle"
		// Current WS contract routes aggregation candles on raw subjects.
		// The concrete candle timeframe is carried in payload.Timeframe.
		timeframe = "raw"
	}

	return strings.concatenate({stream_type, "/", venue, "/", symbol, "/", timeframe})
}

// Stable subject hash built from canonical stream parts without allocating a subject string.
subject_id64_for_stream :: proc(venue, symbol: string, channel: ports.MD_Channel) -> u64 {
	stream_type: string
	timeframe: string

	switch channel {
	case .Trades:
		stream_type = "marketdata.trade"
		timeframe = "raw"
	case .Orderbook:
		stream_type = "marketdata.bookdelta"
		timeframe = "raw"
	case .Stats:
		stream_type = "aggregation.stats"
		timeframe = "raw"
	case .Heatmaps:
		stream_type = "insights.heatmap_snapshot"
		timeframe = "1m"
	case .VPVR:
		stream_type = "insights.volume_profile_snapshot"
		timeframe = "1m"
	case .Candles:
		stream_type = "aggregation.candle"
		timeframe = "raw"
	}

	h := u64(14695981039346656037)
	subject_hash_append(&h, stream_type)
	subject_hash_append(&h, "/")
	subject_hash_append(&h, venue)
	subject_hash_append(&h, "/")
	subject_hash_append(&h, symbol)
	subject_hash_append(&h, "/")
	subject_hash_append(&h, timeframe)
	return h
}

// Returns the stream type prefix before the first '/'.
// e.g. "marketdata.trade/binance/BTCUSDT/raw" → "marketdata.trade"
subject_stream_type :: proc(subject: string) -> string {
	for i in 0 ..< len(subject) {
		if subject[i] == '/' do return subject[:i]
	}
	return subject
}

// Returns the suffix after the final '/'.
// e.g. ".../raw" -> "raw"
subject_timeframe :: proc(subject: string) -> string {
	last_sep := -1
	for i in 0 ..< len(subject) {
		if subject[i] == '/' {
			last_sep = i
		}
	}
	if last_sep < 0 do return ""
	if last_sep + 1 >= len(subject) do return ""
	return subject[last_sep + 1:]
}

// Stable non-cryptographic 64-bit hash for subject routing inside the client.
// FNV-1a is fast and allocation-free.
subject_id64 :: proc(subject: string) -> u64 {
	h := u64(14695981039346656037)
	for c in subject {
		h ~= u64(u8(c))
		h *= u64(1099511628211)
	}
	return h
}
