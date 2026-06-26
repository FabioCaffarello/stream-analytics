package util

// Formatting and timestamp utilities.

import "core:fmt"
import "core:math"
import "core:strings"

// Case-insensitive prefix check (ASCII only).
has_prefix_ci :: proc(s: string, prefix: string) -> bool {
	if len(prefix) > len(s) do return false
	for i in 0 ..< len(prefix) {
		a := s[i]
		b := prefix[i]
		if a >= 'A' && a <= 'Z' do a += 32
		if b >= 'A' && b <= 'Z' do b += 32
		if a != b do return false
	}
	return true
}

// Backend envelopes/payloads use unix milliseconds; core widgets use unix seconds.
// Timestamps above this threshold are assumed to be milliseconds.
UNIX_MS_THRESHOLD :: i64(10_000_000_000)

normalize_unix_seconds :: proc(ts: i64) -> i64 {
	if ts > UNIX_MS_THRESHOLD do return ts / 1000
	return ts
}

// SAFETY: returns temp_allocator string, valid until free_all(context.temp_allocator).
format_price :: proc(price: f64, tick_size: f64) -> string {
	decimal_places := -math.floor(math.log10(tick_size))
	switch decimal_places {
	case 1: return fmt.tprintf("%.1f", price)
	case 2: return fmt.tprintf("%.2f", price)
	case 3: return fmt.tprintf("%.3f", price)
	case 4: return fmt.tprintf("%.4f", price)
	case 5: return fmt.tprintf("%.5f", price)
	case 6: return fmt.tprintf("%.6f", price)
	case 7: return fmt.tprintf("%.7f", price)
	}
	return fmt.tprintf("%.0f", price)
}

// SAFETY: returns temp_allocator string by default, valid until free_all(context.temp_allocator).
// The allocator parameter only affects the final trimmed clone path.
format_value :: proc(value: f64, allocator := context.temp_allocator) -> string {
	if value == 0 {
		return "0"
	}
	if math.floor(value) == value {
		return fmt.tprintf("%d", i64(value))
	}
	str := fmt.tprintf("%.15f", value)
	trimmed := strings.trim_right(str, "0")
	if strings.contains_rune(str, '.') && !strings.contains_rune(trimmed, '.') {
		return fmt.tprintf("%s.", trimmed)
	}
	return strings.clone(trimmed, allocator)
}

// SAFETY: returns temp_allocator string, valid until free_all(context.temp_allocator).
format_size :: proc(size: f64) -> string {
	is_integer :: proc(n: f64) -> bool {
		return math.abs(n - math.floor(n)) < 0.000001
	}

	if size < 1000 {
		if is_integer(size) { return fmt.tprintf("%d", int(size)) }
		return fmt.tprintf("%.3f", size)
	}
	if size < 1_000_000 {
		value := size / 1000
		if is_integer(value) { return fmt.tprintf("%dK", int(value)) }
		return fmt.tprintf("%.2fK", value)
	}
	if size < 1_000_000_000 {
		value := size / 1_000_000
		if is_integer(value) { return fmt.tprintf("%dM", int(value)) }
		return fmt.tprintf("%.2fM", value)
	}
	if size < 1_000_000_000_000 {
		value := size / 1_000_000_000
		if is_integer(value) { return fmt.tprintf("%dB", int(value)) }
		return fmt.tprintf("%.2fB", value)
	}
	value := size / 1_000_000_000_000
	if is_integer(value) { return fmt.tprintf("%dT", int(value)) }
	return fmt.tprintf("%.2fT", value)
}
