package app

import "core:strings"
import "mr:ports"
import "mr:streams"
import "mr:util"

// ---------------------------------------------------------------------------
// Subscription reconciliation
// Extracted from stream_views.odin for cohesion.
// ---------------------------------------------------------------------------

// ===============================================================
// Smart Subscription Management (PRD-0006-B M5)
// Subscribe only to channels cells actually need.
// ===============================================================

CHANNEL_COUNT :: 6

// Returns a bitmask of channels a widget type needs.
channels_for_widget :: proc(kind: Widget_Kind) -> u8 {
	CH_TRADES    :: u8(1 << u8(ports.MD_Channel.Trades))
	CH_ORDERBOOK :: u8(1 << u8(ports.MD_Channel.Orderbook))
	CH_STATS     :: u8(1 << u8(ports.MD_Channel.Stats))
	CH_HEATMAPS  :: u8(1 << u8(ports.MD_Channel.Heatmaps))
	CH_VPVR      :: u8(1 << u8(ports.MD_Channel.VPVR))
	CH_CANDLES   :: u8(1 << u8(ports.MD_Channel.Candles))

	switch kind {
	case .Candle:    return CH_CANDLES | CH_STATS | CH_HEATMAPS | CH_VPVR
	case .Orderbook: return CH_ORDERBOOK
	case .DOM:       return CH_ORDERBOOK | CH_TRADES
	case .Trades:    return CH_TRADES
	case .Stats:     return CH_STATS
	case .Counter:   return CH_CANDLES | CH_STATS
	case .Heatmap:   return CH_HEATMAPS
	case .VPVR:      return CH_VPVR
	case .Empty:     return 0
	}
	return 0
}

// Wanted subscription: venue + symbol + channel bitmask + TF for TF-sensitive channels.
Sub_Want :: struct {
	venue:           string,
	symbol:          string,
	channels:        u8,
	tf:              string,  // for TF-sensitive channels (Candles, Heatmaps, VPVR)
	has_explicit_tf: bool,    // true = at least one cell has per-cell TF override
}

// Snapshot of a previous subscription entry -- stored as inline fixed buffers to avoid lifetime issues.
Prev_Sub_Entry :: struct {
	venue:      [24]u8,
	venue_len:  u8,
	symbol:     [32]u8,
	symbol_len: u8,
	channels:   u8,
	tf:         [8]u8,
	tf_len:     u8,
}

SUB_WANT_CAP :: 128

// TF-sensitive channel bitmask.
@(private = "file")
CH_TF_SENSITIVE :: u8(1 << u8(ports.MD_Channel.Candles)) |
                   u8(1 << u8(ports.MD_Channel.Heatmaps)) |
                   u8(1 << u8(ports.MD_Channel.VPVR))

@(private = "file")
sanitize_market_field :: proc(s: string) -> string {
	if len(s) == 0 do return ""
	n := len(s)
	for i in 0 ..< n {
		if s[i] == 0 {
			n = i
			break
		}
	}
	if n <= 0 do return ""
	return s[:n]
}

@(private = "file")
sanitize_market_pair :: proc(venue, symbol: string) -> (string, string, bool) {
	v := sanitize_market_field(venue)
	s := sanitize_market_field(symbol)
	if len(v) == 0 || len(s) == 0 do return "", "", false
	return v, s, true
}

@(private = "file")
seed_stream_slot_for_subject :: proc(
	state: ^App_State,
	venue, symbol: string,
	channel: ports.MD_Channel,
	tf: string = "",
) {
	if state == nil || state.stream_views == nil do return
	subject := util.build_subject_with_timeframe(venue, symbol, channel, tf)
	defer delete(subject)
	if len(subject) == 0 do return
	subject_id := util.subject_id64(subject)
	if subject_id == 0 do return

	slot := stream_view_get_or_alloc_slot(state.stream_views, subject_id, state.frame, state)
	if slot == nil do return
	stream_view_set_stream_info(slot, ports.MD_Stream_Info{
		subject_id = subject_id,
		channel    = channel,
		venue      = venue,
		symbol     = symbol,
		timeframe  = tf,
		subject    = subject,
	})
	slot.has_channel = true
	slot.channel = channel
}

// Reconcile subscriptions: subscribe to channels cells need, unsubscribe excess.
reconcile_subscriptions :: proc(state: ^App_State) {
	if state.marketdata.subscribe == nil do return
	if state.marketdata.unsubscribe == nil do return
	reg := state.stream_views
	if reg == nil do return

	// Build wanted set by scanning all cells.
	wanted: [SUB_WANT_CAP]Sub_Want
	wanted_count := 0

	tf_opts := TF_OPTIONS

	for ci in 0 ..< state.world.count {
		ch_mask := channels_for_widget(state.world.widgets[ci].kind)
		if ch_mask == 0 do continue
		if state.bp_assist.degrade_heatmap {
			ch_mask &= ~u8(1 << u8(ports.MD_Channel.Heatmaps))
		}
		if state.bp_assist.degrade_vpvr {
			ch_mask &= ~u8(1 << u8(ports.MD_Channel.VPVR))
		}
		if ch_mask == 0 do continue

		// Resolve venue/symbol for this cell -- PRD-0009: prefer bound fields.
		venue, symbol: string
		if binding_has(&state.world.bindings[ci]) {
			// Intent-driven: use bound venue/symbol directly (can subscribe venues with NO slot yet).
			venue = binding_venue(&state.world.bindings[ci])
			symbol = binding_symbol(&state.world.bindings[ci])
		} else if state.world.bindings[ci].stream_idx >= 0 && state.world.bindings[ci].stream_idx < STREAM_VIEW_CAP && reg.slots[state.world.bindings[ci].stream_idx].used {
			slot := &reg.slots[state.world.bindings[ci].stream_idx]
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			if !slot.has_stream_info do continue
			venue = slot.stream_info.venue
			symbol = slot.stream_info.symbol
		} else {
			// No binding, no valid slot -- follow active stream.
			slot := stream_view_active_slot(reg)
			if slot == nil do continue
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			if !slot.has_stream_info do continue
			venue = slot.stream_info.venue
			symbol = slot.stream_info.symbol
		}

		san_venue, san_symbol, ok := sanitize_market_pair(venue, symbol)
		if !ok do continue
		venue = san_venue
		symbol = san_symbol

		// Resolve TF for this cell (per-cell or global).
		cell_tf := cell_effective_tf_string(state, ci)
		cell_has_per_cell_tf := state.world.timeframes[ci].tf_idx >= 0 && state.world.timeframes[ci].tf_idx < len(TF_OPTIONS)

		// Merge into wanted set (keyed by venue + symbol + tf).
		found := false
		for wi in 0 ..< wanted_count {
			if wanted[wi].venue == venue && wanted[wi].symbol == symbol && wanted[wi].tf == cell_tf {
				wanted[wi].channels |= ch_mask
				if cell_has_per_cell_tf { wanted[wi].has_explicit_tf = true }
				found = true
				break
			}
		}
		if !found && wanted_count < SUB_WANT_CAP {
			wanted[wanted_count] = Sub_Want{
				venue = venue, symbol = symbol, channels = ch_mask,
				tf = cell_tf, has_explicit_tf = cell_has_per_cell_tf,
			}
			wanted_count += 1
		}
	}

	// Compare mode: ensure compare slot streams are subscribed for the selected widget type.
	if state.compare.active && state.compare.count > 0 {
		cmp_widget: Widget_Kind
		switch state.compare.widget_idx {
		case 0:  cmp_widget = .Orderbook
		case 1:  cmp_widget = .Trades
		case 2:  cmp_widget = .Candle
		case:    cmp_widget = .Candle
		}
		cmp_ch_mask := channels_for_widget(cmp_widget)
		for csi in 0 ..< state.compare.count {
			sid := state.compare.slots[csi]
			slot_idx := stream_view_find_slot(reg, sid)
			if slot_idx < 0 do continue
			slot := &reg.slots[slot_idx]
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			if !slot.has_stream_info do continue
			cmp_venue := slot.stream_info.venue
			cmp_symbol := slot.stream_info.symbol
			san_cmp_venue, san_cmp_symbol, ok := sanitize_market_pair(cmp_venue, cmp_symbol)
			if !ok do continue
			cmp_venue = san_cmp_venue
			cmp_symbol = san_cmp_symbol
			eff_tf_idx := state.active_tf_idx
			cmp_tf := tf_opts[eff_tf_idx] if eff_tf_idx >= 0 && eff_tf_idx < len(tf_opts) else tf_opts[0]
			// Merge into wanted set.
			found_cmp := false
			for wi in 0 ..< wanted_count {
				if wanted[wi].venue == cmp_venue && wanted[wi].symbol == cmp_symbol && wanted[wi].tf == cmp_tf {
					wanted[wi].channels |= cmp_ch_mask
					found_cmp = true
					break
				}
			}
			if !found_cmp && wanted_count < SUB_WANT_CAP {
				wanted[wanted_count] = Sub_Want{
					venue = cmp_venue, symbol = cmp_symbol, channels = cmp_ch_mask,
					tf = cmp_tf,
				}
				wanted_count += 1
			}
		}
	}

	// Sync stream registry wiring + refcount ownership from the wanted set.
	streams.registry_reset_ref_counts(&state.stream_registry)
	conn := current_conn_status(state)
	for wi in 0 ..< wanted_count {
		w := wanted[wi]
		stream_id_buf: [streams.STREAM_ID_CAP]u8
		stream_id := build_stream_id_from_market_into(stream_id_buf[:], w.venue, w.symbol)
		_, market_type := streams.split_symbol_market_type(w.symbol)
		if handle := streams.registry_acquire(&state.stream_registry, stream_id, normalized_venue(w.venue), w.symbol, market_type); handle != nil {
			streams.controller_mark_connected(&handle.status, conn == .Connected)
		}
	}
	if active_slot := stream_view_active_slot(reg); active_slot != nil {
		if !active_slot.has_stream_info {
			refresh_stream_info_for_slot(state, active_slot)
		}
		if active_slot.has_stream_info {
			stream_id_buf: [streams.STREAM_ID_CAP]u8
			stream_id := build_stream_id_from_market_into(stream_id_buf[:], active_slot.stream_info.venue, active_slot.stream_info.symbol)
			streams.registry_set_active(&state.stream_registry, stream_id)
		}
	}
	streams.registry_prune_unused(&state.stream_registry)

	// Unsubscribe stale channels BEFORE subscribing new ones.
	// This ensures the adapter still has the OLD subject when unsubscribe fires,
	// which is critical for per-cell TF changes where the subject changes.
	for pi in 0 ..< state.prev_subs_count {
		prev := state.prev_subs[pi]
		pv := string(prev.venue[:int(prev.venue_len)])
		ps := string(prev.symbol[:int(prev.symbol_len)])
		san_pv, san_ps, ok := sanitize_market_pair(pv, ps)
		if !ok do continue
		pv = san_pv
		ps = san_ps
		// For each channel bit in prev, check if still in wanted.
		for chi in 0 ..< CHANNEL_COUNT {
			if (prev.channels & (1 << u8(chi))) == 0 do continue
			is_tf_ch := ((1 << u8(chi)) & CH_TF_SENSITIVE) != 0
			still_wanted := false
			for wi in 0 ..< wanted_count {
				if wanted[wi].venue == pv && wanted[wi].symbol == ps && (wanted[wi].channels & (1 << u8(chi))) != 0 {
					// For TF-sensitive channels, also compare timeframe — a TF mismatch
					// means the old sub is stale and must be unsubscribed.
					if is_tf_ch {
						pt := string(prev.tf[:int(prev.tf_len)])
						if pt != wanted[wi].tf do continue // TF mismatch — not the same sub
					}
					still_wanted = true
					break
				}
			}
			if !still_wanted {
				ch := ports.MD_Channel(chi)
				state.marketdata.unsubscribe(pv, ps, ch)
			}
		}
	}

	// Subscribe to wanted channels (skip channels already in prev_subs to avoid duplicates).
	for wi in 0 ..< wanted_count {
		w := wanted[wi]
		wv, ws, ok := sanitize_market_pair(w.venue, w.symbol)
		if !ok do continue
		w.venue = wv
		w.symbol = ws
		endpoint := streams.endpoint_for_venue(w.venue)
		for chi in 0 ..< CHANNEL_COUNT {
			if (w.channels & (1 << u8(chi))) == 0 do continue
			ch := ports.MD_Channel(chi)
			if !streams.endpoint_supports_channel(endpoint, ch) do continue
			is_tf_ch := ((1 << u8(chi)) & CH_TF_SENSITIVE) != 0
			eff_tf := is_tf_ch ? w.tf : ""
			seed_stream_slot_for_subject(state, w.venue, w.symbol, ch, eff_tf)
			// Skip subscribe if this exact channel+venue+symbol+tf is already in prev_subs.
			already_subscribed := false
			for pi in 0 ..< state.prev_subs_count {
				prev := state.prev_subs[pi]
				pv := string(prev.venue[:int(prev.venue_len)])
				ps := string(prev.symbol[:int(prev.symbol_len)])
				san_pv, san_ps, ok := sanitize_market_pair(pv, ps)
				if !ok do continue
				pv = san_pv
				ps = san_ps
				if pv == w.venue && ps == w.symbol && (prev.channels & (1 << u8(chi))) != 0 {
					// For TF-sensitive channels, also verify timeframe matches.
					if is_tf_ch {
						pt := string(prev.tf[:int(prev.tf_len)])
						if pt != w.tf do continue // TF mismatch -- not already subscribed
					}
					already_subscribed = true
					break
				}
			}
			if already_subscribed do continue
			if is_tf_ch && len(w.tf) > 0 && state.marketdata.subscribe_tf != nil {
				state.marketdata.subscribe_tf(w.venue, w.symbol, ch, w.tf)
			} else {
				state.marketdata.subscribe(w.venue, w.symbol, ch)
			}
		}
	}

	// Save current wanted set into prev_subs for next reconcile diff.
	state.prev_subs_count = wanted_count
	for wi in 0 ..< wanted_count {
		w := wanted[wi]
		entry: Prev_Sub_Entry
		entry.channels = w.channels
		vn := min(len(w.venue), len(entry.venue))
		for i in 0 ..< vn { entry.venue[i] = w.venue[i] }
		entry.venue_len = u8(vn)
		sn := min(len(w.symbol), len(entry.symbol))
		for i in 0 ..< sn { entry.symbol[i] = w.symbol[i] }
		entry.symbol_len = u8(sn)
		tn := min(len(w.tf), len(entry.tf))
		for i in 0 ..< tn { entry.tf[i] = w.tf[i] }
		entry.tf_len = u8(tn)
		state.prev_subs[wi] = entry
	}
}

normalized_venue :: proc(v: string) -> string {
	return streams.endpoint_normalize_venue(v)
}

normalized_symbol :: proc(s: string) -> string {
	if len(s) == 0 do return ""
	if sep := strings.index(s, ":"); sep > 0 {
		return s[:sep]
	}
	return s
}

build_stream_id_from_market_into :: proc(buf: []u8, venue: string, symbol: string) -> string {
	return streams.endpoint_build_stream_id_into(buf, venue, symbol)
}

stream_ids_same_market :: proc(state: ^App_State, a_subject_id, b_subject_id: u64) -> bool {
	if a_subject_id == 0 || b_subject_id == 0 do return false
	if a_subject_id == b_subject_id do return true
	reg := state.stream_views
	if reg == nil do return false

	a_idx := stream_view_find_slot(reg, a_subject_id)
	b_idx := stream_view_find_slot(reg, b_subject_id)
	if a_idx < 0 || b_idx < 0 do return false

	a_slot := &reg.slots[a_idx]
	b_slot := &reg.slots[b_idx]
	if !slot_market_key_known(a_slot) do refresh_stream_info_for_slot(state, a_slot)
	if !slot_market_key_known(b_slot) do refresh_stream_info_for_slot(state, b_slot)
	if !slot_market_key_known(a_slot) || !slot_market_key_known(b_slot) do return false

	return normalized_venue(a_slot.stream_info.venue) == normalized_venue(b_slot.stream_info.venue) &&
		normalized_symbol(a_slot.stream_info.symbol) == normalized_symbol(b_slot.stream_info.symbol)
}
