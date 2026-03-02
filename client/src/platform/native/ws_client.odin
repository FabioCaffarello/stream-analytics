package main

// Minimal RFC 6455 WebSocket client — ported from MarketMonkey ws.odin.
// Supports: dial (ws://), read_message, write_text, close.

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
	Invalid_Port,
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
	if !strings.has_prefix(url, "ws://") {
		return {}, .Invalid_Url
	}
	url_no_scheme := url[5:]
	host_port, path := url_no_scheme, "/"
	if idx := strings.index(url_no_scheme, "/"); idx != -1 {
		host_port = url_no_scheme[:idx]
		path = url_no_scheme[idx:]
	}
	host, port_str := host_port, "80"
	if colon_idx := strings.index(host_port, ":"); colon_idx != -1 {
		host = host_port[:colon_idx]
		port_str = host_port[colon_idx + 1:]
	}

	ip, ok := net.parse_ip4_address(host)
	if !ok {
		if host == "localhost" {
			ip = net.IP4_Address{127, 0, 0, 1}
		} else {
			return {}, .Invalid_Url
		}
	}
	port, port_ok := strconv.parse_int(port_str)
	if !port_ok || port < 0 || port > 65535 {
		return {}, .Invalid_Port
	}

	endpoint := net.Endpoint{address = ip, port = port}
	conn, dial_err := net.dial_tcp(endpoint)
	if dial_err != nil {
		return {socket = conn}, .Dial_Error
	}
	// Bound the handshake phase so reconnect cannot stay in .Connecting indefinitely.
	_ = net.set_option(conn, .Receive_Timeout, WS_HANDSHAKE_TIMEOUT)
	_ = net.set_option(conn, .Send_Timeout, WS_HANDSHAKE_TIMEOUT)

	// WebSocket handshake.
	key := ws_generate_key()
	handshake := fmt.tprintf(
		"GET %s HTTP/1.1\r\n" +
		"Host: %s\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: %s\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"%s" +
		"\r\n",
		path, host_port, key, extra_headers,
	)
	if _, err := tcp_send_all(conn, transmute([]u8)handshake); err != nil {
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

ws_read_message :: proc(
	conn: ^WS_Connection,
	allocator := context.temp_allocator,
) -> (opcode: u8, payload: []u8, err: WS_Error) {
	in_message := false
	message_opcode: u8
	payload_accumulator: [dynamic]u8

	for {
		header: [2]u8
		n, recv_err := tcp_recv_exact_ws(conn, header[:])
		if recv_err != nil || n != 2 {
			return 0, nil, .Failed_Header_Read
		}

		fin := (header[0] & 0x80) != 0
		frame_opcode := header[0] & 0x0F
		is_masked := (header[1] & 0x80) != 0
		payload_len := u64(header[1] & 0x7F)

		if payload_len == 126 {
			ext_len: [2]u8
			n, recv_err = tcp_recv_exact_ws(conn, ext_len[:])
			if recv_err != nil || n != 2 {
				return 0, nil, .Failed_Payload_Read_16
			}
			len16, _ := endian.get_u16(ext_len[:], .Big)
			payload_len = u64(len16)
		} else if payload_len == 127 {
			ext_len: [8]u8
			n, recv_err = tcp_recv_exact_ws(conn, ext_len[:])
			if recv_err != nil || n != 8 {
				return 0, nil, .Failed_Payload_Read_64
			}
			payload_len, _ = endian.get_u64(ext_len[:], .Big)
		}

		if payload_len > WS_MAX_PAYLOAD_BYTES {
			return 0, nil, .Payload_Too_Large
		}

		frame_payload := ws_read_frame_payload(conn, payload_len, is_masked, allocator) or_return

		// Control frames.
		if frame_opcode >= 0x8 {
			if !fin do return 0, nil, .Invalid_Control_Frame
			if frame_opcode == 0x8 {
				ws_write_frame(conn.socket, 0x8, frame_payload) or_return
				return 0, nil, .Read_Conn_Closed
			} else if frame_opcode == 0x9 {
				ws_write_frame(conn.socket, 0xA, frame_payload) or_return
			}
			// 0xA = pong, ignore.
		} else {
			if !in_message {
				if frame_opcode == 0x0 do return 0, nil, .Invalid_Frame_Sequence
				in_message = true
				message_opcode = frame_opcode
				payload_accumulator = make([dynamic]u8, allocator)
			} else {
				if frame_opcode != 0x0 do return 0, nil, .Invalid_Frame_Sequence
			}
			append(&payload_accumulator, ..frame_payload)
			if len(payload_accumulator) > WS_MAX_PAYLOAD_BYTES {
				return 0, nil, .Payload_Too_Large
			}
			if fin {
				return message_opcode, payload_accumulator[:], nil
			}
		}
	}
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
