package main

import "core:testing"
import "mr:ports"
import "mr:services"
import "mr:util"

@(test)
test_native_fill_evidence_event_contract :: proc(t: ^testing.T) {
	parsed: services.Parsed_Evidence
	parsed.subject_id = 0xC0FFEE
	parsed.seq = 77
	parsed.unix = 1_700_000_000_321
	parsed.confidence = 0.82
	parsed.feature_count = 2
	parsed.feature_vals[0] = 12.5
	parsed.feature_vals[1] = 7.25

	out: ports.MD_Event
	native_fill_evidence_event(&out, parsed)

	testing.expect_value(t, out.kind, ports.MD_Event_Kind.Evidence)
	testing.expect_value(t, out.source.channel, ports.MD_Channel.Evidence)
	testing.expect_value(t, out.source.subject_id, parsed.subject_id)
	testing.expect_value(t, out.source.seq, parsed.seq)
	testing.expect_value(t, out.unix, util.normalize_unix_seconds(parsed.unix))
	testing.expect_value(t, out.data.evidence.feature_count, parsed.feature_count)
	testing.expect_value(t, out.data.evidence.feature_vals[0], parsed.feature_vals[0])
	testing.expect_value(t, out.data.evidence.feature_vals[1], parsed.feature_vals[1])
}
