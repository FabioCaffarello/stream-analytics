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

@(private = "file")
channel_to_stream_type :: proc(channel: ports.MD_Channel) -> string {
	switch channel {
	case .Trades:
		return "marketdata.trade"
	case .Orderbook:
		return "marketdata.bookdelta"
	case .Stats:
		return "aggregation.stats"
	case .Heatmaps:
		return "insights.heatmap_snapshot"
	case .VPVR:
		return "insights.volume_profile_snapshot"
	case .Candles:
		return "aggregation.candle"
	}
	return ""
}

@(private = "file")
timeframe_for_channel :: proc(channel: ports.MD_Channel, timeframe: string) -> string {
	switch channel {
	case .Heatmaps, .VPVR, .Candles:
		// Timeframe-aware streams follow the active candle timeframe.
		if len(timeframe) > 0 do return timeframe
		return "1m"
	case .Trades, .Orderbook, .Stats:
		return "raw"
	}
	return ""
}

// Maps a channel enum to the canonical (stream_type, timeframe) parts
// used in MR WS subject strings.
channel_to_stream_parts :: proc(channel: ports.MD_Channel) -> (stream_type: string, timeframe: string) {
	return channel_to_stream_parts_with_timeframe(channel, "")
}

// Variant that allows overriding timeframe for timeframe-aware channels.
channel_to_stream_parts_with_timeframe :: proc(channel: ports.MD_Channel, timeframe: string) -> (stream_type: string, out_timeframe: string) {
	return channel_to_stream_type(channel), timeframe_for_channel(channel, timeframe)
}

// Builds a subject string allocated on the heap (context.allocator).
// Caller owns the returned string; it survives temp_allocator resets.
build_subject :: proc(venue, symbol: string, channel: ports.MD_Channel) -> string {
	return build_subject_with_timeframe(venue, symbol, channel, "")
}

// Build subject variant that allows timeframe override for heatmap/VPVR streams.
build_subject_with_timeframe :: proc(venue, symbol: string, channel: ports.MD_Channel, timeframe: string) -> string {
	stream_type, tf := channel_to_stream_parts_with_timeframe(channel, timeframe)
	return strings.concatenate({stream_type, "/", venue, "/", symbol, "/", tf})
}

// Stable subject hash built from canonical stream parts without allocating a subject string.
subject_id64_for_stream :: proc(venue, symbol: string, channel: ports.MD_Channel) -> u64 {
	stream_type, timeframe := channel_to_stream_parts(channel)

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
