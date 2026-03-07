package app

import "mr:layers"
import "mr:md_common"

@(private = "file")
unix_to_ms :: proc(unix: i64) -> i64 {
	if unix <= 0 do return 0
	if unix >= 1_000_000_000_000 do return unix
	return unix * 1000
}

@(private = "file")
sync_evidence_state_from_stream :: proc(state: ^App_State, stream: ^layers.Market_Stream) {
	if state == nil || stream == nil do return
	state.evidence = {}
	count := min(stream.evidence_count, EVIDENCE_HISTORY_CAP)
	for i := count - 1; i >= 0; i -= 1 {
		entry, ok := layers.market_stream_evidence_get_newest(stream, i)
		if !ok do continue
		idx := state.evidence.head
		state.evidence.entries[idx] = Evidence_Entry{
			kind          = entry.kind,
			kind_len      = entry.kind_len,
			confidence    = entry.confidence,
			reason        = entry.reason,
			reason_len    = entry.reason_len,
			feature_tags  = entry.feature_tags,
			feature_vals  = entry.feature_vals,
			feature_count = entry.feature_count,
			unix          = entry.unix,
			subject_id    = entry.subject_id,
		}
		state.evidence.head = (state.evidence.head + 1) % EVIDENCE_HISTORY_CAP
		if state.evidence.count < EVIDENCE_HISTORY_CAP do state.evidence.count += 1
	}
}

@(private = "file")
sync_legacy_stores_from_layer_store :: proc(state: ^App_State) {
	if state == nil do return
	active := layers.market_store_active_stream(&state.layer_store)
	if active == nil {
		reset_active_apply_state(state)
		return
	}

	state.stores.trades = active.trades
	state.stores.orderbook = active.orderbook
	state.stores.stats = active.stats
	state.stores.heatmap = active.heatmap
	state.stores.vpvr = active.vpvr
	state.stores.candle = active.candles
	state.stores.signals = active.signals

	sync_evidence_state_from_stream(state, active)

	// S24/S32: Apply state is single source of truth; metrics synced via adapter.
	state.active_apply_state.has_live[.Stats] = active.stats.count > 0
	state.active_apply_state.has_live[.Heatmap] = active.heatmap.count > 0
	state.active_apply_state.has_live[.VPVR] = active.vpvr.count > 0
	state.active_apply_state.has_live[.Candle] = active.candles.count > 0
	// S32: Per-artifact timing via apply_state so adapter can sync to metrics.
	msg_ts := unix_to_ms(active.last_unix)
	if active.stats.count > 0 {
		state.active_apply_state.last_recv_ms[.Stats] = msg_ts
	}
	if active.orderbook.ask_count > 0 || active.orderbook.bid_count > 0 {
		state.active_apply_state.last_recv_ms[.Orderbook] = unix_to_ms(active.orderbook.unix)
	}
	apply_state_sync_all(state)
	state.active_metrics.last_msg_ts_ms = msg_ts
}

// Canonical marketdata drain for layer architecture.
// Single source of truth: DataSource -> MarketStore.
drain_layer_marketdata :: proc(state: ^App_State) -> int {
	if state == nil do return 0
	result := layers.data_source_poll_and_apply(&state.layer_datasource, state.marketdata, &state.layer_store)

	if state.stream_views != nil {
		for i in 0 ..< result.subject_count {
			subject_id := result.subject_ids[i]
			if subject_id == 0 do continue
			slot := stream_view_get_or_alloc_slot(state.stream_views, subject_id, state.frame, state)
			if slot == nil do continue
			slot.last_seen_frame = state.frame
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
		}
	}

	if state.has_pending_active_subject && state.pending_active_subject_id != 0 {
		if layers.market_store_stream_for_subject(&state.layer_store, state.pending_active_subject_id) != nil {
			layers.market_store_set_active_subject(&state.layer_store, state.pending_active_subject_id)
			if state.stream_views != nil {
				state.stream_views.has_active = true
				state.stream_views.active_subject_id = state.pending_active_subject_id
			}
			state.has_pending_active_subject = false
			state.pending_active_subject_id = 0
		}
	}

	if state.layer_store.active_subject_id == 0 && state.stream_views != nil && state.stream_views.has_active {
		layers.market_store_set_active_subject(&state.layer_store, state.stream_views.active_subject_id)
	}
	if state.stream_views != nil && state.layer_store.active_subject_id != 0 {
		state.stream_views.has_active = true
		state.stream_views.active_subject_id = state.layer_store.active_subject_id
	}

	if result.last_subject != 0 && state.layer_store.active_subject_id == 0 {
		layers.market_store_set_active_subject(&state.layer_store, result.last_subject)
	}

	sync_legacy_stores_from_layer_store(state)
	return result.processed
}
