package app

import "mr:layers"
import "mr:md_common"
import "mr:services"

// S100: Direct accessors for the active stream's stores from layer_store.
// These replace the removed Global_Stores mirror fields.

active_candle_store :: proc(state: ^App_State) -> ^services.Candle_Store {
	if active := layers.market_store_active_stream(&state.layer_store); active != nil {
		return &active.candles
	}
	return nil
}

active_trades_store :: proc(state: ^App_State) -> ^services.Trades_Store {
	if active := layers.market_store_active_stream(&state.layer_store); active != nil {
		return &active.trades
	}
	return nil
}

active_orderbook_store :: proc(state: ^App_State) -> ^services.Orderbook_Store {
	if active := layers.market_store_active_stream(&state.layer_store); active != nil {
		return &active.orderbook
	}
	return nil
}

active_heatmap_store :: proc(state: ^App_State) -> ^services.Heatmap_Store {
	if active := layers.market_store_active_stream(&state.layer_store); active != nil {
		return &active.heatmap
	}
	return nil
}

active_vpvr_store :: proc(state: ^App_State) -> ^services.VPVR_Store {
	if active := layers.market_store_active_stream(&state.layer_store); active != nil {
		return &active.vpvr
	}
	return nil
}

active_stats_store :: proc(state: ^App_State) -> ^services.Stats_Store {
	if active := layers.market_store_active_stream(&state.layer_store); active != nil {
		return &active.stats
	}
	return nil
}

active_signals_store :: proc(state: ^App_State) -> ^services.Signal_Store {
	if active := layers.market_store_active_stream(&state.layer_store); active != nil {
		return &active.signals
	}
	return nil
}

active_analytics_store :: proc(state: ^App_State) -> ^services.Analytics_Store {
	if active := layers.market_store_active_stream(&state.layer_store); active != nil {
		return &active.analytics
	}
	return nil
}

active_candle_count :: proc(state: ^App_State) -> int {
	if cs := active_candle_store(state); cs != nil do return cs.count
	return 0
}

active_heatmap_count :: proc(state: ^App_State) -> int {
	if hs := active_heatmap_store(state); hs != nil do return hs.count
	return 0
}

active_vpvr_count :: proc(state: ^App_State) -> int {
	if vs := active_vpvr_store(state); vs != nil do return vs.count
	return 0
}

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

// S100: Sync apply_state + evidence from active stream (no store mirroring).
@(private = "file")
sync_apply_state_from_active_stream :: proc(state: ^App_State) {
	if state == nil do return
	active := layers.market_store_active_stream(&state.layer_store)
	if active == nil {
		reset_active_apply_state(state)
		return
	}

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

	sync_apply_state_from_active_stream(state)

	// S97: Reconnection detection — migrated from legacy drain path.
	conn := current_conn_status(state)
	if conn == .Connected && state.conn.prev_conn_for_reconcile != .Connected {
		state.conn.needs_reconcile = true
		state.prev_subs_count = 0
		for ci in 0 ..< state.world.count {
			state.world.getranges[ci].pending = false
		}
		for cpi in 0 ..< state.compare.count {
			state.compare.getranges[cpi] = {}
		}
		if state.stream_views != nil {
			for si in 0 ..< STREAM_VIEW_CAP {
				if !state.stream_views.slots[si].used do continue
				md_common.apply_state_on_reconnect(&state.stream_views.slots[si].apply_state)
			}
		}
		reconnect_active_apply_state(state)
		state.freshness.loaded = false
		state.timeline = {}
		state.was_ever_connected = true
		if state.compare.active {
			for cpi in 0 ..< state.compare.count {
				request_compare_pane_candle_range(state, cpi)
			}
		}
	}
	state.conn.prev_conn_for_reconcile = conn
	if state.conn.needs_reconcile {
		state.conn.needs_reconcile = false
		reconcile_subscriptions(state)
	}

	// S97: GetRange timeout — migrated from legacy drain path.
	GETRANGE_TIMEOUT_FRAMES :: u64(300)
	if md_common.apply_state_check_getrange_timeout(state.active_apply_state, state.frame, GETRANGE_TIMEOUT_FRAMES) {
		state.active_apply_state.getrange_pending = false
		state.active_apply_state.getrange_request_id = 0
		apply_state_sync_to_getrange(state)
		record_error(state, .GetRange_Timeout, "GetRange timeout (global)")
	}
	for ci in 0 ..< state.world.count {
		gr := &state.world.getranges[ci]
		if gr.pending && state.frame > gr.sent_frame + GETRANGE_TIMEOUT_FRAMES {
			gr.pending = false
			record_error(state, .GetRange_Timeout, "GetRange timeout (cell)")
		}
	}
	if state.compare.active {
		for cpi in 0 ..< state.compare.count {
			cgr := &state.compare.getranges[cpi]
			if cgr.pending && state.frame > cgr.sent_frame + GETRANGE_TIMEOUT_FRAMES {
				cgr.pending = false
				record_error(state, .GetRange_Timeout, "GetRange timeout (compare pane)")
			}
		}
	}

	// S97: Lazy candle loading — migrated from legacy drain path.
	check_lazy_candle_loading(state)

	// S97: Lifecycle derivation — migrated from legacy drain path.
	state.lifecycle = md_common.derive_lifecycle(
		state.bootstrap.has_session,
		state.bootstrap.ready,
		state.stores.markets.loaded,
		conn == .Connected,
		state.was_ever_connected,
		state.active_metrics.subscribe_acks > 0,
		result.processed > 0,
		state.active_metrics.desync_reason != .None,
	)

	// S97: Lazy re-resolution — migrated from legacy drain path.
	if result.processed > 0 && state.stream_views != nil {
		for ci in 0 ..< state.world.count {
			bind := &state.world.bindings[ci]
			if bind.stream_idx >= 0 do continue
			if !binding_has(bind) do continue
			bv := binding_venue(bind)
			bs := binding_symbol(bind)
			for si in 0 ..< STREAM_VIEW_CAP {
				if !state.stream_views.slots[si].used do continue
				slot := &state.stream_views.slots[si]
				if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
				if slot.has_stream_info && slot.stream_info.venue == bv && slot.stream_info.symbol == bs {
					bind.stream_idx = si
					break
				}
			}
		}
	}

	// S97: Slot repair + seeding.
	if state.stream_views != nil && result.processed > 0 {
		if stream_view_repair_invariants(state.stream_views) {
			sync_active_stream_view_registry(state)
		}
		if state.stream_views.has_active && active_candle_count(state) <= 0 {
			request_active_stream_candle_range(state)
		}
	}

	return result.processed
}
