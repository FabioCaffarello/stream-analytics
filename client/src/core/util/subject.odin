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
normalize_subject_venue_into :: proc(buf: []u8, venue: string) -> string {
	if len(venue) == 0 do return ""
	if has_prefix_ci(venue, "binance") do return "binance"
	if has_prefix_ci(venue, "bybit") do return "bybit"
	if has_prefix_ci(venue, "coinbase") do return "coinbase"
	if has_prefix_ci(venue, "kraken") do return "kraken"
	if has_prefix_ci(venue, "hyperliquid") do return "hyperliquid"
	base := venue
	if dash := strings.index(base, "-"); dash > 0 {
		base = base[:dash]
	}
	out := 0
	for c in base {
		ch := c
		if ch >= 'A' && ch <= 'Z' {
			ch += 32
		}
		is_alnum := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
		is_allowed_punct := ch == '-' || ch == '_' || ch == '.'
		if !is_alnum && !is_allowed_punct do break
		if out >= len(buf) do break
		buf[out] = u8(ch)
		out += 1
	}
	if out <= 0 do return ""
	return string(buf[:out])
}

@(private = "file")
normalize_subject_symbol_into :: proc(buf: []u8, symbol: string) -> string {
	if len(symbol) == 0 do return ""
	base := symbol
	if sep := strings.index(base, ":"); sep > 0 {
		base = base[:sep]
	}
	out := 0
	started := false
	for c in base {
		ch := c
		if ch >= 'a' && ch <= 'z' {
			ch -= 32
		}
		is_alnum := (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
		if is_alnum {
			if out >= len(buf) do break
			buf[out] = u8(ch)
			out += 1
			started = true
			continue
		}
		// Keep common separators out of canonical symbol while preserving valid tails.
		if ch == '-' || ch == '_' || ch == '.' {
			if !started do continue
			continue
		}
		// Any other byte (control, slash, etc.) terminates canonicalization.
		break
	}
	if out <= 0 do return ""
	return string(buf[:out])
}

@(private = "file")
channel_to_stream_type :: proc(channel: ports.MD_Channel) -> string {
	switch channel {
	case .Trades:
		return "marketdata.trade"
	case .Orderbook:
		return "aggregation.snapshot"
	case .Stats:
		return "aggregation.stats"
	case .Heatmaps:
		return "insights.heatmap_snapshot"
	case .VPVR:
		return "insights.volume_profile_snapshot"
	case .Candles:
		return "aggregation.candle"
	case .Evidence:
		return "insights.microstructure_evidence"
	case .Signals:
		return "signal/composite"
	}
	return ""
}

@(private = "file")
timeframe_for_channel :: proc(channel: ports.MD_Channel, timeframe: string) -> string {
	switch channel {
	case .Heatmaps, .VPVR, .Candles, .Signals:
		// Timeframe-aware streams follow the active candle timeframe.
		if len(timeframe) > 0 do return timeframe
		return "1m"
	case .Trades, .Orderbook, .Stats, .Evidence:
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
	if len(stream_type) == 0 || len(tf) == 0 do return ""
	venue_buf: [32]u8
	norm_venue := normalize_subject_venue_into(venue_buf[:], venue)
	sym_buf: [64]u8
	norm_symbol := normalize_subject_symbol_into(sym_buf[:], symbol)
	if len(norm_venue) == 0 || len(norm_symbol) == 0 do return ""
	return strings.concatenate({stream_type, "/", norm_venue, "/", norm_symbol, "/", tf})
}

// Stable subject hash built from canonical stream parts without allocating a subject string.
subject_id64_for_stream :: proc(venue, symbol: string, channel: ports.MD_Channel) -> u64 {
	stream_type, timeframe := channel_to_stream_parts(channel)
	if len(stream_type) == 0 || len(timeframe) == 0 do return 0
	venue_buf: [32]u8
	norm_venue := normalize_subject_venue_into(venue_buf[:], venue)
	sym_buf: [64]u8
	norm_symbol := normalize_subject_symbol_into(sym_buf[:], symbol)
	if len(norm_venue) == 0 || len(norm_symbol) == 0 do return 0

	h := u64(14695981039346656037)
	subject_hash_append(&h, stream_type)
	subject_hash_append(&h, "/")
	subject_hash_append(&h, norm_venue)
	subject_hash_append(&h, "/")
	subject_hash_append(&h, norm_symbol)
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

// Market-level hash: aggregates ALL channels for the same venue+symbol into
// one identity.  Used by MarketStore so candle/stats/trade/heatmap/etc events
// for the same market converge into a single Market_Stream.
market_id64 :: proc(venue, symbol: string) -> u64 {
	venue_buf: [32]u8
	norm_venue := normalize_subject_venue_into(venue_buf[:], venue)
	sym_buf: [64]u8
	norm_symbol := normalize_subject_symbol_into(sym_buf[:], symbol)
	if len(norm_venue) == 0 || len(norm_symbol) == 0 do return 0
	h := u64(14695981039346656037)
	subject_hash_append(&h, norm_venue)
	subject_hash_append(&h, "/")
	subject_hash_append(&h, norm_symbol)
	return h
}
