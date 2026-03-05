package services

@(private = "package")
batch_is_ws :: proc(c: u8) -> bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

@(private = "package")
batch_skip_ws :: proc(raw: []u8, idx: ^int) {
	for idx^ < len(raw) && batch_is_ws(raw[idx^]) {
		idx^ += 1
	}
}

@(private = "package")
batch_parse_string_span :: proc(raw: []u8, idx: ^int, start: ^int, end: ^int) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ >= len(raw) || raw[idx^] != '"' do return false
	idx^ += 1
	start^ = idx^
	escaped := false
	for idx^ < len(raw) {
		c := raw[idx^]
		if escaped {
			escaped = false
			idx^ += 1
			continue
		}
		if c == '\\' {
			escaped = true
			idx^ += 1
			continue
		}
		if c == '"' {
			end^ = idx^
			idx^ += 1
			return true
		}
		idx^ += 1
	}
	return false
}

@(private = "package")
batch_parse_int :: proc(raw: []u8, idx: ^int, out: ^i64) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ >= len(raw) do return false
	sign := i64(1)
	if raw[idx^] == '-' {
		sign = -1
		idx^ += 1
	}
	if idx^ >= len(raw) || raw[idx^] < '0' || raw[idx^] > '9' do return false
	value := i64(0)
	for idx^ < len(raw) {
		c := raw[idx^]
		if c < '0' || c > '9' do break
		value = value * 10 + i64(c - '0')
		idx^ += 1
	}
	out^ = value * sign
	return true
}

@(private = "package")
batch_skip_number :: proc(raw: []u8, idx: ^int) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ >= len(raw) do return false
	c := raw[idx^]
	if c == '-' || c == '+' do idx^ += 1
	have_digit := false
	for idx^ < len(raw) {
		ch := raw[idx^]
		if ch < '0' || ch > '9' do break
		have_digit = true
		idx^ += 1
	}
	if idx^ < len(raw) && raw[idx^] == '.' {
		idx^ += 1
		for idx^ < len(raw) {
			ch := raw[idx^]
			if ch < '0' || ch > '9' do break
			have_digit = true
			idx^ += 1
		}
	}
	if idx^ < len(raw) && (raw[idx^] == 'e' || raw[idx^] == 'E') {
		idx^ += 1
		if idx^ < len(raw) && (raw[idx^] == '+' || raw[idx^] == '-') do idx^ += 1
		for idx^ < len(raw) {
			ch := raw[idx^]
			if ch < '0' || ch > '9' do break
			have_digit = true
			idx^ += 1
		}
	}
	return have_digit
}

@(private = "package")
batch_skip_literal :: proc(raw: []u8, idx: ^int, lit: string) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ + len(lit) > len(raw) do return false
	for i in 0 ..< len(lit) {
		if raw[idx^ + i] != lit[i] do return false
	}
	idx^ += len(lit)
	return true
}

@(private = "package")
batch_skip_value :: proc(raw: []u8, idx: ^int) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ >= len(raw) do return false
	switch raw[idx^] {
	case '"':
		s, e := 0, 0
		return batch_parse_string_span(raw, idx, &s, &e)
	case '{':
		idx^ += 1
		batch_skip_ws(raw, idx)
		if idx^ < len(raw) && raw[idx^] == '}' {
			idx^ += 1
			return true
		}
		for {
			ks, ke := 0, 0
			if !batch_parse_string_span(raw, idx, &ks, &ke) do return false
			batch_skip_ws(raw, idx)
			if idx^ >= len(raw) || raw[idx^] != ':' do return false
			idx^ += 1
			if !batch_skip_value(raw, idx) do return false
			batch_skip_ws(raw, idx)
			if idx^ >= len(raw) do return false
			if raw[idx^] == ',' {
				idx^ += 1
				continue
			}
			if raw[idx^] == '}' {
				idx^ += 1
				return true
			}
			return false
		}
	case '[':
		idx^ += 1
		batch_skip_ws(raw, idx)
		if idx^ < len(raw) && raw[idx^] == ']' {
			idx^ += 1
			return true
		}
		for {
			if !batch_skip_value(raw, idx) do return false
			batch_skip_ws(raw, idx)
			if idx^ >= len(raw) do return false
			if raw[idx^] == ',' {
				idx^ += 1
				continue
			}
			if raw[idx^] == ']' {
				idx^ += 1
				return true
			}
			return false
		}
	case 't':
		return batch_skip_literal(raw, idx, "true")
	case 'f':
		return batch_skip_literal(raw, idx, "false")
	case 'n':
		return batch_skip_literal(raw, idx, "null")
	case:
		return batch_skip_number(raw, idx)
	}
}

@(private = "package")
batch_key_equals :: proc(raw: []u8, start, end: int, lit: string) -> bool {
	if end-start != len(lit) do return false
	for i in 0 ..< len(lit) {
		if raw[start + i] != lit[i] do return false
	}
	return true
}

@(private = "package")
batch_copy_string :: proc(dst: []u8, raw: []u8, start, end: int) -> u8 {
	n := end - start
	if n < 0 do return 0
	if n > len(dst) do n = len(dst)
	for i in 0 ..< n {
		dst[i] = raw[start + i]
	}
	return u8(n)
}

@(private = "package")
batch_parse_events_segment :: proc(
	raw: []u8,
	idx: ^int,
	skip_events: int,
	out: ^Parsed_Batched_Frame,
) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ >= len(raw) || raw[idx^] != '[' do return false
	idx^ += 1

	total := 0
	stored := 0

	batch_skip_ws(raw, idx)
	if idx^ < len(raw) && raw[idx^] == ']' {
		idx^ += 1
		out.total_events = 0
		out.event_count = 0
		out.has_more = false
		return true
	}

	for {
		batch_skip_ws(raw, idx)
		if idx^ >= len(raw) || raw[idx^] != '{' do return false
		idx^ += 1

		event := Parsed_Batch_Event_View{
			event_index = total,
		}
		for {
			ks, ke := 0, 0
			if !batch_parse_string_span(raw, idx, &ks, &ke) do return false
			batch_skip_ws(raw, idx)
			if idx^ >= len(raw) || raw[idx^] != ':' do return false
			idx^ += 1

			switch {
			case batch_key_equals(raw, ks, ke, "dseq"):
				if !batch_parse_int(raw, idx, &event.dseq) do return false
			case batch_key_equals(raw, ks, ke, "dprev"):
				if !batch_parse_int(raw, idx, &event.dprev) do return false
			case batch_key_equals(raw, ks, ke, "dts"):
				if !batch_parse_int(raw, idx, &event.dts) do return false
			case batch_key_equals(raw, ks, ke, "dti"):
				if !batch_parse_int(raw, idx, &event.dti) do return false
			case batch_key_equals(raw, ks, ke, "p"):
				batch_skip_ws(raw, idx)
				event.payload_start = idx^
				if !batch_skip_value(raw, idx) do return false
				event.payload_end = idx^
			case:
				if !batch_skip_value(raw, idx) do return false
			}

			batch_skip_ws(raw, idx)
			if idx^ >= len(raw) do return false
			if raw[idx^] == ',' {
				idx^ += 1
				continue
			}
			if raw[idx^] == '}' {
				idx^ += 1
				break
			}
			return false
		}

		if total >= skip_events && stored < BATCH_EVENT_VIEW_CAP {
			out.events[stored] = event
			stored += 1
		}
		total += 1

		batch_skip_ws(raw, idx)
		if idx^ >= len(raw) do return false
		if raw[idx^] == ',' {
			idx^ += 1
			continue
		}
		if raw[idx^] == ']' {
			idx^ += 1
			break
		}
		return false
	}

	out.total_events = total
	out.event_count = stored
	out.has_more = skip_events + stored < total
	return true
}

// Parse a batched frame and expose event payload views without allocating.
// skip_events allows deterministic split processing when event count exceeds cap.
parse_batched_frame :: proc(raw: []u8, out: ^Parsed_Batched_Frame, skip_events: int = 0) -> bool {
	if out == nil do return false
	out^ = {}
	skip := skip_events
	if skip < 0 do skip = 0

	idx := 0
	batch_skip_ws(raw, &idx)
	if idx >= len(raw) || raw[idx] != '{' do return false
	idx += 1

	is_batch := false
	for {
		ks, ke := 0, 0
		if !batch_parse_string_span(raw, &idx, &ks, &ke) do return false
		batch_skip_ws(raw, &idx)
		if idx >= len(raw) || raw[idx] != ':' do return false
		idx += 1

		switch {
		case batch_key_equals(raw, ks, ke, "type"):
			vs, ve := 0, 0
			if !batch_parse_string_span(raw, &idx, &vs, &ve) do return false
			is_batch = batch_key_equals(raw, vs, ve, "batch")
		case batch_key_equals(raw, ks, ke, "stream_id"):
			vs, ve := 0, 0
			if !batch_parse_string_span(raw, &idx, &vs, &ve) do return false
			out.stream_id_len = batch_copy_string(out.stream_id_buf[:], raw, vs, ve)
		case batch_key_equals(raw, ks, ke, "venue"):
			vs, ve := 0, 0
			if !batch_parse_string_span(raw, &idx, &vs, &ve) do return false
			out.venue_len = batch_copy_string(out.venue_buf[:], raw, vs, ve)
		case batch_key_equals(raw, ks, ke, "symbol"):
			vs, ve := 0, 0
			if !batch_parse_string_span(raw, &idx, &vs, &ve) do return false
			out.symbol_len = batch_copy_string(out.symbol_buf[:], raw, vs, ve)
		case batch_key_equals(raw, ks, ke, "channel"):
			vs, ve := 0, 0
			if !batch_parse_string_span(raw, &idx, &vs, &ve) do return false
			out.channel_len = batch_copy_string(out.channel_buf[:], raw, vs, ve)
		case batch_key_equals(raw, ks, ke, "base_seq"):
			if !batch_parse_int(raw, &idx, &out.base_seq) do return false
		case batch_key_equals(raw, ks, ke, "count"):
			tmp := i64(0)
			if !batch_parse_int(raw, &idx, &tmp) do return false
			out.count = int(tmp)
		case batch_key_equals(raw, ks, ke, "ts_server_base"):
			if !batch_parse_int(raw, &idx, &out.ts_server_base) do return false
		case batch_key_equals(raw, ks, ke, "ts_ingest_base"):
			if !batch_parse_int(raw, &idx, &out.ts_ingest_base) do return false
		case batch_key_equals(raw, ks, ke, "events"):
			if !batch_parse_events_segment(raw, &idx, skip, out) do return false
		case:
			if !batch_skip_value(raw, &idx) do return false
		}

		batch_skip_ws(raw, &idx)
		if idx >= len(raw) do return false
		if raw[idx] == ',' {
			idx += 1
			continue
		}
		if raw[idx] == '}' {
			idx += 1
			break
		}
		return false
	}

	// Defensive default when "count" field is absent.
	if out.count <= 0 do out.count = out.total_events
	return is_batch
}

// Fast-path parser for batched event payload views.
// Supports channels commonly emitted in batched frames without rebuilding
// synthetic envelope JSON.
parse_batched_event_payload :: proc(
	channel: string,
	payload_raw: []u8,
	seq: i64,
	ts_server_ms: i64,
	ts_ingest_ms: i64,
	subject_id: u64,
	is_snapshot: bool = false,
) -> (Parse_Result, bool) {
	result: Parse_Result
	result.meta.seq = seq
	result.meta.subject_id = subject_id
	result.meta.has_ts_server = ts_server_ms > 0
	result.meta.server_ts_ms = ts_server_ms if ts_server_ms > 0 else ts_ingest_ms
	result.meta.is_snapshot = is_snapshot

	switch channel {
	case "marketdata.trade":
		if r, ok := parse_trade_payload(payload_raw, ts_ingest_ms, subject_id); ok {
			r.seq = seq
			result.kind = .Trade
			result.data.trade = r
			return result, true
		}
	case "aggregation.tape":
		if r, ok := parse_tape_payload(payload_raw, ts_ingest_ms, subject_id); ok {
			r.seq = seq
			result.kind = .Tape
			result.data.tape = r
			return result, true
		}
	case "marketdata.bookdelta":
		if r, ok := parse_book_delta_payload(payload_raw, ts_ingest_ms, subject_id); ok {
			if is_snapshot do r.is_snapshot = true
			r.seq = seq
			result.meta.is_snapshot = r.is_snapshot
			result.kind = .Orderbook
			result.data.ob = r
			return result, true
		}
	case:
	}
	return {}, false
}
