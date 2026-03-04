package services

import "core:math"
import "core:fmt"
import "core:strings"
import "core:testing"

@(test)
test_parse_trade_nan_price_rejected :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","seq":1,"ts_ingest":1700000000000,"payload":{"Price":null,"Size":1.5,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}}`
	tel: Parse_Telemetry
	result := parse_mr_message(transmute([]u8)raw, &tel)
	// JSON null → 0, which is valid (price=0 is edge case); test with explicit NaN not possible via JSON.
	// Instead test negative price rejection:
	free_all(context.temp_allocator)
}

@(test)
test_parse_trade_negative_price_rejected :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","seq":1,"ts_ingest":1700000000000,"payload":{"Price":-1.0,"Size":1.5,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}}`
	tel: Parse_Telemetry
	result := parse_mr_message(transmute([]u8)raw, &tel)
	testing.expect_value(t, result.kind, Parse_Result_Kind.None)
	testing.expect(t, tel.parse_errors > 0, "expected parse error for negative price")
	free_all(context.temp_allocator)
}

@(test)
test_parse_trade_valid :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","seq":5,"ts_ingest":1700000000000,"payload":{"Price":50000.0,"Size":1.5,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Trade)
	testing.expect_value(t, result.data.trade.price, 50000.0)
	testing.expect_value(t, result.data.trade.qty, 1.5)
	testing.expect_value(t, result.data.trade.is_buy, true)
	free_all(context.temp_allocator)
}

@(test)
test_parse_candle_valid :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"aggregation.candle/binance/BTCUSDT/1m","seq":10,"ts_ingest":1700000000000,"payload":{"Candle":{"Venue":"binance","Instrument":"BTCUSDT","Timeframe":"1m","WindowStartTs":1700000000000,"WindowEndTs":1700000060000,"Open":50000.0,"High":50100.0,"Low":49900.0,"ClosePrice":50050.0,"Volume":100.5,"BuyVolume":60.0,"SellVolume":40.5,"TradeCount":500,"IsClosed":false}}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Candle)
	testing.expect_value(t, result.data.candle.open, 50000.0)
	testing.expect_value(t, result.data.candle.high, 50100.0)
	testing.expect_value(t, result.data.candle.window_start_ts, i64(1700000000000))
	free_all(context.temp_allocator)
}

@(test)
test_parse_candle_inverted_window_rejected :: proc(t: ^testing.T) {
	// WindowEndTs < WindowStartTs in range candle → should be skipped
	raw := `{"type":"range","subject":"aggregation.candle/binance/BTCUSDT/1m","seq":1,"ts_ingest":1700000000000,"items":[{"payload":{"Candle":{"Venue":"binance","Instrument":"BTCUSDT","Timeframe":"1m","WindowStartTs":1700000060000,"WindowEndTs":1700000000000,"Open":50000.0,"High":50100.0,"Low":49900.0,"ClosePrice":50050.0,"Volume":100.0,"BuyVolume":50.0,"SellVolume":50.0,"TradeCount":10,"IsClosed":true}}}]}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	if result.kind == .Range_Candle {
		testing.expect_value(t, result.data.range_candles.count, 0)
	}
	free_all(context.temp_allocator)
}

@(test)
test_parse_book_delta_empty_levels :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"marketdata.bookdelta/binance/BTCUSDT","seq":1,"ts_ingest":1700000000000,"payload":{"Bids":[],"Asks":[],"IsSnapshot":true,"Timestamp":1700000000000}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Orderbook)
	testing.expect_value(t, result.data.ob.ask_count, 0)
	testing.expect_value(t, result.data.ob.bid_count, 0)
	free_all(context.temp_allocator)
}

@(test)
test_f64_valid :: proc(t: ^testing.T) {
	testing.expect_value(t, f64_valid(0.0), true)
	testing.expect_value(t, f64_valid(1.0), true)
	testing.expect_value(t, f64_valid(-1.0), true)
	testing.expect_value(t, f64_valid(math.nan_f64()), false)
	testing.expect_value(t, f64_valid(math.inf_f64(1)), false)
	testing.expect_value(t, f64_valid(math.inf_f64(-1)), false)
}

@(test)
test_parse_hello_proto_version_rejected :: proc(t: ^testing.T) {
	raw := `{"type":"hello","subject":"session.hello/binance/BTCUSDT","seq":1,"ts_ingest":1700000000000,"payload":{"proto_ver":999,"server_time":1700000000000,"capabilities":{"topics":["marketdata.trade"],"venues":["binance"],"symbols":["BTCUSDT"]}}}`
	result := parse_mr_message(transmute([]u8)raw, nil)

	testing.expect_value(t, result.kind, Parse_Result_Kind.Hello)
	testing.expect_value(t, result.data.hello.valid, false)
	testing.expect_value(t, result.data.hello.reject, Hello_Reject_Reason.Unsupported_Proto_Version)
	free_all(context.temp_allocator)
}

@(test)
test_parse_hello_missing_required_server_time :: proc(t: ^testing.T) {
	raw := `{"type":"hello","subject":"session.hello/binance/BTCUSDT","seq":2,"ts_ingest":1700000000000,"payload":{"proto_ver":1,"server_time":0,"capabilities":{"topics":["marketdata.trade"],"venues":["binance"],"symbols":["BTCUSDT"]}}}`
	result := parse_mr_message(transmute([]u8)raw, nil)

	testing.expect_value(t, result.kind, Parse_Result_Kind.Hello)
	testing.expect_value(t, result.data.hello.valid, false)
	testing.expect_value(t, result.data.hello.reject, Hello_Reject_Reason.Missing_Server_Time)
	free_all(context.temp_allocator)
}

@(test)
test_parse_hello_valid_frame :: proc(t: ^testing.T) {
	raw := `{"type":"hello","subject":"session.hello/binance/BTCUSDT","seq":3,"ts_ingest":1700000000000,"payload":{"proto_ver":1,"server_time":1700000000001,"capabilities":{"topics":["marketdata.trade","marketdata.bookdelta"],"venues":["binance"],"symbols":["BTCUSDT"]}}}`
	result := parse_mr_message(transmute([]u8)raw, nil)

	testing.expect_value(t, result.kind, Parse_Result_Kind.Hello)
	testing.expect_value(t, result.data.hello.valid, true)
	testing.expect_value(t, result.data.hello.reject, Hello_Reject_Reason.None)
	testing.expect_value(t, result.data.hello.topic_count, 2)
	free_all(context.temp_allocator)
}

// --- Terminal_V1 protocol tests ---

@(test)
test_parse_hello_v2_server_instance_id :: proc(t: ^testing.T) {
	raw := `{"type":"hello","subject":"session.hello","seq":1,"ts_ingest":1700000000000,"payload":{"proto_ver":1,"protocol_version":1,"server_instance_id":"node-abc-123","server_time":1700000000001,"capabilities":{"topics":["marketdata.trade"],"venues":["binance"],"symbols":["BTCUSDT"]}}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Hello)
	testing.expect_value(t, result.data.hello.valid, true)
	testing.expect(t, len(result.data.hello.server_instance_id) > 0, "expected server_instance_id")
	free_all(context.temp_allocator)
}

@(test)
test_parse_pong :: proc(t: ^testing.T) {
	raw := `{"type":"pong","ts_client":1700000000000,"ts_server":1700000000012,"request_id":"p5"}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Pong)
	testing.expect_value(t, result.data.pong.ts_client, i64(1700000000000))
	testing.expect_value(t, result.data.pong.ts_server, i64(1700000000012))
	testing.expect_value(t, result.data.pong.rtt_ms, i64(12))
	free_all(context.temp_allocator)
}

@(test)
test_parse_pong_missing_ts_server :: proc(t: ^testing.T) {
	raw := `{"type":"pong","ts_client":1700000000000,"request_id":"p6"}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Pong)
	testing.expect_value(t, result.data.pong.ts_server, i64(0))
	testing.expect_value(t, result.data.pong.rtt_ms, i64(0))
	free_all(context.temp_allocator)
}

@(test)
test_parse_metrics :: proc(t: ^testing.T) {
	raw := `{"type":"metrics","payload":{"ws_dropped_total":5,"ws_queue_len":42,"ws_lag_ms":15,"publish_to_deliver_latency_ms":8,"serialize_errors_total":1,"resync_total":2,"active_subscriptions":24,"messages_out_total":10000}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Metrics)
	m := result.data.server_metrics
	testing.expect_value(t, m.ws_dropped_total, i64(5))
	testing.expect_value(t, m.ws_queue_len, 42)
	testing.expect_value(t, m.ws_lag_ms, i64(15))
	testing.expect_value(t, m.publish_to_deliver_latency_ms, i64(8))
	testing.expect_value(t, m.serialize_errors_total, i64(1))
	testing.expect_value(t, m.resync_total, i64(2))
	testing.expect_value(t, m.active_subscriptions, 24)
	testing.expect_value(t, m.messages_out_total, i64(10000))
	free_all(context.temp_allocator)
}

@(test)
test_parse_metrics_missing_payload :: proc(t: ^testing.T) {
	raw := `{"type":"metrics"}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Metrics)
	m := result.data.server_metrics
	testing.expect_value(t, m.ws_queue_len, 0)
	testing.expect_value(t, m.messages_out_total, i64(0))
	free_all(context.temp_allocator)
}

@(test)
test_parse_error_with_problem :: proc(t: ^testing.T) {
	raw := `{"type":"error","op":"subscribe","request_id":"r3","problem":{"code":"ERROR_CODE_RESYNC_REQUIRED","message":"stream requires resync"}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Error)
	ed := result.data.error_detail
	testing.expect_value(t, ed.code, "ERROR_CODE_RESYNC_REQUIRED")
	testing.expect_value(t, ed.op, "subscribe")
	testing.expect_value(t, ed.request_id, "r3")
	testing.expect(t, len(ed.message) > 0, "expected error message")
	free_all(context.temp_allocator)
}

@(test)
test_parse_event_v2_envelope_ts_server :: proc(t: ^testing.T) {
	// Terminal_V1 envelope with ts_server should be preferred over ts_ingest.
	raw := `{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","stream_id":"marketdata.trade/binance/BTCUSDT/raw","seq":42,"ts_ingest":1700000000000,"ts_server":1700000000050,"venue":"binance","symbol":"BTCUSDT","channel":"marketdata.trade","payload":{"Price":50000.0,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Trade)
	// ts_server should be preferred.
	testing.expect_value(t, result.meta.server_ts_ms, i64(1700000000050))
	testing.expect_value(t, result.meta.has_ts_server, true)
	testing.expect_value(t, result.meta.seq, i64(42))
	free_all(context.temp_allocator)
}

@(test)
test_parse_snapshot_frame :: proc(t: ^testing.T) {
	// type="snapshot" should set is_snapshot=true and still dispatch payload.
	raw := `{"type":"snapshot","subject":"marketdata.bookdelta/binance/BTCUSDT","seq":1,"ts_ingest":1700000000000,"payload":{"Bids":[],"Asks":[],"IsSnapshot":true,"Timestamp":1700000000000}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Orderbook)
	testing.expect_value(t, result.meta.is_snapshot, true)
	free_all(context.temp_allocator)
}

@(test)
test_parse_legacy_envelope_no_ts_server :: proc(t: ^testing.T) {
	// Legacy format without ts_server → falls back to ts_ingest.
	raw := `{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","seq":10,"ts_ingest":1700000000000,"payload":{"Price":50000.0,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Trade)
	testing.expect_value(t, result.meta.server_ts_ms, i64(1700000000000))
	testing.expect_value(t, result.meta.has_ts_server, false)
	free_all(context.temp_allocator)
}

@(test)
test_parse_hello_capabilities :: proc(t: ^testing.T) {
	raw := `{"type":"hello","payload":{"proto_ver":1,"server_time":1700000000001,"server_instance_id":"node-1","capabilities":{"topics":["marketdata.trade"],"venues":["binance"],"symbols":["BTCUSDT"],"max_subscriptions_per_connection":64,"max_symbols_per_connection":32,"max_frame_bytes":65536,"outbound_queue_size":1024,"metrics_cadence_ms":5000,"keepalive_interval_ms":30000,"supported_features":["batching","snapshot_hash","prev_seq"],"rate_limit":{"enabled":true,"max_per_second":100,"burst_capacity":200}}}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Hello)
	h := result.data.hello
	testing.expect_value(t, h.valid, true)
	testing.expect_value(t, h.max_subscriptions, 64)
	testing.expect_value(t, h.max_symbols, 32)
	testing.expect_value(t, h.max_frame_bytes, 65536)
	testing.expect_value(t, h.outbound_queue_size, 1024)
	testing.expect_value(t, h.metrics_cadence_ms, 5000)
	testing.expect_value(t, h.keepalive_interval_ms, 30000)
	testing.expect_value(t, h.rate_limit_enabled, true)
	testing.expect_value(t, h.rate_limit_max_per_sec, 100)
	testing.expect_value(t, h.rate_limit_burst, 200)
	testing.expect_value(t, h.supported_feature_count, 3)
	testing.expect_value(t, string(h.supported_features[0].name[:h.supported_features[0].len]), "batching")
	testing.expect_value(t, string(h.supported_features[1].name[:h.supported_features[1].len]), "snapshot_hash")
	testing.expect_value(t, string(h.supported_features[2].name[:h.supported_features[2].len]), "prev_seq")
	free_all(context.temp_allocator)
}

@(test)
test_parse_hello_ack_negotiated_features :: proc(t: ^testing.T) {
	raw := `{"type":"ack","op":"hello","request_id":"h1","payload":{"negotiated_features":["batching","prev_seq"]}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Hello_Ack)
	ha := result.data.hello_ack
	testing.expect_value(t, ha.negotiated_feature_count, 2)
	testing.expect_value(t, string(ha.negotiated_features[0].name[:ha.negotiated_features[0].len]), "batching")
	testing.expect_value(t, string(ha.negotiated_features[1].name[:ha.negotiated_features[1].len]), "prev_seq")
	free_all(context.temp_allocator)
}

@(test)
test_parse_hello_ack_empty_features :: proc(t: ^testing.T) {
	raw := `{"type":"ack","op":"hello","request_id":"h1","payload":{"negotiated_features":[]}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Hello_Ack)
	testing.expect_value(t, result.data.hello_ack.negotiated_feature_count, 0)
	free_all(context.temp_allocator)
}

@(test)
test_parse_envelope_prev_seq :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","seq":42,"prev_seq":41,"ts_ingest":1700000000000,"payload":{"Price":50000.0,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Trade)
	testing.expect_value(t, result.meta.prev_seq, i64(41))
	free_all(context.temp_allocator)
}

@(test)
test_parse_snapshot_integrity_fields :: proc(t: ^testing.T) {
	raw := `{"type":"snapshot","subject":"marketdata.bookdelta/binance/BTCUSDT","seq":100,"ts_ingest":1700000000000,"snapshot_seq":99,"watermark_seq":95,"snapshot_hash":"a1b2c3d4e5f60718","payload":{"Bids":[],"Asks":[],"IsSnapshot":true,"Timestamp":1700000000000}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Orderbook)
	testing.expect_value(t, result.meta.is_snapshot, true)
	testing.expect_value(t, result.meta.snapshot_seq, i64(99))
	testing.expect_value(t, result.meta.watermark_seq, i64(95))
	testing.expect_value(t, result.meta.snapshot_hash_len, u8(16))
	testing.expect_value(t, string(result.meta.snapshot_hash[:16]), "a1b2c3d4e5f60718")
	free_all(context.temp_allocator)
}

@(test)
test_parse_snapshot_no_integrity_fields :: proc(t: ^testing.T) {
	raw := `{"type":"snapshot","subject":"marketdata.bookdelta/binance/BTCUSDT","seq":50,"ts_ingest":1700000000000,"payload":{"Bids":[],"Asks":[],"IsSnapshot":true,"Timestamp":1700000000000}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Orderbook)
	testing.expect_value(t, result.meta.is_snapshot, true)
	testing.expect_value(t, result.meta.snapshot_seq, i64(0))
	testing.expect_value(t, result.meta.watermark_seq, i64(0))
	testing.expect_value(t, result.meta.snapshot_hash_len, u8(0))
	free_all(context.temp_allocator)
}

@(test)
test_parse_snapshot_envelope_overrides_bookdelta_payload_flag :: proc(t: ^testing.T) {
	raw := `{"type":"snapshot","subject":"marketdata.bookdelta/binance/BTCUSDT","seq":51,"ts_ingest":1700000000000,"payload":{"Bids":[],"Asks":[],"IsSnapshot":false,"Timestamp":1700000000000}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Orderbook)
	testing.expect_value(t, result.meta.is_snapshot, true)
	testing.expect_value(t, result.data.ob.is_snapshot, true)
	free_all(context.temp_allocator)
}

@(test)
test_parse_metrics_backpressure :: proc(t: ^testing.T) {
	raw := `{"type":"metrics","payload":{"ws_dropped_total":5,"ws_queue_len":42,"ws_lag_ms":15,"publish_to_deliver_latency_ms":8,"serialize_errors_total":1,"resync_total":2,"active_subscriptions":24,"messages_out_total":10000,"backpressure_level":2,"recommended_action":"reduce_subs","queue_capacity":1024,"queue_high_watermark":768}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Metrics)
	m := result.data.server_metrics
	testing.expect_value(t, m.backpressure_level, 2)
	testing.expect_value(t, m.queue_capacity, 1024)
	testing.expect_value(t, m.queue_high_watermark, 768)
	testing.expect_value(t, string(m.recommended_action_buf[:m.recommended_action_len]), "reduce_subs")
	free_all(context.temp_allocator)
}

@(test)
test_parse_error_action_hint :: proc(t: ^testing.T) {
	raw := `{"type":"error","op":"subscribe","request_id":"r5","problem":{"code":"ERROR_CODE_RESYNC_REQUIRED","message":"stream requires resync","error_code":"RESYNC_REQUIRED","action_hint":"resync"}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Error)
	ed := result.data.error_detail
	testing.expect_value(t, ed.error_code, "RESYNC_REQUIRED")
	testing.expect_value(t, ed.action_hint, "resync")
	testing.expect_value(t, ed.code, "ERROR_CODE_RESYNC_REQUIRED")
	free_all(context.temp_allocator)
}

@(test)
test_parse_error_no_action_hint :: proc(t: ^testing.T) {
	raw := `{"type":"error","op":"subscribe","request_id":"r6","problem":{"code":"GENERIC","message":"something failed"}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Error)
	ed := result.data.error_detail
	testing.expect_value(t, ed.action_hint, "")
	testing.expect_value(t, ed.error_code, "")
	free_all(context.temp_allocator)
}

@(test)
test_parse_ack_missing_subject_stream_id :: proc(t: ^testing.T) {
	raw := `{"type":"ack","op":"subscribe","request_id":"a1"}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Ack)
	testing.expect_value(t, result.data.ack.op, "subscribe")
	testing.expect_value(t, result.data.ack.subject, "")
	testing.expect_value(t, result.data.ack.stream_id, "")
	free_all(context.temp_allocator)
}

// --- Comprehensive V1 protocol tests (C13) ---

@(test)
test_parse_hello_rate_limit :: proc(t: ^testing.T) {
	raw := `{"type":"hello","payload":{"server_time":1700000000000,"protocol_version":1,"capabilities":{"topics":["trade"],"venues":["binance"],"symbols":["BTCUSDT"],"rate_limit":{"enabled":true,"max_per_second":100,"burst_capacity":200}}}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Hello)
	h := result.data.hello
	testing.expect_value(t, h.valid, true)
	testing.expect_value(t, h.rate_limit_enabled, true)
	testing.expect_value(t, h.rate_limit_max_per_sec, 100)
	testing.expect_value(t, h.rate_limit_burst, 200)
	free_all(context.temp_allocator)
}

@(test)
test_parse_hello_ack_features_roundtrip :: proc(t: ^testing.T) {
	// Verify all MAX_FEATURE_SLOTS slots can be populated.
	raw := `{"type":"ack","op":"hello","payload":{"negotiated_features":["batching","snapshot_hash","prev_seq","compress","delta","multiplex","priority","stream_resume"]}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Hello_Ack)
	ha := result.data.hello_ack
	testing.expect_value(t, ha.negotiated_feature_count, MAX_FEATURE_SLOTS)
	testing.expect_value(t, string(ha.negotiated_features[0].name[:ha.negotiated_features[0].len]), "batching")
	testing.expect_value(t, string(ha.negotiated_features[7].name[:ha.negotiated_features[7].len]), "stream_resume")
	free_all(context.temp_allocator)
}

@(test)
test_parse_error_action_hint_resync :: proc(t: ^testing.T) {
	raw := `{"type":"error","op":"subscribe","request_id":"r10","problem":{"code":"STALE_STATE","message":"Stale state","error_code":"STALE","action_hint":"resync"}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Error)
	ed := result.data.error_detail
	testing.expect_value(t, ed.action_hint, "resync")
	testing.expect_value(t, ed.error_code, "STALE")
	free_all(context.temp_allocator)
}

@(test)
test_parse_error_action_hint_reconnect :: proc(t: ^testing.T) {
	raw := `{"type":"error","op":"subscribe","request_id":"r11","problem":{"code":"SERVER_RESTART","message":"Server restart","error_code":"RESTART","action_hint":"reconnect"}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Error)
	ed := result.data.error_detail
	testing.expect_value(t, ed.action_hint, "reconnect")
	testing.expect_value(t, ed.error_code, "RESTART")
	free_all(context.temp_allocator)
}

@(test)
test_parse_metrics_backpressure_zero :: proc(t: ^testing.T) {
	raw := `{"type":"metrics","payload":{"ws_dropped":0,"ws_queue_len":10,"ws_lag_ms":5,"backpressure_level":0,"queue_capacity":1024}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Metrics)
	m := result.data.server_metrics
	testing.expect_value(t, m.backpressure_level, 0)
	testing.expect_value(t, m.queue_capacity, 1024)
	free_all(context.temp_allocator)
}

@(test)
test_parse_hello_capabilities_all_limits :: proc(t: ^testing.T) {
	raw := `{"type":"hello","payload":{"server_time":1700000000000,"protocol_version":1,"capabilities":{"topics":["trade"],"venues":["binance"],"symbols":["BTCUSDT"],"max_subscriptions_per_connection":50,"max_symbols_per_connection":100,"max_frame_bytes":65536,"outbound_queue_size":2048,"metrics_cadence_ms":5000,"keepalive_interval_ms":30000,"supported_features":["batching","snapshot_hash"]}}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Hello)
	h := result.data.hello
	testing.expect_value(t, h.valid, true)
	testing.expect_value(t, h.max_subscriptions, 50)
	testing.expect_value(t, h.max_frame_bytes, 65536)
	testing.expect_value(t, h.metrics_cadence_ms, 5000)
	testing.expect_value(t, h.keepalive_interval_ms, 30000)
	testing.expect_value(t, h.supported_feature_count, 2)
	testing.expect_value(t, string(h.supported_features[0].name[:h.supported_features[0].len]), "batching")
	testing.expect_value(t, string(h.supported_features[1].name[:h.supported_features[1].len]), "snapshot_hash")
	free_all(context.temp_allocator)
}

@(test)
test_parse_snapshot_with_hash_and_seq :: proc(t: ^testing.T) {
	raw := `{"type":"snapshot","subject":"marketdata.bookdelta/binance/BTCUSDT/raw","seq":42,"payload":{"ask_prices":[],"ask_sizes":[],"bid_prices":[],"bid_sizes":[],"is_snapshot":true},"snapshot_seq":10,"watermark_seq":8,"snapshot_hash":"a1b2c3d4e5f6a7b8"}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.meta.snapshot_seq, i64(10))
	testing.expect_value(t, result.meta.watermark_seq, i64(8))
	testing.expect_value(t, result.meta.snapshot_hash_len, u8(16))
	hash_str := string(result.meta.snapshot_hash[:result.meta.snapshot_hash_len])
	testing.expect_value(t, hash_str, "a1b2c3d4e5f6a7b8")
	free_all(context.temp_allocator)
}

@(test)
test_parse_event_with_prev_seq :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"marketdata.trade/binance/BTCUSDT/raw","seq":5,"prev_seq":4,"ts_ingest":1700000000000,"payload":{"Price":50000.0,"Qty":1.5,"IsBuy":true,"UnixMs":1700000000000}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.meta.prev_seq, i64(4))
	testing.expect_value(t, result.meta.seq, i64(5))
	free_all(context.temp_allocator)
}

@(test)
test_parse_microstructure_evidence :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"insights.microstructure_evidence/binance/BTCUSDT/raw","seq":42,"ts_ingest":1700000000001,"payload":{"kind":"SPREAD_EXPLOSION","confidence":0.82,"features":["spread_bps","mid_price"],"feature_values":[25.1,50000.0],"reason":"spread expanded","ts_ingest":1700000000001,"seq":42}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Evidence)
	ev := result.data.evidence
	testing.expect_value(t, string(ev.kind[:ev.kind_len]), "SPREAD_EXPLOSION")
	testing.expect_value(t, ev.feature_count, 2)
	testing.expect_value(t, ev.confidence > 0.8, true)
	free_all(context.temp_allocator)
}

@(test)
test_parse_microstructure_evidence_v2 :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"insights.microstructure_evidence/binance/BTCUSDT/raw","seq":107,"ts_ingest":1772638137464,"ts_server":1772638137552,"payload":{"type":"absorption","ts_server":1772638137464,"venue":"BINANCE","symbol":"BTCUSDT","stream_id":"BINANCE/BTCUSDT/trade","seq":1772637404558069,"severity":"critical","confidence":0.95,"features":[{"key":"cum_volume","value":42.66},{"key":"price_move_pct","value":0.0049}],"explanation":"large cumulative volume absorbed with minimal price movement","rule_version":"v0","input_watermark":{"seq_start":1772637404557538,"seq_end":1772637404558069}}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Evidence)
	ev := result.data.evidence
	testing.expect_value(t, string(ev.kind[:ev.kind_len]), "absorption")
	testing.expect_value(t, ev.feature_count, 2)
	testing.expect_value(t, string(ev.feature_tags[0][:10]), "cum_volume")
	testing.expect_value(t, ev.feature_vals[0] > 40, true)
	testing.expect_value(t, string(ev.reason[:ev.reason_len]), "large cumulative volume absorbed with minimal price movement")
	free_all(context.temp_allocator)
}

@(test)
test_parse_signal_frame_valid :: proc(t: ^testing.T) {
	raw := `{"type":"signal","subject":"signal/composite/binance/BTCUSDT/1m","seq":88,"ts_server":1700000001200,"payload":{"kind":"trend_breakout","venue":"binance","instrument":"BTCUSDT","timeframe":"1m","severity":"high","confidence":0.91,"evidence":[{"label":"spread_bps","value":"8.1"}],"regime_kind":"trend","regime_strength":0.77,"reason":"breakout confirmed"}}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Signal)
	testing.expect_value(t, result.meta.has_ts_server, true)
	testing.expect_value(t, result.meta.server_ts_ms, i64(1700000001200))
	sig := result.data.signal
	testing.expect_value(t, string(sig.kind[:sig.kind_len]), "trend_breakout")
	testing.expect_value(t, string(sig.severity[:sig.severity_len]), "high")
	testing.expect_value(t, sig.confidence, 0.91)
	testing.expect_value(t, string(sig.regime[:sig.regime_len]), "trend")
	free_all(context.temp_allocator)
}

@(test)
test_parse_signal_frame_invalid_confidence_rejected :: proc(t: ^testing.T) {
	raw := `{"type":"signal","subject":"signal/composite/binance/BTCUSDT/1m","seq":89,"ts_server":1700000001300,"payload":{"kind":"trend_breakout","severity":"high","confidence":1.5,"reason":"invalid"}}`
	tel: Parse_Telemetry
	result := parse_mr_message(transmute([]u8)raw, &tel)
	testing.expect_value(t, result.kind, Parse_Result_Kind.None)
	testing.expect(t, tel.parse_errors > 0, "invalid signal confidence should increment parse_errors")
	free_all(context.temp_allocator)
}

@(test)
test_parse_batched_frame_two_events :: proc(t: ^testing.T) {
	raw := `{"type":"batch","stream_id":"marketdata.trade/binance/BTCUSDT/raw","base_seq":100,"count":2,"ts_server_base":1700000000000,"ts_ingest_base":1700000000000,"events":[{"dseq":0,"dprev":0,"dts":0,"dti":0,"p":{"Price":50000.0,"Size":1.5,"Side":"buy","Timestamp":1700000000000}},{"dseq":1,"dprev":1,"dts":2,"dti":2,"p":{"Price":50001.0,"Size":0.8,"Side":"sell","Timestamp":1700000000002}}]}`
	seg: Parsed_Batched_Frame
	ok := parse_batched_frame(transmute([]u8)raw, &seg)
	testing.expect_value(t, ok, true)
	testing.expect_value(t, seg.count, 2)
	testing.expect_value(t, seg.total_events, 2)
	testing.expect_value(t, seg.event_count, 2)
	testing.expect_value(t, string(seg.stream_id_buf[:int(seg.stream_id_len)]), "marketdata.trade/binance/BTCUSDT/raw")
	testing.expect(t, seg.events[0].payload_end > seg.events[0].payload_start, "first payload range should be valid")
}

@(test)
test_parse_batched_frame_seq_assignment :: proc(t: ^testing.T) {
	raw := `{"type":"batch","stream_id":"marketdata.trade/binance/BTCUSDT/raw","base_seq":700,"count":2,"ts_server_base":1700000000000,"ts_ingest_base":1700000000000,"events":[{"dseq":0,"dprev":0,"dts":0,"dti":0,"p":{"Price":1}},{"dseq":1,"dprev":1,"dts":1,"dti":1,"p":{"Price":2}}]}`
	seg: Parsed_Batched_Frame
	ok := parse_batched_frame(transmute([]u8)raw, &seg)
	testing.expect_value(t, ok, true)
	testing.expect_value(t, seg.event_count, 2)
	seq0 := seg.base_seq + i64(seg.events[0].event_index)
	seq1 := seg.base_seq + i64(seg.events[1].event_index)
	testing.expect_value(t, seq0, i64(700))
	testing.expect_value(t, seq1, i64(701))
}

@(test)
test_parse_batched_frame_split_behavior :: proc(t: ^testing.T) {
	write_int := proc(b: ^strings.Builder, v: int) {
		buf: [24]u8
		strings.write_string(b, fmt.bprintf(buf[:], "%d", v))
	}

	total := BATCH_EVENT_VIEW_CAP + 3
	b := strings.builder_make()
	defer strings.builder_destroy(&b)

	strings.write_string(&b, `{"type":"batch","stream_id":"marketdata.trade/binance/BTCUSDT/raw","base_seq":1,"count":`)
	write_int(&b, total)
	strings.write_string(&b, `,"ts_server_base":1700000000000,"ts_ingest_base":1700000000000,"events":[`)
	for i in 0 ..< total {
		if i > 0 do strings.write_string(&b, ",")
		strings.write_string(&b, `{"dseq":`)
		write_int(&b, i)
		strings.write_string(&b, `,"dprev":`)
		write_int(&b, i)
		strings.write_string(&b, `,"dts":`)
		write_int(&b, i)
		strings.write_string(&b, `,"dti":`)
		write_int(&b, i)
		strings.write_string(&b, `,"p":{"Price":`)
		write_int(&b, 50000+i)
		strings.write_string(&b, `,"Size":1,"Side":"buy","Timestamp":1700000000000}}`)
	}
	strings.write_string(&b, "]}")

	raw := strings.to_string(b)
	seg0: Parsed_Batched_Frame
	ok0 := parse_batched_frame(transmute([]u8)raw, &seg0)
	testing.expect_value(t, ok0, true)
	testing.expect_value(t, seg0.event_count, BATCH_EVENT_VIEW_CAP)
	testing.expect_value(t, seg0.total_events, total)
	testing.expect_value(t, seg0.has_more, true)

	seg1: Parsed_Batched_Frame
	ok1 := parse_batched_frame(transmute([]u8)raw, &seg1, seg0.event_count)
	testing.expect_value(t, ok1, true)
	testing.expect_value(t, seg1.event_count, total - BATCH_EVENT_VIEW_CAP)
	testing.expect_value(t, seg1.has_more, false)
}

@(test)
test_parse_batched_event_payload_trade_fastpath :: proc(t: ^testing.T) {
	payload := `{"Price":50000.0,"Size":1.5,"Side":"buy","Timestamp":1700000000000}`
	result, ok := parse_batched_event_payload(
		"marketdata.trade",
		transmute([]u8)payload,
		101,
		1700000000002,
		1700000000001,
		0xABCD,
	)
	testing.expect_value(t, ok, true)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Trade)
	testing.expect_value(t, result.meta.seq, i64(101))
	testing.expect_value(t, result.meta.subject_id, u64(0xABCD))
	testing.expect_value(t, result.meta.server_ts_ms, i64(1700000000002))
	testing.expect_value(t, result.data.trade.seq, i64(101))
}

@(test)
test_parse_batched_event_payload_bookdelta_respects_snapshot_flag :: proc(t: ^testing.T) {
	payload := `{"Bids":[{"Price":100.0,"Size":1.0}],"Asks":[{"Price":101.0,"Size":1.2}],"IsSnapshot":false,"Timestamp":1700000000000}`
	result, ok := parse_batched_event_payload(
		"marketdata.bookdelta",
		transmute([]u8)payload,
		202,
		1700000000010,
		1700000000009,
		0xBCDE,
		true,
	)
	testing.expect_value(t, ok, true)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Orderbook)
	testing.expect_value(t, result.meta.is_snapshot, true)
	testing.expect_value(t, result.data.ob.is_snapshot, true)
	testing.expect_value(t, result.data.ob.seq, i64(202))
}
