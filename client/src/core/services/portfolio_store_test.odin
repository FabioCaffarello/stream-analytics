package services

import "core:strings"
import "core:testing"

// S74: Tests for portfolio data layer parsers — validates deserialization of the
// three backend portfolio read model endpoints.

// ---------------------------------------------------------------------------
// Portfolio State
// ---------------------------------------------------------------------------

@(test)
test_portfolio_state_parse_full :: proc(t: ^testing.T) {
	json := `{"state_id":"st-001","scope":"venue_account","account_id":"acc-1","venue":"binance","projected_at_ms":1700000000000,"balances":[{"asset":"USDT","total":10000.0,"available":8000.0,"locked":2000.0}],"positions":[{"venue":"binance","symbol":"BTCUSDT","side":"long","quantity":0.5,"avg_entry_price":42000.0,"notional_usd":21000.0,"realized_pnl":500.0,"unrealized_pnl":1000.0,"trade_count":12,"volume_traded_usd":84000.0,"last_fill_ms":1699999999000}],"exposures":[{"symbol":"BTCUSDT","net_qty":0.5,"gross_notional_usd":21000.0,"leverage":2.1}],"equity_usd":11000.0,"realized_pnl_usd":500.0,"unrealized_pnl_usd":1000.0,"risk":{"margin_used_usd":5000.0,"margin_available_usd":5000.0,"maintenance_margin_usd":2500.0,"var_95_usd":800.0},"fill_summary":{"total_trade_count":12,"total_volume_traded_usd":84000.0,"win_count":8,"loss_count":4,"largest_win_usd":200.0,"largest_loss_usd":50.0,"turnover_usd":84000.0},"provenance":{"source_execution_event_id":"evt-123","source_execution_seq":42,"correlation_id":"corr-abc","trace_id":"trace-xyz","projector_version":"1.0.0"}}`
	data := transmute([]u8)json

	result: Portfolio_State_Result
	ok, truncated := portfolio_state_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect(t, truncated == {}, "no truncation expected")
	testing.expect_value(t, result.state_id, "st-001")
	testing.expect_value(t, result.scope, "venue_account")
	testing.expect_value(t, result.account_id, "acc-1")
	testing.expect_value(t, result.venue, "binance")
	testing.expect_value(t, result.projected_at_ms, i64(1700000000000))
	testing.expect_value(t, result.equity_usd, 11000.0)
	testing.expect_value(t, result.realized_pnl_usd, 500.0)
	testing.expect_value(t, result.unrealized_pnl_usd, 1000.0)
	// Balances.
	testing.expect_value(t, result.balance_count, 1)
	testing.expect_value(t, result.balances[0].asset, "USDT")
	testing.expect_value(t, result.balances[0].total, 10000.0)
	testing.expect_value(t, result.balances[0].available, 8000.0)
	testing.expect_value(t, result.balances[0].locked, 2000.0)
	// Positions.
	testing.expect_value(t, result.position_count, 1)
	testing.expect_value(t, result.positions[0].venue, "binance")
	testing.expect_value(t, result.positions[0].symbol, "BTCUSDT")
	testing.expect_value(t, result.positions[0].side, "long")
	testing.expect_value(t, result.positions[0].quantity, 0.5)
	testing.expect_value(t, result.positions[0].avg_entry_price, 42000.0)
	testing.expect_value(t, result.positions[0].trade_count, i32(12))
	testing.expect_value(t, result.positions[0].last_fill_ms, i64(1699999999000))
	// Exposures.
	testing.expect_value(t, result.exposure_count, 1)
	testing.expect_value(t, result.exposures[0].symbol, "BTCUSDT")
	testing.expect_value(t, result.exposures[0].leverage, 2.1)
	// Risk.
	testing.expect_value(t, result.risk.margin_used_usd, 5000.0)
	testing.expect_value(t, result.risk.margin_available_usd, 5000.0)
	testing.expect_value(t, result.risk.var_95_usd, 800.0)
	// Fill summary.
	testing.expect_value(t, result.fill_summary.total_trade_count, i32(12))
	testing.expect_value(t, result.fill_summary.win_count, i32(8))
	testing.expect_value(t, result.fill_summary.loss_count, i32(4))
	// Provenance.
	testing.expect_value(t, result.provenance.source_execution_event_id, "evt-123")
	testing.expect_value(t, result.provenance.source_execution_seq, i64(42))
	testing.expect_value(t, result.provenance.projector_version, "1.0.0")
}

@(test)
test_portfolio_state_parse_empty_arrays :: proc(t: ^testing.T) {
	json := `{"state_id":"st-002","scope":"account","account_id":"acc-2","venue":"bybit","projected_at_ms":100,"balances":[],"positions":[],"exposures":[],"equity_usd":0,"realized_pnl_usd":0,"unrealized_pnl_usd":0,"risk":{"margin_used_usd":0,"margin_available_usd":0,"maintenance_margin_usd":0,"var_95_usd":0},"fill_summary":{"total_trade_count":0,"total_volume_traded_usd":0,"win_count":0,"loss_count":0,"largest_win_usd":0,"largest_loss_usd":0,"turnover_usd":0},"provenance":{"source_execution_event_id":"e1","source_execution_seq":1,"correlation_id":"c1","trace_id":"t1","projector_version":"1.0"}}`
	data := transmute([]u8)json

	result: Portfolio_State_Result
	ok, _ := portfolio_state_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed with empty arrays")
	testing.expect_value(t, result.balance_count, 0)
	testing.expect_value(t, result.position_count, 0)
	testing.expect_value(t, result.exposure_count, 0)
}

@(test)
test_portfolio_state_parse_nil_data :: proc(t: ^testing.T) {
	result: Portfolio_State_Result
	ok, _ := portfolio_state_parse_json(nil, &result)
	testing.expect(t, !ok, "nil data should fail")
}

@(test)
test_portfolio_state_parse_nil_result :: proc(t: ^testing.T) {
	data := transmute([]u8)string(`{"state_id":"x"}`)
	ok, _ := portfolio_state_parse_json(data, nil)
	testing.expect(t, !ok, "nil result should fail")
}

@(test)
test_portfolio_state_parse_invalid_json :: proc(t: ^testing.T) {
	data := transmute([]u8)string("not json")
	result: Portfolio_State_Result
	ok, _ := portfolio_state_parse_json(data, &result)
	testing.expect(t, !ok, "invalid JSON should fail")
}

// ---------------------------------------------------------------------------
// Account Snapshot
// ---------------------------------------------------------------------------

@(test)
test_portfolio_account_snapshot_parse_full :: proc(t: ^testing.T) {
	json := `{"snapshot_id":"snap-001","account_id":"acc-1","projected_at_ms":1700000000000,"venues":[{"venue":"binance","positions":[{"venue":"binance","symbol":"BTCUSDT","side":"long","quantity":0.5,"avg_entry_price":42000.0,"notional_usd":21000.0,"realized_pnl":500.0,"unrealized_pnl":1000.0,"trade_count":12,"volume_traded_usd":84000.0,"last_fill_ms":1699999999000}],"balances":[{"asset":"USDT","total":10000.0,"available":8000.0,"locked":2000.0}],"equity_usd":11000.0,"realized_pnl_usd":500.0,"unrealized_pnl_usd":1000.0,"margin_used_usd":5000.0},{"venue":"bybit","positions":[],"balances":[{"asset":"USDC","total":5000.0,"available":5000.0,"locked":0}],"equity_usd":5000.0,"realized_pnl_usd":0,"unrealized_pnl_usd":0,"margin_used_usd":0}],"total_equity_usd":16000.0,"total_realized_usd":500.0,"total_unrealized_usd":1000.0,"total_margin_used_usd":5000.0,"total_leverage":1.3,"fill_summary":{"total_trade_count":12,"total_volume_traded_usd":84000.0,"win_count":8,"loss_count":4,"largest_win_usd":200.0,"largest_loss_usd":50.0,"turnover_usd":84000.0}}`
	data := transmute([]u8)json

	result: Portfolio_Account_Snapshot_Result
	ok, truncated := portfolio_account_snapshot_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect(t, truncated == {}, "no truncation expected")
	testing.expect_value(t, result.snapshot_id, "snap-001")
	testing.expect_value(t, result.account_id, "acc-1")
	testing.expect_value(t, result.projected_at_ms, i64(1700000000000))
	testing.expect_value(t, result.total_equity_usd, 16000.0)
	testing.expect_value(t, result.total_leverage, 1.3)
	// Venues.
	testing.expect_value(t, result.venue_count, 2)
	testing.expect_value(t, result.venues[0].venue, "binance")
	testing.expect_value(t, result.venues[0].position_count, 1)
	testing.expect_value(t, result.venues[0].balance_count, 1)
	testing.expect_value(t, result.venues[0].equity_usd, 11000.0)
	testing.expect_value(t, result.venues[0].margin_used_usd, 5000.0)
	testing.expect_value(t, result.venues[1].venue, "bybit")
	testing.expect_value(t, result.venues[1].position_count, 0)
	testing.expect_value(t, result.venues[1].balance_count, 1)
	// Fill summary.
	testing.expect_value(t, result.fill_summary.total_trade_count, i32(12))
}

@(test)
test_portfolio_account_snapshot_parse_nil_data :: proc(t: ^testing.T) {
	result: Portfolio_Account_Snapshot_Result
	ok, _ := portfolio_account_snapshot_parse_json(nil, &result)
	testing.expect(t, !ok, "nil data should fail")
}

@(test)
test_portfolio_account_snapshot_parse_nil_result :: proc(t: ^testing.T) {
	data := transmute([]u8)string(`{"snapshot_id":"x"}`)
	ok, _ := portfolio_account_snapshot_parse_json(data, nil)
	testing.expect(t, !ok, "nil result should fail")
}

@(test)
test_portfolio_account_snapshot_parse_invalid_json :: proc(t: ^testing.T) {
	data := transmute([]u8)string("broken")
	result: Portfolio_Account_Snapshot_Result
	ok, _ := portfolio_account_snapshot_parse_json(data, &result)
	testing.expect(t, !ok, "invalid JSON should fail")
}

// ---------------------------------------------------------------------------
// Portfolio Summary
// ---------------------------------------------------------------------------

@(test)
test_portfolio_summary_parse_full :: proc(t: ^testing.T) {
	json := `{"summary_id":"sum-001","projected_at_ms":1700000000000,"accounts":[{"account_id":"acc-1","venue_count":2,"position_count":3,"equity_usd":16000.0,"realized_pnl_usd":500.0,"unrealized_pnl_usd":1000.0},{"account_id":"acc-2","venue_count":1,"position_count":1,"equity_usd":5000.0,"realized_pnl_usd":100.0,"unrealized_pnl_usd":-50.0}],"global_equity_usd":21000.0,"global_realized_usd":600.0,"global_unrealized_usd":950.0,"global_margin_used_usd":7000.0,"global_leverage":1.5,"total_position_count":4,"total_open_orders":2,"fill_summary":{"total_trade_count":20,"total_volume_traded_usd":150000.0,"win_count":14,"loss_count":6,"largest_win_usd":300.0,"largest_loss_usd":80.0,"turnover_usd":150000.0}}`
	data := transmute([]u8)json

	result: Portfolio_Summary_Result
	ok, truncated := portfolio_summary_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect(t, truncated == {}, "no truncation expected")
	testing.expect_value(t, result.summary_id, "sum-001")
	testing.expect_value(t, result.projected_at_ms, i64(1700000000000))
	testing.expect_value(t, result.global_equity_usd, 21000.0)
	testing.expect_value(t, result.global_realized_usd, 600.0)
	testing.expect_value(t, result.global_unrealized_usd, 950.0)
	testing.expect_value(t, result.global_margin_used_usd, 7000.0)
	testing.expect_value(t, result.global_leverage, 1.5)
	testing.expect_value(t, result.total_position_count, i32(4))
	testing.expect_value(t, result.total_open_orders, i32(2))
	// Accounts.
	testing.expect_value(t, result.account_count, 2)
	testing.expect_value(t, result.accounts[0].account_id, "acc-1")
	testing.expect_value(t, result.accounts[0].venue_count, i32(2))
	testing.expect_value(t, result.accounts[0].position_count, i32(3))
	testing.expect_value(t, result.accounts[0].equity_usd, 16000.0)
	testing.expect_value(t, result.accounts[1].account_id, "acc-2")
	testing.expect_value(t, result.accounts[1].unrealized_pnl_usd, -50.0)
	// Fill summary.
	testing.expect_value(t, result.fill_summary.total_trade_count, i32(20))
	testing.expect_value(t, result.fill_summary.win_count, i32(14))
	testing.expect_value(t, result.fill_summary.turnover_usd, 150000.0)
}

@(test)
test_portfolio_summary_parse_empty_accounts :: proc(t: ^testing.T) {
	json := `{"summary_id":"sum-002","projected_at_ms":100,"accounts":[],"global_equity_usd":0,"global_realized_usd":0,"global_unrealized_usd":0,"global_margin_used_usd":0,"global_leverage":0,"total_position_count":0,"total_open_orders":0,"fill_summary":{"total_trade_count":0,"total_volume_traded_usd":0,"win_count":0,"loss_count":0,"largest_win_usd":0,"largest_loss_usd":0,"turnover_usd":0}}`
	data := transmute([]u8)json

	result: Portfolio_Summary_Result
	ok, _ := portfolio_summary_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed with empty accounts")
	testing.expect_value(t, result.account_count, 0)
	testing.expect_value(t, result.global_equity_usd, 0.0)
}

@(test)
test_portfolio_summary_parse_nil_data :: proc(t: ^testing.T) {
	result: Portfolio_Summary_Result
	ok, _ := portfolio_summary_parse_json(nil, &result)
	testing.expect(t, !ok, "nil data should fail")
}

@(test)
test_portfolio_summary_parse_nil_result :: proc(t: ^testing.T) {
	data := transmute([]u8)string(`{"summary_id":"x"}`)
	ok, _ := portfolio_summary_parse_json(data, nil)
	testing.expect(t, !ok, "nil result should fail")
}

@(test)
test_portfolio_summary_parse_invalid_json :: proc(t: ^testing.T) {
	data := transmute([]u8)string("{{bad")
	result: Portfolio_Summary_Result
	ok, _ := portfolio_summary_parse_json(data, &result)
	testing.expect(t, !ok, "invalid JSON should fail")
}

// ---------------------------------------------------------------------------
// S76: Symbol Exposure Computation
// ---------------------------------------------------------------------------

@(test)
test_portfolio_compute_symbol_exposures_basic :: proc(t: ^testing.T) {
	snap: Portfolio_Account_Snapshot_Result
	snap.venue_count = 2

	// Venue 0: binance with BTCUSDT long.
	snap.venues[0].venue = "binance"
	snap.venues[0].position_count = 1
	snap.venues[0].positions[0] = Portfolio_Position{
		symbol = "BTCUSDT", side = "long", quantity = 0.5,
		notional_usd = 21000.0,
	}

	// Venue 1: bybit with BTCUSDT short + ETHUSDT long.
	snap.venues[1].venue = "bybit"
	snap.venues[1].position_count = 2
	snap.venues[1].positions[0] = Portfolio_Position{
		symbol = "BTCUSDT", side = "short", quantity = 0.3,
		notional_usd = 12600.0,
	}
	snap.venues[1].positions[1] = Portfolio_Position{
		symbol = "ETHUSDT", side = "long", quantity = 2.0,
		notional_usd = 6000.0,
	}

	out: [PORTFOLIO_SYMBOL_EXPOSURE_CAP]Portfolio_Symbol_Exposure
	count := portfolio_compute_symbol_exposures(&snap, &out)
	testing.expect_value(t, count, 2)

	// BTCUSDT: 0.5 (long) - 0.3 (short) = 0.2 net.
	testing.expect_value(t, out[0].symbol, "BTCUSDT")
	testing.expect(t, out[0].net_qty > 0.19 && out[0].net_qty < 0.21, "BTCUSDT net_qty should be ~0.2")
	testing.expect_value(t, out[0].venue_count, i32(2))
	testing.expect(t, out[0].gross_notional_usd > 33599 && out[0].gross_notional_usd < 33601,
		"BTCUSDT gross_notional should be ~33600")

	// ETHUSDT: 2.0 net.
	testing.expect_value(t, out[1].symbol, "ETHUSDT")
	testing.expect_value(t, out[1].net_qty, 2.0)
	testing.expect_value(t, out[1].venue_count, i32(1))
}

@(test)
test_portfolio_compute_symbol_exposures_nil :: proc(t: ^testing.T) {
	out: [PORTFOLIO_SYMBOL_EXPOSURE_CAP]Portfolio_Symbol_Exposure
	count := portfolio_compute_symbol_exposures(nil, &out)
	testing.expect_value(t, count, 0)
}

@(test)
test_portfolio_compute_symbol_exposures_empty :: proc(t: ^testing.T) {
	snap: Portfolio_Account_Snapshot_Result
	snap.venue_count = 1
	snap.venues[0].venue = "binance"
	snap.venues[0].position_count = 0

	out: [PORTFOLIO_SYMBOL_EXPOSURE_CAP]Portfolio_Symbol_Exposure
	count := portfolio_compute_symbol_exposures(&snap, &out)
	testing.expect_value(t, count, 0)
}

// ---------------------------------------------------------------------------
// S76: Stale Position Detection
// ---------------------------------------------------------------------------

@(test)
test_portfolio_position_is_stale_true :: proc(t: ^testing.T) {
	pos := Portfolio_Position{last_fill_ms = 1000}
	// now_ms = 400_000 → age = 399_000 > threshold 300_000.
	testing.expect(t, portfolio_position_is_stale(&pos, 400_000, 300_000), "should be stale")
}

@(test)
test_portfolio_position_is_stale_false :: proc(t: ^testing.T) {
	pos := Portfolio_Position{last_fill_ms = 200_000}
	// now_ms = 400_000 → age = 200_000 < threshold 300_000.
	testing.expect(t, !portfolio_position_is_stale(&pos, 400_000, 300_000), "should not be stale")
}

@(test)
test_portfolio_position_is_stale_nil :: proc(t: ^testing.T) {
	testing.expect(t, !portfolio_position_is_stale(nil, 400_000, 300_000), "nil should not be stale")
}

@(test)
test_portfolio_position_is_stale_zero_fill :: proc(t: ^testing.T) {
	pos := Portfolio_Position{last_fill_ms = 0}
	testing.expect(t, !portfolio_position_is_stale(&pos, 400_000, 300_000), "zero fill_ms should not be stale")
}

// ---------------------------------------------------------------------------
// S76: Exposure Divergence Detection
// ---------------------------------------------------------------------------

@(test)
test_portfolio_has_exposure_divergence_true :: proc(t: ^testing.T) {
	snap: Portfolio_Account_Snapshot_Result
	snap.venue_count = 2

	snap.venues[0].venue = "binance"
	snap.venues[0].position_count = 1
	snap.venues[0].positions[0] = Portfolio_Position{symbol = "BTCUSDT", side = "long"}

	snap.venues[1].venue = "bybit"
	snap.venues[1].position_count = 1
	snap.venues[1].positions[0] = Portfolio_Position{symbol = "BTCUSDT", side = "short"}

	testing.expect(t, portfolio_has_exposure_divergence(&snap), "opposing sides on same symbol = divergence")
}

@(test)
test_portfolio_has_exposure_divergence_false :: proc(t: ^testing.T) {
	snap: Portfolio_Account_Snapshot_Result
	snap.venue_count = 2

	snap.venues[0].venue = "binance"
	snap.venues[0].position_count = 1
	snap.venues[0].positions[0] = Portfolio_Position{symbol = "BTCUSDT", side = "long"}

	snap.venues[1].venue = "bybit"
	snap.venues[1].position_count = 1
	snap.venues[1].positions[0] = Portfolio_Position{symbol = "ETHUSDT", side = "short"}

	testing.expect(t, !portfolio_has_exposure_divergence(&snap), "different symbols = no divergence")
}

@(test)
test_portfolio_has_exposure_divergence_nil :: proc(t: ^testing.T) {
	testing.expect(t, !portfolio_has_exposure_divergence(nil), "nil should be false")
}

@(test)
test_portfolio_has_exposure_divergence_same_side :: proc(t: ^testing.T) {
	snap: Portfolio_Account_Snapshot_Result
	snap.venue_count = 2

	snap.venues[0].venue = "binance"
	snap.venues[0].position_count = 1
	snap.venues[0].positions[0] = Portfolio_Position{symbol = "BTCUSDT", side = "long"}

	snap.venues[1].venue = "bybit"
	snap.venues[1].position_count = 1
	snap.venues[1].positions[0] = Portfolio_Position{symbol = "BTCUSDT", side = "long"}

	testing.expect(t, !portfolio_has_exposure_divergence(&snap), "same side = no divergence")
}

// ---------------------------------------------------------------------------
// S79: Truncation Detection
// ---------------------------------------------------------------------------

@(test)
test_portfolio_snapshot_venue_truncation :: proc(t: ^testing.T) {
	// Build JSON with 10 venues (exceeds PORTFOLIO_VENUE_CAP=8).
	venues_json := `[`
	for i in 0 ..< 10 {
		if i > 0 do venues_json = strings.concatenate({venues_json, ","})
		venues_json = strings.concatenate({venues_json, `{"venue":"v`, venue_idx(i), `","positions":[],"balances":[],"equity_usd":0,"realized_pnl_usd":0,"unrealized_pnl_usd":0,"margin_used_usd":0}`})
	}
	venues_json = strings.concatenate({venues_json, `]`})

	full_json := strings.concatenate({`{"snapshot_id":"snap-trunc","account_id":"a","projected_at_ms":100,"venues":`, venues_json, `,"total_equity_usd":0,"total_realized_usd":0,"total_unrealized_usd":0,"total_margin_used_usd":0,"total_leverage":0,"fill_summary":{"total_trade_count":0,"total_volume_traded_usd":0,"win_count":0,"loss_count":0,"largest_win_usd":0,"largest_loss_usd":0,"turnover_usd":0}}`})

	data := transmute([]u8)full_json
	result: Portfolio_Account_Snapshot_Result
	ok, truncated := portfolio_account_snapshot_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed even with truncation")
	testing.expect(t, .Venues in truncated, "venues truncation flag should be set")
	testing.expect_value(t, result.venue_count, PORTFOLIO_VENUE_CAP)
}

@(private = "file")
venue_idx :: proc(i: int) -> string {
	digits := "0123456789"
	return digits[i:i+1]
}

// ---------------------------------------------------------------------------
// S79: Deterministic Venue Sorting
// ---------------------------------------------------------------------------

@(test)
test_portfolio_snapshot_venue_sort_determinism :: proc(t: ^testing.T) {
	// Venues arrive in reverse alphabetical order.
	json_str := `{"snapshot_id":"snap-sort","account_id":"a","projected_at_ms":100,"venues":[{"venue":"kraken","positions":[],"balances":[],"equity_usd":3000,"realized_pnl_usd":0,"unrealized_pnl_usd":0,"margin_used_usd":0},{"venue":"coinbase","positions":[],"balances":[],"equity_usd":2000,"realized_pnl_usd":0,"unrealized_pnl_usd":0,"margin_used_usd":0},{"venue":"binance","positions":[],"balances":[],"equity_usd":1000,"realized_pnl_usd":0,"unrealized_pnl_usd":0,"margin_used_usd":0}],"total_equity_usd":6000,"total_realized_usd":0,"total_unrealized_usd":0,"total_margin_used_usd":0,"total_leverage":0,"fill_summary":{"total_trade_count":0,"total_volume_traded_usd":0,"win_count":0,"loss_count":0,"largest_win_usd":0,"largest_loss_usd":0,"turnover_usd":0}}`

	data := transmute([]u8)json_str
	result: Portfolio_Account_Snapshot_Result
	ok, _ := portfolio_account_snapshot_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.venue_count, 3)

	// Should be sorted alphabetically: binance, coinbase, kraken.
	testing.expect_value(t, result.venues[0].venue, "binance")
	testing.expect_value(t, result.venues[1].venue, "coinbase")
	testing.expect_value(t, result.venues[2].venue, "kraken")

	// Verify equity follows the sorted venues.
	testing.expect_value(t, result.venues[0].equity_usd, 1000.0)
	testing.expect_value(t, result.venues[1].equity_usd, 2000.0)
	testing.expect_value(t, result.venues[2].equity_usd, 3000.0)
}
