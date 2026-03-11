package md_common

// Shared marketdata helpers used by both native and web platform implementations.
// Eliminates ~200 LOC of byte-for-byte duplication between the two backends.

import "core:fmt"
import "core:strings"
import "core:time"
import "mr:ports"
import "mr:services"
import "mr:util"

// --- JSON message builders ---
// Build WS protocol messages into a caller-supplied buffer.
// Return (message_string, ok). ok=false on buffer overflow.

@(private = "package")
subject_is_json_safe :: proc(subject: string) -> bool {
	if len(subject) == 0 do return false
	for c in subject {
		ch := u8(c)
		if ch < 0x20 do return false
		if ch == '"' || ch == '\\' do return false
	}
	return true
}

build_subscribe_msg :: proc(buf: []u8, subject: string, rid: u32) -> (string, bool) {
	if !subject_is_json_safe(subject) do return "", false
	n := 0
	prefix :: `{"op":"subscribe","subject":"`
	for c in prefix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in subject { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	mid :: `","request_id":"r`
	for c in mid { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	rid_buf: [16]u8
	rid_str := fmt.bprintf(rid_buf[:], "%d", rid)
	for c in rid_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	return string(buf[:n]), true
}

build_unsubscribe_msg :: proc(buf: []u8, subject: string, rid: u32) -> (string, bool) {
	if !subject_is_json_safe(subject) do return "", false
	n := 0
	prefix :: `{"op":"unsubscribe","subject":"`
	for c in prefix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in subject { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	mid :: `","request_id":"r`
	for c in mid { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	rid_buf: [16]u8
	rid_str := fmt.bprintf(rid_buf[:], "%d", rid)
	for c in rid_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	return string(buf[:n]), true
}

build_getrange_msg :: proc(buf: []u8, subject: string, limit: int, end_ts: i64, rid: u32) -> (string, bool) {
	if !subject_is_json_safe(subject) do return "", false
	n := 0
	prefix :: `{"op":"getrange","subject":"`
	for c in prefix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in subject { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	mid :: `","params":{"limit":`
	for c in mid { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	limit_buf: [16]u8
	limit_str := fmt.bprintf(limit_buf[:], "%d", limit)
	for c in limit_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	if end_ts > 0 {
		end_mid :: `,"to_ms":`
		for c in end_mid { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
		end_buf: [24]u8
		end_str := fmt.bprintf(end_buf[:], "%d", end_ts)
		for c in end_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	}
	mid2 :: `},"request_id":"gr`
	for c in mid2 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	rid_buf: [16]u8
	rid_str := fmt.bprintf(rid_buf[:], "%d", rid)
	for c in rid_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	return string(buf[:n]), true
}

// Terminal_V1 getrange: includes component fields for server-side routing (S148-BUG-1).
// Legacy getrange only sent "subject" — server in Terminal_V1 mode requires venue/symbol/channel.
build_getrange_msg_v2 :: proc(buf: []u8, subject: string, venue: string, symbol: string, channel: string, aggregation: string, limit: int, end_ts: i64, rid: u32) -> (string, bool) {
	if !subject_is_json_safe(subject) do return "", false
	n := 0
	p1 :: `{"op":"getrange","subject":"`
	for c in p1 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in subject { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	p2 :: `","venue":"`
	for c in p2 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in venue { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	p3 :: `","symbol":"`
	for c in p3 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in symbol { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	p4 :: `","channel":"`
	for c in p4 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in channel { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	p5 :: `","aggregation":"`
	for c in p5 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in aggregation { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	p6 :: `","params":{"limit":`
	for c in p6 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	limit_buf: [16]u8
	limit_str := fmt.bprintf(limit_buf[:], "%d", limit)
	for c in limit_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	if end_ts > 0 {
		end_mid :: `,"to_ms":`
		for c in end_mid { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
		end_buf: [24]u8
		end_str := fmt.bprintf(end_buf[:], "%d", end_ts)
		for c in end_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	}
	p7 :: `},"request_id":"gr`
	for c in p7 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	rid_buf: [16]u8
	rid_str := fmt.bprintf(rid_buf[:], "%d", rid)
	for c in rid_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	return string(buf[:n]), true
}

// Terminal_V1 subscribe: includes component fields alongside subject for richer server-side routing.
build_subscribe_msg_v2 :: proc(buf: []u8, subject: string, venue: string, symbol: string, channel: string, aggregation: string, rid: u32) -> (string, bool) {
	if !subject_is_json_safe(subject) do return "", false
	n := 0
	p1 :: `{"op":"subscribe","subject":"`
	for c in p1 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in subject { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	p2 :: `","venue":"`
	for c in p2 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in venue { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	p3 :: `","symbol":"`
	for c in p3 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in symbol { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	p4 :: `","channel":"`
	for c in p4 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in channel { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	p5 :: `","aggregation":"`
	for c in p5 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in aggregation { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	mid :: `","request_id":"r`
	for c in mid { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	rid_buf: [16]u8
	rid_str := fmt.bprintf(rid_buf[:], "%d", rid)
	for c in rid_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	return string(buf[:n]), true
}

// Terminal_V1 unsubscribe: uses stream_id when available (from ACK).
build_unsubscribe_msg_v2 :: proc(buf: []u8, stream_id: string, rid: u32) -> (string, bool) {
	if !subject_is_json_safe(stream_id) do return "", false
	n := 0
	prefix :: `{"op":"unsubscribe","stream_id":"`
	for c in prefix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in stream_id { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	mid :: `","request_id":"r`
	for c in mid { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	rid_buf: [16]u8
	rid_str := fmt.bprintf(rid_buf[:], "%d", rid)
	for c in rid_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	return string(buf[:n]), true
}

// Extract subject components (venue, symbol, channel, aggregation) from a subject string.
// Subject format: "marketdata.<channel>/<venue>/<symbol>/<aggregation>"
parse_subject_components :: proc(subject: string) -> (venue: string, symbol: string, channel: string, aggregation: string) {
	// Find first '/' — everything before it is the channel prefix (e.g. "marketdata.trade")
	first_slash := -1
	for i in 0 ..< len(subject) {
		if subject[i] == '/' { first_slash = i; break }
	}
	if first_slash < 0 do return "", "", "", ""
	channel = subject[:first_slash]

	rest := subject[first_slash + 1:]
	// Find second '/' — venue
	second_slash := -1
	for i in 0 ..< len(rest) {
		if rest[i] == '/' { second_slash = i; break }
	}
	if second_slash < 0 {
		venue = rest
		return venue, "", channel, ""
	}
	venue = rest[:second_slash]

	rest2 := rest[second_slash + 1:]
	// Find third '/' — symbol
	third_slash := -1
	for i in 0 ..< len(rest2) {
		if rest2[i] == '/' { third_slash = i; break }
	}
	if third_slash < 0 {
		symbol = rest2
		return venue, symbol, channel, ""
	}
	symbol = rest2[:third_slash]
	aggregation = rest2[third_slash + 1:]
	return venue, symbol, channel, aggregation
}

@(private = "package")
url_ascii_lower :: proc(ch: u8) -> u8 {
	if ch >= 'A' && ch <= 'Z' do return ch + 32
	return ch
}

@(private = "package")
url_segment_contains_fold :: proc(segment: string, needle: string) -> bool {
	if len(needle) == 0 || len(segment) < len(needle) do return false
	last := len(segment) - len(needle)
	for i in 0 ..< last + 1 {
		match := true
		for j in 0 ..< len(needle) {
			if url_ascii_lower(segment[i + j]) != needle[j] {
				match = false
				break
			}
		}
		if match do return true
	}
	return false
}

@(private = "package")
url_segment_has_sensitive_keyword :: proc(segment: string) -> bool {
	if len(segment) == 0 do return false
	return url_segment_contains_fold(segment, "token") ||
		url_segment_contains_fold(segment, "secret") ||
		url_segment_contains_fold(segment, "apikey") ||
		url_segment_contains_fold(segment, "api_key") ||
		url_segment_contains_fold(segment, "api-key") ||
		url_segment_contains_fold(segment, "password") ||
		url_segment_contains_fold(segment, "passwd") ||
		url_segment_contains_fold(segment, "signature") ||
		url_segment_contains_fold(segment, "access_key") ||
		url_segment_contains_fold(segment, "private_key") ||
		url_segment_contains_fold(segment, "bearer") ||
		url_segment_contains_fold(segment, "jwt")
}

// sanitize_url_for_log removes query/fragment, redacts userinfo and hides
// path segments likely to carry credentials.
sanitize_url_for_log :: proc(url: string) -> string {
	if len(url) == 0 do return ""
	sanitized := url
	if fragment_idx := strings.index(sanitized, "#"); fragment_idx >= 0 {
		sanitized = sanitized[:fragment_idx]
	}
	if query_idx := strings.index(sanitized, "?"); query_idx >= 0 {
		sanitized = sanitized[:query_idx]
	}
	scheme_idx := strings.index(sanitized, "://")
	if scheme_idx < 0 do return sanitized
	authority_start := scheme_idx + 3
	if authority_start >= len(sanitized) do return sanitized
	authority_end := len(sanitized)
	if rel_path_idx := strings.index(sanitized[authority_start:], "/"); rel_path_idx >= 0 {
		authority_end = authority_start + rel_path_idx
	}
	authority := sanitized[authority_start:authority_end]
	if at_idx := strings.last_index(authority, "@"); at_idx >= 0 {
		host := authority[at_idx + 1:]
		sanitized = strings.concatenate(
			{sanitized[:authority_start], "<redacted>@", host, sanitized[authority_end:]},
			context.temp_allocator,
		)
		authority_end = authority_start + len("<redacted>@") + len(host)
	}
	if authority_end >= len(sanitized) || sanitized[authority_end] != '/' do return sanitized
	segment_start := authority_end + 1
	for i := segment_start; i <= len(sanitized); i += 1 {
		if i < len(sanitized) && sanitized[i] != '/' do continue
		segment := sanitized[segment_start:i]
		if url_segment_has_sensitive_keyword(segment) {
			return strings.concatenate({sanitized[:segment_start], "<redacted>"}, context.temp_allocator)
		}
		segment_start = i + 1
	}
	return sanitized
}

build_hello_msg :: proc(buf: []u8, rid: u32) -> (string, bool) {
	n := 0
	prefix :: `{"op":"hello","type":"hello","request_id":"h`
	for c in prefix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	rid_buf: [16]u8
	rid_str := fmt.bprintf(rid_buf[:], "%d", rid)
	for c in rid_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	return string(buf[:n]), true
}

// --- Backpressure state derivation ---

Backpressure_State :: enum u8 {
	Normal,    // level 0
	Elevated,  // level 1
	High,      // level 2
	Critical,  // level 3+
}

backpressure_state_from_level :: proc(level: int) -> Backpressure_State {
	if level <= 0 do return .Normal
	if level == 1 do return .Elevated
	if level == 2 do return .High
	return .Critical
}

// Maximum number of requested features we can encode.
MAX_REQUESTED_FEATURES :: 8

Platform_Kind :: enum u8 {
	Native,
	WASM,
}

// Determine if a feature should be requested based on setting + server state.
// setting: "auto" (or "") / "on"/"1"/"true" / "off"/"0"/"false"
// server_known: true if we received a HELLO with supported_features list.
// server_has: true if the server advertised this feature.
feature_should_request :: proc(setting: string, server_known: bool, server_has: bool) -> bool {
	// Explicit override always wins.
	if setting == "off" || setting == "0" || setting == "false" do return false
	if setting == "on" || setting == "1" || setting == "true" do return true
	// Auto: request if server unknown (optimistic first-connect) or server supports it.
	return !server_known || server_has
}

// Resolve requested features deterministically from mode, platform, server state, and settings.
// mode: Legacy_JSON never requests features.
// platform: reserved for future WASM-vs-native priority differentiation.
// server_known: true after first HELLO received (enables filtering to server-supported only).
// server_has_*: per-feature server support flags.
// *_setting: per-feature user override ("auto"/"on"/"off").
// Returns count of features written to the out arrays.
resolve_requested_features :: proc(
	mode: util.Transport_Mode,
	platform: Platform_Kind,
	server_known: bool,
	server_has_batching: bool,
	server_has_snapshot_hash: bool,
	server_has_prev_seq: bool,
	batching_setting: string,
	snapshot_hash_setting: string,
	prev_seq_setting: string,
	out: ^[MAX_REQUESTED_FEATURES][24]u8,
	out_lens: ^[MAX_REQUESTED_FEATURES]u8,
	server_has_compress: bool = false,
	compress_setting: string = "auto",
	client_supports_decompress: bool = false,
) -> int {
	// Legacy mode doesn't support feature negotiation.
	if mode == .Legacy_JSON do return 0

	count := 0
	if feature_should_request(batching_setting, server_known, server_has_batching) {
		f := "batching"
		for i in 0 ..< len(f) { out[count][i] = f[i] }
		out_lens[count] = u8(len(f))
		count += 1
	}
	if feature_should_request(snapshot_hash_setting, server_known, server_has_snapshot_hash) {
		f := "snapshot_hash"
		for i in 0 ..< len(f) { out[count][i] = f[i] }
		out_lens[count] = u8(len(f))
		count += 1
	}
	if feature_should_request(prev_seq_setting, server_known, server_has_prev_seq) {
		f := "prev_seq"
		for i in 0 ..< len(f) { out[count][i] = f[i] }
		out_lens[count] = u8(len(f))
		count += 1
	}
	if client_supports_decompress &&
		feature_should_request(compress_setting, server_known, server_has_compress) {
		f := "compress"
		for i in 0 ..< len(f) { out[count][i] = f[i] }
		out_lens[count] = u8(len(f))
		count += 1
	}
	return count
}

// Build hello with requested_features array.
// Manual byte-by-byte JSON builder (no fmt.tprintf with braces).
build_hello_msg_v2 :: proc(
	buf: []u8,
	rid: u32,
	features: ^[MAX_REQUESTED_FEATURES][24]u8,
	feature_lens: ^[MAX_REQUESTED_FEATURES]u8,
	feature_count: int,
) -> (string, bool) {
	n := 0
	// Helper: append string literal.
	append_lit :: proc(buf: []u8, n: ^int, s: string) -> bool {
		for c in s {
			if n^ >= len(buf) - 1 do return false
			buf[n^] = u8(c)
			n^ += 1
		}
		return true
	}

	if !append_lit(buf, &n, `{"op":"hello","type":"hello","request_id":"h`) do return "", false
	rid_buf: [16]u8
	rid_str := fmt.bprintf(rid_buf[:], "%d", rid)
	if !append_lit(buf, &n, rid_str) do return "", false
	if !append_lit(buf, &n, `"`) do return "", false

	if feature_count > 0 {
		if !append_lit(buf, &n, `,"requested_features":[`) do return "", false
		for i in 0 ..< feature_count {
			if i > 0 {
				if !append_lit(buf, &n, `,`) do return "", false
			}
			if !append_lit(buf, &n, `"`) do return "", false
			flen := int(feature_lens[i])
			for j in 0 ..< flen {
				if n >= len(buf) - 1 do return "", false
				buf[n] = features[i][j]
				n += 1
			}
			if !append_lit(buf, &n, `"`) do return "", false
		}
		if !append_lit(buf, &n, `]`) do return "", false
	}
	if !append_lit(buf, &n, `}`) do return "", false
	return string(buf[:n]), true
}

build_ping_msg :: proc(buf: []u8, ts_client: i64, rid: u32) -> (string, bool) {
	n := 0
	prefix :: `{"op":"ping","type":"ping","ts_client":`
	for c in prefix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	ts_buf: [24]u8
	ts_str := fmt.bprintf(ts_buf[:], "%d", ts_client)
	for c in ts_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	mid :: `,"request_id":"p`
	for c in mid { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	rid_buf: [16]u8
	rid_str := fmt.bprintf(rid_buf[:], "%d", rid)
	for c in rid_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	return string(buf[:n]), true
}

build_resync_msg :: proc(buf: []u8, stream_id: string, last_seq: i64, rid: u32) -> (string, bool) {
	if !subject_is_json_safe(stream_id) do return "", false
	n := 0
	prefix :: `{"op":"resync","stream_id":"`
	for c in prefix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	for c in stream_id { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	mid :: `","last_seq":`
	for c in mid { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	seq_buf: [24]u8
	seq_str := fmt.bprintf(seq_buf[:], "%d", last_seq)
	for c in seq_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	mid2 :: `,"request_id":"rs`
	for c in mid2 { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	rid_buf: [16]u8
	rid_str := fmt.bprintf(rid_buf[:], "%d", rid)
	for c in rid_str { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { if n >= len(buf) - 1 do return "", false; buf[n] = u8(c); n += 1 }
	return string(buf[:n]), true
}

// --- Parse rate tracking ---

Rate_State :: struct {
	parsed_msgs_total:    u64,
	parsed_bytes_total:   u64,
	rate_window_msgs:     u64,
	rate_window_bytes:    u64,
	rate_window_start_ms: i64,
	msg_rate:             f64,
	bytes_rate:           f64,
}

update_parse_rates :: proc(rs: ^Rate_State, now_ms: i64, bytes: int) {
	if rs == nil do return
	safe_bytes := bytes
	if safe_bytes < 0 do safe_bytes = 0
	rs.parsed_msgs_total += 1
	rs.parsed_bytes_total += u64(safe_bytes)
	rs.rate_window_msgs += 1
	rs.rate_window_bytes += u64(safe_bytes)
	if rs.rate_window_start_ms <= 0 {
		rs.rate_window_start_ms = now_ms
		return
	}
	elapsed_ms := now_ms - rs.rate_window_start_ms
	if elapsed_ms < 1 do return
	if elapsed_ms >= 1000 {
		secs := f64(elapsed_ms) / 1000.0
		rs.msg_rate = f64(rs.rate_window_msgs) / secs
		rs.bytes_rate = f64(rs.rate_window_bytes) / secs
		rs.rate_window_msgs = 0
		rs.rate_window_bytes = 0
		rs.rate_window_start_ms = now_ms
	}
}

// --- Parse result classification ---

parse_result_has_data :: proc(kind: services.Parse_Result_Kind) -> bool {
	switch kind {
	case .Trade, .Orderbook, .Stats, .Heatmap, .VPVR, .Candle, .Evidence, .Signal, .Tape, .Range_Candle,
	     .Open_Interest, .Delta_Volume, .CVD, .Bar_Stats,
	     .Session_Volume_Profile, .TPO_Profile:
		return true
	case .None, .Ack, .Hello, .Hello_Ack, .Heartbeat, .Health, .Error, .Pong, .Metrics:
		return false
	}
	return false
}

parse_result_requires_ts_server :: proc(kind: services.Parse_Result_Kind) -> bool {
	switch kind {
	case .Trade, .Orderbook, .Stats, .Heatmap, .VPVR, .Candle, .Evidence, .Signal, .Tape,
	     .Open_Interest, .Delta_Volume, .CVD, .Bar_Stats,
	     .Session_Volume_Profile, .TPO_Profile:
		return true
	case .None, .Range_Candle, .Ack, .Hello, .Hello_Ack, .Heartbeat, .Health, .Error, .Pong, .Metrics:
		return false
	}
	return false
}

missing_ts_server_gap :: proc(
	has_ts_server: bool,
	kind: services.Parse_Result_Kind,
	mode: util.Transport_Mode,
) -> bool {
	if mode != .Terminal_V1 do return false
	if !parse_result_requires_ts_server(kind) do return false
	return !has_ts_server
}

detect_no_metrics_gap :: proc(last_metrics_ts_ms, now_ms, stale_ms: i64) -> (bool, i64) {
	if last_metrics_ts_ms <= 0 || stale_ms <= 0 do return false, last_metrics_ts_ms
	if now_ms-last_metrics_ts_ms <= stale_ms do return false, last_metrics_ts_ms
	return true, now_ms
}

detect_pong_timeout_gap :: proc(last_ping_sent_ms, last_pong_ts_ms, now_ms, timeout_ms: i64) -> (bool, i64) {
	if last_ping_sent_ms <= 0 || timeout_ms <= 0 do return false, last_pong_ts_ms
	if last_pong_ts_ms >= last_ping_sent_ms do return false, last_pong_ts_ms
	if now_ms-last_ping_sent_ms <= timeout_ms do return false, last_pong_ts_ms
	return true, now_ms
}

detect_resync_ack_timeout :: proc(
	resync_pending_subject_id: u64,
	resync_sent_ms, now_ms, timeout_ms: i64,
) -> bool {
	if resync_pending_subject_id == 0 do return false
	if resync_sent_ms <= 0 || timeout_ms <= 0 do return false
	return now_ms-resync_sent_ms > timeout_ms
}

seq_gap_transition :: proc(prev_seq, next_seq: i64, streak, recurring_threshold: int) -> (bool, int, bool) {
	if prev_seq <= 0 || next_seq <= 0 do return false, 0, false
	if next_seq == prev_seq+1 do return false, 0, false
	if next_seq > prev_seq+1 || next_seq < prev_seq {
		next_streak := streak + 1
		if recurring_threshold > 0 && next_streak >= recurring_threshold {
			return true, 0, true
		}
		return true, next_streak, false
	}
	return false, 0, false
}

// --- Snapshot integrity validation ---

// Validate prev_seq chaining: if prev_seq > 0, it must match the last delivered seq.
// prev_seq=0 is valid (first message after subscribe/resync).
// Returns: (mismatch: bool).
validate_prev_seq :: proc(prev_seq: i64, last_delivered_seq: i64) -> bool {
	if prev_seq <= 0 do return false
	if last_delivered_seq <= 0 do return false
	return prev_seq != last_delivered_seq
}

// Check if a snapshot_hash is well-formed: exactly 16 hex characters.
// Returns true if valid format.
validate_snapshot_hash_format :: proc(hash: [16]u8, hash_len: u8) -> bool {
	if hash_len != 16 do return false
	for i in 0 ..< 16 {
		c := hash[i]
		is_hex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !is_hex do return false
	}
	return true
}

// Validate snapshot_seq monotonicity: must be > last_snapshot_seq (when both > 0).
// Returns true if violation detected.
validate_snapshot_seq_monotonic :: proc(snapshot_seq: i64, last_snapshot_seq: i64) -> bool {
	if snapshot_seq <= 0 || last_snapshot_seq <= 0 do return false
	return snapshot_seq <= last_snapshot_seq
}

// Validate per-frame snapshot integrity consistency:
// - watermark_seq cannot be greater than snapshot_seq
// - snapshot_seq cannot be greater than envelope seq when both are present
// Returns true when a consistency violation is detected.
validate_snapshot_integrity_consistency :: proc(seq: i64, snapshot_seq: i64, watermark_seq: i64) -> bool {
	if snapshot_seq > 0 && watermark_seq > snapshot_seq do return true
	if seq > 0 && snapshot_seq > 0 && snapshot_seq > seq do return true
	return false
}

// --- Hello rejection → desync reason ---

desync_reason_from_hello_reject :: proc(reject: services.Hello_Reject_Reason) -> ports.MD_Desync_Reason {
	switch reject {
	case .Unsupported_Proto_Version:
		return .Protocol_Version
	case .Missing_Proto_Version, .Missing_Server_Time, .Missing_Capabilities:
		return .Protocol_Invalid
	case .None:
	}
	return .Protocol_Invalid
}

// --- Time helpers ---

now_ms :: proc() -> i64 {
	return time.now()._nsec / 1_000_000
}

// --- Subject builder for timeframe-qualified channels ---

subject_for_channel :: proc(venue, symbol, tf_filter: string, channel: ports.MD_Channel) -> string {
	tf := ""
	#partial switch channel {
	case .Heatmaps, .VPVR, .Candles, .Signals, .Tape,
	     .Analytics_CVD, .Analytics_Delta_Volume, .Analytics_OI, .Analytics_Bar_Stats:
		tf = tf_filter
	case: // static channels — no TF
	}
	return util.build_subject_with_timeframe(venue, symbol, channel, tf)
}

// --- Sub_Tracker: generic subscription entry lookup ---
// Works with any struct that has subject: string, subject_id: u64,
// venue: string, symbol: string, channel: ports.MD_Channel.

find_sub_by_subject :: proc(subs: []$T, subject: string) -> int {
	for i in 0 ..< len(subs) {
		if subs[i].subject == subject do return i
	}
	return -1
}

find_sub_by_key :: proc(subs: []$T, venue: string, symbol: string, channel: ports.MD_Channel) -> int {
	for i in 0 ..< len(subs) {
		sub := subs[i]
		if sub.channel == channel && sub.venue == venue && sub.symbol == symbol do return i
	}
	return -1
}

find_sub_by_subject_id :: proc(subs: []$T, subject_id: u64) -> int {
	for i in 0 ..< len(subs) {
		if subs[i].subject_id == subject_id do return i
	}
	return -1
}

// --- Backoff with jitter ---
// Returns a value in [75%, 100%] of base_ms using a simple LCG PRNG.
// Avoids thundering herd when multiple clients reconnect simultaneously.

backoff_with_jitter :: proc(base_ms: int, seed: ^u32) -> int {
	if base_ms <= 0 do return 0
	seed^ = seed^ * 1664525 + 1013904223
	jitter_frac := f32(seed^ & 0xFF) / 1024.0  // 0..~0.25
	return base_ms - int(f32(base_ms) * jitter_frac)
}

// --- Backpressure assist decision ---
// Pure function: computes next BP assist state from current state + metrics level.
// No side effects — caller applies the result and logs transitions.

BP_Assist_Input :: struct {
	prev_enabled:          bool,
	prev_degrade_heatmap:  bool,
	prev_degrade_vpvr:     bool,
	prev_getrange_divisor: int,
	user_enabled:          bool,
	cooldown_frames:       int,
	level:                 int,
}

BP_Assist_Decision :: struct {
	enabled:          bool,
	degrade_heatmap:  bool,
	degrade_vpvr:     bool,
	getrange_divisor: int,
	cooldown_frames:  int,
	changed:          bool,
	auto_activated:   bool,  // true when auto_assist flips enabled from false→true
}

compute_bp_assist_decision :: proc(input: BP_Assist_Input) -> BP_Assist_Decision {
	prev_divisor := input.prev_getrange_divisor
	if prev_divisor <= 0 do prev_divisor = 1
	next_enabled := input.prev_enabled
	next_degrade_heatmap := input.prev_degrade_heatmap
	next_degrade_vpvr := input.prev_degrade_vpvr
	next_divisor := prev_divisor
	next_cooldown := input.cooldown_frames
	auto_assist := input.user_enabled && input.level >= 2

	if auto_assist {
		next_enabled = true
		next_degrade_heatmap = true
		next_degrade_vpvr = input.level >= 3
		next_divisor = input.level >= 3 ? 4 : 2
		next_cooldown = 120
	} else if input.level >= 2 {
		// Keep current policy until user explicitly applies recommendation.
	} else if next_cooldown > 0 {
		next_cooldown -= 1
	} else {
		next_enabled = false
		next_degrade_heatmap = false
		next_degrade_vpvr = false
		next_divisor = 1
	}

	changed := next_enabled != input.prev_enabled ||
		next_degrade_heatmap != input.prev_degrade_heatmap ||
		next_degrade_vpvr != input.prev_degrade_vpvr ||
		next_divisor != prev_divisor

	auto_activated := auto_assist && !input.prev_enabled

	return BP_Assist_Decision{
		enabled          = next_enabled,
		degrade_heatmap  = next_degrade_heatmap,
		degrade_vpvr     = next_degrade_vpvr,
		getrange_divisor = next_divisor,
		cooldown_frames  = next_cooldown,
		changed          = changed,
		auto_activated   = auto_activated,
	}
}

free_sub_entry :: proc(entry: ^$T) {
	if entry == nil do return
	if len(entry.venue) > 0 do delete(entry.venue)
	if len(entry.symbol) > 0 do delete(entry.symbol)
	if len(entry.subject) > 0 do delete(entry.subject)
	entry^ = {}
}

// --- Server capabilities struct ---
// Consolidates flat capability fields from Parsed_Hello into a single struct.
// Both native and web embed this as `caps: Server_Capabilities` in their state.

Server_Capabilities :: struct {
	max_subscriptions:       int,
	max_frame_bytes:         int,
	metrics_cadence_ms:      int,
	keepalive_interval_ms:   int,
	rate_limit_enabled:      bool,
	supported_features:      [services.MAX_FEATURE_SLOTS]services.Parsed_Feature_Slot,
	supported_feature_count: int,
	proto_ver:               int,
	server_instance_id:      [32]u8,
	server_instance_id_len:  u8,
	received:                bool,
}

// Copy all capability fields from a Parsed_Hello into Server_Capabilities.
// Platform HELLO handlers call this, then handle transport state transitions locally.
apply_hello_to_capabilities :: proc(caps: ^Server_Capabilities, h: services.Parsed_Hello) {
	if caps == nil do return
	caps.proto_ver = h.proto_ver
	caps.max_subscriptions = h.max_subscriptions
	caps.max_frame_bytes = h.max_frame_bytes
	caps.metrics_cadence_ms = h.metrics_cadence_ms
	caps.keepalive_interval_ms = h.keepalive_interval_ms
	caps.rate_limit_enabled = h.rate_limit_enabled
	caps.supported_feature_count = h.supported_feature_count
	for i in 0 ..< h.supported_feature_count {
		caps.supported_features[i] = h.supported_features[i]
	}
	// Store server_instance_id (truncate to fixed buffer).
	sid := h.server_instance_id
	sid_len := min(len(sid), len(caps.server_instance_id))
	for i in 0 ..< sid_len { caps.server_instance_id[i] = sid[i] }
	caps.server_instance_id_len = u8(sid_len)
	caps.received = true
}

// --- Centralized limit/capability helpers ---
// Pure functions used by both native and web platforms.

// Effective subscription limit: min(server, local), server=0 means "use local".
effective_sub_limit :: proc(server_max: int, local_cap: int) -> int {
	if local_cap <= 0 do return 0
	if server_max <= 0 do return local_cap
	return min(server_max, local_cap)
}

// Whether a new subscription can be added given current count and limits.
can_add_subscription :: proc(active_count: int, server_max: int, local_cap: int) -> bool {
	limit := effective_sub_limit(server_max, local_cap)
	if limit <= 0 do return false
	return active_count < limit
}

// Derive metrics staleness timeout from server cadence. 3× cadence, minimum 3s.
metrics_stale_timeout_ms :: proc(server_cadence_ms: int, fallback_ms: i64) -> i64 {
	fallback := fallback_ms
	if fallback <= 0 do fallback = 20_000
	if server_cadence_ms <= 0 do return fallback
	cadence_ms := i64(server_cadence_ms)
	return max(cadence_ms * 3, i64(3000))
}

// Check if a feature name exists in a fixed-size slot array.
feature_slot_has_name :: proc(
	slots: [services.MAX_FEATURE_SLOTS]services.Parsed_Feature_Slot,
	count: int,
	name: string,
) -> bool {
	for i in 0 ..< count {
		slot := slots[i]
		if int(slot.len) != len(name) do continue
		match := true
		for j in 0 ..< int(slot.len) {
			if slot.name[j] != name[j] {
				match = false
				break
			}
		}
		if match do return true
	}
	return false
}

// Pure check: does the frame exceed the server's max_frame_bytes limit?
// Returns true if the frame should be dropped. server_max=0 means no limit.
frame_exceeds_limit :: proc(server_max_frame_bytes: int, frame_len: int) -> bool {
	if server_max_frame_bytes <= 0 do return false
	if frame_len < 0 do return false
	return frame_len > server_max_frame_bytes
}
