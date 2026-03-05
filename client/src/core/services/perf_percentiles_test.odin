package services

import "core:testing"

@(test)
test_ring_percentiles_p95_p99_sorted_values :: proc(t: ^testing.T) {
	samples: [8]i64 = {100, 200, 300, 400, 500, 600, 700, 800}
	p95 := ring_percentile_i64(samples, 8, 8, 95)
	p99 := ring_percentile_i64(samples, 8, 8, 99)
	testing.expect_value(t, p95, i64(800))
	testing.expect_value(t, p99, i64(800))
}

@(test)
test_ring_percentile_wraparound :: proc(t: ^testing.T) {
	// head=2,count=4 in this ring means logical window: [40, 50, 20, 30].
	samples: [6]i64 = {20, 30, 0, 0, 40, 50}
	p50 := ring_percentile_i64(samples, 2, 4, 50)
	p95 := ring_percentile_i64(samples, 2, 4, 95)
	testing.expect_value(t, p50, i64(40))
	testing.expect_value(t, p95, i64(50))
}

@(test)
test_ring_percentile_clamps_pct :: proc(t: ^testing.T) {
	samples: [4]i64 = {7, 9, 11, 13}
	low := ring_percentile_i64(samples, 4, 4, -10)
	high := ring_percentile_i64(samples, 4, 4, 999)
	testing.expect_value(t, low, i64(7))
	testing.expect_value(t, high, i64(13))
}
