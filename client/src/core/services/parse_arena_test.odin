package services

import "core:testing"

@(test)
test_parse_arena_trade_message_reset_per_message :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","seq":7,"ts_ingest":1700000000000,"payload":{"Price":50200.5,"Size":0.8,"Side":"buy","TradeID":"t7","Timestamp":1700000000000}}`
	iterations := 128
	arena: Parse_Arena
	tel: Parse_Telemetry
	for _ in 0 ..< iterations {
		result := parse_mr_message_with_arena(&arena, transmute([]u8)raw, &tel)
		testing.expect_value(t, result.kind, Parse_Result_Kind.Trade)
		parse_arena_reset_message(&arena)
	}
	testing.expect_value(t, arena.msg_count, u64(iterations))
	testing.expect_value(t, arena.message_resets, u64(iterations))
	testing.expect_value(t, arena.bytes_total, u64(iterations * len(raw)))
	testing.expect_value(t, tel.parse_errors, 0)
}

@(test)
test_parse_arena_invalid_message_still_resets :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"aggregation.tape/binance/BTCUSDT/1s","seq":1,"ts_ingest":1700000000000,"payload":{"TradeCount":2,"BuyVolume":1.0,"SellVolume":1.0,"TotalVolume":2.0,"LastPrice":50000.0,"Rate":2.0,"Imbalance":1.7}}`
	arena: Parse_Arena
	tel: Parse_Telemetry
	result := parse_mr_message_with_arena(&arena, transmute([]u8)raw, &tel)
	testing.expect_value(t, result.kind, Parse_Result_Kind.None)
	parse_arena_reset_message(&arena)
	testing.expect_value(t, arena.msg_count, u64(1))
	testing.expect_value(t, arena.message_resets, u64(1))
	testing.expect_value(t, tel.parse_errors > 0, true)
}
