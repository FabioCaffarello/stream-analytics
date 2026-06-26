package streams

import "core:strings"

STREAM_ID_CAP :: 96
STREAM_VENUE_CAP :: 32
STREAM_SYMBOL_CAP :: 48
STREAM_MARKET_TYPE_CAP :: 24
STREAM_DESYNC_REASON_CAP :: 64

Stream_State :: enum u8 {
	Offline,
	Live,
	Lag,
	Desync,
}

Stream_Desync_Reason :: enum u8 {
	None,
	Sequence_Gap,
	Snapshot_Gap,
	Snapshot_Stale,
	Clock_Drift,
	Protocol_Version,
	Protocol_Invalid,
	Missing_Hello,
	Resync_Required,
	Manual,
}

Stream_Status :: struct {
	state:             Stream_State,
	connected:         bool,
	last_seq:          i64,
	last_server_ts_ms: i64,
	last_local_ts_ms:  i64,
	last_snapshot_ts_ms: i64,
	last_message_age_ms: i64,
	rtt_ms:            i64,
	lag_ms:            i64,
	drop_count:        int,
	reconnect_count:   int,
	subscribe_acks:    int,
	desync_reason:     Stream_Desync_Reason,
}

Stream_Handle :: struct {
	used:        bool,
	id_buf:      [STREAM_ID_CAP]u8,
	id_len:      u8,
	venue_buf:   [STREAM_VENUE_CAP]u8,
	venue_len:   u8,
	symbol_buf:  [STREAM_SYMBOL_CAP]u8,
	symbol_len:  u8,
	market_type_buf: [STREAM_MARKET_TYPE_CAP]u8,
	market_type_len: u8,
	ref_count:   int,
	paused:      bool,
	status:      Stream_Status,
}

stream_id :: proc(h: ^Stream_Handle) -> string {
	if h == nil || h.id_len == 0 do return ""
	n := int(h.id_len)
	if n > len(h.id_buf) do n = len(h.id_buf)
	return string(h.id_buf[:n])
}

stream_venue :: proc(h: ^Stream_Handle) -> string {
	if h == nil || h.venue_len == 0 do return ""
	n := int(h.venue_len)
	if n > len(h.venue_buf) do n = len(h.venue_buf)
	return string(h.venue_buf[:n])
}

stream_symbol :: proc(h: ^Stream_Handle) -> string {
	if h == nil || h.symbol_len == 0 do return ""
	n := int(h.symbol_len)
	if n > len(h.symbol_buf) do n = len(h.symbol_buf)
	return string(h.symbol_buf[:n])
}

stream_market_type :: proc(h: ^Stream_Handle) -> string {
	if h == nil || h.market_type_len == 0 do return ""
	n := int(h.market_type_len)
	if n > len(h.market_type_buf) do n = len(h.market_type_buf)
	return string(h.market_type_buf[:n])
}

@(private = "file")
copy_into_buf :: proc(dst: []u8, src: string) -> int {
	n := min(len(dst), len(src))
	for i in 0 ..< n {
		dst[i] = src[i]
	}
	return n
}

@(private = "file")
append_part_into :: proc(buf: []u8, index: ^int, part: string) {
	if index == nil do return
	n := index^
	for c in part {
		if n >= len(buf) do break
		buf[n] = u8(c)
		n += 1
	}
	index^ = n
}

set_stream_identity :: proc(h: ^Stream_Handle, id: string, venue: string, symbol: string, market_type: string) {
	if h == nil do return
	h.id_len = u8(copy_into_buf(h.id_buf[:], id))
	h.venue_len = u8(copy_into_buf(h.venue_buf[:], venue))
	h.symbol_len = u8(copy_into_buf(h.symbol_buf[:], symbol))
	h.market_type_len = u8(copy_into_buf(h.market_type_buf[:], market_type))
}

format_stream_id_into :: proc(buf: []u8, venue: string, symbol: string, market_type: string) -> string {
	if len(buf) <= 0 do return ""
	n := 0
	append_part_into(buf, &n, "stream://")
	append_part_into(buf, &n, venue)
	append_part_into(buf, &n, "/")
	append_part_into(buf, &n, symbol)
	if len(market_type) > 0 && !strings.contains(symbol, ":") {
		append_part_into(buf, &n, ":")
		append_part_into(buf, &n, market_type)
	}
	return string(buf[:n])
}

split_symbol_market_type :: proc(symbol: string) -> (base_symbol: string, market_type: string) {
	sep := strings.last_index(symbol, ":")
	if sep <= 0 || sep >= len(symbol) - 1 do return symbol, ""
	return symbol[:sep], symbol[sep + 1:]
}
