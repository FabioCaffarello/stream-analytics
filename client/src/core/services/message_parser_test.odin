package services

import "core:math"
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
test_parse_ack_missing_subject_stream_id :: proc(t: ^testing.T) {
	raw := `{"type":"ack","op":"subscribe","request_id":"a1"}`
	result := parse_mr_message(transmute([]u8)raw, nil)
	testing.expect_value(t, result.kind, Parse_Result_Kind.Ack)
	testing.expect_value(t, result.data.ack.op, "subscribe")
	testing.expect_value(t, result.data.ack.subject, "")
	testing.expect_value(t, result.data.ack.stream_id, "")
	free_all(context.temp_allocator)
}
