package streams

import "core:testing"

@(test)
test_registry_subscribe_unsubscribe_refcount_transitions :: proc(t: ^testing.T) {
	reg: Stream_Registry
	registry_init(&reg, true)

	stream_id_buf: [STREAM_ID_CAP]u8
	sid := format_stream_id_into(stream_id_buf[:], "binance", "BTCUSDT:SPOT", "")

	h1 := registry_acquire(&reg, sid, "binance", "BTCUSDT:SPOT", "SPOT")
	testing.expect(t, h1 != nil)
	testing.expect_value(t, h1.ref_count, 1)
	testing.expect_value(t, h1.paused, false)

	h2 := registry_acquire(&reg, sid, "binance", "BTCUSDT:SPOT", "SPOT")
	testing.expect(t, h2 != nil)
	testing.expect_value(t, h2.ref_count, 2)
	testing.expect_value(t, h2.paused, false)

	ok_first_release := registry_release(&reg, sid)
	testing.expect_value(t, ok_first_release, true)
	testing.expect_value(t, h2.ref_count, 1)
	testing.expect_value(t, h2.paused, false)

	ok_second_release := registry_release(&reg, sid)
	testing.expect_value(t, ok_second_release, true)
	testing.expect_value(t, h2.ref_count, 0)
	testing.expect_value(t, h2.paused, true)
}
