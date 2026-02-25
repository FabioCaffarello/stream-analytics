package util

// Subject builder — maps (venue, symbol, channel) to MR canonical WS subjects.
// Format: "<stream_type>/<venue>/<symbol>/<timeframe>"

import "core:strings"
import "mr:ports"

// Builds a subject string allocated on the heap (context.allocator).
// Caller owns the returned string; it survives temp_allocator resets.
build_subject :: proc(venue, symbol: string, channel: ports.MD_Channel) -> string {
	stream_type: string
	timeframe: string

	switch channel {
	case .Trades:
		stream_type = "marketdata.trade"
		timeframe = "raw"
	case .Orderbook:
		stream_type = "marketdata.bookdelta"
		timeframe = "raw"
	case .Stats:
		stream_type = "aggregation.stats"
		// Delivery route uses raw; window/timeframe is carried inside payload.
		timeframe = "raw"
	case .Heatmaps:
		stream_type = "insights.heatmap_snapshot"
		timeframe = "1m"
	case .VPVR:
		stream_type = "insights.volume_profile_snapshot"
		timeframe = "1m"
	}

	return strings.concatenate({stream_type, "/", venue, "/", symbol, "/", timeframe})
}

// Returns the stream type prefix before the first '/'.
// e.g. "marketdata.trade/binance/BTCUSDT/raw" → "marketdata.trade"
subject_stream_type :: proc(subject: string) -> string {
	for i in 0 ..< len(subject) {
		if subject[i] == '/' do return subject[:i]
	}
	return subject
}
