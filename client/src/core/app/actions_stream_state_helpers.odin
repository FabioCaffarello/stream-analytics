package app

reset_active_stream_live_metrics :: proc(state: ^App_State) {
	if state == nil do return
	state.active_metrics.has_live_stats = false
	state.active_metrics.has_live_heatmap = false
	state.active_metrics.has_live_vpvr = false
	state.active_metrics.has_live_candle = false
	state.active_metrics.context_stage = .Empty
	state.active_metrics.last_stats_ts_ms = 0
	state.active_metrics.last_orderbook_ts_ms = 0
}
