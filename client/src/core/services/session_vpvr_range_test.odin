package services

// S82: Tests for session volume profile snapshot parser.

import "core:testing"

@(test)
test_parse_session_vpvr_empty :: proc(t: ^testing.T) {
	store: Session_VPVR_Store
	count := parse_session_vpvr_snapshot(&store, transmute([]u8)string("{}"))
	testing.expect_value(t, count, 0)
}

@(test)
test_parse_session_vpvr_snapshot :: proc(t: ^testing.T) {
	store: Session_VPVR_Store
	json_str := `{"venue":"BINANCE","instrument":"BTCUSDT","session_anchor":"US_2026-03-08","window_start_ts":1709856000000,"window_end_ts":0,"is_active":true,"poc_price":67500.0,"value_area_low":67000.0,"value_area_high":68000.0,"total_volume":360.0,"buy_volume":200.0,"sell_volume":160.0,"buckets":[{"price_low":67000.0,"price_high":67100.0,"buy_volume":100.0,"sell_volume":80.0,"total_volume":180.0},{"price_low":67100.0,"price_high":67200.0,"buy_volume":100.0,"sell_volume":80.0,"total_volume":180.0}]}`
	count := parse_session_vpvr_snapshot(&store, transmute([]u8)string(json_str))
	testing.expect_value(t, count, 2)
	testing.expect_value(t, store.count, 2)
	testing.expect(t, store.vah_price > 67999.0, "vah should be ~68000")
	testing.expect(t, store.val_price > 66999.0, "val should be ~67000")
	testing.expect(t, store.total_volume > 359.0, "total vol should be ~360")
	testing.expect(t, store.buy_volume > 199.0, "buy vol should be ~200")
	testing.expect(t, store.sell_volume > 159.0, "sell vol should be ~160")
	testing.expect_value(t, store.session_start, i64(1709856000000))
	testing.expect_value(t, store.session_end, i64(0))
	// Session label should be set.
	label := get_session_vpvr_label(&store)
	testing.expect(t, label == "US_2026-03-08", "label should match anchor")
}

@(test)
test_parse_session_vpvr_nil_store :: proc(t: ^testing.T) {
	count := parse_session_vpvr_snapshot(nil, transmute([]u8)string("{}"))
	testing.expect_value(t, count, 0)
}

@(test)
test_parse_session_vpvr_is_active :: proc(t: ^testing.T) {
	active := parse_session_vpvr_is_active(transmute([]u8)string(`{"is_active":true}`))
	testing.expect(t, active, "should be active")

	inactive := parse_session_vpvr_is_active(transmute([]u8)string(`{"is_active":false}`))
	testing.expect(t, !inactive, "should not be active")
}

@(test)
test_parse_session_vpvr_session_times :: proc(t: ^testing.T) {
	json_str := `{"window_start_ts":1709856000000,"window_end_ts":1709942400000}`
	start_ms, end_ms := parse_session_vpvr_session_times(transmute([]u8)string(json_str))
	testing.expect_value(t, start_ms, i64(1709856000000))
	testing.expect_value(t, end_ms, i64(1709942400000))
}

@(test)
test_parse_session_vpvr_no_buckets :: proc(t: ^testing.T) {
	store: Session_VPVR_Store
	json_str := `{"venue":"BINANCE","instrument":"BTCUSDT","poc_price":67500.0}`
	count := parse_session_vpvr_snapshot(&store, transmute([]u8)string(json_str))
	testing.expect_value(t, count, 0)
}

@(test)
test_parse_session_vpvr_empty_buckets :: proc(t: ^testing.T) {
	store: Session_VPVR_Store
	json_str := `{"venue":"BINANCE","instrument":"BTCUSDT","poc_price":67500.0,"buckets":[]}`
	count := parse_session_vpvr_snapshot(&store, transmute([]u8)string(json_str))
	testing.expect_value(t, count, 0)
}

@(test)
test_parse_session_vpvr_poc_tracking :: proc(t: ^testing.T) {
	store: Session_VPVR_Store
	// Second bucket has higher total_volume, so POC should be at index 1.
	json_str := `{"poc_price":67150.0,"value_area_low":67000.0,"value_area_high":68000.0,"total_volume":480.0,"buy_volume":260.0,"sell_volume":220.0,"window_start_ts":100,"window_end_ts":200,"buckets":[{"price_low":67000.0,"price_high":67100.0,"buy_volume":80.0,"sell_volume":70.0,"total_volume":150.0},{"price_low":67100.0,"price_high":67200.0,"buy_volume":180.0,"sell_volume":150.0,"total_volume":330.0}]}`
	count := parse_session_vpvr_snapshot(&store, transmute([]u8)string(json_str))
	testing.expect_value(t, count, 2)
	// POC should be at index 1 (closest to poc_price=67150, midpoint of bucket 1 = 67150).
	testing.expect_value(t, store.poc_index, 1)
}

@(test)
test_parse_session_vpvr_is_active_short_input :: proc(t: ^testing.T) {
	active := parse_session_vpvr_is_active(transmute([]u8)string("x"))
	testing.expect(t, !active, "short input should not be active")
}

@(test)
test_parse_session_vpvr_session_times_short_input :: proc(t: ^testing.T) {
	start_ms, end_ms := parse_session_vpvr_session_times(transmute([]u8)string("x"))
	testing.expect_value(t, start_ms, i64(0))
	testing.expect_value(t, end_ms, i64(0))
}
