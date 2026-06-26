package main

// Minimal RFC 6455 WebSocket client — ported from MarketMonkey ws.odin.
// Supports: dial (ws:// + hostname/IP resolution for IPv4+IPv6),
// wss:// parsed (native TLS termination handled upstream), read_message,
// write_text, close.

import "core:crypto/hash"
import "core:encoding/base64"
import "core:encoding/endian"
import "core:fmt"
import "core:math/rand"
import "core:net"
import "core:strings"
import "core:strconv"
import "core:time"

WS_GUID :: "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
WS_HANDSHAKE_TIMEOUT :: 1500 * time.Millisecond
WS_DNS_RESOLVE_TIMEOUT :: 1200 * time.Millisecond
WS_MAX_PAYLOAD_BYTES :: 256 * 1024

WS_Error :: enum {
	None,
	Failed_Header_Read,
	Failed_Payload_Read_16,
	Failed_Payload_Read_64,
	Failed_Payload_Read,
	Read_Conn_Closed,
	Failed_Mask_Read,
	Payload_Too_Large,
	Invalid_Url,
	Invalid_Host,
	Invalid_Port,
	DNS_Error,
	TLS_Not_Supported,
	Dial_Error,
	Handshake_Error,
	Invalid_Frame_Sequence,
	Invalid_Control_Frame,
	Send_Error,
}

WS_Connection :: struct {
	socket:       net.TCP_Socket,
	prefetch:     [dynamic]u8,
	prefetch_pos: int,
}

WS_Scheme :: enum u8 { WS, WSS }

WS_Parsed_Url :: struct {
	scheme:    WS_Scheme,
	host:      string,      // slice into original url (zero alloc)
	port_str:  string,      // slice or "80"/"443" literal
	port:      int,
	path:      string,      // request path (slice or "/")
	query:     string,      // request query, with leading "?" (slice or "")
	host_port: string,      // "host:port" — slice for Host header
}

WS_Host_Kind :: enum u8 {
	Invalid,
	Hostname,
	IPv4,
	IPv6,
}

WS_IP_Order :: enum u8 {
	IPv4_Then_IPv6,
	IPv6_Then_IPv4,
}

WS_Resolve_Proc :: #type proc(host: string) -> (net.Endpoint, net.Network_Error)

WS_Host_Resolver :: struct {
	resolve_ip4: WS_Resolve_Proc,
	resolve_ip6: WS_Resolve_Proc,
}

@(private = "file")
ws_resolve_ip4_default :: proc(host: string) -> (net.Endpoint, net.Network_Error) {
	return net.resolve_ip4(host)
}

@(private = "file")
ws_resolve_ip6_default :: proc(host: string) -> (net.Endpoint, net.Network_Error) {
	return net.resolve_ip6(host)
}

WS_DEFAULT_HOST_RESOLVER :: WS_Host_Resolver{
	resolve_ip4 = ws_resolve_ip4_default,
	resolve_ip6 = ws_resolve_ip6_default,
}

@(private = "file")
ws_has_forbidden_url_chars :: proc(s: string) -> bool {
	for c in s {
		if c <= ' ' || c == 0x7F {
			return true
		}
	}
	return false
}

ws_classify_host :: proc(host: string) -> WS_Host_Kind {
	if len(host) == 0 || ws_has_forbidden_url_chars(host) {
		return .Invalid
	}
	target, parse_err := net.parse_hostname_or_endpoint(host)
	if parse_err != .None {
		return .Invalid
	}
	switch t in target {
	case net.Endpoint:
		switch _ in t.address {
		case net.IP4_Address:
			return .IPv4
		case net.IP6_Address:
			return .IPv6
		}
	case net.Host:
		return .Hostname
	}
	return .Invalid
}

@(private = "file")
ws_parse_url_ex :: proc(url: string) -> (WS_Parsed_Url, WS_Error) {
	if len(url) == 0 || ws_has_forbidden_url_chars(url) {
		return {}, .Invalid_Url
	}

	scheme: WS_Scheme
	scheme_len: int
	if strings.has_prefix(url, "wss://") {
		scheme = .WSS
		scheme_len = 6
	} else if strings.has_prefix(url, "ws://") {
		scheme = .WS
		scheme_len = 5
	} else {
		return {}, .Invalid_Url
	}

	rest := url[scheme_len:]
	if len(rest) == 0 {
		return {}, .Invalid_Url
	}
	if strings.index(rest, "#") >= 0 {
		return {}, .Invalid_Url
	}

	authority_end := len(rest)
	slash_idx := strings.index(rest, "/")
	query_idx := strings.index(rest, "?")
	if slash_idx >= 0 && query_idx >= 0 {
		authority_end = min(slash_idx, query_idx)
	} else if slash_idx >= 0 {
		authority_end = slash_idx
	} else if query_idx >= 0 {
		authority_end = query_idx
	}
	authority := rest[:authority_end]
	if len(authority) == 0 {
		return {}, .Invalid_Host
	}

	host := authority
	default_port := scheme == .WSS ? "443" : "80"
	port_str := default_port
	has_explicit_port := false
	if authority[0] == '[' {
		close_idx := strings.index(authority, "]")
		if close_idx <= 1 {
			return {}, .Invalid_Host
		}
		host = authority[1:close_idx]
		tail := authority[close_idx + 1:]
		if len(tail) > 0 {
			if tail[0] != ':' {
				return {}, .Invalid_Host
			}
			port_str = tail[1:]
			has_explicit_port = true
		}
		if ws_classify_host(host) != .IPv6 {
			return {}, .Invalid_Host
		}
	} else {
		if strings.count(authority, ":") > 1 {
			// Raw IPv6 literals must be bracketed in ws:// URLs.
			return {}, .Invalid_Host
		}
		if colon_idx := strings.last_index(authority, ":"); colon_idx != -1 {
			host = authority[:colon_idx]
			port_str = authority[colon_idx + 1:]
			has_explicit_port = true
		}
	}

	if len(host) == 0 {
		return {}, .Invalid_Host
	}
	host_kind := ws_classify_host(host)
	if host_kind == .Invalid {
		return {}, .Invalid_Host
	}
	if host_kind == .IPv6 && authority[0] != '[' {
		return {}, .Invalid_Host
	}
	if has_explicit_port && len(port_str) == 0 {
		return {}, .Invalid_Port
	}

	port, port_ok := strconv.parse_int(port_str)
	if !port_ok || port <= 0 || port > 65535 {
		return {}, .Invalid_Port
	}

	path := "/"
	query := ""
	remainder := rest[authority_end:]
	if len(remainder) > 0 {
		switch remainder[0] {
		case '/':
			if q_idx := strings.index(remainder, "?"); q_idx >= 0 {
				path = remainder[:q_idx]
				query = remainder[q_idx:]
			} else {
				path = remainder
			}
		case '?':
			query = remainder
		case:
			return {}, .Invalid_Url
		}
	}
	if len(path) == 0 || path[0] != '/' {
		return {}, .Invalid_Url
	}

	return WS_Parsed_Url{
		scheme    = scheme,
		host      = host,
		port_str  = port_str,
		port      = port,
		path      = path,
		query     = query,
		host_port = authority,
	}, .None
}

// Parse a ws:// or wss:// URL into components.
// Returned strings are slices into the original URL except default literals.
ws_parse_url :: proc(url: string) -> (WS_Parsed_Url, bool) {
	parsed, parse_err := ws_parse_url_ex(url)
	return parsed, parse_err == .None
}

@(private = "file")
ws_try_resolve_family :: proc(
	resolve_fn: WS_Resolve_Proc,
	host: string,
	port: int,
) -> (net.Endpoint, bool) {
	if resolve_fn == nil {
		return {}, false
	}
	ep, err := resolve_fn(host)
	if err != nil {
		return {}, false
	}
	ep.port = port
	return ep, true
}

// Resolve host into candidate endpoints in deterministic order.
// DNS attempts are bounded by timeout budget.
ws_resolve_host_candidates_with :: proc(
	host: string,
	port: int,
	timeout: time.Duration = WS_DNS_RESOLVE_TIMEOUT,
	order: WS_IP_Order = .IPv4_Then_IPv6,
	resolver: WS_Host_Resolver = WS_DEFAULT_HOST_RESOLVER,
) -> (candidates: [2]net.Endpoint, count: int, err: WS_Error) {
	if port <= 0 || port > 65535 {
		return candidates, 0, .Invalid_Port
	}

	host_kind := ws_classify_host(host)
	switch host_kind {
	case .IPv4:
		ip4, _ := net.parse_ip4_address(host)
		candidates[0] = net.Endpoint{address = ip4, port = port}
		return candidates, 1, .None
	case .IPv6:
		ip6, _ := net.parse_ip6_address(host)
		candidates[0] = net.Endpoint{address = ip6, port = port}
		return candidates, 1, .None
	case .Hostname:
	case .Invalid:
		return candidates, 0, .Invalid_Host
	}

	if strings.equal_fold(host, "localhost") {
		if order == .IPv6_Then_IPv4 {
			candidates[0] = net.Endpoint{address = net.IP6_Loopback, port = port}
			candidates[1] = net.Endpoint{address = net.IP4_Loopback, port = port}
		} else {
			candidates[0] = net.Endpoint{address = net.IP4_Loopback, port = port}
			candidates[1] = net.Endpoint{address = net.IP6_Loopback, port = port}
		}
		return candidates, 2, .None
	}

	active_resolver := resolver
	if active_resolver.resolve_ip4 == nil || active_resolver.resolve_ip6 == nil {
		active_resolver = WS_DEFAULT_HOST_RESOLVER
	}

	start := time.tick_now()
	have4 := false
	have6 := false
	ep4, ep6: net.Endpoint

	try4_then_6 := order == .IPv4_Then_IPv6
	for step in 0 ..< 2 {
		if timeout > 0 && time.tick_since(start) >= timeout {
			break
		}
		if (step == 0 && try4_then_6) || (step == 1 && !try4_then_6) {
			if ep, ok := ws_try_resolve_family(active_resolver.resolve_ip4, host, port); ok {
				ep4 = ep
				have4 = true
			}
		} else {
			if ep, ok := ws_try_resolve_family(active_resolver.resolve_ip6, host, port); ok {
				ep6 = ep
				have6 = true
			}
		}
	}

	if order == .IPv6_Then_IPv4 {
		if have6 {
			candidates[count] = ep6
			count += 1
		}
		if have4 {
			candidates[count] = ep4
			count += 1
		}
	} else {
		if have4 {
			candidates[count] = ep4
			count += 1
		}
		if have6 {
			candidates[count] = ep6
			count += 1
		}
	}

	if count == 0 {
		return candidates, 0, .DNS_Error
	}
	return candidates, count, .None
}

ws_resolve_host_candidates :: proc(
	host: string,
	port: int,
) -> (candidates: [2]net.Endpoint, count: int, err: WS_Error) {
	return ws_resolve_host_candidates_with(host, port)
}

// Legacy helper for single-endpoint callers.
ws_resolve_host :: proc(host: string, port: int) -> (net.Endpoint, WS_Error) {
	candidates, count, err := ws_resolve_host_candidates(host, port)
	if err != nil {
		return {}, err
	}
	if count <= 0 {
		return {}, .DNS_Error
	}
	return candidates[0], .None
}

@(private = "file")
tcp_recv_exact :: proc(socket: net.TCP_Socket, buf: []u8) -> (int, net.TCP_Recv_Error) {
	total := 0
	for total < len(buf) {
		n, err := net.recv_tcp(socket, buf[total:])
		if err != nil {
			return total, err
		}
		if n <= 0 {
			return total, nil
		}
		total += n
	}
	return total, nil
}

@(private = "file")
tcp_send_all :: proc(socket: net.TCP_Socket, buf: []u8) -> (int, net.TCP_Send_Error) {
	total := 0
	for total < len(buf) {
		n, err := net.send_tcp(socket, buf[total:])
		if err != nil {
			return total, err
		}
		if n <= 0 {
			return total, nil
		}
		total += n
	}
	return total, nil
}

// --- Public API ---

ws_dial :: proc(url: string, extra_headers: string = "") -> (WS_Connection, WS_Error) {
	parsed, parse_err := ws_parse_url_ex(url)
	if parse_err != nil {
		return {}, parse_err
	}
	// wss:// is parsed correctly but TLS is not yet implemented.
	// Workaround: use a local TLS termination proxy (e.g. socat) and connect via ws://.
	if parsed.scheme == .WSS {
		return {}, .TLS_Not_Supported
	}

	candidates, candidate_count, resolve_err := ws_resolve_host_candidates(parsed.host, parsed.port)
	if resolve_err != nil {
		return {}, resolve_err
	}
	if candidate_count <= 0 {
		return {}, .DNS_Error
	}

	conn: net.TCP_Socket
	connected := false
	for i in 0 ..< candidate_count {
		candidate := candidates[i]
		dialed_conn, dial_err := net.dial_tcp(candidate)
		if dial_err == nil {
			conn = dialed_conn
			connected = true
			break
		}
	}
	if !connected {
		return {socket = conn}, .Dial_Error
	}
	// Bound the handshake phase so reconnect cannot stay in .Connecting indefinitely.
	_ = net.set_option(conn, .Receive_Timeout, WS_HANDSHAKE_TIMEOUT)
	_ = net.set_option(conn, .Send_Timeout, WS_HANDSHAKE_TIMEOUT)

	// WebSocket handshake.
	key := ws_generate_key()
	handshake_buf: [2048]u8
	handshake := fmt.bprintf(
		handshake_buf[:],
		"GET %s%s HTTP/1.1\r\n" +
		"Host: %s\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: %s\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"%s" +
		"\r\n",
		parsed.path, parsed.query, parsed.host_port, key, extra_headers,
	)
	if len(handshake) <= 0 {
		return {socket = conn}, .Handshake_Error
	}
	handshake_bytes := handshake_buf[:len(handshake)]
	if _, err := tcp_send_all(conn, handshake_bytes); err != nil {
		return {socket = conn}, .Handshake_Error
	}

	response_builder := strings.builder_make()
	defer strings.builder_destroy(&response_builder)
	buf: [512]u8
	for {
		n, err := net.recv_tcp(conn, buf[:])
		if err != nil || n <= 0 {
			return {socket = conn}, .Handshake_Error
		}
		strings.write_bytes(&response_builder, buf[:n])
		if strings.contains(strings.to_string(response_builder), "\r\n\r\n") {
			break
		}
	}
	// Restore blocking behavior for the long-lived WS read loop after handshake completes.
	_ = net.set_option(conn, .Receive_Timeout, time.Duration(0))
	_ = net.set_option(conn, .Send_Timeout, time.Duration(0))

	response_str := strings.to_string(response_builder)
	header_end := strings.index(response_str, "\r\n\r\n")
	if header_end == -1 {
		return {socket = conn}, .Handshake_Error
	}
	expected_accept := ws_compute_accept_key(key)
	if !strings.has_prefix(response_str, "HTTP/1.1 101") ||
	   !strings.contains(response_str, fmt.tprintf("Sec-WebSocket-Accept: %s", expected_accept)) {
		return {socket = conn}, .Handshake_Error
	}
	result := WS_Connection{socket = conn}
	body_start := header_end + len("\r\n\r\n")
	if body_start < len(response_str) {
		append(&result.prefetch, ..transmute([]u8)response_str[body_start:])
		result.prefetch_pos = 0
	}
	return result, nil
}

ws_close :: proc(conn: ^WS_Connection) {
	if len(conn.prefetch) > 0 {
		delete(conn.prefetch)
	}
	conn.prefetch = nil
	conn.prefetch_pos = 0
	if conn.socket != {} {
		net.close(conn.socket)
		conn.socket = {}
	}
}

ws_is_connected :: proc(conn: WS_Connection) -> bool {
	return conn.socket != {}
}

ws_read_message_ex :: proc(
	conn: ^WS_Connection,
	allocator := context.temp_allocator,
) -> (opcode: u8, payload: []u8, rsv1: bool, err: WS_Error) {
	in_message := false
	message_opcode: u8
	message_rsv1 := false
	payload_accumulator: [dynamic]u8

	for {
		header: [2]u8
		n, recv_err := tcp_recv_exact_ws(conn, header[:])
		if recv_err != nil || n != 2 {
			return 0, nil, false, .Failed_Header_Read
		}

		fin := (header[0] & 0x80) != 0
		frame_rsv1 := (header[0] & 0x40) != 0
		frame_opcode := header[0] & 0x0F
		is_masked := (header[1] & 0x80) != 0
		payload_len := u64(header[1] & 0x7F)

		if payload_len == 126 {
			ext_len: [2]u8
			n, recv_err = tcp_recv_exact_ws(conn, ext_len[:])
			if recv_err != nil || n != 2 {
				return 0, nil, false, .Failed_Payload_Read_16
			}
			len16, _ := endian.get_u16(ext_len[:], .Big)
			payload_len = u64(len16)
		} else if payload_len == 127 {
			ext_len: [8]u8
			n, recv_err = tcp_recv_exact_ws(conn, ext_len[:])
			if recv_err != nil || n != 8 {
				return 0, nil, false, .Failed_Payload_Read_64
			}
			payload_len, _ = endian.get_u64(ext_len[:], .Big)
		}

		if payload_len > WS_MAX_PAYLOAD_BYTES {
			return 0, nil, false, .Payload_Too_Large
		}

		frame_payload := ws_read_frame_payload(conn, payload_len, is_masked, allocator) or_return

		// Control frames.
		if frame_opcode >= 0x8 {
			if !fin do return 0, nil, false, .Invalid_Control_Frame
			if frame_opcode == 0x8 {
				ws_write_frame(conn.socket, 0x8, frame_payload) or_return
				return 0, nil, false, .Read_Conn_Closed
			} else if frame_opcode == 0x9 {
				ws_write_frame(conn.socket, 0xA, frame_payload) or_return
			}
			// 0xA = pong, ignore.
		} else {
			if !in_message {
				if frame_opcode == 0x0 do return 0, nil, false, .Invalid_Frame_Sequence
				in_message = true
				message_opcode = frame_opcode
				message_rsv1 = frame_rsv1
				payload_accumulator = make([dynamic]u8, allocator)
			} else {
				if frame_opcode != 0x0 do return 0, nil, false, .Invalid_Frame_Sequence
			}
			append(&payload_accumulator, ..frame_payload)
			if len(payload_accumulator) > WS_MAX_PAYLOAD_BYTES {
				return 0, nil, false, .Payload_Too_Large
			}
			if fin {
				return message_opcode, payload_accumulator[:], message_rsv1, nil
			}
		}
	}
}

ws_read_message :: proc(
	conn: ^WS_Connection,
	allocator := context.temp_allocator,
) -> (opcode: u8, payload: []u8, err: WS_Error) {
	opcode, payload, _, err = ws_read_message_ex(conn, allocator)
	return
}

ws_write_text :: proc(conn: WS_Connection, msg: string) -> WS_Error {
	return ws_write_frame(conn.socket, 0x1, transmute([]u8)msg)
}

// Send a WS ping frame (opcode 0x9) with empty payload.
ws_write_ping :: proc(conn: WS_Connection) -> WS_Error {
	empty: [0]u8
	return ws_write_frame(conn.socket, 0x9, empty[:])
}

// --- Internal helpers ---

@(private = "file")
tcp_recv_exact_ws :: proc(conn: ^WS_Connection, buf: []u8) -> (int, net.TCP_Recv_Error) {
	total := 0
	if conn != nil && conn.prefetch_pos < len(conn.prefetch) {
		avail := len(conn.prefetch) - conn.prefetch_pos
		ncopy := min(len(buf), avail)
		copy(buf[:ncopy], conn.prefetch[conn.prefetch_pos:conn.prefetch_pos + ncopy])
		conn.prefetch_pos += ncopy
		total += ncopy
		if conn.prefetch_pos >= len(conn.prefetch) {
			delete(conn.prefetch)
			conn.prefetch = nil
			conn.prefetch_pos = 0
		}
	}
	for total < len(buf) {
		n, err := net.recv_tcp(conn.socket, buf[total:])
		if err != nil {
			return total, err
		}
		if n <= 0 {
			return total, nil
		}
		total += n
	}
	return total, nil
}

@(private = "file")
ws_generate_key :: proc() -> string {
	key := make([]u8, 16, context.temp_allocator)
	for i in 0 ..< 16 {
		key[i] = u8(rand.int31() % 256)
	}
	return base64.encode(key)
}

@(private = "file")
ws_compute_accept_key :: proc(key: string) -> string {
	concatenated := strings.concatenate([]string{key, WS_GUID}, context.temp_allocator)
	ctx: hash.Context
	digest := make([]byte, hash.DIGEST_SIZES[hash.Algorithm.Insecure_SHA1], context.temp_allocator)
	hash.init(&ctx, hash.Algorithm.Insecure_SHA1)
	hash.update(&ctx, transmute([]byte)concatenated)
	hash.final(&ctx, digest)
	return base64.encode(digest)
}

@(private = "file")
ws_read_frame_payload :: proc(
	conn: ^WS_Connection,
	payload_len: u64,
	is_masked: bool,
	allocator := context.temp_allocator,
) -> ([]u8, WS_Error) {
	mask: [4]u8
	if is_masked {
		n, err := tcp_recv_exact_ws(conn, mask[:])
		if err != nil || n != 4 {
			return nil, .Failed_Mask_Read
		}
	}
	payload := make([]u8, payload_len, allocator)
	n, err := tcp_recv_exact_ws(conn, payload)
	if err != nil {
		return nil, .Failed_Payload_Read
	}
	if n != int(payload_len) {
		return nil, .Read_Conn_Closed
	}
	if is_masked {
		for i in 0 ..< int(payload_len) {
			payload[i] ~= mask[i % 4]
		}
	}
	return payload, nil
}

@(private = "file")
ws_write_frame :: proc(socket: net.TCP_Socket, opcode: u8, payload: []u8) -> WS_Error {
	if opcode >= 0x8 && len(payload) > 125 {
		return .Invalid_Frame_Sequence
	}
	mask: [4]u8
	for i in 0 ..< 4 {
		mask[i] = u8(rand.int31() % 256)
	}
	header_buf: [14]u8
	header: []u8
	payload_len := len(payload)
	if payload_len <= 125 {
		header = header_buf[:6]
		header[0] = opcode | 0x80
		header[1] = u8(payload_len) | 0x80
		copy(header[2:], mask[:])
	} else if payload_len <= 65535 {
		header = header_buf[:8]
		header[0] = opcode | 0x80
		header[1] = 126 | 0x80
		endian.put_u16(header[2:4], .Big, u16(payload_len))
		copy(header[4:], mask[:])
	} else {
		header = header_buf[:14]
		header[0] = opcode | 0x80
		header[1] = 127 | 0x80
		endian.put_u64(header[2:10], .Big, u64(payload_len))
		copy(header[10:], mask[:])
	}
	if n, err := tcp_send_all(socket, header); err != nil || n != len(header) {
		return .Send_Error
	}
	masked_payload := make([]u8, payload_len, context.temp_allocator)
	for i := 0; i < payload_len; i += 1 {
		masked_payload[i] = payload[i] ~ mask[i % 4]
	}
	if n, err := tcp_send_all(socket, masked_payload); err != nil || n != len(masked_payload) {
		return .Send_Error
	}
	return nil
}
