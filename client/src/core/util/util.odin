package util

// Formatting utilities migrated from MarketMonkey (pure).

import "core:fmt"
import "core:math"
import "core:strings"

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
