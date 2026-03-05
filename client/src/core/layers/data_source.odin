package layers

import "mr:ports"
import "mr:util"

// Canonical DataSource: polls WS Terminal V1 normalized events from the port
// and applies them to the bounded MarketStore.
//
// IMPORTANT: The WS protocol uses separate subject strings per channel type
// (e.g. "aggregation.candle/binance/BTCUSDT/1m" vs "aggregation.stats/binance/BTCUSDT/1m").
// Each subject produces a distinct subject_id (FNV-1a hash).
//
// Market_Store, however, stores ONE Market_Stream per market (venue+symbol) with
// sub-stores for every data type (candle, stats, trades, etc.).
//
// This DataSource bridges the gap: it resolves a market-level ID from the
// subscription's venue+symbol via describe_stream, then redirects the event
// so all channels for the same market converge into the same Market_Stream.

DATA_SOURCE_POLL_CAP :: 96

// Small inline cache: per-channel subject_id → market_id.
MARKET_ID_CACHE_CAP :: 32

@(private = "file")
Market_Id_Entry :: struct {
	channel_sid: u64,
	market_id:   u64,
}

Data_Source :: struct {
	poll_buf:          [DATA_SOURCE_POLL_CAP]ports.MD_Event,
	processed_total:   u64,
	stream_info_hits:  u64,
	stream_info_miss:  u64,
	mid_cache:         [MARKET_ID_CACHE_CAP]Market_Id_Entry,
	mid_count:         int,
}

Data_Source_Result :: struct {
	processed:      int,
	subject_ids:    [DATA_SOURCE_POLL_CAP]u64,
	subject_count:  int,
	last_subject:   u64,
}

@(private = "file")
subject_seen_idx :: proc(result: ^Data_Source_Result, subject_id: u64) -> int {
	if result == nil do return -1
	for i in 0 ..< result.subject_count {
		if result.subject_ids[i] == subject_id do return i
	}
	return -1
}

// Look up cached market_id for a per-channel subject_id.
@(private = "file")
mid_cache_lookup :: proc(ds: ^Data_Source, channel_sid: u64) -> (u64, bool) {
	for i in 0 ..< ds.mid_count {
		if ds.mid_cache[i].channel_sid == channel_sid {
			return ds.mid_cache[i].market_id, true
		}
	}
	return 0, false
}

// Insert into cache (overwrite oldest on overflow).
@(private = "file")
mid_cache_insert :: proc(ds: ^Data_Source, channel_sid: u64, market_id: u64) {
	if ds.mid_count < MARKET_ID_CACHE_CAP {
		ds.mid_cache[ds.mid_count] = {channel_sid, market_id}
		ds.mid_count += 1
	} else {
		// Overwrite slot 0 (simple eviction — cache is small).
		ds.mid_cache[0] = {channel_sid, market_id}
	}
}

// Resolve the market-level subject_id for an event.
// Uses describe_stream to get venue+symbol, then computes market_id64.
// Falls back to the event's original subject_id if resolution fails.
@(private = "file")
resolve_market_id :: proc(ds: ^Data_Source, md: ports.Marketdata_Port, channel_sid: u64, store: ^Market_Store) -> u64 {
	// Fast path: cached.
	if mid, ok := mid_cache_lookup(ds, channel_sid); ok {
		return mid
	}
	// Slow path: ask adapter for stream metadata.
	if md.describe_stream != nil {
		info: ports.MD_Stream_Info
		if md.describe_stream(channel_sid, &info) {
			ds.stream_info_hits += 1
			mid := util.market_id64(info.venue, info.symbol)
			if mid != 0 {
				mid_cache_insert(ds, channel_sid, mid)
				market_store_set_stream_info(store, mid, info)
				return mid
			}
		} else {
			ds.stream_info_miss += 1
		}
	}
	// Fallback: use per-channel subject_id (creates isolated stream — not ideal
	// but keeps data from being lost).
	return channel_sid
}

data_source_poll_and_apply :: proc(ds: ^Data_Source, md: ports.Marketdata_Port, store: ^Market_Store) -> Data_Source_Result {
	result: Data_Source_Result
	if ds == nil || store == nil do return result
	if md.poll == nil do return result

	n := md.poll(ds.poll_buf[:])
	result.processed = n
	store.last_frame_seq += 1
	if md.now_ms != nil {
		store.last_now_ms = md.now_ms()
	}

	for i in 0 ..< n {
		evt := &ds.poll_buf[i]
		channel_sid := evt.source.subject_id
		if channel_sid == 0 do continue

		// Resolve market-level subject_id so ALL channels for the same market
		// aggregate into one Market_Stream.
		market_id := resolve_market_id(ds, md, channel_sid, store)
		evt.source.subject_id = market_id

		_ = market_store_apply_event(store, evt)
		if result.subject_count < len(result.subject_ids) && subject_seen_idx(&result, market_id) < 0 {
			result.subject_ids[result.subject_count] = market_id
			result.subject_count += 1
		}
		result.last_subject = market_id
	}

	ds.processed_total += u64(max(n, 0))
	if store.active_subject_id == 0 && result.last_subject != 0 {
		store.active_subject_id = result.last_subject
	}
	return result
}
