package main

import "core:testing"
import "mr:ports"
import "mr:services"
import "mr:util"

@(test)
test_native_fill_signal_event_contract :: proc(t: ^testing.T) {
	parsed: services.Parsed_Signal
	parsed.subject_id = 0xD00D
	parsed.seq = 91
	parsed.unix = 1_700_000_000_777
	parsed.confidence = 0.93
	parsed.regime_strength = 0.66
	parsed.kind_len = 4
	parsed.kind[0] = 't'
	parsed.kind[1] = 'e'
	parsed.kind[2] = 's'
	parsed.kind[3] = 't'

	out: ports.MD_Event
	native_fill_signal_event(&out, parsed)

	testing.expect_value(t, out.kind, ports.MD_Event_Kind.Signal)
	testing.expect_value(t, out.source.channel, ports.MD_Channel.Signals)
	testing.expect_value(t, out.source.subject_id, parsed.subject_id)
	testing.expect_value(t, out.source.seq, parsed.seq)
	testing.expect_value(t, out.unix, util.normalize_unix_seconds(parsed.unix))
	testing.expect_value(t, out.data.signal.confidence, parsed.confidence)
	testing.expect_value(t, out.data.signal.regime_strength, parsed.regime_strength)
}
