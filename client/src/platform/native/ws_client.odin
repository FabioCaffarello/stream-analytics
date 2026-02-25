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

WS_GUID :: "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

WS_Error :: enum {
	None,
	Failed_Header_Read,
	Failed_Payload_Read_16,
	Failed_Payload_Read_64,
	Failed_Payload_Read,
	Read_Conn_Closed,
	Failed_Mask_Read,
	Invalid_Url,
	Invalid_Port,
	Dial_Error,
	Handshake_Error,
	Invalid_Frame_Sequence,
	Invalid_Control_Frame,
	Send_Error,
}

WS_Connection :: struct {
	socket: net.TCP_Socket,
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
	net.send_tcp(conn, transmute([]u8)handshake)

	response_builder := strings.builder_make()
	defer strings.builder_destroy(&response_builder)
	buf: [512]u8
	for {
		n, err := net.recv_tcp(conn, buf[:])
		if err != nil {
			return {socket = conn}, .Handshake_Error
		}
		strings.write_bytes(&response_builder, buf[:n])
		if strings.contains(string(buf[:n]), "\r\n\r\n") {
			break
		}
	}

	response_str := strings.to_string(response_builder)
	expected_accept := ws_compute_accept_key(key)
	if !strings.has_prefix(response_str, "HTTP/1.1 101") ||
	   !strings.contains(response_str, fmt.tprintf("Sec-WebSocket-Accept: %s", expected_accept)) {
		return {socket = conn}, .Handshake_Error
	}

	return {socket = conn}, nil
}

ws_close :: proc(conn: ^WS_Connection) {
	if conn.socket != {} {
		net.close(conn.socket)
		conn.socket = {}
	}
}

ws_is_connected :: proc(conn: WS_Connection) -> bool {
	return conn.socket != {}
}

ws_read_message :: proc(
	conn: WS_Connection,
	allocator := context.temp_allocator,
) -> (opcode: u8, payload: []u8, err: WS_Error) {
	in_message := false
	message_opcode: u8
	payload_accumulator: [dynamic]u8

	for {
		header: [2]u8
		n, recv_err := net.recv_tcp(conn.socket, header[:])
		if recv_err != nil || n != 2 {
			return 0, nil, .Failed_Header_Read
		}

		fin := (header[0] & 0x80) != 0
		frame_opcode := header[0] & 0x0F
		is_masked := (header[1] & 0x80) != 0
		payload_len := u64(header[1] & 0x7F)

		if payload_len == 126 {
			ext_len: [2]u8
			n, recv_err = net.recv_tcp(conn.socket, ext_len[:])
			if recv_err != nil || n != 2 {
				return 0, nil, .Failed_Payload_Read_16
			}
			len16, _ := endian.get_u16(ext_len[:], .Big)
			payload_len = u64(len16)
		} else if payload_len == 127 {
			ext_len: [8]u8
			n, recv_err = net.recv_tcp(conn.socket, ext_len[:])
			if recv_err != nil || n != 8 {
				return 0, nil, .Failed_Payload_Read_64
			}
			payload_len, _ = endian.get_u64(ext_len[:], .Big)
		}

		frame_payload := ws_read_frame_payload(conn.socket, payload_len, is_masked, allocator) or_return

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
			if fin {
				return message_opcode, payload_accumulator[:], nil
			}
		}
	}
}

ws_write_text :: proc(conn: WS_Connection, msg: string) -> WS_Error {
	return ws_write_frame(conn.socket, 0x1, transmute([]u8)msg)
}

// --- Internal helpers ---

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
	socket: net.TCP_Socket,
	payload_len: u64,
	is_masked: bool,
	allocator := context.temp_allocator,
) -> ([]u8, WS_Error) {
	mask: [4]u8
	if is_masked {
		n, err := net.recv_tcp(socket, mask[:])
		if err != nil || n != 4 {
			return nil, .Failed_Mask_Read
		}
	}
	payload := make([]u8, payload_len, allocator)
	total_received := 0
	for total_received < int(payload_len) {
		n, err := net.recv_tcp(socket, payload[total_received:])
		if err != nil {
			return nil, .Failed_Payload_Read
		}
		if n == 0 {
			return nil, .Read_Conn_Closed
		}
		total_received += n
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
	if _, err := net.send_tcp(socket, header); err != nil {
		return .Send_Error
	}
	masked_payload := make([]u8, payload_len, context.temp_allocator)
	for i := 0; i < payload_len; i += 1 {
		masked_payload[i] = payload[i] ~ mask[i % 4]
	}
	if _, err := net.send_tcp(socket, masked_payload); err != nil {
		return .Send_Error
	}
	return nil
}
