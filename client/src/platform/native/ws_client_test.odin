package main

import "core:net"
import "core:testing"
import "core:time"
import "mr:md_common"

@(test)
test_parse_ws_url_basic :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("ws://127.0.0.1:8080/ws")
	testing.expect(t, ok, "should parse ws://127.0.0.1:8080/ws")
	testing.expect_value(t, parsed.scheme, WS_Scheme.WS)
	testing.expect_value(t, parsed.host, "127.0.0.1")
	testing.expect_value(t, parsed.port_str, "8080")
	testing.expect_value(t, parsed.port, 8080)
	testing.expect_value(t, parsed.path, "/ws")
	testing.expect_value(t, parsed.query, "")
	testing.expect_value(t, parsed.host_port, "127.0.0.1:8080")
}

@(test)
test_parse_ws_url_hostname :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("ws://api.example.com:9090/v1/stream")
	testing.expect(t, ok, "should parse hostname URL")
	testing.expect_value(t, parsed.scheme, WS_Scheme.WS)
	testing.expect_value(t, parsed.host, "api.example.com")
	testing.expect_value(t, parsed.port_str, "9090")
	testing.expect_value(t, parsed.port, 9090)
	testing.expect_value(t, parsed.path, "/v1/stream")
	testing.expect_value(t, parsed.query, "")
	testing.expect_value(t, parsed.host_port, "api.example.com:9090")
}

@(test)
test_parse_ws_url_default_port_ws :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("ws://localhost/ws")
	testing.expect(t, ok, "should parse without explicit port")
	testing.expect_value(t, parsed.scheme, WS_Scheme.WS)
	testing.expect_value(t, parsed.host, "localhost")
	testing.expect_value(t, parsed.port_str, "80")
	testing.expect_value(t, parsed.port, 80)
	testing.expect_value(t, parsed.path, "/ws")
	testing.expect_value(t, parsed.query, "")
}

@(test)
test_parse_wss_url_default_port :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("wss://secure.example.com/ws")
	testing.expect(t, ok, "should parse wss:// with default port")
	testing.expect_value(t, parsed.scheme, WS_Scheme.WSS)
	testing.expect_value(t, parsed.host, "secure.example.com")
	testing.expect_value(t, parsed.port_str, "443")
	testing.expect_value(t, parsed.port, 443)
	testing.expect_value(t, parsed.path, "/ws")
	testing.expect_value(t, parsed.query, "")
}

@(test)
test_parse_wss_url_explicit_port :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("wss://host:8443/api")
	testing.expect(t, ok, "should parse wss:// with explicit port")
	testing.expect_value(t, parsed.scheme, WS_Scheme.WSS)
	testing.expect_value(t, parsed.port_str, "8443")
	testing.expect_value(t, parsed.port, 8443)
	testing.expect_value(t, parsed.path, "/api")
	testing.expect_value(t, parsed.query, "")
}

@(test)
test_parse_url_no_path :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("ws://host.com:3000")
	testing.expect(t, ok, "should parse URL without path")
	testing.expect_value(t, parsed.host, "host.com")
	testing.expect_value(t, parsed.port, 3000)
	testing.expect_value(t, parsed.path, "/")
	testing.expect_value(t, parsed.query, "")
}

@(test)
test_parse_url_custom_path_and_query :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("ws://md.example.com:8080/ws/marketdata?token=abc123&channel=trades")
	testing.expect(t, ok, "custom path + query should parse")
	testing.expect_value(t, parsed.host, "md.example.com")
	testing.expect_value(t, parsed.path, "/ws/marketdata")
	testing.expect_value(t, parsed.query, "?token=abc123&channel=trades")
	testing.expect_value(t, parsed.host_port, "md.example.com:8080")
}

@(test)
test_parse_url_query_without_path :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("ws://md.example.com:8080?token=abc123")
	testing.expect(t, ok, "query-only URL should parse")
	testing.expect_value(t, parsed.path, "/")
	testing.expect_value(t, parsed.query, "?token=abc123")
}

@(test)
test_parse_url_ipv6_bracketed :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("ws://[2001:db8::1]:8080/ws")
	testing.expect(t, ok, "bracketed IPv6 URL should parse")
	testing.expect_value(t, parsed.host, "2001:db8::1")
	testing.expect_value(t, parsed.host_port, "[2001:db8::1]:8080")
	testing.expect_value(t, parsed.port, 8080)
	testing.expect_value(t, parsed.path, "/ws")
}

@(test)
test_parse_url_ipv6_bracketed_default_port :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("ws://[::1]/ws")
	testing.expect(t, ok, "bracketed IPv6 without explicit port should parse")
	testing.expect_value(t, parsed.host, "::1")
	testing.expect_value(t, parsed.host_port, "[::1]")
	testing.expect_value(t, parsed.port_str, "80")
	testing.expect_value(t, parsed.port, 80)
	testing.expect_value(t, parsed.path, "/ws")
}

@(test)
test_parse_url_invalid_scheme :: proc(t: ^testing.T) {
	_, ok1 := ws_parse_url("http://example.com/ws")
	testing.expect(t, !ok1, "http:// should fail")

	_, ok2 := ws_parse_url("ftp://example.com/ws")
	testing.expect(t, !ok2, "ftp:// should fail")

	_, ok3 := ws_parse_url("")
	testing.expect(t, !ok3, "empty string should fail")
}

@(test)
test_parse_url_invalid_port :: proc(t: ^testing.T) {
	_, ok1 := ws_parse_url("ws://host:99999/ws")
	testing.expect(t, !ok1, "port 99999 should fail")

	_, ok2 := ws_parse_url("ws://host:abc/ws")
	testing.expect(t, !ok2, "port 'abc' should fail")

	_, ok3 := ws_parse_url("ws://host:-1/ws")
	testing.expect(t, !ok3, "port -1 should fail")
}

@(test)
test_parse_url_empty_host :: proc(t: ^testing.T) {
	_, ok := ws_parse_url("ws://:8080/ws")
	testing.expect(t, !ok, "empty host should fail")
}

@(test)
test_parse_url_invalid_host :: proc(t: ^testing.T) {
	_, ok1 := ws_parse_url("ws://bad host:8080/ws")
	testing.expect(t, !ok1, "host with spaces should fail")

	_, ok2 := ws_parse_url("ws://host^name:8080/ws")
	testing.expect(t, !ok2, "host with invalid chars should fail")

	_, ok3 := ws_parse_url("ws://2001:db8::1:8080/ws")
	testing.expect(t, !ok3, "unbracketed IPv6 should fail")
}

@(test)
test_parse_url_fragment_rejected :: proc(t: ^testing.T) {
	_, ok := ws_parse_url("ws://example.com:8080/ws#frag")
	testing.expect(t, !ok, "URL fragment should fail")
}

@(test)
test_parse_url_localhost :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("ws://localhost:8080/ws")
	testing.expect(t, ok, "should parse localhost")
	testing.expect_value(t, parsed.host, "localhost")
	testing.expect_value(t, parsed.port, 8080)
	testing.expect_value(t, parsed.path, "/ws")
}

@(test)
test_parse_url_ipv4_literal :: proc(t: ^testing.T) {
	parsed, ok := ws_parse_url("ws://192.168.1.100:5000/feed")
	testing.expect(t, ok, "should parse IPv4 literal")
	testing.expect_value(t, parsed.host, "192.168.1.100")
	testing.expect_value(t, parsed.port, 5000)
	testing.expect_value(t, parsed.path, "/feed")
	testing.expect_value(t, parsed.host_port, "192.168.1.100:5000")
}

@(test)
test_parse_url_zero_alloc :: proc(t: ^testing.T) {
	url := "ws://api.example.com:9090/v1/stream"
	parsed, ok := ws_parse_url(url)
	testing.expect(t, ok, "should parse for zero-alloc check")

	// Verify that parsed.host is a slice into the original url string.
	url_data := uintptr(raw_data(url))
	url_end := url_data + uintptr(len(url))

	host_data := uintptr(raw_data(parsed.host))
	testing.expect(t,
		host_data >= url_data && host_data < url_end,
		"parsed.host should point within original url (zero alloc)")

	path_data := uintptr(raw_data(parsed.path))
	testing.expect(t,
		path_data >= url_data && path_data < url_end,
		"parsed.path should point within original url (zero alloc)")
}

@(test)
test_classify_host_variants :: proc(t: ^testing.T) {
	testing.expect_value(t, ws_classify_host("localhost"), WS_Host_Kind.Hostname)
	testing.expect_value(t, ws_classify_host("api.example.com"), WS_Host_Kind.Hostname)
	testing.expect_value(t, ws_classify_host("127.0.0.1"), WS_Host_Kind.IPv4)
	testing.expect_value(t, ws_classify_host("2001:db8::1"), WS_Host_Kind.IPv6)
	testing.expect_value(t, ws_classify_host("bad host"), WS_Host_Kind.Invalid)
}

@(private = "file")
mock_resolve_ip4_ok :: proc(host: string) -> (net.Endpoint, net.Network_Error) {
	_ = host
	return net.Endpoint{
		address = net.IP4_Address{10, 0, 0, 10},
		port    = 0,
	}, nil
}

@(private = "file")
mock_resolve_ip6_ok :: proc(host: string) -> (net.Endpoint, net.Network_Error) {
	_ = host
	ip6, ok := net.parse_ip6_address("2001:db8::10")
	if !ok {
		return {}, .Unable_To_Resolve
	}
	return net.Endpoint{
		address = ip6,
		port    = 0,
	}, nil
}

@(test)
test_resolve_host_candidates_ipv4_then_ipv6 :: proc(t: ^testing.T) {
	resolver := WS_Host_Resolver{
		resolve_ip4 = mock_resolve_ip4_ok,
		resolve_ip6 = mock_resolve_ip6_ok,
	}
	candidates, count, err := ws_resolve_host_candidates_with(
		"api.example.com",
		8080,
		100*time.Millisecond,
		.IPv4_Then_IPv6,
		resolver,
	)
	testing.expect_value(t, err, WS_Error.None)
	testing.expect_value(t, count, 2)
	_, is_v4 := candidates[0].address.(net.IP4_Address)
	_, is_v6 := candidates[1].address.(net.IP6_Address)
	testing.expect(t, is_v4, "first candidate should be IPv4")
	testing.expect(t, is_v6, "second candidate should be IPv6")
}

@(test)
test_resolve_host_candidates_ipv6_then_ipv4 :: proc(t: ^testing.T) {
	resolver := WS_Host_Resolver{
		resolve_ip4 = mock_resolve_ip4_ok,
		resolve_ip6 = mock_resolve_ip6_ok,
	}
	candidates, count, err := ws_resolve_host_candidates_with(
		"api.example.com",
		8080,
		100*time.Millisecond,
		.IPv6_Then_IPv4,
		resolver,
	)
	testing.expect_value(t, err, WS_Error.None)
	testing.expect_value(t, count, 2)
	_, is_v6 := candidates[0].address.(net.IP6_Address)
	_, is_v4 := candidates[1].address.(net.IP4_Address)
	testing.expect(t, is_v6, "first candidate should be IPv6")
	testing.expect(t, is_v4, "second candidate should be IPv4")
}

@(private = "file")
mock_resolve_fail :: proc(host: string) -> (net.Endpoint, net.Network_Error) {
	_ = host
	return {}, .Unable_To_Resolve
}

@(test)
test_resolve_host_candidates_dns_error :: proc(t: ^testing.T) {
	resolver := WS_Host_Resolver{
		resolve_ip4 = mock_resolve_fail,
		resolve_ip6 = mock_resolve_fail,
	}
	_, count, err := ws_resolve_host_candidates_with(
		"api.example.com",
		8080,
		100*time.Millisecond,
		.IPv4_Then_IPv6,
		resolver,
	)
	testing.expect_value(t, count, 0)
	testing.expect_value(t, err, WS_Error.DNS_Error)
}

@(private = "file")
mock_resolve_ip4_slow_fail :: proc(host: string) -> (net.Endpoint, net.Network_Error) {
	_ = host
	time.sleep(20 * time.Millisecond)
	return {}, .Unable_To_Resolve
}

@(test)
test_resolve_host_candidates_timeout_budget :: proc(t: ^testing.T) {
	resolver := WS_Host_Resolver{
		resolve_ip4 = mock_resolve_ip4_slow_fail,
		resolve_ip6 = mock_resolve_ip6_ok,
	}
	_, count, err := ws_resolve_host_candidates_with(
		"api.example.com",
		8080,
		5*time.Millisecond,
		.IPv4_Then_IPv6,
		resolver,
	)
	testing.expect_value(t, count, 0)
	testing.expect_value(t, err, WS_Error.DNS_Error)
}

@(test)
test_client_respects_server_max_subs :: proc(t: ^testing.T) {
	limit := md_common.effective_sub_limit(32, MAX_SUBS)
	testing.expect_value(t, limit, 32)
	testing.expect_value(t, md_common.can_add_subscription(31, 32, MAX_SUBS), true)
	testing.expect_value(t, md_common.can_add_subscription(32, 32, MAX_SUBS), false)
}

@(test)
test_client_falls_back_when_server_no_limits :: proc(t: ^testing.T) {
	limit := md_common.effective_sub_limit(0, MAX_SUBS)
	testing.expect_value(t, limit, MAX_SUBS)
	testing.expect_value(t, md_common.can_add_subscription(MAX_SUBS - 1, 0, MAX_SUBS), true)
	testing.expect_value(t, md_common.can_add_subscription(MAX_SUBS, 0, MAX_SUBS), false)
}

@(test)
test_client_updates_metrics_timeout_from_server :: proc(t: ^testing.T) {
	timeout := md_common.metrics_stale_timeout_ms(1000, METRICS_STALE_MS)
	testing.expect_value(t, timeout, i64(3000))
	fallback := md_common.metrics_stale_timeout_ms(0, METRICS_STALE_MS)
	testing.expect_value(t, fallback, i64(METRICS_STALE_MS))
}
