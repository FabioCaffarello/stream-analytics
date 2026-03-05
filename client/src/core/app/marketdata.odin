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
		window_ms  = st.window_ms,
		ts_ingest_ms = st.ts_ingest_ms,
		quality_flags = st.quality_flags,
		unix       = st.unix,
	})
}

build_heatmap_snapshot :: proc(hm: ports.MD_Heatmap_Event) -> services.Heatmap_Snapshot {
	snap: services.Heatmap_Snapshot
	snap.unix = hm.unix
	snap.window_start_ms = hm.window_start_ms
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
		window_ms  = 0,
		ts_ingest_ms = t.unix * 1000,
		quality_flags = 0,
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

// ---------------------------------------------------------------------------
// Per-event-type handlers (extracted from drain_marketdata for readability).
// ---------------------------------------------------------------------------

@(private = "file")
should_skip_event_by_backpressure_policy :: proc(state: ^App_State, kind: ports.MD_Event_Kind) -> bool {
	if state == nil do return false
	level := max(state.active_metrics.server_backpressure_level, 0)

	// Priority policy by event type:
	// - Always keep: trade/orderbook/candle/range/signal/stats.
	// - Assist-managed degrade: heatmap/vpvr.
	// - Critical overload (L3+): evidence is dropped first.
	#partial switch kind {
	case .Trade, .Orderbook_Snapshot, .Stats, .Candle, .Range_Candle_Batch, .Signal, .Tape:
		return false
	case .Heatmap:
		return state.bp_assist.enabled && state.bp_assist.degrade_heatmap
	case .VPVR:
		return state.bp_assist.enabled && state.bp_assist.degrade_vpvr
	case .Evidence:
		return level >= 3
	}
	return false
}

@(private = "file")
record_backpressure_policy_skip :: proc(state: ^App_State, kind: ports.MD_Event_Kind) {
	if state == nil do return
	#partial switch kind {
	case .Heatmap:
		state.bp_assist.dropped_heatmap += 1
	case .VPVR:
		state.bp_assist.dropped_vpvr += 1
	case .Evidence:
		state.bp_assist.dropped_evidence += 1
	case:
	}
}

handle_trade_event :: proc(state: ^App_State, slot: ^Stream_View_Slot, t: ports.MD_Trade_Event, unix: i64, is_active_stream: bool) {
	record_stream_event(state, slot, .Trade, unix, 0, false, is_active_stream)
	if slot != nil {
		apply_trade_to_store(&slot.trades_store, t)
	}
	if is_active_stream {
		apply_trade_to_store(&state.stores.trades, t)
		// DOM fill tracking.
		dom_group := state.stores.dom.price_group
		if dom_group <= 0 && state.stores.orderbook.last_price > 0 {
			dom_group = orderbook_auto_price_group(state.stores.orderbook.last_price)
		}
		services.dom_store_push_trade(&state.stores.dom, t.price, t.qty, t.is_buy, t.unix, dom_group)
		// Footprint accumulation: bucket trade into per-candle price bins.
		fp_tf := active_timeframe_ms(state)
		fp_group := dom_group > 0 ? dom_group : 1.0
		services.footprint_store_push_trade(&state.stores.footprint, t.price, t.qty, t.is_buy, t.unix * 1000, fp_tf, fp_group)
		if !state.active_metrics.has_live_stats {
			apply_synthetic_stats_from_trade(&state.stores.stats, t)
		}
		if !state.active_metrics.has_live_candle {
			apply_synthetic_candle_from_trade(&state.stores.candle, t, active_timeframe_ms(state), current_now_ms(state))
		}
		// Trades on active stream prove the feed is alive for candle health tracking.
		if now_ms := current_now_ms(state); now_ms > 0 {
			state.candle_last_recv_local_ms = now_ms
		}
		// Whale alert: EMA of trade qty, fire on >3x average.
		if t.qty > 0 {
			if state.whale.avg_qty <= 0 {
				state.whale.avg_qty = t.qty
			} else {
				state.whale.avg_qty = state.whale.avg_qty * 0.95 + t.qty * 0.05
			}
			if t.qty > state.whale.avg_qty * 3 && state.whale.avg_qty > 0 {
				state.whale.price = t.price
				state.whale.qty = t.qty
				state.whale.buy = t.is_buy
				state.whale.frame = state.frame
			}
		}
	}
}

// Returns true when the caller should `continue` the event loop (snapshot gap triggers resync).
@(private = "package")
orderbook_snapshot_gate :: proc(snapshot_seen: bool, is_snapshot: bool, ask_count: int, bid_count: int) -> (next_snapshot_seen: bool, mark_as_snapshot: bool, has_snapshot_gap: bool) {
	if snapshot_seen {
		return true, is_snapshot, false
	}
	if is_snapshot {
		return true, true, false
	}
	// Some venues can stream a valid bootstrap delta before emitting explicit snapshot frames.
	// Accept the first non-empty book as baseline to avoid permanent startup desync loops.
	if ask_count > 0 && bid_count > 0 {
		return true, true, false
	}
	return false, false, true
}

// Returns true when the caller should `continue` the event loop (snapshot gap triggers resync).
handle_orderbook_event :: proc(state: ^App_State, slot: ^Stream_View_Slot, ob: ports.MD_Orderbook_Event, unix: i64, seq: i64, is_active_stream: bool) -> (skip: bool) {
	has_snapshot_gap := false
	is_snapshot_for_stream := ob.is_snapshot
	if slot != nil {
		next_seen, mark_snapshot, gap := orderbook_snapshot_gate(slot.orderbook_snapshot_seen, ob.is_snapshot, ob.ask_count, ob.bid_count)
		slot.orderbook_snapshot_seen = next_seen
		is_snapshot_for_stream = mark_snapshot
		has_snapshot_gap = gap
	} else {
		_, mark_snapshot, gap := orderbook_snapshot_gate(false, ob.is_snapshot, ob.ask_count, ob.bid_count)
		is_snapshot_for_stream = mark_snapshot
		has_snapshot_gap = gap
	}
	if has_snapshot_gap {
		if is_active_stream {
			if active := streams.registry_active(&state.stream_registry); active != nil {
				streams.controller_mark_desync(&active.status, .Snapshot_Gap)
			}
			state.active_metrics.state = .Desync
			state.active_metrics.desync_reason = .Snapshot_Gap
			state.prev_subs_count = 0
			reconcile_subscriptions(state)
			record_error(state, .Connection, "DESYNC: snapshot gap")
		}
		return true
	}
	record_stream_event(state, slot, .Orderbook_Snapshot, unix, seq, is_snapshot_for_stream, is_active_stream)
	if slot != nil {
		apply_orderbook_to_store(&slot.orderbook_store, ob)
		if !slot.has_heatmap_snapshot {
			synth_group := synthetic_heatmap_price_group(ob.last_price)
			snap := build_synthetic_heatmap_snapshot_from_orderbook(ob, synth_group)
			if snap.level_count > 0 {
				tf_s := active_timeframe_ms(state) / 1000
				if tf_s <= 0 do tf_s = 60
				window_s := (ob.unix / tf_s) * tf_s
				snap.unix = window_s + tf_s
				snap.window_start_ms = window_s * 1000
				services.push_heatmap_snapshot(&slot.heatmap_store, snap)
			}
		}
	}
	if is_active_stream {
		if now_ms := current_now_ms(state); now_ms > 0 {
			state.active_metrics.last_orderbook_ts_ms = now_ms
		}
		apply_orderbook_to_store(&state.stores.orderbook, ob)
		// Always generate synthetic heatmap from orderbook — it provides current
		// depth (WHERE liquidity IS) while live heatmap provides aggregated
		// activity (WHERE activity HAPPENED). Both feed the same ring buffer;
		// window dedup keeps latest per timestamp.
		{
			tf_s := active_timeframe_ms(state) / 1000
			if tf_s <= 0 do tf_s = 60
			window := (ob.unix / tf_s) * tf_s
			if window != state.synth_heatmap_last_window {
				synth_group := synthetic_heatmap_price_group(ob.last_price)
				snap := build_synthetic_heatmap_snapshot_from_orderbook(ob, synth_group)
				if snap.level_count > 0 {
					snap.unix = window + tf_s
					snap.window_start_ms = window * 1000
					services.push_heatmap_snapshot(&state.stores.heatmap, snap)
				}
				state.synth_heatmap_last_window = window
			}
		}
		if !state.active_metrics.has_live_vpvr {
			group := orderbook_auto_price_group(ob.last_price)
			apply_synthetic_vpvr_from_orderbook(&state.stores.vpvr, ob, group)
		}
	}
	return false
}

handle_stats_event :: proc(state: ^App_State, slot: ^Stream_View_Slot, st: ports.MD_Stats_Event, unix: i64, is_active_stream: bool) {
	record_stream_event(state, slot, .Stats, unix, 0, false, is_active_stream)
	if slot != nil {
		apply_stats_to_store(&slot.stats_store, st)
	}
	if is_active_stream {
		if now_ms := current_now_ms(state); now_ms > 0 {
			state.active_metrics.last_stats_ts_ms = now_ms
		}
		state.active_metrics.has_live_stats = true
		apply_stats_to_store(&state.stores.stats, st)
	}
}

handle_tape_event :: proc(state: ^App_State, slot: ^Stream_View_Slot, tp: ports.MD_Tape_Event, unix: i64, is_active_stream: bool) {
	record_stream_event(state, slot, .Tape, unix, 0, false, is_active_stream)
	if tp.last_price <= 0 do return

	qty := max(tp.total_volume, tp.buy_volume + tp.sell_volume)
	if qty <= 0 do return
	t := ports.MD_Trade_Event{
		price  = tp.last_price,
		qty    = qty,
		is_buy = tp.buy_volume >= tp.sell_volume,
		unix   = tp.unix,
	}

	if slot != nil {
		apply_trade_to_store(&slot.trades_store, t)
	}
	if is_active_stream {
		apply_trade_to_store(&state.stores.trades, t)
	}
}

handle_heatmap_event :: proc(state: ^App_State, slot: ^Stream_View_Slot, hm: ports.MD_Heatmap_Event, unix: i64, is_active_stream: bool) {
	record_stream_event(state, slot, .Heatmap, unix, 0, false, is_active_stream)
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
		// TF guard: only write to global store if heatmap TF matches active TF.
		tf_opts := TF_OPTIONS
		active_tf := tf_opts[clamp(state.active_tf_idx, 0, len(TF_OPTIONS) - 1)]
		slot_tf_match := slot == nil || !slot.has_stream_info ||
			len(slot.stream_info.timeframe) == 0 ||
			slot.stream_info.timeframe == active_tf
		if slot_tf_match {
			state.active_metrics.has_live_heatmap = true
			services.push_heatmap_snapshot(&state.stores.heatmap, snap)
		}
	}
}

handle_vpvr_event :: proc(state: ^App_State, slot: ^Stream_View_Slot, vpvr: ports.MD_VPVR_Event, unix: i64, is_active_stream: bool) {
	record_stream_event(state, slot, .VPVR, unix, 0, false, is_active_stream)
	if slot != nil {
		slot.has_live_vpvr = true
		apply_vpvr_to_store(&slot.vpvr_store, vpvr)
	}
	if is_active_stream {
		// TF guard: only write to global store if VPVR TF matches active TF.
		tf_opts := TF_OPTIONS
		active_tf := tf_opts[clamp(state.active_tf_idx, 0, len(TF_OPTIONS) - 1)]
		slot_tf_match := slot == nil || !slot.has_stream_info ||
			len(slot.stream_info.timeframe) == 0 ||
			slot.stream_info.timeframe == active_tf
		if slot_tf_match {
			state.active_metrics.has_live_vpvr = true
			apply_vpvr_to_store(&state.stores.vpvr, vpvr)
		}
	}
}

handle_candle_event :: proc(state: ^App_State, slot: ^Stream_View_Slot, cd: ports.MD_Candle_Event, unix: i64, subject_id: u64, is_active_stream: bool) {
	record_stream_event(state, slot, .Candle, unix, 0, false, is_active_stream)
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
	// TF guard: only write to global store if subject matches the active candle TF.
	if is_active_stream && (state.getrange.active_candle_subject_id == 0 || subject_id == state.getrange.active_candle_subject_id) {
		state.active_metrics.has_live_candle = true
		state.active_metrics.context_stage = .Live
		apply_candle_to_store(&state.stores.candle, cd)
		if !state.getrange.seeded {
			request_active_stream_candle_range(state)
		}
	}
}

push_evidence :: proc(state: ^App_State, evt: ports.MD_Evidence_Event, subject_id: u64) {
	if state == nil do return
	idx := state.evidence.head
	state.evidence.entries[idx] = Evidence_Entry{
		kind          = evt.kind,
		kind_len      = evt.kind_len,
		confidence    = evt.confidence,
		reason        = evt.reason,
		reason_len    = evt.reason_len,
		feature_tags  = evt.feature_tags,
		feature_vals  = evt.feature_vals,
		feature_count = evt.feature_count,
		unix          = evt.unix,
		subject_id    = subject_id,
	}
	state.evidence.head = (state.evidence.head + 1) % EVIDENCE_HISTORY_CAP
	if state.evidence.count < EVIDENCE_HISTORY_CAP do state.evidence.count += 1
}

handle_evidence_event :: proc(state: ^App_State, slot: ^Stream_View_Slot, ev: ports.MD_Evidence_Event, unix: i64, subject_id: u64, is_active_stream: bool) {
	record_stream_event(state, slot, .Evidence, unix, 0, false, is_active_stream)
	push_evidence(state, ev, subject_id)
}

push_signal :: proc(state: ^App_State, evt: ports.MD_Signal_Event, subject_id: u64, seq: i64) {
	if state == nil || subject_id == 0 do return
	services.signal_store_push(&state.stores.signals, services.Signal_Entry{
		kind            = evt.kind,
		kind_len        = evt.kind_len,
		severity        = evt.severity,
		severity_len    = evt.severity_len,
		confidence      = evt.confidence,
		reason          = evt.reason,
		reason_len      = evt.reason_len,
		regime          = evt.regime,
		regime_len      = evt.regime_len,
		regime_strength = evt.regime_strength,
		unix            = evt.unix,
		subject_id      = subject_id,
		seq             = seq,
	})
}

handle_signal_event :: proc(state: ^App_State, slot: ^Stream_View_Slot, sig: ports.MD_Signal_Event, unix: i64, subject_id: u64, seq: i64, is_active_stream: bool) {
	record_stream_event(state, slot, .Signal, unix, seq, false, is_active_stream)
	push_signal(state, sig, subject_id, seq)
}

handle_range_candle_batch :: proc(
	state: ^App_State,
	slot: ^Stream_View_Slot,
	batch: ports.MD_Range_Candle_Batch,
	unix: i64,
	subject_id: u64,
	is_active_stream: bool,
	is_active_range_batch: bool,
	is_active_getrange_subject: bool,
) {
	record_stream_event(state, slot, .Range_Candle_Batch, unix, 0, false, is_active_range_batch)
	oldest_before := state.getrange.oldest_ts
	batch_slot_idx := stream_view_find_slot(state.stream_views, subject_id)
	// Guard: only apply to global candle store if subject matches current active candle subject.
	is_valid_range_batch := (state.getrange.active_candle_subject_id != 0 && subject_id == state.getrange.active_candle_subject_id) ||
		is_active_getrange_subject
	for bci in 0 ..< batch.count {
		cd := batch.candles[bci]
		if cd.window_start_ts <= 0 do continue
		if cd.window_end_ts <= cd.window_start_ts do continue
		if slot != nil {
			apply_historical_candle_to_store(&slot.candle_store, cd)
		}
		if is_valid_range_batch {
			apply_historical_candle_to_store(&state.stores.candle, cd)
			if state.getrange.oldest_ts <= 0 || cd.window_start_ts < state.getrange.oldest_ts {
				state.getrange.oldest_ts = cd.window_start_ts
			}
		}
		// Per-cell: update oldest_ts for cells referencing this slot.
		for cell_ci in 0 ..< state.world.count {
			gr := &state.world.getranges[cell_ci]
			bind := &state.world.bindings[cell_ci]
			if !gr.pending || !gr.seeded do continue
			if bind.stream_idx < 0 do continue
			if bind.stream_idx != batch_slot_idx do continue
			if gr.oldest_ts <= 0 || cd.window_start_ts < gr.oldest_ts {
				gr.oldest_ts = cd.window_start_ts
			}
		}
	}
	// GetRange data counts as "received" for health tracking.
	if is_valid_range_batch && batch.count > 0 {
		if now_ms := current_now_ms(state); now_ms > 0 {
			state.candle_last_recv_local_ms = now_ms
		}
		if state.active_metrics.context_stage == .Empty {
			state.active_metrics.context_stage = .Backfilled
		}
	}
	if batch.is_last {
		if is_active_getrange_subject || (state.getrange.subject_id == 0 && is_active_stream) {
			state.getrange.pending = false
			state.getrange.subject_id = 0
			target := min(FETCH_CANDLES_RANGE_LEN, services.CANDLE_CAP)
			if target <= 0 do target = services.CANDLE_CAP
			oldest_advanced := state.getrange.oldest_ts > 0 &&
				(oldest_before <= 0 || state.getrange.oldest_ts < oldest_before)
			if oldest_advanced && state.stores.candle.count < target {
				request_older_candles(state)
			}
		}
		// Clear per-cell getrange_pending for cells referencing this slot.
		for cell_ci in 0 ..< state.world.count {
			gr := &state.world.getranges[cell_ci]
			if !gr.pending do continue
			bind := &state.world.bindings[cell_ci]
			if bind.stream_idx >= 0 && bind.stream_idx == batch_slot_idx {
				gr.pending = false
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Main event drain — polls and dispatches to per-event-type handlers above.
// ---------------------------------------------------------------------------

drain_marketdata :: proc(state: ^App_State) -> int {
	processed := 0

	// G3 fix: detect reconnection by watching connection status transitions.
	conn := current_conn_status(state)
	if conn == .Connected && state.conn.prev_conn_for_reconcile != .Connected {
		state.conn.needs_reconcile = true
		// Clear prev_subs so reconcile re-subscribes everything (server lost subscriptions).
		state.prev_subs_count = 0
		// Clear in-flight getrange state — server has no memory of requests after reconnect.
		if state.getrange.pending {
			state.getrange.pending = false
			state.getrange.subject_id = 0
		}
		for ci in 0 ..< state.world.count {
			state.world.getranges[ci].pending = false
		}
		if state.stream_views != nil {
			for si in 0 ..< STREAM_VIEW_CAP {
				if !state.stream_views.slots[si].used do continue
				state.stream_views.slots[si].orderbook_snapshot_seen = false
			}
		}
	}
	state.conn.prev_conn_for_reconcile = conn

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
					state.active_metrics.has_live_stats = false
					state.active_metrics.has_live_heatmap = false
					state.active_metrics.has_live_vpvr = false
					state.active_metrics.has_live_candle = false
					state.active_metrics.context_stage = .Empty
					if state.stores.candle.count <= 0 {
						request_active_stream_candle_range(state)
					}
				}
				state.has_pending_active_subject = false
				state.pending_active_subject_id = 0
			}
			is_active_stream := subject_id == 0 || state.stream_views == nil || !state.stream_views.has_active ||
				stream_ids_same_market(state, state.stream_views.active_subject_id, subject_id)
			is_active_getrange_subject := state.getrange.subject_id != 0 && subject_id == state.getrange.subject_id
			is_active_range_batch := is_active_stream || is_active_getrange_subject
			if should_skip_event_by_backpressure_policy(state, evt.kind) {
				record_backpressure_policy_skip(state, evt.kind)
				continue
			}
			switch evt.kind {
			case .Trade:
				handle_trade_event(state, slot, evt.data.trade, evt.unix, is_active_stream)
			case .Orderbook_Snapshot:
				if handle_orderbook_event(state, slot, evt.data.ob, evt.unix, evt.source.seq, is_active_stream) {
					continue
				}
			case .Stats:
				handle_stats_event(state, slot, evt.data.stats, evt.unix, is_active_stream)
			case .Tape:
				handle_tape_event(state, slot, evt.data.tape, evt.unix, is_active_stream)
			case .Heatmap:
				handle_heatmap_event(state, slot, evt.data.heatmap, evt.unix, is_active_stream)
			case .VPVR:
				handle_vpvr_event(state, slot, evt.data.vpvr, evt.unix, is_active_stream)
			case .Candle:
				handle_candle_event(state, slot, evt.data.candle, evt.unix, subject_id, is_active_stream)
			case .Range_Candle_Batch:
				handle_range_candle_batch(state, slot, evt.data.range_candles, evt.unix, subject_id, is_active_stream, is_active_range_batch, is_active_getrange_subject)
			case .Evidence:
				handle_evidence_event(state, slot, evt.data.evidence, evt.unix, subject_id, is_active_stream)
			case .Signal:
				handle_signal_event(state, slot, evt.data.signal, evt.unix, subject_id, evt.source.seq, is_active_stream)
			}
		}
	}
	if state.stream_views != nil && processed > 0 {
		if stream_view_repair_invariants(state.stream_views) {
			sync_active_stream_view_to_global_stores(state)
		}
		if state.stream_views.has_active && state.stores.candle.count <= 0 {
			request_active_stream_candle_range(state)
		}
	}
	// G3 fix: reconcile after reconnect so per-cell bindings get re-subscribed.
	if state.conn.needs_reconcile {
		state.conn.needs_reconcile = false
		reconcile_subscriptions(state)
	}

	// PRD-0009: lazy re-resolution — after events, try to resolve cells with unresolved bindings.
	if processed > 0 && state.stream_views != nil {
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

	// GetRange timeout: clear stuck pending state after ~5 seconds (300 frames at 60fps).
	GETRANGE_TIMEOUT_FRAMES :: u64(300)
	if state.getrange.pending && state.frame > state.getrange.sent_frame + GETRANGE_TIMEOUT_FRAMES {
		state.getrange.pending = false
		state.getrange.subject_id = 0
		record_error(state, .GetRange_Timeout, "GetRange timeout (global)")
	}
	for ci in 0 ..< state.world.count {
		gr := &state.world.getranges[ci]
		if gr.pending && state.frame > gr.sent_frame + GETRANGE_TIMEOUT_FRAMES {
			gr.pending = false
			record_error(state, .GetRange_Timeout, "GetRange timeout (cell)")
		}
	}

	// Lazy loading: check if user has scrolled near the oldest loaded data.
	check_lazy_candle_loading(state)

	return processed
}

// Trigger a GetRange request for older candles when the user scrolls near the left edge.
check_lazy_candle_loading :: proc(state: ^App_State) {
	// Global active stream lazy loading — use focused candle cell's view state.
	if !state.getrange.pending && state.getrange.seeded && state.stores.candle.count > 0 &&
	   state.stores.candle.count < services.CANDLE_CAP && state.getrange.oldest_ts > 0 {
		fci := max(state.world.focused, 0)
		if fci >= state.world.count do fci = 0
		fvw := &state.world.views[fci]
		visible := fvw.candle_zoom > 0 ? max(int(fvw.candle_zoom), 1) : max(state.stores.candle.count, 1)
		scroll := int(fvw.candle_scroll_x)
		end_idx := state.stores.candle.count - scroll
		start_idx := max(end_idx - visible, 0)
		LAZY_LOAD_THRESHOLD :: 10
		if start_idx < LAZY_LOAD_THRESHOLD {
			request_older_candles(state)
		}
	}

	// Per-cell lazy loading: throttle to max 2 concurrent getrange globally.
	pending_count := 0
	if state.getrange.pending do pending_count += 1
	for ci in 0 ..< state.world.count {
		if state.world.getranges[ci].pending do pending_count += 1
	}
	MAX_CONCURRENT_GETRANGE :: 2
	if pending_count >= MAX_CONCURRENT_GETRANGE do return

	for ci in 0 ..< state.world.count {
		wid := &state.world.widgets[ci]
		gr := &state.world.getranges[ci]
		vw := &state.world.views[ci]
		if wid.kind != .Candle do continue
		if gr.pending do continue
		if !gr.seeded do continue
		if gr.oldest_ts <= 0 do continue

		// Resolve the candle store for this cell.
		stores := resolve_stores_for_cell(state, ci)
		if stores.candle == nil do continue
		if stores.candle.count <= 0 do continue
		if stores.candle.count >= services.CANDLE_CAP do continue

		visible := vw.candle_zoom > 0 ? max(int(vw.candle_zoom), 1) : max(stores.candle.count, 1)
		scroll := int(vw.candle_scroll_x)
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
record_stream_event :: proc(
	state: ^App_State,
	slot: ^Stream_View_Slot,
	kind: ports.MD_Event_Kind,
	unix: i64,
	seq: i64,
	is_snapshot: bool,
	is_active_stream: bool,
) {
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
	// Only pass server_ts for real-time event types (trades, orderbook) to the
	// stream controller's regression check. Aggregation types (stats, candles,
	// heatmap, vpvr) use window boundaries that can lag real-time by an entire
	// aggregation window, causing false timestamp regressions when mixed with
	// real-time trade timestamps on the same stream handle.
	effective_server_ms := server_ms
	#partial switch kind {
	case .Stats, .Tape, .Candle, .Heatmap, .VPVR, .Range_Candle_Batch, .Evidence, .Signal:
		effective_server_ms = 0
	case:
	}
	if is_active_stream && handle.status.desync_reason == .Manual {
		streams.controller_clear_desync(&handle.status)
	}
	streams.controller_mark_message(&handle.status, local_ms, effective_server_ms, seq, is_snapshot)
	if is_active_stream {
		if kind == .Stats && handle.status.desync_reason == .Snapshot_Stale {
			streams.controller_clear_desync(&handle.status)
		}
		if kind == .Orderbook_Snapshot && is_snapshot &&
			(handle.status.desync_reason == .Snapshot_Gap || handle.status.desync_reason == .Snapshot_Stale) {
			streams.controller_clear_desync(&handle.status)
		}
	}
	streams.controller_mark_connected(&handle.status, current_conn_status(state) == .Connected)
	if is_active_stream {
		streams.registry_set_active(&state.stream_registry, stream_id)
		state.active_metrics.state = handle.status.state
		state.active_metrics.desync_reason = handle.status.desync_reason
	}
}
