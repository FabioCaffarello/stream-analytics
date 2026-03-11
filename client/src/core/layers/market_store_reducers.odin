package layers

import "mr:ports"
import "mr:services"

market_store_reduce_trade :: proc(stream: ^Market_Stream, evt: ^ports.MD_Event, active_tf_ms: i64 = 0) {
	if stream == nil || evt == nil do return
	stream.trades_frames += 1
	if stream.trades.count >= services.TRADES_CAP do stream.evictions += 1
	if stream.trades.count >= services.TRADES_CAP do stream.trades_drops += 1

	price := evt.data.trade.price
	qty   := evt.data.trade.qty
	is_buy := evt.data.trade.is_buy
	unix  := evt.data.trade.unix

	services.push_trade(&stream.trades, services.Trade_Entry{
		price = price,
		qty   = qty,
		side  = is_buy ? .Buy : .Sell,
		unix  = unix,
	})

	// S148: Accumulate into per-stream DOM store (TF-independent fill tracking).
	// price_group=0 lets DOM_Store use its internal default (1.0).
	services.dom_store_push_trade(&stream.dom, price, qty, is_buy, unix, 0)

	// S155: Accumulate into per-stream footprint store (candle-aligned bins).
	// Requires active TF to bucket trades into candle windows.
	// price_group=0 lets Footprint_Store use its internal default (1.0).
	if active_tf_ms > 0 {
		services.footprint_store_push_trade(
			&stream.footprint, price, qty, is_buy,
			unix, active_tf_ms, 0,
		)
	}
}

market_store_reduce_tape :: proc(stream: ^Market_Stream, evt: ^ports.MD_Event) {
	if stream == nil || evt == nil do return
	stream.tape_frames += 1
	if stream.trades.count >= services.TRADES_CAP do stream.evictions += 1
	if stream.trades.count >= services.TRADES_CAP do stream.tape_drops += 1
	qty := max(evt.data.tape.total_volume, evt.data.tape.buy_volume + evt.data.tape.sell_volume)
	if qty > 0 && evt.data.tape.last_price > 0 {
		stream.tape_fallbacks += 1
		services.push_trade(&stream.trades, services.Trade_Entry{
			price = evt.data.tape.last_price,
			qty   = qty,
			side  = evt.data.tape.buy_volume >= evt.data.tape.sell_volume ? .Buy : .Sell,
			unix  = evt.data.tape.unix,
		})
	}
}

market_store_reduce_orderbook :: proc(stream: ^Market_Stream, evt: ^ports.MD_Event) {
	if stream == nil || evt == nil do return
	stream.orderbook_frames += 1
	if evt.data.ob.ask_count <= 0 || evt.data.ob.bid_count <= 0 {
		stream.orderbook_fallbacks += 1
	}
	services.update_orderbook(
		&stream.orderbook,
		evt.data.ob.ask_prices[:evt.data.ob.ask_count],
		evt.data.ob.ask_sizes[:evt.data.ob.ask_count],
		evt.data.ob.bid_prices[:evt.data.ob.bid_count],
		evt.data.ob.bid_sizes[:evt.data.ob.bid_count],
		evt.data.ob.last_price,
		evt.data.ob.unix,
	)
}

market_store_reduce_stats :: proc(stream: ^Market_Stream, evt: ^ports.MD_Event) {
	if stream == nil || evt == nil do return
	stream.stats_frames += 1
	if stream.stats.count >= services.STATS_CAP do stream.evictions += 1
	if stream.stats.count >= services.STATS_CAP do stream.stats_drops += 1
	if evt.data.stats.quality_flags != 0 {
		stream.stats_fallbacks += 1
	}
	services.push_stats(&stream.stats, services.Stats_Entry{
		mark_price    = evt.data.stats.mark_price,
		funding       = evt.data.stats.funding,
		liq_buy       = evt.data.stats.tbuy,
		liq_sell      = evt.data.stats.tsell,
		window_ms     = evt.data.stats.window_ms,
		ts_ingest_ms  = evt.data.stats.ts_ingest_ms,
		quality_flags = evt.data.stats.quality_flags,
		unix          = evt.data.stats.unix,
	})
}

market_store_reduce_heatmap :: proc(stream: ^Market_Stream, evt: ^ports.MD_Event) {
	if stream == nil || evt == nil do return
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
}

market_store_reduce_vpvr :: proc(stream: ^Market_Stream, evt: ^ports.MD_Event) {
	if stream == nil || evt == nil do return
	count := min(evt.data.vpvr.level_count, services.VPVR_BUCKET_CAP)
	services.update_vpvr(
		&stream.vpvr,
		evt.data.vpvr.prices,
		evt.data.vpvr.buys,
		evt.data.vpvr.sells,
		count,
		evt.data.vpvr.price_group,
	)
}

market_store_reduce_candle :: proc(stream: ^Market_Stream, evt: ^ports.MD_Event) {
	if stream == nil || evt == nil do return
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
}

market_store_reduce_range_candles :: proc(stream: ^Market_Stream, evt: ^ports.MD_Event) {
	if stream == nil || evt == nil do return
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
}

market_store_reduce_evidence :: proc(stream: ^Market_Stream, evt: ^ports.MD_Event, subject_id: u64) {
	if stream == nil || evt == nil do return
	stream.evidence_frames += 1
	stream.last_evidence_seq = evt.source.seq
	if evt.data.evidence.feature_count <= 0 || evt.data.evidence.reason_len == 0 {
		stream.evidence_fallbacks += 1
	}
	market_stream_push_evidence(stream, evt.data.evidence, subject_id)
}

market_store_reduce_signal :: proc(stream: ^Market_Stream, evt: ^ports.MD_Event, subject_id: u64) {
	if stream == nil || evt == nil do return
	stream.signal_frames += 1
	stream.last_signal_seq = evt.source.seq
	if stream.last_evidence_seq > 0 {
		stream.last_linked_evidence_seq = stream.last_evidence_seq
		stream.signal_evidence_links += 1
	} else {
		stream.signal_fallbacks += 1
	}
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

market_store_reduce_analytics :: proc(stream: ^Market_Stream, evt: ^ports.MD_Event) {
	if stream == nil || evt == nil do return
	entry: services.Analytics_Entry
	entry.ts_ms = evt.unix
	entry.seq = evt.source.seq
	#partial switch evt.kind {
	case .Open_Interest:
		entry.kind = .Open_Interest
		entry.values[0] = evt.data.open_interest.open_interest
		entry.values[1] = evt.data.open_interest.delta
		entry.values[2] = evt.data.open_interest.delta_pct
		entry.window_start_ms = evt.data.open_interest.window_start_ts
		entry.window_end_ms = evt.data.open_interest.window_end_ts
	case .Delta_Volume:
		entry.kind = .Delta_Volume
		entry.values[0] = evt.data.delta_volume.buy_volume
		entry.values[1] = evt.data.delta_volume.sell_volume
		entry.values[2] = evt.data.delta_volume.delta_volume
		entry.window_start_ms = evt.data.delta_volume.window_start_ts
		entry.window_end_ms = evt.data.delta_volume.window_end_ts
	case .CVD:
		entry.kind = .CVD
		entry.values[0] = evt.data.cvd.delta_volume
		entry.values[1] = evt.data.cvd.cvd
		entry.window_start_ms = evt.data.cvd.window_start_ts
		entry.window_end_ms = evt.data.cvd.window_end_ts
	case .Bar_Stats:
		entry.kind = .Bar_Stats
		entry.values[0] = f64(evt.data.bar_stats.trade_count)
		entry.values[1] = f64(evt.data.bar_stats.buy_count)
		entry.values[2] = f64(evt.data.bar_stats.sell_count)
		entry.values[3] = evt.data.bar_stats.total_volume
		entry.values[4] = evt.data.bar_stats.buy_volume
		entry.values[5] = evt.data.bar_stats.sell_volume
		entry.values[6] = evt.data.bar_stats.vwap_price
		entry.values[7] = evt.data.bar_stats.imbalance
		if evt.data.bar_stats.is_burst do entry.flags = 1
		entry.window_start_ms = evt.data.bar_stats.window_start_ts
		entry.window_end_ms = evt.data.bar_stats.window_end_ts
	case:
		return
	}
	services.push_analytics(&stream.analytics, entry)
}
