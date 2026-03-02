package services

import "core:testing"

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
