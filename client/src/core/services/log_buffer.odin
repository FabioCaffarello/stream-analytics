package services

// Fixed-capacity ring buffer for client-side log entries.
// Zero allocation, stack-only. Used for telemetry HUD log viewer.

LOG_BUFFER_CAP     :: 256
LOG_ENTRY_TEXT_CAP  :: 128

Log_Level :: enum u8 { INFO, WARN, ERR }

Log_Entry :: struct {
	text:     [LOG_ENTRY_TEXT_CAP]u8,
	text_len: u8,
	level:    Log_Level,
	frame:    u64,
}

Log_Buffer :: struct {
	entries: [LOG_BUFFER_CAP]Log_Entry,
	head:    int,
	count:   int,
}

// Push a log entry into the ring buffer.
log_push :: proc(buf: ^Log_Buffer, level: Log_Level, msg: string, frame: u64) {
	if buf == nil do return
	entry := &buf.entries[buf.head]
	n := min(len(msg), int(LOG_ENTRY_TEXT_CAP))
	for i in 0 ..< n {
		entry.text[i] = msg[i]
	}
	entry.text_len = u8(n)
	entry.level = level
	entry.frame = frame
	buf.head = (buf.head + 1) % LOG_BUFFER_CAP
	if buf.count < LOG_BUFFER_CAP {
		buf.count += 1
	}
}

// Get a log entry by offset from newest (0 = newest).
log_get :: proc(buf: ^Log_Buffer, offset: int) -> (Log_Entry, bool) {
	if buf == nil || offset < 0 || offset >= buf.count do return {}, false
	idx := (buf.head - 1 - offset + LOG_BUFFER_CAP) % LOG_BUFFER_CAP
	return buf.entries[idx], true
}

log_count :: proc(buf: ^Log_Buffer) -> int {
	if buf == nil do return 0
	return buf.count
}
