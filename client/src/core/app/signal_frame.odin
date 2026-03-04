package app

import "mr:ports"
import "mr:util"

// Build deterministic signal subject id for a market + timeframe.
// Subject format: signal/composite/{venue}/{symbol}/{timeframe}
build_signal_subject_id :: proc(venue, symbol, timeframe: string) -> u64 {
	subject := util.build_subject_with_timeframe(venue, symbol, ports.MD_Channel.Signals, timeframe)
	defer delete(subject)
	if len(subject) == 0 do return 0
	return util.subject_id64(subject)
}
