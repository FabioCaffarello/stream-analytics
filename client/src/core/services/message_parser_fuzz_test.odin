package services

import "core:encoding/json"
import "core:math"
import "core:testing"
import "mr:util"

@(private = "file")
lcg_next :: proc(seed: ^u32) -> u32 {
	seed^ = seed^ * 1664525 + 1013904223
	return seed^
}

@(test)
test_parse_message_fuzz_random_bytes_never_panics :: proc(t: ^testing.T) {
	seed := u32(0xC0FFEE11)
	total_parse_errors := 0

	for _ in 0 ..< 2000 {
		raw_len := int(lcg_next(&seed) % 512)
		raw := make([]u8, raw_len, context.temp_allocator)
		for i in 0 ..< raw_len {
			raw[i] = u8(lcg_next(&seed) & 0xFF)
		}

		tel: Parse_Telemetry
		_ = parse_mr_message(raw, &tel)
		total_parse_errors += tel.parse_errors
		free_all(context.temp_allocator)
	}

	testing.expect(t, total_parse_errors > 0, "expected parse errors for fuzz corpus")
}

@(test)
test_parse_message_degenerate_inputs_count_errors :: proc(t: ^testing.T) {
	degenerate := [8]string{
		"",
		"{",
		`{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","seq":1,"payload":`,
		`{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","seq":9223372036854775807,"ts_server":-9223372036854775808,"payload":{"Price":NaN}}`,
		`{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","seq":9223372036854775807,"payload":{"Price":Inf,"Size":1}}`,
		`{"type":"event","subject":"marketdata.bookdelta/binance/BTCUSDT","seq":1,"payload":{"Bids":[{"Price":1.0}],"Asks":[{"Size":2.0}]}}`,
		`{"type":"hello","subject":"session.hello","payload":{"protocol_version":1}}`,
		`{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","payload":{"Price":1.0,"Size":1.0,"Side":"buy"}}`,
	}
	expect_parse_error := [8]bool{true, true, true, true, true, false, false, false}

	for i in 0 ..< len(degenerate) {
		tel: Parse_Telemetry
		_ = parse_mr_message(transmute([]u8)degenerate[i], &tel)
		if expect_parse_error[i] {
			testing.expect(t, tel.parse_errors > 0, "expected parse_errors increment for invalid/degenerate frame")
		}
		free_all(context.temp_allocator)
	}
}

@(test)
test_parse_bookdelta_large_array_is_bounded_and_valid :: proc(t: ^testing.T) {
	Book_Frame :: struct {
		type_str:  string               `json:"type"`,
		subject:   string               `json:"subject"`,
		seq:       i64                  `json:"seq"`,
		ts_ingest: i64                  `json:"ts_ingest"`,
		payload:   util.MR_Book_Delta   `json:"payload"`,
	}

	asks := make([dynamic]util.MR_Price_Level, context.temp_allocator)
	bids := make([dynamic]util.MR_Price_Level, context.temp_allocator)
	for i in 0 ..< 256 {
		ask_price := f64(10_000 + i)
		ask_size := f64(i % 5 + 1)
		bid_price := f64(9_999 - i)
		bid_size := f64((i + 1) % 7 + 1)
		if i % 11 == 0 {
			ask_price = -1
		}
		if i % 17 == 0 {
			bid_size = -1
		}
		append(&asks, util.MR_Price_Level{price = ask_price, size = ask_size})
		append(&bids, util.MR_Price_Level{price = bid_price, size = bid_size})
	}

	frame := Book_Frame{
		type_str  = "event",
		subject   = "marketdata.bookdelta/binance/BTCUSDT",
		seq       = 12,
		ts_ingest = 1_700_000_000_000,
		payload   = util.MR_Book_Delta{
			bids        = bids[:],
			asks        = asks[:],
			is_snapshot = true,
			timestamp_ms = 1_700_000_000_000,
		},
	}
	raw, err := json.marshal(frame)
	testing.expect_value(t, err, nil)

	tel: Parse_Telemetry
	r := parse_mr_message(raw, &tel)
	testing.expect_value(t, r.kind, Parse_Result_Kind.Orderbook)
	ob := r.data.ob
	testing.expect(t, ob.ask_count <= OB_STAGING_DEPTH, "ask_count must be bounded")
	testing.expect(t, ob.bid_count <= OB_STAGING_DEPTH, "bid_count must be bounded")
	for i in 0 ..< ob.ask_count {
		testing.expect(t, f64_valid(ob.ask_prices[i]), "ask price must be finite")
		testing.expect(t, f64_valid(ob.ask_sizes[i]), "ask size must be finite")
		testing.expect(t, ob.ask_prices[i] >= 0, "ask price must be non-negative")
		testing.expect(t, ob.ask_sizes[i] >= 0, "ask size must be non-negative")
	}
	for i in 0 ..< ob.bid_count {
		testing.expect(t, f64_valid(ob.bid_prices[i]), "bid price must be finite")
		testing.expect(t, f64_valid(ob.bid_sizes[i]), "bid size must be finite")
		testing.expect(t, ob.bid_prices[i] >= 0, "bid price must be non-negative")
		testing.expect(t, ob.bid_sizes[i] >= 0, "bid size must be non-negative")
	}
	free_all(context.temp_allocator)
}

@(test)
test_parse_message_truncated_frames_count_errors :: proc(t: ^testing.T) {
	valid := `{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","seq":42,"ts_ingest":1700000000000,"ts_server":1700000000050,"payload":{"Price":50000.0,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1700000000000}}`

	for cut := 1; cut < len(valid); cut += 11 {
		tel: Parse_Telemetry
		_ = parse_mr_message(transmute([]u8)valid[:cut], &tel)
		testing.expect(t, tel.parse_errors > 0, "expected parse_errors on truncated frame")
		free_all(context.temp_allocator)
	}
}

@(test)
test_parse_message_extreme_numeric_payload_never_panics :: proc(t: ^testing.T) {
	raw := `{"type":"event","subject":"marketdata.trade/binance/BTCUSDT","seq":9223372036854775807,"ts_ingest":1700000000000,"ts_server":1700000000001,"payload":{"Price":1.7976931348623157e308,"Size":4.9406564584124654e-324,"Side":"buy","TradeID":"x","Timestamp":1700000000000}}`
	tel: Parse_Telemetry
	r := parse_mr_message(transmute([]u8)raw, &tel)
	if r.kind == .Trade {
		testing.expect(t, f64_valid(r.data.trade.price), "price must stay finite")
		testing.expect(t, f64_valid(r.data.trade.qty), "qty must stay finite")
		testing.expect(t, !math.is_nan(r.data.trade.price), "price cannot be NaN")
	}
	free_all(context.temp_allocator)
}
