package services

import "core:testing"

@(test)
test_log_push_and_get :: proc(t: ^testing.T) {
	buf: Log_Buffer

	log_push(&buf, .INFO, "hello", 1)
	log_push(&buf, .WARN, "world", 2)
	log_push(&buf, .ERR, "error", 3)

	testing.expect_value(t, log_count(&buf), 3)

	// Offset 0 = newest.
	e0, ok0 := log_get(&buf, 0)
	testing.expect(t, ok0)
	testing.expect_value(t, e0.level, Log_Level.ERR)
	testing.expect_value(t, string(e0.text[:e0.text_len]), "error")
	testing.expect_value(t, e0.frame, u64(3))

	e1, ok1 := log_get(&buf, 1)
	testing.expect(t, ok1)
	testing.expect_value(t, e1.level, Log_Level.WARN)
	testing.expect_value(t, string(e1.text[:e1.text_len]), "world")

	e2, ok2 := log_get(&buf, 2)
	testing.expect(t, ok2)
	testing.expect_value(t, e2.level, Log_Level.INFO)
	testing.expect_value(t, string(e2.text[:e2.text_len]), "hello")
}

@(test)
test_log_ring_overflow :: proc(t: ^testing.T) {
	buf: Log_Buffer

	// Push more than LOG_BUFFER_CAP entries.
	for i in 0 ..< LOG_BUFFER_CAP + 10 {
		frame_buf: [8]u8
		n := 0
		v := i
		if v == 0 {
			frame_buf[0] = '0'
			n = 1
		} else {
			// Simple int-to-string for test.
			digits: [8]u8
			dc := 0
			for v > 0 {
				digits[dc] = u8('0' + v % 10)
				dc += 1
				v /= 10
			}
			for di in 0 ..< dc {
				frame_buf[n] = digits[dc - 1 - di]
				n += 1
			}
		}
		log_push(&buf, .INFO, string(frame_buf[:n]), u64(i))
	}

	// Count should be capped.
	testing.expect_value(t, log_count(&buf), LOG_BUFFER_CAP)

	// Newest entry should be the last pushed.
	newest, ok := log_get(&buf, 0)
	testing.expect(t, ok)
	testing.expect_value(t, newest.frame, u64(LOG_BUFFER_CAP + 9))
}

@(test)
test_log_get_out_of_range :: proc(t: ^testing.T) {
	buf: Log_Buffer

	// Empty buffer.
	_, ok0 := log_get(&buf, 0)
	testing.expect(t, !ok0)

	// Negative offset.
	_, ok_neg := log_get(&buf, -1)
	testing.expect(t, !ok_neg)

	// Push one, then access beyond count.
	log_push(&buf, .INFO, "x", 1)
	_, ok_beyond := log_get(&buf, 1)
	testing.expect(t, !ok_beyond)

	// Valid access.
	_, ok_valid := log_get(&buf, 0)
	testing.expect(t, ok_valid)
}
