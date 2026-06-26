package services

// S81: Analytics range fetch + store population.
// Parses JSON array responses from /api/v1/{cvd,delta_volume,bar_stats} cold reader
// and populates Analytics_Store in chronological order (oldest first → newest last).
// Deterministic: same response always produces same store state.

import "core:strconv"

// Budget: max entries to parse per fetch to bound CPU.
ANALYTICS_RANGE_BUDGET :: 64

// Parse CVD range response: [{"DeltaVolume":..,"CVD":..,"WindowStartTs":..,"WindowEndTs":..,"Seq":..,"TsIngestMs":..}, ...]
parse_analytics_cvd_range :: proc(store: ^Analytics_Store, raw: []u8) -> int {
	if store == nil || len(raw) == 0 do return 0
	count := 0
	pos := 0
	for pos < len(raw) && count < ANALYTICS_RANGE_BUDGET {
		// Find next object start.
		obj_start := find_byte(raw, pos, '{')
		if obj_start < 0 do break
		obj_end := find_byte(raw, obj_start, '}')
		if obj_end < 0 do break
		obj := raw[obj_start:obj_end + 1]

		entry := Analytics_Entry{kind = .CVD}
		entry.values[0] = json_extract_f64(obj, "DeltaVolume")
		entry.values[1] = json_extract_f64(obj, "CVD")
		entry.window_start_ms = json_extract_i64(obj, "WindowStartTs")
		entry.window_end_ms = json_extract_i64(obj, "WindowEndTs")
		entry.seq = json_extract_i64(obj, "Seq")
		entry.ts_ms = json_extract_i64(obj, "TsIngestMs")

		push_analytics(store, entry)
		count += 1
		pos = obj_end + 1
	}
	return count
}

// Parse Delta Volume range response.
parse_analytics_delta_volume_range :: proc(store: ^Analytics_Store, raw: []u8) -> int {
	if store == nil || len(raw) == 0 do return 0
	count := 0
	pos := 0
	for pos < len(raw) && count < ANALYTICS_RANGE_BUDGET {
		obj_start := find_byte(raw, pos, '{')
		if obj_start < 0 do break
		obj_end := find_byte(raw, obj_start, '}')
		if obj_end < 0 do break
		obj := raw[obj_start:obj_end + 1]

		entry := Analytics_Entry{kind = .Delta_Volume}
		entry.values[0] = json_extract_f64(obj, "BuyVolume")
		entry.values[1] = json_extract_f64(obj, "SellVolume")
		entry.values[2] = json_extract_f64(obj, "DeltaVolume")
		entry.window_start_ms = json_extract_i64(obj, "WindowStartTs")
		entry.window_end_ms = json_extract_i64(obj, "WindowEndTs")
		entry.seq = json_extract_i64(obj, "Seq")
		entry.ts_ms = json_extract_i64(obj, "TsIngestMs")

		push_analytics(store, entry)
		count += 1
		pos = obj_end + 1
	}
	return count
}

// Parse Bar Stats range response.
parse_analytics_bar_stats_range :: proc(store: ^Analytics_Store, raw: []u8) -> int {
	if store == nil || len(raw) == 0 do return 0
	count := 0
	pos := 0
	for pos < len(raw) && count < ANALYTICS_RANGE_BUDGET {
		obj_start := find_byte(raw, pos, '{')
		if obj_start < 0 do break
		obj_end := find_byte(raw, obj_start, '}')
		if obj_end < 0 do break
		obj := raw[obj_start:obj_end + 1]

		entry := Analytics_Entry{kind = .Bar_Stats}
		entry.values[0] = json_extract_f64(obj, "TradeCount")
		entry.values[1] = json_extract_f64(obj, "BuyCount")
		entry.values[2] = json_extract_f64(obj, "SellCount")
		entry.values[3] = json_extract_f64(obj, "TotalVolume")
		entry.values[4] = json_extract_f64(obj, "BuyVolume")
		entry.values[5] = json_extract_f64(obj, "SellVolume")
		entry.values[6] = json_extract_f64(obj, "VwapPrice")
		entry.values[7] = json_extract_f64(obj, "Imbalance")
		is_burst := json_extract_bool(obj, "IsBurst")
		if is_burst do entry.flags = 1
		entry.window_start_ms = json_extract_i64(obj, "WindowStartTs")
		entry.window_end_ms = json_extract_i64(obj, "WindowEndTs")
		entry.seq = json_extract_i64(obj, "Seq")
		entry.ts_ms = json_extract_i64(obj, "TsIngestMs")

		push_analytics(store, entry)
		count += 1
		pos = obj_end + 1
	}
	return count
}

// Parse OI range response: [{"open_interest":..,"delta":..,"delta_pct":..,"cadence_hint_ms":..,"confidence":"high","window_start_ts":..,"window_end_ts":..,"seq":..,"ts_ingest_ms":..}, ...]
parse_analytics_oi_range :: proc(store: ^Analytics_Store, raw: []u8) -> int {
	if store == nil || len(raw) == 0 do return 0
	count := 0
	pos := 0
	for pos < len(raw) && count < ANALYTICS_RANGE_BUDGET {
		obj_start := find_byte(raw, pos, '{')
		if obj_start < 0 do break
		obj_end := find_byte(raw, obj_start, '}')
		if obj_end < 0 do break
		obj := raw[obj_start:obj_end + 1]

		entry := Analytics_Entry{kind = .Open_Interest}
		entry.values[0] = json_extract_f64(obj, "open_interest")
		entry.values[1] = json_extract_f64(obj, "delta")
		entry.values[2] = json_extract_f64(obj, "delta_pct")
		entry.cadence_hint_ms = json_extract_i64(obj, "cadence_hint_ms")
		entry.confidence = parse_oi_confidence(obj)
		entry.window_start_ms = json_extract_i64(obj, "window_start_ts")
		entry.window_end_ms = json_extract_i64(obj, "window_end_ts")
		entry.seq = json_extract_i64(obj, "seq")
		entry.ts_ms = json_extract_i64(obj, "ts_ingest_ms")

		push_analytics(store, entry)
		count += 1
		pos = obj_end + 1
	}
	return count
}

// Map confidence string to u8: "high"->1, "medium"->2, "low"->3, else->0.
@(private = "file")
parse_oi_confidence :: proc(obj: []u8) -> u8 {
	pos := json_find_key(obj, "confidence")
	if pos < 0 do return 0
	colon := find_byte(obj, pos + len("confidence") + 1, ':')
	if colon < 0 do return 0
	// Find opening quote of string value.
	start := colon + 1
	for start < len(obj) && (obj[start] == ' ' || obj[start] == '\t') do start += 1
	if start >= len(obj) || obj[start] != '"' do return 0
	start += 1 // skip opening quote
	end := find_byte(obj, start, '"')
	if end < 0 do return 0
	val := string(obj[start:end])
	switch val {
	case "high":   return 1
	case "medium": return 2
	case "low":    return 3
	}
	return 0
}

// --- JSON helpers (zero-alloc, linear scan) ---
// Package-visible so other service parsers (e.g. session_vpvr_range) can reuse.

find_byte :: proc(data: []u8, start: int, b: u8) -> int {
	for i := start; i < len(data); i += 1 {
		if data[i] == b do return i
	}
	return -1
}

json_extract_f64 :: proc(obj: []u8, key: string) -> f64 {
	pos := json_find_key(obj, key)
	if pos < 0 do return 0
	// Skip to colon, then to value start.
	colon := find_byte(obj, pos + len(key) + 1, ':')
	if colon < 0 do return 0
	start := colon + 1
	for start < len(obj) && (obj[start] == ' ' || obj[start] == '\t') do start += 1
	if start >= len(obj) do return 0
	end := start
	for end < len(obj) && obj[end] != ',' && obj[end] != '}' && obj[end] != ' ' do end += 1
	val, ok := strconv.parse_f64(string(obj[start:end]))
	if !ok do return 0
	return val
}

json_extract_i64 :: proc(obj: []u8, key: string) -> i64 {
	return i64(json_extract_f64(obj, key))
}

json_extract_bool :: proc(obj: []u8, key: string) -> bool {
	pos := json_find_key(obj, key)
	if pos < 0 do return false
	colon := find_byte(obj, pos + len(key) + 1, ':')
	if colon < 0 do return false
	start := colon + 1
	for start < len(obj) && (obj[start] == ' ' || obj[start] == '\t') do start += 1
	if start >= len(obj) do return false
	return obj[start] == 't'
}

json_find_key :: proc(obj: []u8, key: string) -> int {
	kb := transmute([]u8)key
	for i := 0; i < len(obj) - len(key) - 2; i += 1 {
		if obj[i] == '"' {
			match := true
			for k := 0; k < len(key); k += 1 {
				if i + 1 + k >= len(obj) || obj[i + 1 + k] != kb[k] {
					match = false
					break
				}
			}
			if match && i + 1 + len(key) < len(obj) && obj[i + 1 + len(key)] == '"' {
				return i
			}
		}
	}
	return -1
}
