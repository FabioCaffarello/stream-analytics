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

// S131: Sync a per-slot apply_state from its corresponding Market_Stream.
// Same logic as sync_apply_state_from_active_stream but operates on a single slot.
// Preserves getrange and recovery state — only syncs store-derived fields.
@(private = "package")
sync_slot_apply_state_from_stream :: proc(s: ^md_common.Stream_Apply_State, ms: ^layers.Market_Stream) {
	if s == nil || ms == nil do return

	ob_has_data := ms.orderbook.ask_count > 0 || ms.orderbook.bid_count > 0
	s.has_live[.Stats]     = ms.stats.count > 0
	s.has_live[.Heatmap]   = ms.heatmap.count > 0
	s.has_live[.VPVR]      = ms.vpvr.count > 0
	s.has_live[.Candle]    = ms.candles.count > 0
	s.has_live[.Trade]     = ms.trades.count > 0
	s.has_live[.Orderbook] = ob_has_data
	s.has_live[.Signal]    = ms.signal_frames > 0
	s.has_live[.Evidence]  = ms.evidence_count > 0

	s.snapshot_seen[.Orderbook] = ob_has_data

	s.artifact_event_count[.Trade]     = ms.trades_frames
	s.artifact_event_count[.Orderbook] = ms.orderbook_frames
	s.artifact_event_count[.Stats]     = ms.stats_frames
	s.artifact_event_count[.Evidence]  = ms.evidence_frames
	s.artifact_event_count[.Signal]    = ms.signal_frames
	s.artifact_event_count[.Tape]      = ms.tape_frames
	if ms.candles.count > 0 {
		s.artifact_event_count[.Candle] = max(s.artifact_event_count[.Candle], u64(ms.candles.count))
	}
	if ms.heatmap.count > 0 {
		s.artifact_event_count[.Heatmap] = max(s.artifact_event_count[.Heatmap], u64(ms.heatmap.count))
	}
	if ms.vpvr.count > 0 {
		s.artifact_event_count[.VPVR] = max(s.artifact_event_count[.VPVR], 1)
	}
	if ms.analytics.count > 0 {
		s.artifact_event_count[.CVD] = max(s.artifact_event_count[.CVD], u64(ms.analytics.count))
	}

	s.event_count = max(s.event_count, ms.event_count)

	msg_ts := unix_to_ms(ms.last_unix)
	if ms.stats.count > 0 {
		newest := services.get_stats(&ms.stats, 0)
		s.last_recv_ms[.Stats] = unix_to_ms(newest.unix) if newest.unix > 0 else msg_ts
	}
	if ob_has_data {
		s.last_recv_ms[.Orderbook] = unix_to_ms(ms.orderbook.unix)
	}
	if ms.trades.count > 0 {
		newest := services.get_trade(&ms.trades, 0)
		s.last_recv_ms[.Trade] = unix_to_ms(newest.unix) if newest.unix > 0 else msg_ts
	}
	if ms.candles.count > 0 {
		newest := services.get_candle_newest(&ms.candles, 0)
		ts := newest.window_end_ts if newest.window_end_ts > 0 else newest.window_start_ts
		if ts > 0 { s.last_recv_ms[.Candle] = unix_to_ms(ts) }
	}
	if ms.signal_frames > 0 {
		s.last_recv_ms[.Signal] = msg_ts
	}
}

// S100/S131: Sync apply_state + evidence from active stream (no store mirroring).
// S131: Complete artifact coverage — all has_live, artifact_event_count, snapshot_seen,
// and last_recv_ms fields synced from Market_Stream counters and store state.
// This enables correct widget readiness states and staleness/auto-recovery detection.
@(private = "package")
sync_apply_state_from_active_stream :: proc(state: ^App_State) {
	if state == nil do return
	active := layers.market_store_active_stream(&state.layer_store)
	if active == nil {
		reset_active_apply_state(state)
		return
	}

	sync_evidence_state_from_stream(state, active)

	s := &state.active_apply_state
	msg_ts := unix_to_ms(active.last_unix)

	// S131: has_live — derived from store occupancy for all artifacts.
	// Previously only Stats/Heatmap/VPVR/Candle were synced (S24).
	ob_has_data := active.orderbook.ask_count > 0 || active.orderbook.bid_count > 0
	s.has_live[.Stats]    = active.stats.count > 0
	s.has_live[.Heatmap]  = active.heatmap.count > 0
	s.has_live[.VPVR]     = active.vpvr.count > 0
	s.has_live[.Candle]   = active.candles.count > 0
	s.has_live[.Trade]    = active.trades.count > 0
	s.has_live[.Orderbook] = ob_has_data
	s.has_live[.Signal]   = active.signal_frames > 0
	s.has_live[.Evidence] = active.evidence_count > 0

	// S131: snapshot_seen — OB gate tracks whether a valid snapshot has populated the store.
	s.snapshot_seen[.Orderbook] = ob_has_data

	// S131: artifact_event_count — from per-artifact frame counters on Market_Stream.
	// Required for staleness detection (skips artifacts with event_count == 0).
	s.artifact_event_count[.Trade]     = active.trades_frames
	s.artifact_event_count[.Orderbook] = active.orderbook_frames
	s.artifact_event_count[.Stats]     = active.stats_frames
	s.artifact_event_count[.Evidence]  = active.evidence_frames
	s.artifact_event_count[.Signal]    = active.signal_frames
	s.artifact_event_count[.Tape]      = active.tape_frames
	// Candle/Heatmap/VPVR/Analytics: no per-artifact frame counter on Market_Stream.
	// Use store count as floor — enables staleness detection once data arrives.
	if active.candles.count > 0 {
		s.artifact_event_count[.Candle] = max(s.artifact_event_count[.Candle], u64(active.candles.count))
	}
	if active.heatmap.count > 0 {
		s.artifact_event_count[.Heatmap] = max(s.artifact_event_count[.Heatmap], u64(active.heatmap.count))
	}
	if active.vpvr.count > 0 {
		s.artifact_event_count[.VPVR] = max(s.artifact_event_count[.VPVR], 1)
	}
	if active.analytics.count > 0 {
		// Analytics store is shared across OI/CVD/DV/BS — at least one kind is active.
		s.artifact_event_count[.CVD] = max(s.artifact_event_count[.CVD], u64(active.analytics.count))
	}

	// S131: Total event count from stream — enables health_tick_evaluate.
	s.event_count = max(s.event_count, active.event_count)

	// S131: last_recv_ms — per-artifact timing for staleness age computation.
	// Use per-store timestamps where available, falling back to stream last_unix.
	if active.stats.count > 0 {
		newest_stats := services.get_stats(&active.stats, 0)
		if newest_stats.unix > 0 {
			s.last_recv_ms[.Stats] = unix_to_ms(newest_stats.unix)
		} else {
			s.last_recv_ms[.Stats] = msg_ts
		}
	}
	if ob_has_data {
		s.last_recv_ms[.Orderbook] = unix_to_ms(active.orderbook.unix)
	}
	if active.trades.count > 0 {
		newest_trade := services.get_trade(&active.trades, 0)
		if newest_trade.unix > 0 {
			s.last_recv_ms[.Trade] = unix_to_ms(newest_trade.unix)
		} else {
			s.last_recv_ms[.Trade] = msg_ts
		}
	}
	if active.candles.count > 0 {
		newest_candle := services.get_candle_newest(&active.candles, 0)
		ts := newest_candle.window_end_ts
		if ts <= 0 { ts = newest_candle.window_start_ts }
		if ts > 0 {
			s.last_recv_ms[.Candle] = unix_to_ms(ts)
		}
	}
	if active.signal_frames > 0 {
		s.last_recv_ms[.Signal] = msg_ts
	}
	if active.analytics.count > 0 {
		s.last_recv_ms[.CVD] = msg_ts
	}

	apply_state_sync_all(state)
	state.active_metrics.last_msg_ts_ms = msg_ts
}

// Canonical marketdata drain for layer architecture.
// Single source of truth: DataSource -> MarketStore.
drain_layer_marketdata :: proc(state: ^App_State) -> int {
	if state == nil do return 0
	// S155: Set workspace TF on store so trade reducer can bin into footprint candles.
	tf_idx := state.active_tf_idx
	tf_ms_table := TF_OPTION_MS
	if tf_idx >= 0 && tf_idx < len(tf_ms_table) {
		state.layer_store.active_tf_ms = tf_ms_table[tf_idx]
	}
	result := layers.data_source_poll_and_apply(&state.layer_datasource, state.marketdata, &state.layer_store)

	// S147: Mark GetRange seeding complete when a Range_Candle_Batch with is_last arrives.
	// This transitions Composition_Stage from Live_Only → Seeded → Composed, unblocking
	// historical data display and preventing the recovery mechanism from triggering.
	if result.range_complete {
		md_common.apply_state_mark_range_complete(&state.active_apply_state, result.range_oldest_ts)
		apply_state_sync_to_getrange(state)
		// Also mark completion on the per-slot apply_state if we have stream views.
		if state.stream_views != nil {
			for si in 0 ..< STREAM_VIEW_CAP {
				slot := &state.stream_views.slots[si]
				if !slot.used do continue
				if slot.apply_state.getrange_pending {
					md_common.apply_state_mark_range_complete(&slot.apply_state, result.range_oldest_ts)
				}
			}
		}
		// Clear per-cell getrange pending flags for bound candle cells.
		for ci in 0 ..< state.world.count {
			if state.world.getranges[ci].pending {
				state.world.getranges[ci].pending = false
			}
		}
		// Clear compare pane getrange pending flags.
		if state.compare.active {
			for cpi in 0 ..< state.compare.count {
				if state.compare.getranges[cpi].pending {
					state.compare.getranges[cpi].pending = false
				}
			}
		}
	}

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
			// S131: Sync per-slot apply_state from the slot's Market_Stream in layer_store.
			// This enables correct widget readiness and staleness for bound cells.
			if ms := layers.market_store_stream_for_subject(&state.layer_store, subject_id); ms != nil {
				sync_slot_apply_state_from_stream(&slot.apply_state, ms)
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
			state.world.getranges[ci].retry_count = 0 // S138: reset retry on reconnect
		}
		state.getrange.retry_count = 0 // S138: reset global retry on reconnect
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
		// S138: Seed bound candle cells on reconnect — not just active stream.
		for ci in 0 ..< state.world.count {
			if state.world.widgets[ci].kind != .Candle do continue
			if !binding_has(&state.world.bindings[ci]) do continue
			request_cell_candle_range(state, ci)
		}
		if state.compare.active {
			for cpi in 0 ..< state.compare.count {
				request_compare_pane_candle_range(state, cpi)
			}
		}
		// S138: Bootstrap subplot analytics on reconnect so subplots render immediately.
		request_active_subplot_analytics(state)
	}
	state.conn.prev_conn_for_reconcile = conn
	if state.conn.needs_reconcile {
		state.conn.needs_reconcile = false
		reconcile_subscriptions(state)
	}

	// S97: GetRange timeout — migrated from legacy drain path.
	// S138: Auto-retry on timeout before logging error.
	// S152: TF-adaptive timeout and retry budget from Backfill_Policy.
	active_tf_ms: i64 = 60_000
	{
		tf_options := TF_OPTION_MS
		if state.active_tf_idx >= 0 && state.active_tf_idx < len(tf_options) {
			active_tf_ms = tf_options[state.active_tf_idx]
		}
	}
	bf_policy := md_common.backfill_policy_for_tf_ms(active_tf_ms)
	if md_common.apply_state_check_getrange_timeout(state.active_apply_state, state.frame, bf_policy.timeout_frames) {
		state.active_apply_state.getrange_pending = false
		state.active_apply_state.getrange_request_id = 0
		apply_state_sync_to_getrange(state)
		if state.getrange.retry_count < bf_policy.max_retries {
			state.getrange.retry_count += 1
			request_active_stream_candle_range(state)
		} else {
			record_error(state, .GetRange_Timeout, "GetRange timeout (global)")
		}
	}
	for ci in 0 ..< state.world.count {
		gr := &state.world.getranges[ci]
		// S152: Per-cell uses the same TF-adaptive policy as active stream.
		if gr.pending && state.frame > gr.sent_frame + bf_policy.timeout_frames {
			gr.pending = false
			if gr.retry_count < bf_policy.max_retries {
				gr.retry_count += 1
				request_cell_candle_range(state, ci)
			} else {
				record_error(state, .GetRange_Timeout, "GetRange timeout (cell)")
			}
		}
	}
	if state.compare.active {
		for cpi in 0 ..< state.compare.count {
			cgr := &state.compare.getranges[cpi]
			// S152: Compare pane uses per-pane TF for its policy.
			cpi_tf_ms := compare_pane_effective_tf_ms(state, cpi)
			cpi_policy := md_common.backfill_policy_for_tf_ms(cpi_tf_ms)
			if cgr.pending && state.frame > cgr.sent_frame + cpi_policy.timeout_frames {
				cgr.pending = false
				if cgr.retry_count < cpi_policy.max_retries {
					cgr.retry_count += 1
					request_compare_pane_candle_range(state, cpi)
				} else {
					record_error(state, .GetRange_Timeout, "GetRange timeout (compare pane)")
				}
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
	// S138: Also seed bound candle cells that haven't been seeded yet.
	if state.stream_views != nil && result.processed > 0 {
		if stream_view_repair_invariants(state.stream_views) {
			sync_active_stream_view_registry(state)
		}
		if state.stream_views.has_active && active_candle_count(state) <= 0 {
			request_active_stream_candle_range(state)
		}
		for ci in 0 ..< state.world.count {
			gr := &state.world.getranges[ci]
			if gr.seeded || gr.pending do continue
			if state.world.widgets[ci].kind != .Candle do continue
			if !binding_has(&state.world.bindings[ci]) do continue
			request_cell_candle_range(state, ci)
		}
	}

	return result.processed
}
