package services

import "core:testing"

// S58: Tests for instrument_overview_parse_json — validates deserialization of the
// backend-owned composed read model.

@(test)
test_overview_parse_minimal :: proc(t: ^testing.T) {
	json := `{"venue":"binance","instrument":"BTC-USDT","status":"ready","checked_at":1000,"readiness":{"status":"ready"},"freshness":{"status":"flowing","active":true,"channels":{}},"resync":{"status":"stable","resync_total":0,"drops_total":0,"streams":2,"max_lag_ms":5},"artifacts":[]}`
	data := transmute([]u8)json
	result: Instrument_Overview_Result
	ok := instrument_overview_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.venue, "binance")
	testing.expect_value(t, result.instrument, "BTC-USDT")
	testing.expect_value(t, result.status, "ready")
	testing.expect_value(t, result.checked_at, i64(1000))
	testing.expect_value(t, result.readiness_status, "ready")
	testing.expect_value(t, result.freshness_status, "flowing")
	testing.expect_value(t, result.freshness_active, true)
	testing.expect_value(t, result.channel_count, 0)
	testing.expect_value(t, result.resync_status, "stable")
	testing.expect_value(t, result.streams, 2)
	testing.expect_value(t, result.max_lag_ms, i64(5))
	testing.expect_value(t, result.artifact_count, 0)
}

@(test)
test_overview_parse_with_channels :: proc(t: ^testing.T) {
	json := `{"venue":"binance","instrument":"BTC-USDT","status":"degraded","checked_at":2000,"readiness":{"status":"ready"},"freshness":{"status":"stale","active":false,"channels":{"marketdata.trade":{"last_event_ts":1900,"lag_ms":100,"flowing":true},"aggregation.candle":{"last_event_ts":1800,"lag_ms":200,"flowing":false}}},"resync":{"status":"recovering","resync_total":3,"drops_total":1,"streams":4,"max_lag_ms":200},"artifacts":[]}`
	data := transmute([]u8)json
	result: Instrument_Overview_Result
	ok := instrument_overview_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.status, "degraded")
	testing.expect_value(t, result.freshness_status, "stale")
	testing.expect_value(t, result.freshness_active, false)
	testing.expect_value(t, result.channel_count, 2)
	testing.expect_value(t, result.resync_status, "recovering")
	testing.expect_value(t, result.resync_total, u64(3))
	testing.expect_value(t, result.drops_total, u64(1))

	// Verify channels are present (order from map is non-deterministic).
	found_trade := false
	found_candle := false
	for ci in 0 ..< result.channel_count {
		ch := result.channels[ci]
		if ch.name == "marketdata.trade" {
			found_trade = true
			testing.expect(t, ch.flowing, "trade channel should be flowing")
			testing.expect_value(t, ch.lag_ms, i64(100))
		}
		if ch.name == "aggregation.candle" {
			found_candle = true
			testing.expect(t, !ch.flowing, "candle channel should be stale")
			testing.expect_value(t, ch.lag_ms, i64(200))
		}
	}
	testing.expect(t, found_trade, "should have trade channel")
	testing.expect(t, found_candle, "should have candle channel")
}

@(test)
test_overview_parse_with_artifacts :: proc(t: ^testing.T) {
	json := `{"venue":"bybit","instrument":"ETH-USDT","status":"ready","checked_at":3000,"readiness":{"status":"ready"},"freshness":{"status":"flowing","active":true,"channels":{}},"resync":{"status":"stable","resync_total":0,"drops_total":0,"streams":1,"max_lag_ms":0},"artifacts":[{"name":"candle","endpoint":"/api/v1/candles","timeframes":["1s","5s","1m","5m","15m","30m","1h","4h","1d"],"timeline":{"timeframe":"1m","first_ts":1000,"last_ts":2000,"status":"available"}},{"name":"stats","endpoint":"/api/v1/stats","timeframes":["1m","5m"],"timeline":{"timeframe":"1m","first_ts":0,"last_ts":0,"status":"empty"}}]}`
	data := transmute([]u8)json
	result: Instrument_Overview_Result
	ok := instrument_overview_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.artifact_count, 2)

	// Artifacts are in order from JSON array.
	testing.expect_value(t, result.artifacts[0].name, "candle")
	testing.expect_value(t, result.artifacts[0].endpoint, "/api/v1/candles")
	testing.expect_value(t, result.artifacts[0].tf_count, 9)
	testing.expect_value(t, result.artifacts[0].timeline.timeframe, "1m")
	testing.expect_value(t, result.artifacts[0].timeline.first_ts, i64(1000))
	testing.expect_value(t, result.artifacts[0].timeline.last_ts, i64(2000))
	testing.expect_value(t, result.artifacts[0].timeline.status, "available")

	testing.expect_value(t, result.artifacts[1].name, "stats")
	testing.expect_value(t, result.artifacts[1].tf_count, 2)
	testing.expect_value(t, result.artifacts[1].timeline.status, "empty")
}

@(test)
test_overview_parse_empty_data :: proc(t: ^testing.T) {
	result: Instrument_Overview_Result
	ok := instrument_overview_parse_json(nil, &result)
	testing.expect(t, !ok, "nil data should fail")
}

@(test)
test_overview_parse_invalid_json :: proc(t: ^testing.T) {
	data := transmute([]u8)string("not json at all")
	result: Instrument_Overview_Result
	ok := instrument_overview_parse_json(data, &result)
	testing.expect(t, !ok, "invalid JSON should fail")
}

@(test)
test_overview_parse_nil_result :: proc(t: ^testing.T) {
	json := `{"venue":"x","instrument":"y","status":"ready","checked_at":0,"readiness":{"status":"ready"},"freshness":{"status":"flowing","active":true,"channels":{}},"resync":{"status":"stable","resync_total":0,"drops_total":0,"streams":0,"max_lag_ms":0},"artifacts":[]}`
	data := transmute([]u8)json
	ok := instrument_overview_parse_json(data, nil)
	testing.expect(t, !ok, "nil result should fail")
}

@(test)
test_overview_parse_not_ready_inactive :: proc(t: ^testing.T) {
	json := `{"venue":"kraken","instrument":"XBT-USD","status":"not_ready","checked_at":500,"readiness":{"status":"not_ready"},"freshness":{"status":"inactive","active":false,"channels":{}},"resync":{"status":"stable","resync_total":0,"drops_total":0,"streams":0,"max_lag_ms":0},"artifacts":[]}`
	data := transmute([]u8)json
	result: Instrument_Overview_Result
	ok := instrument_overview_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.status, "not_ready")
	testing.expect_value(t, result.readiness_status, "not_ready")
	testing.expect_value(t, result.freshness_status, "inactive")
	testing.expect_value(t, result.freshness_active, false)
}
