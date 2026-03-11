package services

import "core:testing"

// S59: Tests for delivery_health_parse_json — validates deserialization of the
// backend-owned delivery health dashboard read model.

@(test)
test_delivery_health_parse_ready :: proc(t: ^testing.T) {
	json := `{"server_time_ms":1700000000000,"status":"ready","readiness":{"status":"ready"},"freshness":{"status":"flowing","active_instruments":6,"stale_instruments":0,"flowing_channels":12,"stale_channels":0,"checked_at":1700000000000},"resync":{"status":"stable","connections_active":2,"streams":12,"resync_total":0,"drops_total":0,"max_lag_ms":5},"artifacts":[{"name":"candle","endpoint":"/api/v1/candles","default_timeframe":"1m","timeframes":["1s","5s","1m","5m","15m","30m","1h","4h","1d"],"coverage":{"status":"available","total_instruments":6,"available_instruments":6,"empty_instruments":0,"unavailable_instruments":0}},{"name":"stats","endpoint":"/api/v1/stats","default_timeframe":"1m","timeframes":["1m","5m"],"coverage":{"status":"available","total_instruments":6,"available_instruments":6,"empty_instruments":0,"unavailable_instruments":0}}],"summary":{"venues":3,"instruments":6}}`
	data := transmute([]u8)json

	result: Delivery_Health_Result
	ok := delivery_health_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.server_time_ms, i64(1700000000000))
	testing.expect_value(t, result.status, "ready")
	testing.expect_value(t, result.readiness_status, "ready")
	// Freshness.
	testing.expect_value(t, result.freshness.status, "flowing")
	testing.expect_value(t, result.freshness.active_instruments, 6)
	testing.expect_value(t, result.freshness.stale_instruments, 0)
	testing.expect_value(t, result.freshness.flowing_channels, 12)
	testing.expect_value(t, result.freshness.stale_channels, 0)
	// Resync.
	testing.expect_value(t, result.resync.status, "stable")
	testing.expect_value(t, result.resync.connections_active, i64(2))
	testing.expect_value(t, result.resync.streams, 12)
	testing.expect_value(t, result.resync.resync_total, u64(0))
	testing.expect_value(t, result.resync.drops_total, u64(0))
	testing.expect_value(t, result.resync.max_lag_ms, i64(5))
	// Artifacts.
	testing.expect_value(t, result.artifact_count, 2)
	testing.expect_value(t, result.artifacts[0].name, "candle")
	testing.expect_value(t, result.artifacts[0].tf_count, 9)
	testing.expect_value(t, result.artifacts[0].coverage.status, "available")
	testing.expect_value(t, result.artifacts[0].coverage.available_instruments, 6)
	testing.expect_value(t, result.artifacts[1].name, "stats")
	testing.expect_value(t, result.artifacts[1].tf_count, 2)
	// Summary.
	testing.expect_value(t, result.summary.venues, 3)
	testing.expect_value(t, result.summary.instruments, 6)
}

@(test)
test_delivery_health_parse_degraded :: proc(t: ^testing.T) {
	json := `{"server_time_ms":1700000001000,"status":"degraded","readiness":{"status":"ready"},"freshness":{"status":"partial","active_instruments":4,"stale_instruments":2,"flowing_channels":8,"stale_channels":4,"checked_at":1700000001000},"resync":{"status":"recovering","connections_active":2,"streams":12,"resync_total":3,"drops_total":1,"max_lag_ms":200},"artifacts":[],"summary":{"venues":3,"instruments":6}}`
	data := transmute([]u8)json

	result: Delivery_Health_Result
	ok := delivery_health_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.status, "degraded")
	testing.expect_value(t, result.freshness.status, "partial")
	testing.expect_value(t, result.freshness.active_instruments, 4)
	testing.expect_value(t, result.freshness.stale_instruments, 2)
	testing.expect_value(t, result.resync.status, "recovering")
	testing.expect_value(t, result.resync.resync_total, u64(3))
	testing.expect_value(t, result.resync.drops_total, u64(1))
	testing.expect_value(t, result.artifact_count, 0)
}

@(test)
test_delivery_health_parse_not_ready :: proc(t: ^testing.T) {
	json := `{"server_time_ms":500,"status":"not_ready","readiness":{"status":"not_ready"},"freshness":{"status":"inactive","active_instruments":0,"stale_instruments":0,"flowing_channels":0,"stale_channels":0,"checked_at":500},"resync":{"status":"stable","connections_active":0,"streams":0,"resync_total":0,"drops_total":0,"max_lag_ms":0},"artifacts":[],"summary":{"venues":0,"instruments":0}}`
	data := transmute([]u8)json

	result: Delivery_Health_Result
	ok := delivery_health_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.status, "not_ready")
	testing.expect_value(t, result.readiness_status, "not_ready")
	testing.expect_value(t, result.freshness.status, "inactive")
	testing.expect_value(t, result.summary.venues, 0)
	testing.expect_value(t, result.summary.instruments, 0)
}

@(test)
test_delivery_health_parse_artifact_coverage_partial :: proc(t: ^testing.T) {
	json := `{"server_time_ms":2000,"status":"ready","readiness":{"status":"ready"},"freshness":{"status":"flowing","active_instruments":6,"stale_instruments":0,"flowing_channels":12,"stale_channels":0,"checked_at":2000},"resync":{"status":"stable","connections_active":1,"streams":6,"resync_total":0,"drops_total":0,"max_lag_ms":0},"artifacts":[{"name":"candle","endpoint":"/api/v1/candles","default_timeframe":"1m","timeframes":["1m"],"coverage":{"status":"partial","total_instruments":6,"available_instruments":4,"empty_instruments":1,"unavailable_instruments":1}}],"summary":{"venues":2,"instruments":6}}`
	data := transmute([]u8)json

	result: Delivery_Health_Result
	ok := delivery_health_parse_json(data, &result)
	testing.expect(t, ok, "parse should succeed")
	testing.expect_value(t, result.artifact_count, 1)
	c := result.artifacts[0].coverage
	testing.expect_value(t, c.status, "partial")
	testing.expect_value(t, c.total_instruments, 6)
	testing.expect_value(t, c.available_instruments, 4)
	testing.expect_value(t, c.empty_instruments, 1)
	testing.expect_value(t, c.unavailable_instruments, 1)
}

@(test)
test_delivery_health_parse_empty_data :: proc(t: ^testing.T) {
	result: Delivery_Health_Result
	ok := delivery_health_parse_json(nil, &result)
	testing.expect(t, !ok, "nil data should fail")
}

@(test)
test_delivery_health_parse_invalid_json :: proc(t: ^testing.T) {
	data := transmute([]u8)string("not json at all")
	result: Delivery_Health_Result
	ok := delivery_health_parse_json(data, &result)
	testing.expect(t, !ok, "invalid JSON should fail")
}

@(test)
test_delivery_health_parse_nil_result :: proc(t: ^testing.T) {
	json := `{"server_time_ms":0,"status":"ready","readiness":{"status":"ready"},"freshness":{"status":"flowing","active_instruments":0,"stale_instruments":0,"flowing_channels":0,"stale_channels":0,"checked_at":0},"resync":{"status":"stable","connections_active":0,"streams":0,"resync_total":0,"drops_total":0,"max_lag_ms":0},"artifacts":[],"summary":{"venues":0,"instruments":0}}`
	data := transmute([]u8)json
	ok := delivery_health_parse_json(data, nil)
	testing.expect(t, !ok, "nil result should fail")
}
