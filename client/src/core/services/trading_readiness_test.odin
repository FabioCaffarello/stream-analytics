package services

import "core:testing"

// S78: Tests for trading readiness surface parser.

@(test)
test_trading_readiness_parse_full :: proc(t: ^testing.T) {
	json := `{"control_plane":{"state":"active","simulation_profile":"","disabled_strategies":["strat-1"],"disabled_adapters":["adapter-1"],"allowlist_restricted":true,"restricted_venues":["binance"],"restricted_symbols":["BTCUSDT"],"updated_at_ms":1700000000000},"accounts":[{"account_id":"acc-1","venues":[{"venue":"binance","trading_status":"enabled","position_count":3,"equity_usd":10000.0,"last_projected_ms":1700000000000,"stale":false,"restricted":false},{"venue":"bybit","trading_status":"degraded","position_count":1,"equity_usd":5000.0,"last_projected_ms":1700000000000,"stale":true,"restricted":true}],"equity_usd":15000.0,"position_count":4,"stale":false}],"safety_flags":["strategies_disabled","adapters_disabled","venue_restricted"],"evaluated_at_ms":1700000000000}`
	data := transmute([]u8)json

	result: Trading_Readiness_Result
	ok := trading_readiness_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")

	// Control plane.
	testing.expect_value(t, result.control_plane.state, "active")
	testing.expect_value(t, result.control_plane.allowlist_restricted, true)
	testing.expect_value(t, result.control_plane.disabled_strategy_count, 1)
	testing.expect_value(t, result.control_plane.disabled_strategies[0], "strat-1")
	testing.expect_value(t, result.control_plane.disabled_adapter_count, 1)
	testing.expect_value(t, result.control_plane.disabled_adapters[0], "adapter-1")
	testing.expect_value(t, result.control_plane.restricted_venue_count, 1)
	testing.expect_value(t, result.control_plane.restricted_venues[0], "binance")
	testing.expect_value(t, result.control_plane.restricted_symbol_count, 1)
	testing.expect_value(t, result.control_plane.restricted_symbols[0], "BTCUSDT")
	testing.expect_value(t, result.control_plane.updated_at_ms, i64(1700000000000))

	// Accounts.
	testing.expect_value(t, result.account_count, 1)
	testing.expect_value(t, result.accounts[0].account_id, "acc-1")
	testing.expect_value(t, result.accounts[0].equity_usd, 15000.0)
	testing.expect_value(t, result.accounts[0].position_count, i32(4))
	testing.expect_value(t, result.accounts[0].stale, false)

	// Venues.
	testing.expect_value(t, result.accounts[0].venue_count, 2)
	testing.expect_value(t, result.accounts[0].venues[0].venue, "binance")
	testing.expect_value(t, result.accounts[0].venues[0].trading_status, Trading_Status.Enabled)
	testing.expect_value(t, result.accounts[0].venues[0].position_count, i32(3))
	testing.expect_value(t, result.accounts[0].venues[0].stale, false)
	testing.expect_value(t, result.accounts[0].venues[0].restricted, false)

	testing.expect_value(t, result.accounts[0].venues[1].venue, "bybit")
	testing.expect_value(t, result.accounts[0].venues[1].trading_status, Trading_Status.Degraded)
	testing.expect_value(t, result.accounts[0].venues[1].stale, true)
	testing.expect_value(t, result.accounts[0].venues[1].restricted, true)

	// Safety flags.
	testing.expect_value(t, result.flag_count, 3)
	testing.expect_value(t, result.safety_flags[0], "strategies_disabled")
	testing.expect_value(t, result.safety_flags[1], "adapters_disabled")
	testing.expect_value(t, result.safety_flags[2], "venue_restricted")

	testing.expect_value(t, result.evaluated_at_ms, i64(1700000000000))
}

@(test)
test_trading_readiness_parse_halted :: proc(t: ^testing.T) {
	json := `{"control_plane":{"state":"halted","disabled_strategies":[],"disabled_adapters":[],"updated_at_ms":100},"accounts":[],"safety_flags":["halted"],"evaluated_at_ms":100}`
	data := transmute([]u8)json

	result: Trading_Readiness_Result
	ok := trading_readiness_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.control_plane.state, "halted")
	testing.expect_value(t, result.account_count, 0)
	testing.expect_value(t, result.flag_count, 1)
	testing.expect_value(t, result.safety_flags[0], "halted")
}

@(test)
test_trading_readiness_parse_nil_data :: proc(t: ^testing.T) {
	result: Trading_Readiness_Result
	ok := trading_readiness_parse_json(nil, &result)
	testing.expect(t, !ok, "nil data should fail")
}

@(test)
test_trading_readiness_parse_nil_result :: proc(t: ^testing.T) {
	data := transmute([]u8)string(`{"control_plane":{"state":"active"}}`)
	ok := trading_readiness_parse_json(data, nil)
	testing.expect(t, !ok, "nil result should fail")
}

@(test)
test_trading_readiness_parse_invalid_json :: proc(t: ^testing.T) {
	data := transmute([]u8)string("{{bad")
	result: Trading_Readiness_Result
	ok := trading_readiness_parse_json(data, &result)
	testing.expect(t, !ok, "invalid JSON should fail")
}

@(test)
test_trading_status_label :: proc(t: ^testing.T) {
	testing.expect_value(t, trading_status_label(.Enabled), "ENABLED")
	testing.expect_value(t, trading_status_label(.Degraded), "DEGRADED")
	testing.expect_value(t, trading_status_label(.Disabled), "DISABLED")
	testing.expect_value(t, trading_status_label(.Halted), "HALTED")
	testing.expect_value(t, trading_status_label(.Unknown), "UNKNOWN")
}

@(test)
test_readiness_has_flag :: proc(t: ^testing.T) {
	result: Trading_Readiness_Result
	result.flag_count = 2
	result.safety_flags[0] = "halted"
	result.safety_flags[1] = "simulation"

	testing.expect(t, readiness_has_flag(&result, "halted"), "should find halted")
	testing.expect(t, readiness_has_flag(&result, "simulation"), "should find simulation")
	testing.expect(t, !readiness_has_flag(&result, "paused"), "should not find paused")
	testing.expect(t, !readiness_has_flag(nil, "halted"), "nil should return false")
}

// ---------------------------------------------------------------------------
// S79: Staleness threshold from server
// ---------------------------------------------------------------------------

@(test)
test_trading_readiness_staleness_threshold_from_server :: proc(t: ^testing.T) {
	json := `{"control_plane":{"state":"active","disabled_strategies":[],"disabled_adapters":[],"updated_at_ms":100},"accounts":[],"safety_flags":["clear"],"evaluated_at_ms":100,"staleness_threshold_ms":300000}`
	data := transmute([]u8)json

	result: Trading_Readiness_Result
	ok := trading_readiness_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.staleness_threshold_ms, i64(300000))
}

@(test)
test_trading_readiness_staleness_threshold_absent :: proc(t: ^testing.T) {
	// When field is missing, should default to 0.
	json := `{"control_plane":{"state":"active","disabled_strategies":[],"disabled_adapters":[],"updated_at_ms":100},"accounts":[],"safety_flags":["clear"],"evaluated_at_ms":100}`
	data := transmute([]u8)json

	result: Trading_Readiness_Result
	ok := trading_readiness_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.staleness_threshold_ms, i64(0))
}
