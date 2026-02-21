package mr_math

// Math utilities migrated from MarketMonkey (pure).
// Directory: math/  Package: mr_math (avoids collision with core:math).
// Import as: import "mr:math" → identifier is mr_math.

import "core:math"

ROUND_SHIFT :: 1e8

round :: proc(value: f64) -> f64 {
	return math.round(value * ROUND_SHIFT) / ROUND_SHIFT
}

round_down_to_tick :: proc(value, tick_size: f64) -> f64 {
	f := tick_size * ROUND_SHIFT
	x := (value * ROUND_SHIFT) / f * f
	return x / ROUND_SHIFT
}

round_unix_to_timeframe :: proc(unix: i64, timeframe: i64) -> i64 {
	return unix - (unix % timeframe)
}

remap :: #force_inline proc(x, x0, x1: $T) -> T {
	return (x - x0) / (x1 - x0)
}
