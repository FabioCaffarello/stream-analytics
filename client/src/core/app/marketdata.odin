package app

import "mr:ports"
import "mr:services"
import "mr:streams"

apply_trade_to_store :: proc(store: ^services.Trades_Store, t: ports.MD_Trade_Event) {
	services.push_trade(store, services.Trade_Entry{
		price = t.price,
		qty   = t.qty,
		side  = t.is_buy ? .Buy : .Sell,
		unix  = t.unix,
	})
}

apply_orderbook_to_store :: proc(store: ^services.Orderbook_Store, ob: ports.MD_Orderbook_Event) {
	services.update_orderbook(store,
		ob.ask_prices[:ob.ask_count], ob.ask_sizes[:ob.ask_count],
		ob.bid_prices[:ob.bid_count], ob.bid_sizes[:ob.bid_count],
		ob.last_price, ob.unix,
	)
}

apply_stats_to_store :: proc(store: ^services.Stats_Store, st: ports.MD_Stats_Event) {
	services.push_stats(store, services.Stats_Entry{
		mark_price = st.mark_price,
		funding    = st.funding,
		liq_buy    = st.tbuy,
		liq_sell   = st.tsell,
		unix       = st.unix,
	})
}

build_heatmap_snapshot :: proc(hm: ports.MD_Heatmap_Event) -> services.Heatmap_Snapshot {
	snap: services.Heatmap_Snapshot
	snap.unix = hm.unix
	snap.price_group = hm.price_group
	snap.min_price = hm.min_price
	snap.max_price = hm.max_price
	snap.max_size = hm.max_size
	snap.level_count = min(hm.level_count, services.HEATMAP_LEVEL_CAP)
	for j in 0 ..< snap.level_count {
		snap.levels[j] = services.Heatmap_Level{
			price = hm.prices[j],
			size  = hm.sizes[j],
		}
	}
	return snap
}

apply_vpvr_to_store :: proc(store: ^services.VPVR_Store, vpvr: ports.MD_VPVR_Event) {
	count := min(vpvr.level_count, services.VPVR_BUCKET_CAP)
	services.update_vpvr(
		store,
		vpvr.prices, vpvr.buys, vpvr.sells,
		count, vpvr.price_group,
	)
}

apply_candle_to_store :: proc(store: ^services.Candle_Store, cd: ports.MD_Candle_Event) {
	services.push_candle(store, services.Candle_Entry{
		open            = cd.open,
		high            = cd.high,
		low             = cd.low,
		close           = cd.close,
		volume          = cd.volume,
		buy_vol         = cd.buy_vol,
		sell_vol        = cd.sell_vol,
		trade_count     = cd.trade_count,
		window_start_ts = cd.window_start_ts,
		window_end_ts   = cd.window_end_ts,
		is_closed       = cd.is_closed,
	})
}

apply_historical_candle_to_store :: proc(store: ^services.Candle_Store, cd: ports.MD_Candle_Event) {
	services.upsert_candle_chrono(store, services.Candle_Entry{
		open            = cd.open,
		high            = cd.high,
		low             = cd.low,
		close           = cd.close,
		volume          = cd.volume,
		buy_vol         = cd.buy_vol,
		sell_vol        = cd.sell_vol,
		trade_count     = cd.trade_count,
		window_start_ts = cd.window_start_ts,
		window_end_ts   = cd.window_end_ts,
		is_closed       = cd.is_closed,
	})
}

active_timeframe_ms :: proc(state: ^App_State) -> i64 {
	options := TF_OPTION_MS
	idx := state.active_tf_idx
	if idx < 0 || idx >= len(options) {
		return options[0]
	}
	return options[idx]
}

apply_synthetic_stats_from_trade :: proc(store: ^services.Stats_Store, t: ports.MD_Trade_Event) {
	liq_buy := f64(0)
	liq_sell := f64(0)
	if t.is_buy {
		liq_buy = t.qty
	} else {
		liq_sell = t.qty
	}
	services.push_stats(store, services.Stats_Entry{
		mark_price = t.price,
		funding    = 0,
		liq_buy    = liq_buy,
		liq_sell   = liq_sell,
		unix       = t.unix,
	})
}

apply_synthetic_candle_from_trade :: proc(store: ^services.Candle_Store, t: ports.MD_Trade_Event, tf_ms: i64, now_ms: i64) {
	if tf_ms <= 0 do return
	if t.unix <= 0 do return

	ts_ms := t.unix * 1000
	window_start := (ts_ms / tf_ms) * tf_ms
	window_end := window_start + tf_ms

	entry := services.Candle_Entry{
		open            = t.price,
		high            = t.price,
		low             = t.price,
		close           = t.price,
		volume          = t.qty,
		buy_vol         = t.is_buy ? t.qty : 0,
		sell_vol        = t.is_buy ? 0 : t.qty,
		trade_count     = 1,
		window_start_ts = window_start,
		window_end_ts   = window_end,
		is_closed       = now_ms > 0 && now_ms >= window_end,
	}

	if store.count > 0 {
		last := services.get_candle_newest(store, 0)
		if last.window_start_ts == window_start {
			entry.open = last.open
			entry.high = max(last.high, t.price)
			entry.low = min(last.low, t.price)
			entry.close = t.price
			entry.volume = last.volume + t.qty
			entry.buy_vol = last.buy_vol + (t.is_buy ? t.qty : 0)
			entry.sell_vol = last.sell_vol + (t.is_buy ? 0 : t.qty)
			entry.trade_count = last.trade_count + 1
		}
	}

	services.push_candle(store, entry)
}

build_synthetic_heatmap_snapshot_from_orderbook :: proc(ob: ports.MD_Orderbook_Event, price_group: f64) -> services.Heatmap_Snapshot {
	snap: services.Heatmap_Snapshot
	snap.unix = ob.unix
	snap.price_group = price_group

	max_levels_per_side := services.HEATMAP_LEVEL_CAP / 2
	bid_levels := min(ob.bid_count, max_levels_per_side)
	ask_levels := min(ob.ask_count, max_levels_per_side)

	idx := 0
	for i in 0 ..< bid_levels {
		p := ob.bid_prices[i]
		s := ob.bid_sizes[i]
		snap.levels[idx] = services.Heatmap_Level{price = p, size = s}
		idx += 1
	}
	for i in 0 ..< ask_levels {
		p := ob.ask_prices[i]
		s := ob.ask_sizes[i]
		snap.levels[idx] = services.Heatmap_Level{price = p, size = s}
		idx += 1
	}

	snap.level_count = idx
	if idx <= 0 do return snap

	snap.min_price = snap.levels[0].price
	snap.max_price = snap.levels[0].price
	for i in 0 ..< idx {
		snap.min_price = min(snap.min_price, snap.levels[i].price)
		snap.max_price = max(snap.max_price, snap.levels[i].price)
		snap.max_size = max(snap.max_size, snap.levels[i].size)
	}
	return snap
}

apply_synthetic_vpvr_from_orderbook :: proc(store: ^services.VPVR_Store, ob: ports.MD_Orderbook_Event, price_group: f64) {
	prices: [services.VPVR_BUCKET_CAP]f64
	buys:   [services.VPVR_BUCKET_CAP]f64
	sells:  [services.VPVR_BUCKET_CAP]f64

	n := 0

	// Keep prices roughly ascending by inserting bids from far-to-near, then asks near-to-far.
	bid_levels := min(ob.bid_count, services.VPVR_BUCKET_CAP)
	for i in 0 ..< bid_levels {
		src_idx := bid_levels - 1 - i
		prices[n] = ob.bid_prices[src_idx]
		buys[n] = ob.bid_sizes[src_idx]
		sells[n] = 0
		n += 1
	}

	ask_levels := min(ob.ask_count, services.VPVR_BUCKET_CAP - n)
	for i in 0 ..< ask_levels {
		prices[n] = ob.ask_prices[i]
		buys[n] = 0
		sells[n] = ob.ask_sizes[i]
		n += 1
	}

	if n <= 0 do return
	services.update_vpvr(store, raw_data(prices[:n]), raw_data(buys[:n]), raw_data(sells[:n]), n, price_group)
}

drain_marketdata :: proc(state: ^App_State) -> int {
	processed := 0

	// G3 fix: detect reconnection by watching connection status transitions.
	conn := current_conn_status(state)
	if conn == .Connected && state.prev_conn_for_reconcile != .Connected {
		state.needs_reconcile = true
		// Clear prev_subs so reconcile re-subscribes everything (server lost subscriptions).
		state.prev_subs_count = 0
		// Clear in-flight getrange state — server has no memory of requests after reconnect.
		if state.getrange_pending {
			state.getrange_pending = false
			state.getrange_subject_id = 0
		}
		for ci in 0 ..< state.cell_count {
			state.cell_assignments[ci].getrange_pending = false
		}
	}
	state.prev_conn_for_reconcile = conn

	// Drain marketdata events (non-blocking).
		if state.marketdata.poll != nil {
		events: [MD_POLL_CAP]ports.MD_Event
		n := state.marketdata.poll(events[:])
			processed = n
			for i in 0 ..< n {
				evt := events[i]
				subject_id := evt.source.subject_id
				slot := stream_view_get_or_alloc_slot(state.stream_views, subject_id, state.frame, state)
					if slot != nil {
						slot.last_seen_frame = state.frame
						slot.has_channel = true
						slot.channel = evt.source.channel
						if !slot.has_stream_info {
							refresh_stream_info_for_slot(state, slot)
						}
					}
					if state.has_pending_active_subject && subject_id != 0 && subject_id == state.pending_active_subject_id {
						if state.stream_views != nil {
							state.stream_views.has_active = true
							state.stream_views.active_subject_id = subject_id
							sync_active_stream_view_to_global_stores(state)
							persist_active_stream_subject(state)
							state.active_has_live_stats = false
							state.active_has_live_heatmap = false
							state.active_has_live_vpvr = false
							state.active_has_live_candle = false
							if state.candle_store.count <= 0 {
								request_active_stream_candle_range(state)
							}
						}
						state.has_pending_active_subject = false
						state.pending_active_subject_id = 0
					}
					is_active_stream := subject_id == 0 || state.stream_views == nil || !state.stream_views.has_active ||
						stream_ids_same_market(state, state.stream_views.active_subject_id, subject_id)
					is_active_getrange_subject := state.getrange_subject_id != 0 && subject_id == state.getrange_subject_id
					is_active_range_batch := is_active_stream || is_active_getrange_subject
					record_stream_event(state, slot, evt.kind, evt.unix, is_active_stream)
				switch evt.kind {
				case .Trade:
					t := evt.data.trade
					if slot != nil {
						apply_trade_to_store(&slot.trades_store, t)
					}
					if is_active_stream {
						apply_trade_to_store(&state.trades_store, t)
						// DOM fill tracking.
						dom_group := state.dom_store.price_group
						if dom_group <= 0 && state.orderbook_store.last_price > 0 {
							dom_group = orderbook_auto_price_group(state.orderbook_store.last_price)
						}
						services.dom_store_push_trade(&state.dom_store, t.price, t.qty, t.is_buy, t.unix, dom_group)
						// Footprint accumulation: bucket trade into per-candle price bins.
						fp_tf := active_timeframe_ms(state)
						fp_group := dom_group > 0 ? dom_group : 1.0
						services.footprint_store_push_trade(&state.footprint_store, t.price, t.qty, t.is_buy, t.unix * 1000, fp_tf, fp_group)
						if !state.active_has_live_stats {
							apply_synthetic_stats_from_trade(&state.stats_store, t)
						}
						if !state.active_has_live_candle {
							apply_synthetic_candle_from_trade(&state.candle_store, t, active_timeframe_ms(state), current_now_ms(state))
						}
						// Trades on active stream prove the feed is alive for candle health tracking,
						// regardless of whether live candle events arrive from the backend.
						if now_ms := current_now_ms(state); now_ms > 0 {
							state.candle_last_recv_local_ms = now_ms
						}
						// Whale alert: EMA of trade qty, fire on >3x average.
						if t.qty > 0 {
							if state.whale_avg_qty <= 0 {
								state.whale_avg_qty = t.qty
							} else {
								state.whale_avg_qty = state.whale_avg_qty * 0.95 + t.qty * 0.05
							}
							if t.qty > state.whale_avg_qty * 3 && state.whale_avg_qty > 0 {
								state.whale_alert_price = t.price
								state.whale_alert_qty = t.qty
								state.whale_alert_buy = t.is_buy
								state.whale_alert_frame = state.frame
							}
						}
					}
				case .Orderbook_Snapshot:
					ob := evt.data.ob
					if slot != nil {
						apply_orderbook_to_store(&slot.orderbook_store, ob)
						if !slot.has_heatmap_snapshot {
							synth_group := synthetic_heatmap_price_group(ob.last_price)
							snap := build_synthetic_heatmap_snapshot_from_orderbook(ob, synth_group)
							if snap.level_count > 0 {
								tf_s := active_timeframe_ms(state) / 1000
								if tf_s <= 0 do tf_s = 60
								snap.unix = ((ob.unix / tf_s) * tf_s) + tf_s
								services.push_heatmap_snapshot(&slot.heatmap_store, snap)
							}
						}
					}
					if is_active_stream {
						if now_ms := current_now_ms(state); now_ms > 0 {
							state.active_stream_last_orderbook_ts_ms = now_ms
						}
						apply_orderbook_to_store(&state.orderbook_store, ob)
						if !state.active_has_live_heatmap {
							tf_s := active_timeframe_ms(state) / 1000
							if tf_s <= 0 do tf_s = 60
							window := (ob.unix / tf_s) * tf_s
							if window != state.synth_heatmap_last_window {
								synth_group := synthetic_heatmap_price_group(ob.last_price)
								snap := build_synthetic_heatmap_snapshot_from_orderbook(ob, synth_group)
								if snap.level_count > 0 {
									snap.unix = window + tf_s
									services.push_heatmap_snapshot(&state.heatmap_store, snap)
								}
								state.synth_heatmap_last_window = window
							}
						}
						if !state.active_has_live_vpvr {
							group := orderbook_auto_price_group(ob.last_price)
							apply_synthetic_vpvr_from_orderbook(&state.vpvr_store, ob, group)
						}
					}
					case .Stats:
						st := evt.data.stats
						if slot != nil {
							apply_stats_to_store(&slot.stats_store, st)
					}
					if is_active_stream {
						if now_ms := current_now_ms(state); now_ms > 0 {
							state.active_stream_last_stats_ts_ms = now_ms
						}
						state.active_has_live_stats = true
						apply_stats_to_store(&state.stats_store, st)
					}
				case .Heatmap:
					hm := evt.data.heatmap
					snap := build_heatmap_snapshot(hm)
					if slot != nil {
						// First live snapshot replaces synthetic warmup samples for this slot.
						if !slot.has_heatmap_snapshot {
							slot.heatmap_store = {}
						}
						slot.has_heatmap_snapshot = true
						slot.heatmap_snapshot = snap
						services.push_heatmap_snapshot(&slot.heatmap_store, snap)
					}
					if is_active_stream {
						// First live snapshot replaces synthetic warmup samples for active chart.
						if !state.active_has_live_heatmap {
							state.heatmap_store = {}
						}
						state.active_has_live_heatmap = true
						services.push_heatmap_snapshot(&state.heatmap_store, snap)
					}
				case .VPVR:
					vpvr := evt.data.vpvr
					if slot != nil {
						apply_vpvr_to_store(&slot.vpvr_store, vpvr)
					}
					if is_active_stream {
						state.active_has_live_vpvr = true
						apply_vpvr_to_store(&state.vpvr_store, vpvr)
					}
					case .Candle:
						cd := evt.data.candle
						if slot != nil {
							tf_ms := cd.window_end_ts - cd.window_start_ts
							if tf_ms > 0 {
								slot.has_timeframe_ms = true
								slot.timeframe_ms = tf_ms
							}
							apply_candle_to_store(&slot.candle_store, cd)
						}
						now_ms := current_now_ms(state)
						if is_active_stream && now_ms > 0 {
							state.candle_last_recv_local_ms = now_ms
						}
						if is_active_stream {
							state.active_has_live_candle = true
							apply_candle_to_store(&state.candle_store, cd)
							if !state.getrange_seeded {
								request_active_stream_candle_range(state)
							}
						}
				case .Range_Candle_Batch:
					batch := evt.data.range_candles
					oldest_before := state.getrange_oldest_ts
					batch_slot_idx := stream_view_find_slot(state.stream_views, subject_id)
					// Guard: only apply to global candle store if subject matches current active candle subject.
					// This prevents stale getrange batches (from old TF/stream) from polluting the store.
					is_valid_range_batch := (state.active_candle_subject_id != 0 && subject_id == state.active_candle_subject_id) ||
						is_active_getrange_subject
					for bci in 0 ..< batch.count {
						cd := batch.candles[bci]
						if cd.window_start_ts <= 0 do continue
						if cd.window_end_ts <= cd.window_start_ts do continue
						if slot != nil {
							apply_historical_candle_to_store(&slot.candle_store, cd)
						}
						if is_valid_range_batch {
							apply_historical_candle_to_store(&state.candle_store, cd)
							if state.getrange_oldest_ts <= 0 || cd.window_start_ts < state.getrange_oldest_ts {
								state.getrange_oldest_ts = cd.window_start_ts
							}
						}
						// Per-cell: update oldest_ts for cells referencing this slot.
						for cell_ci in 0 ..< state.cell_count {
							cell_ref := &state.cell_assignments[cell_ci]
							if !cell_ref.getrange_pending || !cell_ref.getrange_seeded do continue
							if cell_ref.stream_idx < 0 do continue
							if cell_ref.stream_idx != batch_slot_idx do continue
							if cell_ref.getrange_oldest_ts <= 0 || cd.window_start_ts < cell_ref.getrange_oldest_ts {
								cell_ref.getrange_oldest_ts = cd.window_start_ts
							}
						}
					}
					// GetRange data counts as "received" for health tracking.
					if is_valid_range_batch && batch.count > 0 {
						if now_ms := current_now_ms(state); now_ms > 0 {
							state.candle_last_recv_local_ms = now_ms
						}
					}
					if batch.is_last {
						if is_active_getrange_subject || (state.getrange_subject_id == 0 && is_active_stream) {
							state.getrange_pending = false
							state.getrange_subject_id = 0
							target := min(FETCH_CANDLES_RANGE_LEN, services.CANDLE_CAP)
							if target <= 0 do target = services.CANDLE_CAP
							oldest_advanced := state.getrange_oldest_ts > 0 &&
								(oldest_before <= 0 || state.getrange_oldest_ts < oldest_before)
							if oldest_advanced && state.candle_store.count < target {
								request_older_candles(state)
							}
						}
						// Clear per-cell getrange_pending for cells referencing this slot.
						for cell_ci in 0 ..< state.cell_count {
							cell_ref := &state.cell_assignments[cell_ci]
							if !cell_ref.getrange_pending do continue
							if cell_ref.stream_idx >= 0 && cell_ref.stream_idx == batch_slot_idx {
								cell_ref.getrange_pending = false
							}
						}
					}
				}
			}
		}
		if state.stream_views != nil && processed > 0 {
			if stream_view_repair_invariants(state.stream_views) {
				sync_active_stream_view_to_global_stores(state)
			}
			if state.stream_views.has_active && state.candle_store.count <= 0 {
				request_active_stream_candle_range(state)
			}
		}
		// G3 fix: reconcile after reconnect so per-cell bindings get re-subscribed.
		if state.needs_reconcile {
			state.needs_reconcile = false
			reconcile_subscriptions(state)
		}

		// PRD-0009: lazy re-resolution — after events, try to resolve cells with unresolved bindings.
		if processed > 0 && state.stream_views != nil {
			for ci in 0 ..< state.cell_count {
				cell := &state.cell_assignments[ci]
				if cell.stream_idx >= 0 do continue
				if !cell_has_binding(cell) do continue
				bv := cell_bound_venue(cell)
				bs := cell_bound_symbol(cell)
				for si in 0 ..< STREAM_VIEW_CAP {
					if !state.stream_views.slots[si].used do continue
					slot := &state.stream_views.slots[si]
					if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
					if slot.has_stream_info && slot.stream_info.venue == bv && slot.stream_info.symbol == bs {
						cell.stream_idx = si
						break
					}
				}
			}
		}

		// GetRange timeout: clear stuck pending state after ~5 seconds (300 frames at 60fps).
		GETRANGE_TIMEOUT_FRAMES :: u64(300)
		if state.getrange_pending && state.frame > state.getrange_sent_frame + GETRANGE_TIMEOUT_FRAMES {
			state.getrange_pending = false
			state.getrange_subject_id = 0
		}
		for ci in 0 ..< state.cell_count {
			cell := &state.cell_assignments[ci]
			if cell.getrange_pending && state.frame > cell.getrange_sent_frame + GETRANGE_TIMEOUT_FRAMES {
				cell.getrange_pending = false
			}
		}

		// Lazy loading: check if user has scrolled near the oldest loaded data.
		check_lazy_candle_loading(state)

		return processed
	}

// Trigger a GetRange request for older candles when the user scrolls near the left edge.
check_lazy_candle_loading :: proc(state: ^App_State) {
	// Global active stream lazy loading (backward compat).
	if !state.getrange_pending && state.getrange_seeded && state.candle_store.count > 0 &&
	   state.candle_store.count < services.CANDLE_CAP && state.getrange_oldest_ts > 0 {
		visible := state.candle_zoom > 0 ? max(int(state.candle_zoom), 1) : max(state.candle_store.count, 1)
		scroll := int(state.candle_scroll_x)
		end_idx := state.candle_store.count - scroll
		start_idx := max(end_idx - visible, 0)
		LAZY_LOAD_THRESHOLD :: 10
		if start_idx < LAZY_LOAD_THRESHOLD {
			request_older_candles(state)
		}
	}

	// Per-cell lazy loading: throttle to max 2 concurrent getrange globally.
	pending_count := 0
	if state.getrange_pending do pending_count += 1
	for ci in 0 ..< state.cell_count {
		if state.cell_assignments[ci].getrange_pending do pending_count += 1
	}
	MAX_CONCURRENT_GETRANGE :: 2
	if pending_count >= MAX_CONCURRENT_GETRANGE do return

	for ci in 0 ..< state.cell_count {
		cell := &state.cell_assignments[ci]
		if cell.widget != .Candle do continue
		if cell.getrange_pending do continue
		if !cell.getrange_seeded do continue
		if cell.getrange_oldest_ts <= 0 do continue

		// Resolve the candle store for this cell.
		stores := resolve_stores_for_cell(state, cell, ci)
		if stores.candle == nil do continue
		if stores.candle.count <= 0 do continue
		if stores.candle.count >= services.CANDLE_CAP do continue

		visible := cell.candle_zoom > 0 ? max(int(cell.candle_zoom), 1) : max(stores.candle.count, 1)
		scroll := int(cell.candle_scroll_x)
		end_idx := stores.candle.count - scroll
		start_idx := max(end_idx - visible, 0)
		if start_idx < 10 {
			request_cell_older_candles(state, ci)
			pending_count += 1
			if pending_count >= MAX_CONCURRENT_GETRANGE do return
		}
	}
}

@(private = "file")
event_unix_to_ms :: proc(unix: i64) -> i64 {
	if unix <= 0 do return 0
	if unix >= 1_000_000_000_000 do return unix
	return unix * 1000
}

@(private = "file")
record_stream_event :: proc(state: ^App_State, slot: ^Stream_View_Slot, kind: ports.MD_Event_Kind, unix: i64, is_active_stream: bool) {
	if state == nil || slot == nil do return
	if !slot.has_stream_info {
		refresh_stream_info_for_slot(state, slot)
	}
	if !slot.has_stream_info do return

	stream_id_buf: [streams.STREAM_ID_CAP]u8
	stream_id := build_stream_id_from_market_into(stream_id_buf[:], slot.stream_info.venue, slot.stream_info.symbol)
	_, market_type := streams.split_symbol_market_type(slot.stream_info.symbol)
	handle := streams.registry_get_or_create(
		&state.stream_registry,
		stream_id,
		normalized_venue(slot.stream_info.venue),
		slot.stream_info.symbol,
		market_type,
	)
	if handle == nil do return
	local_ms := current_now_ms(state)
	server_ms := event_unix_to_ms(unix)
	if local_ms <= 0 do local_ms = server_ms
	is_snapshot := kind == .Orderbook_Snapshot || kind == .Heatmap || kind == .VPVR || kind == .Range_Candle_Batch
	streams.controller_mark_message(&handle.status, local_ms, server_ms, 0, is_snapshot)
	streams.controller_mark_connected(&handle.status, current_conn_status(state) == .Connected)
	if is_active_stream {
		streams.registry_set_active(&state.stream_registry, stream_id)
	}
}
