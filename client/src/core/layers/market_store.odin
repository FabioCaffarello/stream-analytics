package layers

import "mr:ports"
import "mr:services"

MARKET_STREAM_CAP  :: 16
EVIDENCE_RING_CAP  :: 96
STREAM_TEXT_CAP    :: 64

Stream_Key :: struct {
	subject_id:    u64,
	channel:       ports.MD_Channel,
	venue:         [24]u8,
	venue_len:     u8,
	symbol:        [32]u8,
	symbol_len:    u8,
	timeframe:     [12]u8,
	timeframe_len: u8,
	subject:       [STREAM_TEXT_CAP]u8,
	subject_len:   u8,
}

Evidence_Entry :: struct {
	kind:          [24]u8,
	kind_len:      u8,
	confidence:    f64,
	reason:        [96]u8,
	reason_len:    u8,
	feature_tags:  [4][24]u8,
	feature_vals:  [4]f64,
	feature_count: int,
	unix:          i64,
	subject_id:    u64,
}

Market_Stream :: struct {
	used:             bool,
	key:              Stream_Key,
	last_seq:         i64,
	last_unix:        i64,
	event_count:      u64,
	evictions:        u64,

	trades:           services.Trades_Store,
	orderbook:        services.Orderbook_Store,
	stats:            services.Stats_Store,
	heatmap:          services.Heatmap_Store,
	vpvr:             services.VPVR_Store,
	candles:          services.Candle_Store,
	signals:          services.Signal_Store,
	evidence:         [EVIDENCE_RING_CAP]Evidence_Entry,
	evidence_head:    int,
	evidence_count:   int,
}

Market_Store :: struct {
	streams:             [MARKET_STREAM_CAP]Market_Stream,
	stream_count:        int,
	next_evict_idx:      int,
	active_subject_id:   u64,
	last_frame_seq:      u64,
	last_now_ms:         i64,
	stream_evictions:    u64,
	applied_events:      u64,
}

copy_fixed_text :: proc(dst: []u8, src: string) -> u8 {
	n := min(len(dst), len(src))
	for i in 0 ..< n {
		dst[i] = src[i]
	}
	return u8(n)
}

fixed_text_string :: proc(buf: []u8, n: u8) -> string {
	m := min(int(n), len(buf))
	if m <= 0 do return ""
	return string(buf[:m])
}

stream_key_venue :: proc(key: ^Stream_Key) -> string {
	if key == nil do return ""
	return fixed_text_string(key.venue[:], key.venue_len)
}

stream_key_symbol :: proc(key: ^Stream_Key) -> string {
	if key == nil do return ""
	return fixed_text_string(key.symbol[:], key.symbol_len)
}

stream_key_timeframe :: proc(key: ^Stream_Key) -> string {
	if key == nil do return ""
	return fixed_text_string(key.timeframe[:], key.timeframe_len)
}

stream_key_subject :: proc(key: ^Stream_Key) -> string {
	if key == nil do return ""
	return fixed_text_string(key.subject[:], key.subject_len)
}

market_store_reset :: proc(store: ^Market_Store) {
	if store == nil do return
	store^ = {}
}

market_store_set_active_subject :: proc(store: ^Market_Store, subject_id: u64) {
	if store == nil do return
	if subject_id == 0 do return
	store.active_subject_id = subject_id
}

market_store_stream_find_idx :: proc(store: ^Market_Store, subject_id: u64) -> int {
	if store == nil do return -1
	if subject_id == 0 do return -1
	for i in 0 ..< MARKET_STREAM_CAP {
		if store.streams[i].used && store.streams[i].key.subject_id == subject_id do return i
	}
	return -1
}

@(private = "file")
market_store_alloc_stream :: proc(store: ^Market_Store, subject_id: u64) -> ^Market_Stream {
	if store == nil || subject_id == 0 do return nil
	for i in 0 ..< MARKET_STREAM_CAP {
		if !store.streams[i].used {
			store.streams[i] = Market_Stream{}
			store.streams[i].used = true
			store.streams[i].key.subject_id = subject_id
			store.stream_count += 1
			return &store.streams[i]
		}
	}

	start := store.next_evict_idx
	idx := start
	for _ in 0 ..< MARKET_STREAM_CAP {
		if !store.streams[idx].used {
			break
		}
		if store.streams[idx].key.subject_id != store.active_subject_id {
			break
		}
		idx = (idx + 1) % MARKET_STREAM_CAP
	}
	store.next_evict_idx = (idx + 1) % MARKET_STREAM_CAP
	if store.streams[idx].used {
		store.stream_evictions += 1
	}
	store.streams[idx] = Market_Stream{}
	store.streams[idx].used = true
	store.streams[idx].key.subject_id = subject_id
	if store.stream_count < MARKET_STREAM_CAP do store.stream_count += 1
	return &store.streams[idx]
}

market_store_stream_get_or_alloc :: proc(store: ^Market_Store, subject_id: u64) -> ^Market_Stream {
	if store == nil do return nil
	if idx := market_store_stream_find_idx(store, subject_id); idx >= 0 {
		return &store.streams[idx]
	}
	return market_store_alloc_stream(store, subject_id)
}

market_store_stream_for_subject :: proc(store: ^Market_Store, subject_id: u64) -> ^Market_Stream {
	if store == nil do return nil
	if subject_id == 0 do return nil
	if idx := market_store_stream_find_idx(store, subject_id); idx >= 0 {
		return &store.streams[idx]
	}
	return nil
}

market_store_active_stream :: proc(store: ^Market_Store) -> ^Market_Stream {
	if store == nil do return nil
	if store.active_subject_id == 0 do return nil
	return market_store_stream_for_subject(store, store.active_subject_id)
}

market_store_set_stream_info :: proc(store: ^Market_Store, subject_id: u64, info: ports.MD_Stream_Info) {
	if store == nil || subject_id == 0 do return
	stream := market_store_stream_get_or_alloc(store, subject_id)
	if stream == nil do return
	stream.key.subject_id = subject_id
	stream.key.channel = info.channel
	stream.key.venue_len = copy_fixed_text(stream.key.venue[:], info.venue)
	stream.key.symbol_len = copy_fixed_text(stream.key.symbol[:], info.symbol)
	stream.key.timeframe_len = copy_fixed_text(stream.key.timeframe[:], info.timeframe)
	stream.key.subject_len = copy_fixed_text(stream.key.subject[:], info.subject)
}

market_stream_evidence_get_newest :: proc(stream: ^Market_Stream, i: int) -> (Evidence_Entry, bool) {
	if stream == nil do return {}, false
	if i < 0 || i >= stream.evidence_count do return {}, false
	idx := (stream.evidence_head - 1 - i + EVIDENCE_RING_CAP) % EVIDENCE_RING_CAP
	return stream.evidence[idx], true
}

@(private = "file")
market_stream_push_evidence :: proc(stream: ^Market_Stream, evt: ports.MD_Evidence_Event, subject_id: u64) {
	if stream == nil do return
	if stream.evidence_count >= EVIDENCE_RING_CAP {
		stream.evictions += 1
	}
	idx := stream.evidence_head
	stream.evidence[idx] = Evidence_Entry{
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
	stream.evidence_head = (stream.evidence_head + 1) % EVIDENCE_RING_CAP
	if stream.evidence_count < EVIDENCE_RING_CAP do stream.evidence_count += 1
}

market_store_apply_event :: proc(store: ^Market_Store, evt: ^ports.MD_Event) -> ^Market_Stream {
	if store == nil || evt == nil do return nil
	subject_id := evt.source.subject_id
	if subject_id == 0 do return nil
	stream := market_store_stream_get_or_alloc(store, subject_id)
	if stream == nil do return nil

	stream.last_seq = evt.source.seq
	stream.last_unix = evt.unix
	stream.event_count += 1
	store.applied_events += 1

	switch evt.kind {
	case .Trade:
		if stream.trades.count >= services.TRADES_CAP do stream.evictions += 1
		services.push_trade(&stream.trades, services.Trade_Entry{
			price = evt.data.trade.price,
			qty   = evt.data.trade.qty,
			side  = evt.data.trade.is_buy ? .Buy : .Sell,
			unix  = evt.data.trade.unix,
		})
	case .Orderbook_Snapshot:
		services.update_orderbook(
			&stream.orderbook,
			evt.data.ob.ask_prices[:evt.data.ob.ask_count],
			evt.data.ob.ask_sizes[:evt.data.ob.ask_count],
			evt.data.ob.bid_prices[:evt.data.ob.bid_count],
			evt.data.ob.bid_sizes[:evt.data.ob.bid_count],
			evt.data.ob.last_price,
			evt.data.ob.unix,
		)
	case .Stats:
		if stream.stats.count >= services.STATS_CAP do stream.evictions += 1
		services.push_stats(&stream.stats, services.Stats_Entry{
			mark_price = evt.data.stats.mark_price,
			funding    = evt.data.stats.funding,
			liq_buy    = evt.data.stats.tbuy,
			liq_sell   = evt.data.stats.tsell,
			unix       = evt.data.stats.unix,
		})
	case .Heatmap:
		snap: services.Heatmap_Snapshot
		snap.unix = evt.data.heatmap.unix
		snap.window_start_ms = evt.data.heatmap.window_start_ms
		snap.price_group = evt.data.heatmap.price_group
		snap.min_price = evt.data.heatmap.min_price
		snap.max_price = evt.data.heatmap.max_price
		snap.max_size = evt.data.heatmap.max_size
		snap.level_count = min(evt.data.heatmap.level_count, services.HEATMAP_LEVEL_CAP)
		for i in 0 ..< snap.level_count {
			snap.levels[i] = services.Heatmap_Level{
				price = evt.data.heatmap.prices[i],
				size  = evt.data.heatmap.sizes[i],
			}
		}
		if stream.heatmap.count >= services.HEATMAP_SNAP_CAP do stream.evictions += 1
		services.push_heatmap_snapshot(&stream.heatmap, snap)
	case .VPVR:
		count := min(evt.data.vpvr.level_count, services.VPVR_BUCKET_CAP)
		services.update_vpvr(
			&stream.vpvr,
			evt.data.vpvr.prices,
			evt.data.vpvr.buys,
			evt.data.vpvr.sells,
			count,
			evt.data.vpvr.price_group,
		)
	case .Candle:
		if stream.candles.count >= services.CANDLE_CAP do stream.evictions += 1
		services.push_candle(&stream.candles, services.Candle_Entry{
			open            = evt.data.candle.open,
			high            = evt.data.candle.high,
			low             = evt.data.candle.low,
			close           = evt.data.candle.close,
			volume          = evt.data.candle.volume,
			buy_vol         = evt.data.candle.buy_vol,
			sell_vol        = evt.data.candle.sell_vol,
			trade_count     = evt.data.candle.trade_count,
			window_start_ts = evt.data.candle.window_start_ts,
			window_end_ts   = evt.data.candle.window_end_ts,
			is_closed       = evt.data.candle.is_closed,
		})
	case .Range_Candle_Batch:
		for i in 0 ..< evt.data.range_candles.count {
			cd := evt.data.range_candles.candles[i]
			if stream.candles.count >= services.CANDLE_CAP do stream.evictions += 1
			services.upsert_candle_chrono(&stream.candles, services.Candle_Entry{
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
	case .Evidence:
		market_stream_push_evidence(stream, evt.data.evidence, subject_id)
	case .Signal:
		services.signal_store_push(&stream.signals, services.Signal_Entry{
			kind            = evt.data.signal.kind,
			kind_len        = evt.data.signal.kind_len,
			severity        = evt.data.signal.severity,
			severity_len    = evt.data.signal.severity_len,
			confidence      = evt.data.signal.confidence,
			reason          = evt.data.signal.reason,
			reason_len      = evt.data.signal.reason_len,
			regime          = evt.data.signal.regime,
			regime_len      = evt.data.signal.regime_len,
			regime_strength = evt.data.signal.regime_strength,
			unix            = evt.data.signal.unix,
			subject_id      = subject_id,
			seq             = evt.source.seq,
		})
	}

	if store.active_subject_id == 0 {
		store.active_subject_id = subject_id
	}
	return stream
}

market_store_seed_demo :: proc(store: ^Market_Store, subject_id: u64) {
	if store == nil do return
	sid := subject_id
	if sid == 0 do sid = 1
	stream := market_store_stream_get_or_alloc(store, sid)
	if stream == nil do return
	stream.key.subject_id = sid
	stream.key.channel = .Candles
	stream.key.venue_len = copy_fixed_text(stream.key.venue[:], "demo")
	stream.key.symbol_len = copy_fixed_text(stream.key.symbol[:], "DEMO:SPOT")
	stream.key.timeframe_len = copy_fixed_text(stream.key.timeframe[:], "1m")
	stream.key.subject_len = copy_fixed_text(stream.key.subject[:], "marketdata/demo/DEMO:SPOT/candles/1m")
	services.fill_demo_trades(&stream.trades)
	services.fill_demo_orderbook(&stream.orderbook)
	services.fill_demo_stats(&stream.stats)
	services.fill_demo_heatmaps(&stream.heatmap)
	services.fill_demo_vpvr(&stream.vpvr)
	services.fill_demo_candles(&stream.candles)
	store.active_subject_id = sid
}
