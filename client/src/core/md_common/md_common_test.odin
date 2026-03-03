package md_common

import "core:strings"
import "core:testing"
import "mr:ports"
import "mr:services"
import "mr:util"

@(test)
test_backoff_with_jitter_range :: proc(t: ^testing.T) {
	seed := u32(12345)
	base := 1000
	for _ in 0 ..< 100 {
		result := backoff_with_jitter(base, &seed)
		testing.expect(t, result >= 750, "jittered value below 75% of base")
		testing.expect(t, result <= 1000, "jittered value above 100% of base")
	}
}

@(test)
test_backoff_with_jitter_deterministic :: proc(t: ^testing.T) {
	seed1 := u32(42)
	seed2 := u32(42)
	r1 := backoff_with_jitter(2000, &seed1)
	r2 := backoff_with_jitter(2000, &seed2)
	testing.expect_value(t, r1, r2)
}

@(test)
test_backoff_with_jitter_zero_base :: proc(t: ^testing.T) {
	seed := u32(99)
	result := backoff_with_jitter(0, &seed)
	testing.expect_value(t, result, 0)
}

@(test)
test_build_subscribe_msg_overflow :: proc(t: ^testing.T) {
	buf: [10]u8 // too small
	_, ok := build_subscribe_msg(buf[:], "some/subject", 1)
	testing.expect_value(t, ok, false)
}

@(test)
test_build_subscribe_msg_valid :: proc(t: ^testing.T) {
	buf: [256]u8
	msg, ok := build_subscribe_msg(buf[:], "marketdata.trade/binance/BTCUSDT", 42)
	testing.expect_value(t, ok, true)
	testing.expect(t, len(msg) > 0, "expected non-empty message")
}

@(test)
test_subject_is_json_safe_rejects_quotes :: proc(t: ^testing.T) {
	testing.expect_value(t, subject_is_json_safe(`has"quote`), false)
	testing.expect_value(t, subject_is_json_safe(`has\slash`), false)
	testing.expect_value(t, subject_is_json_safe(""), false)
	testing.expect_value(t, subject_is_json_safe("marketdata.trade/binance/BTCUSDT"), true)
}

@(test)
test_update_parse_rates_window_rollover :: proc(t: ^testing.T) {
	rs: Rate_State
	// First call initializes the window.
	update_parse_rates(&rs, 1000, 100)
	testing.expect_value(t, rs.parsed_msgs_total, u64(1))
	testing.expect_value(t, rs.parsed_bytes_total, u64(100))

	// Calls within 1s accumulate.
	update_parse_rates(&rs, 1500, 200)
	testing.expect_value(t, rs.parsed_msgs_total, u64(2))

	// After 1s, rates are computed and window is reset.
	update_parse_rates(&rs, 2100, 300)
	testing.expect(t, rs.msg_rate > 0, "expected non-zero msg_rate after 1s window")
	testing.expect(t, rs.bytes_rate > 0, "expected non-zero bytes_rate after 1s window")
}

@(test)
test_build_unsubscribe_msg_valid :: proc(t: ^testing.T) {
	buf: [256]u8
	msg, ok := build_unsubscribe_msg(buf[:], "marketdata.trade/binance/BTCUSDT", 7)
	testing.expect_value(t, ok, true)
	testing.expect(t, len(msg) > 0, "expected non-empty unsubscribe message")
}

@(test)
test_build_subscribe_msg_unsafe_subject :: proc(t: ^testing.T) {
	buf: [256]u8
	// Quotes in subject should be rejected.
	_, ok := build_subscribe_msg(buf[:], `inject"quote`, 1)
	testing.expect_value(t, ok, false)
}

@(test)
test_backoff_with_jitter_negative_base :: proc(t: ^testing.T) {
	seed := u32(1)
	result := backoff_with_jitter(-100, &seed)
	testing.expect_value(t, result, 0)
}

// --- Terminal_V1 builder tests ---

@(test)
test_build_hello_msg :: proc(t: ^testing.T) {
	buf: [256]u8
	msg, ok := build_hello_msg(buf[:], 1)
	testing.expect_value(t, ok, true)
	testing.expect(t, len(msg) > 0, "expected non-empty hello message")
	// Must contain op and type fields.
	testing.expect(t, strings.contains(msg, `"op":"hello"`), "expected op:hello")
	testing.expect(t, strings.contains(msg, `"type":"hello"`), "expected type:hello")
	testing.expect(t, strings.contains(msg, `"request_id":"h1"`), "expected request_id h1")
}

@(test)
test_build_ping_msg :: proc(t: ^testing.T) {
	buf: [256]u8
	msg, ok := build_ping_msg(buf[:], 1700000000000, 5)
	testing.expect_value(t, ok, true)
	testing.expect(t, len(msg) > 0, "expected non-empty ping message")
	testing.expect(t, strings.contains(msg, `"op":"ping"`), "expected op:ping")
	testing.expect(t, strings.contains(msg, `"ts_client":1700000000000`), "expected ts_client")
}

@(test)
test_build_resync_msg :: proc(t: ^testing.T) {
	buf: [512]u8
	msg, ok := build_resync_msg(buf[:], "marketdata.trade/binance/BTCUSDT/raw", 12345, 3)
	testing.expect_value(t, ok, true)
	testing.expect(t, len(msg) > 0, "expected non-empty resync message")
	testing.expect(t, strings.contains(msg, `"op":"resync"`), "expected op:resync")
	testing.expect(t, strings.contains(msg, `"stream_id":"marketdata.trade/binance/BTCUSDT/raw"`), "expected stream_id")
	testing.expect(t, strings.contains(msg, `"last_seq":12345`), "expected last_seq")
}

@(test)
test_build_subscribe_v2_component_fields :: proc(t: ^testing.T) {
	buf: [768]u8
	msg, ok := build_subscribe_msg_v2(buf[:], "marketdata.trade/binance/BTCUSDT/raw", "binance", "BTCUSDT", "marketdata.trade", "raw", 10)
	testing.expect_value(t, ok, true)
	testing.expect(t, len(msg) > 0, "expected non-empty v2 subscribe message")
	testing.expect(t, strings.contains(msg, `"op":"subscribe"`), "expected op:subscribe")
	testing.expect(t, strings.contains(msg, `"venue":"binance"`), "expected venue field")
	testing.expect(t, strings.contains(msg, `"symbol":"BTCUSDT"`), "expected symbol field")
	testing.expect(t, strings.contains(msg, `"channel":"marketdata.trade"`), "expected channel field")
	testing.expect(t, strings.contains(msg, `"aggregation":"raw"`), "expected aggregation field")
}

@(test)
test_build_unsubscribe_v2_stream_id :: proc(t: ^testing.T) {
	buf: [512]u8
	msg, ok := build_unsubscribe_msg_v2(buf[:], "marketdata.trade/binance/BTCUSDT/raw", 7)
	testing.expect_value(t, ok, true)
	testing.expect(t, len(msg) > 0, "expected non-empty v2 unsubscribe message")
	testing.expect(t, strings.contains(msg, `"op":"unsubscribe"`), "expected op:unsubscribe")
	testing.expect(t, strings.contains(msg, `"stream_id":"marketdata.trade/binance/BTCUSDT/raw"`), "expected stream_id")
}

@(test)
test_parse_subject_components :: proc(t: ^testing.T) {
	venue, symbol, channel, aggregation := parse_subject_components("marketdata.trade/binance/BTCUSDT/raw")
	testing.expect_value(t, channel, "marketdata.trade")
	testing.expect_value(t, venue, "binance")
	testing.expect_value(t, symbol, "BTCUSDT")
	testing.expect_value(t, aggregation, "raw")
}

@(test)
test_parse_subject_components_no_aggregation :: proc(t: ^testing.T) {
	venue, symbol, channel, aggregation := parse_subject_components("marketdata.bookdelta/coinbase/ETHUSDT")
	testing.expect_value(t, channel, "marketdata.bookdelta")
	testing.expect_value(t, venue, "coinbase")
	testing.expect_value(t, symbol, "ETHUSDT")
	testing.expect_value(t, aggregation, "")
}

@(test)
test_ws_fault_action_matrix :: proc(t: ^testing.T) {
	testing.expect_value(t, ws_fault_action(.AuthDenied, true), ports.MD_WS_Error_Action.Stop)
	testing.expect_value(t, ws_fault_action(.HandshakeFailed, true), ports.MD_WS_Error_Action.Retry)
	testing.expect_value(t, ws_fault_action(.ServerClosed, true), ports.MD_WS_Error_Action.Retry)
	testing.expect_value(t, ws_fault_action(.ProtocolError, true), ports.MD_WS_Error_Action.Resync)
	testing.expect_value(t, ws_fault_action(.BackpressureDrop, true), ports.MD_WS_Error_Action.Resync)
	testing.expect_value(t, ws_fault_action(.Timeout, true), ports.MD_WS_Error_Action.Downgrade)
	testing.expect_value(t, ws_fault_action(.Timeout, false), ports.MD_WS_Error_Action.Stop)
}

@(test)
test_legacy_switch_from_text :: proc(t: ^testing.T) {
	testing.expect_value(t, legacy_switch_from_text("on"), true)
	testing.expect_value(t, legacy_switch_from_text("true"), true)
	testing.expect_value(t, legacy_switch_from_text("OFF"), false)
	testing.expect_value(t, legacy_switch_from_text("0"), false)
}

@(test)
test_detect_no_metrics_gap :: proc(t: ^testing.T) {
	triggered, next := detect_no_metrics_gap(1_000, 22_000, 20_000)
	testing.expect_value(t, triggered, true)
	testing.expect_value(t, next, i64(22_000))

	triggered2, next2 := detect_no_metrics_gap(5_000, 10_000, 20_000)
	testing.expect_value(t, triggered2, false)
	testing.expect_value(t, next2, i64(5_000))
}

@(test)
test_detect_pong_timeout_gap :: proc(t: ^testing.T) {
	triggered, next := detect_pong_timeout_gap(10_000, 9_000, 26_000, 15_000)
	testing.expect_value(t, triggered, true)
	testing.expect_value(t, next, i64(26_000))

	triggered2, _ := detect_pong_timeout_gap(10_000, 10_500, 26_000, 15_000)
	testing.expect_value(t, triggered2, false)
}

@(test)
test_detect_resync_ack_timeout :: proc(t: ^testing.T) {
	testing.expect_value(t, detect_resync_ack_timeout(0, 1_000, 10_000, 5_000), false)
	testing.expect_value(t, detect_resync_ack_timeout(0xAA, 1_000, 10_000, 5_000), true)
	testing.expect_value(t, detect_resync_ack_timeout(0xAA, 7_000, 10_000, 5_000), false)
}

@(test)
test_seq_gap_transition_recurring_threshold :: proc(t: ^testing.T) {
	gap1, streak1, recurring1 := seq_gap_transition(10, 13, 0, 3)
	testing.expect_value(t, gap1, true)
	testing.expect_value(t, streak1, 1)
	testing.expect_value(t, recurring1, false)

	gap2, streak2, recurring2 := seq_gap_transition(13, 16, streak1, 3)
	testing.expect_value(t, gap2, true)
	testing.expect_value(t, streak2, 2)
	testing.expect_value(t, recurring2, false)

	gap3, streak3, recurring3 := seq_gap_transition(16, 20, streak2, 3)
	testing.expect_value(t, gap3, true)
	testing.expect_value(t, streak3, 0)
	testing.expect_value(t, recurring3, true)
}

@(test)
test_build_hello_msg_v2_with_features :: proc(t: ^testing.T) {
	buf: [512]u8
	features: [MAX_REQUESTED_FEATURES][24]u8
	feature_lens: [MAX_REQUESTED_FEATURES]u8
	// First connect (server_known=false) with auto settings → request all.
	fc := resolve_requested_features(.Terminal_V1, .Native, false, false, false, false, "auto", "auto", "auto", &features, &feature_lens)
	msg, ok := build_hello_msg_v2(buf[:], 1, &features, &feature_lens, fc)
	testing.expect_value(t, ok, true)
	testing.expect(t, len(msg) > 0, "expected non-empty hello v2 message")
	testing.expect(t, strings.contains(msg, `"op":"hello"`), "expected op:hello")
	testing.expect(t, strings.contains(msg, `"requested_features":[`), "expected requested_features array")
	testing.expect(t, strings.contains(msg, `"batching"`), "expected batching feature")
	testing.expect(t, strings.contains(msg, `"snapshot_hash"`), "expected snapshot_hash feature")
	testing.expect(t, strings.contains(msg, `"prev_seq"`), "expected prev_seq feature")
}

@(test)
test_build_hello_msg_v2_no_features :: proc(t: ^testing.T) {
	buf: [512]u8
	features: [MAX_REQUESTED_FEATURES][24]u8
	feature_lens: [MAX_REQUESTED_FEATURES]u8
	msg, ok := build_hello_msg_v2(buf[:], 2, &features, &feature_lens, 0)
	testing.expect_value(t, ok, true)
	testing.expect(t, strings.contains(msg, `"op":"hello"`), "expected op:hello")
	testing.expect(t, !strings.contains(msg, `"requested_features"`), "no features when count=0")
}

@(test)
test_resolve_requested_features_first_connect :: proc(t: ^testing.T) {
	// First connect: server_known=false → auto requests all optimistically.
	features: [MAX_REQUESTED_FEATURES][24]u8
	feature_lens: [MAX_REQUESTED_FEATURES]u8
	fc := resolve_requested_features(.Terminal_V1, .Native, false, false, false, false, "auto", "auto", "auto", &features, &feature_lens)
	testing.expect_value(t, fc, 3)
	testing.expect_value(t, string(features[0][:feature_lens[0]]), "batching")
	testing.expect_value(t, string(features[1][:feature_lens[1]]), "snapshot_hash")
	testing.expect_value(t, string(features[2][:feature_lens[2]]), "prev_seq")
}

@(test)
test_resolve_requested_features_server_known :: proc(t: ^testing.T) {
	// Reconnect: server_known=true, only batching supported → request only batching.
	features: [MAX_REQUESTED_FEATURES][24]u8
	feature_lens: [MAX_REQUESTED_FEATURES]u8
	fc := resolve_requested_features(.Terminal_V1, .Native, true, true, false, false, "auto", "auto", "auto", &features, &feature_lens)
	testing.expect_value(t, fc, 1)
	testing.expect_value(t, string(features[0][:feature_lens[0]]), "batching")
}

@(test)
test_resolve_requested_features_selective :: proc(t: ^testing.T) {
	// Explicit off overrides even server support.
	features: [MAX_REQUESTED_FEATURES][24]u8
	feature_lens: [MAX_REQUESTED_FEATURES]u8
	fc := resolve_requested_features(.Terminal_V1, .Native, true, true, true, true, "off", "auto", "off", &features, &feature_lens)
	testing.expect_value(t, fc, 1)
	testing.expect_value(t, string(features[0][:feature_lens[0]]), "snapshot_hash")
}

@(test)
test_resolve_requested_features_legacy_mode :: proc(t: ^testing.T) {
	// Legacy mode → no features regardless of settings.
	features: [MAX_REQUESTED_FEATURES][24]u8
	feature_lens: [MAX_REQUESTED_FEATURES]u8
	fc := resolve_requested_features(.Legacy_JSON, .Native, false, false, false, false, "auto", "auto", "auto", &features, &feature_lens)
	testing.expect_value(t, fc, 0)
}

@(test)
test_resolve_requested_features_explicit_on :: proc(t: ^testing.T) {
	// Explicit "on" overrides server_known=true + server_has=false.
	features: [MAX_REQUESTED_FEATURES][24]u8
	feature_lens: [MAX_REQUESTED_FEATURES]u8
	fc := resolve_requested_features(.Terminal_V1, .Native, true, false, false, false, "on", "on", "on", &features, &feature_lens)
	testing.expect_value(t, fc, 3)
}

@(test)
test_feature_should_request_variants :: proc(t: ^testing.T) {
	// Explicit overrides.
	testing.expect_value(t, feature_should_request("on", false, false), true)
	testing.expect_value(t, feature_should_request("1", false, false), true)
	testing.expect_value(t, feature_should_request("true", false, false), true)
	testing.expect_value(t, feature_should_request("off", true, true), false)
	testing.expect_value(t, feature_should_request("0", true, true), false)
	testing.expect_value(t, feature_should_request("false", true, true), false)
	// Auto: server unknown → request.
	testing.expect_value(t, feature_should_request("auto", false, false), true)
	testing.expect_value(t, feature_should_request("", false, false), true)
	// Auto: server known + supported → request.
	testing.expect_value(t, feature_should_request("auto", true, true), true)
	// Auto: server known + not supported → skip.
	testing.expect_value(t, feature_should_request("auto", true, false), false)
}

@(test)
test_action_hint_to_ws_fault :: proc(t: ^testing.T) {
	action, meaningful := action_hint_to_ws_fault(util.MR_Action_Hint.Retry)
	testing.expect_value(t, action, ports.MD_WS_Error_Action.Retry)
	testing.expect_value(t, meaningful, true)

	action2, m2 := action_hint_to_ws_fault(util.MR_Action_Hint.Reconnect)
	testing.expect_value(t, action2, ports.MD_WS_Error_Action.Retry)
	testing.expect_value(t, m2, true)

	action3, m3 := action_hint_to_ws_fault(util.MR_Action_Hint.Resubscribe)
	testing.expect_value(t, action3, ports.MD_WS_Error_Action.Resync)
	testing.expect_value(t, m3, true)

	action4, m4 := action_hint_to_ws_fault(util.MR_Action_Hint.Resync)
	testing.expect_value(t, action4, ports.MD_WS_Error_Action.Resync)
	testing.expect_value(t, m4, true)

	action5, m5 := action_hint_to_ws_fault(util.MR_Action_Hint.None)
	testing.expect_value(t, action5, ports.MD_WS_Error_Action.None)
	testing.expect_value(t, m5, true)

	action6, m6 := action_hint_to_ws_fault(util.MR_Action_Hint.Unspecified)
	testing.expect_value(t, action6, ports.MD_WS_Error_Action.None)
	testing.expect_value(t, m6, false)
}

// --- Backpressure state tests ---

@(test)
test_backpressure_state_derivation :: proc(t: ^testing.T) {
	testing.expect_value(t, backpressure_state_from_level(0), Backpressure_State.Normal)
	testing.expect_value(t, backpressure_state_from_level(-1), Backpressure_State.Normal)
	testing.expect_value(t, backpressure_state_from_level(1), Backpressure_State.Elevated)
	testing.expect_value(t, backpressure_state_from_level(2), Backpressure_State.High)
	testing.expect_value(t, backpressure_state_from_level(3), Backpressure_State.Critical)
	testing.expect_value(t, backpressure_state_from_level(5), Backpressure_State.Critical)
}

// --- Snapshot integrity tests ---

@(test)
test_validate_prev_seq_zero_is_valid :: proc(t: ^testing.T) {
	// prev_seq=0 is first-after-subscribe, never a mismatch.
	testing.expect_value(t, validate_prev_seq(0, 10), false)
	testing.expect_value(t, validate_prev_seq(0, 0), false)
}

@(test)
test_validate_prev_seq_match :: proc(t: ^testing.T) {
	// prev_seq matches last delivered → no mismatch.
	testing.expect_value(t, validate_prev_seq(10, 10), false)
}

@(test)
test_validate_prev_seq_gap :: proc(t: ^testing.T) {
	// prev_seq differs from last delivered → mismatch.
	testing.expect_value(t, validate_prev_seq(8, 10), true)
	testing.expect_value(t, validate_prev_seq(12, 10), true)
}

@(test)
test_validate_snapshot_hash_format_valid :: proc(t: ^testing.T) {
	hash: [16]u8
	s := "a1b2c3d4e5f6a7b8"
	for i in 0 ..< 16 { hash[i] = s[i] }
	testing.expect_value(t, validate_snapshot_hash_format(hash, 16), true)
}

@(test)
test_validate_snapshot_hash_format_invalid :: proc(t: ^testing.T) {
	// Wrong length.
	hash: [16]u8
	testing.expect_value(t, validate_snapshot_hash_format(hash, 0), false)
	testing.expect_value(t, validate_snapshot_hash_format(hash, 8), false)
	// Non-hex character.
	s := "a1b2c3d4e5f6a7gx"
	for i in 0 ..< 16 { hash[i] = s[i] }
	testing.expect_value(t, validate_snapshot_hash_format(hash, 16), false)
}

@(test)
test_validate_snapshot_seq_monotonic :: proc(t: ^testing.T) {
	// snapshot_seq > last → no violation.
	testing.expect_value(t, validate_snapshot_seq_monotonic(11, 10), false)
	// snapshot_seq == last → violation.
	testing.expect_value(t, validate_snapshot_seq_monotonic(10, 10), true)
	// snapshot_seq < last → violation.
	testing.expect_value(t, validate_snapshot_seq_monotonic(9, 10), true)
	// First snapshot (last=0) → no violation.
	testing.expect_value(t, validate_snapshot_seq_monotonic(5, 0), false)
}

@(test)
test_missing_ts_server_gap_terminal_v1_only :: proc(t: ^testing.T) {
	testing.expect_value(t, missing_ts_server_gap(false, services.Parse_Result_Kind.Trade, util.Transport_Mode.Terminal_V1), true)
	testing.expect_value(t, missing_ts_server_gap(true, services.Parse_Result_Kind.Trade, util.Transport_Mode.Terminal_V1), false)
	testing.expect_value(t, missing_ts_server_gap(false, services.Parse_Result_Kind.Range_Candle, util.Transport_Mode.Terminal_V1), false)
	testing.expect_value(t, missing_ts_server_gap(false, services.Parse_Result_Kind.Trade, util.Transport_Mode.Legacy_JSON), false)
}
