package services

// Parse arena ownership for message_parser hot path.
//
// NOTE:
// - Parsing uses context.temp_allocator internally (json.unmarshal + transient strings).
// - Callers MUST reset this arena at the end of each processed message.
// - Parsed values copied into fixed staging structs remain valid after reset.

Parse_Arena :: struct {
	msg_count:      u64,
	bytes_total:    u64,
	message_resets: u64,
}

parse_mr_message_with_arena :: proc(arena: ^Parse_Arena, raw: []u8, telemetry: ^Parse_Telemetry) -> Parse_Result {
	if arena != nil {
		arena.msg_count += 1
		if len(raw) > 0 do arena.bytes_total += u64(len(raw))
	}
	return parse_mr_message(raw, telemetry)
}

parse_arena_reset_message :: proc(arena: ^Parse_Arena) {
	free_all(context.temp_allocator)
	if arena != nil {
		arena.message_resets += 1
	}
}
