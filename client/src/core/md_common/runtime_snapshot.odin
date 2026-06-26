package md_common

// S46: Deterministic Runtime Snapshot — canonical state capture for incident
// reproduction and debugging. Pure structs + serialization, no app dependency.
//
// A Runtime_Snapshot captures the minimum canonical state needed to reproduce
// client behavior at a point in time. It includes per-stream apply states,
// stream identity, cell configuration, compare mode, and derived summaries.
//
// Non-deterministic state (frame counters, scroll positions, transport metrics,
// data store contents) is explicitly excluded.

RUNTIME_SNAPSHOT_VERSION :: 3 // S82: bumped for show_oi indicator flag (bit 10)

// Maximum slots and cells in a snapshot (matches runtime caps).
SNAPSHOT_MAX_SLOTS :: 32
SNAPSHOT_MAX_CELLS :: 12
SNAPSHOT_MAX_COMPARE_PANES :: 4

// Per-stream slot snapshot — canonical state + identity for one slot.
Snapshot_Slot :: struct {
	used:         bool,
	subject_id:   u64,
	venue:        [32]u8,
	venue_len:    u8,
	symbol:       [32]u8,
	symbol_len:   u8,
	channel:      u8,        // ports.MD_Channel ordinal
	timeframe_ms: i64,
	apply_state:  Stream_Apply_State,
}

// Per-cell snapshot — widget kind, binding, TF override, and display state.
Snapshot_Cell :: struct {
	widget_kind:     u8,        // Widget_Kind ordinal
	stream_idx:      int,       // binding slot index (-1 = follow active)
	has_binding:     bool,      // has explicit venue/symbol binding
	bind_venue:      [32]u8,
	bind_venue_len:  u8,
	bind_symbol:     [32]u8,
	bind_symbol_len: u8,
	tf_idx:          int,       // per-cell TF (-1 = global)
	// S80: Chart display + indicator flags for deterministic reproduction.
	chart_display:   int,       // packed chart display (same encoding as V6 layout)
	indicator_flags: int,       // packed indicator flags (8 bools → bitmask)
}

// Compare mode snapshot.
Snapshot_Compare :: struct {
	active:       bool,
	count:        int,
	widget_idx:   int,
	focused_pane: int,
	slots:        [SNAPSHOT_MAX_COMPARE_PANES]u64,
	tf_idx:       [SNAPSHOT_MAX_COMPARE_PANES]int,
	getranges:    [SNAPSHOT_MAX_COMPARE_PANES]Snapshot_Compare_Getrange,
}

Snapshot_Compare_Getrange :: struct {
	pending:    bool,
	seeded:     bool,
	oldest_ts:  i64,
	sent_frame: u64,
}

// Full runtime snapshot.
Runtime_Snapshot :: struct {
	// Version for forward compatibility.
	version:           int,

	// Capture timestamp (local ms).
	capture_ts_ms:     i64,

	// Active stream identity.
	active_subject_id: u64,
	active_tf_idx:     int,

	// S80: Active route for scene state reproduction.
	active_route:      u8,       // Route ordinal (0=Dashboard, 1=Markets, etc.)

	// Per-stream slot state.
	slot_count:        int,
	slots:             [SNAPSHOT_MAX_SLOTS]Snapshot_Slot,

	// Active stream canonical apply state (separate from slot for clarity).
	active_apply_state: Stream_Apply_State,

	// Cell configuration.
	cell_count:        int,
	cells:             [SNAPSHOT_MAX_CELLS]Snapshot_Cell,

	// Compare mode.
	compare:           Snapshot_Compare,

	// Recovery event log.
	recovery_log:      Recovery_Event_Log,

	// Derived summaries (included for convenience — can be recomputed).
	aggregate_health:  Aggregate_Health_Summary,
}

// --- Serialization: pipe-delimited ASCII (matches existing persistence style) ---

// SNAPSHOT_SERIALIZE_CAP is the maximum buffer size for a serialized snapshot.
// 32 slots × ~300 bytes + cells + compare + header ≈ 12KB.
SNAPSHOT_SERIALIZE_CAP :: 16384

// runtime_snapshot_serialize writes a snapshot to a fixed buffer.
// Returns the number of bytes written (0 on failure).
// Format: "SNAP1|capture_ts|active_sid|active_tf|...\n"
// Pure function — no allocations.
runtime_snapshot_serialize :: proc(snap: ^Runtime_Snapshot, buf: []u8) -> int {
	if snap == nil || len(buf) < 64 do return 0
	n := 0

	// Helper: append string.
	append_str :: proc(buf: []u8, n: ^int, s: string) {
		for c in s {
			if n^ >= len(buf) do return
			buf[n^] = u8(c)
			n^ += 1
		}
	}
	// Helper: append integer.
	append_int :: proc(buf: []u8, n: ^int, val: i64) {
		ibuf: [24]u8
		neg := val < 0
		v := val
		if neg do v = -v
		pos := len(ibuf) - 1
		if v == 0 {
			ibuf[pos] = '0'
			pos -= 1
		} else {
			for v > 0 {
				ibuf[pos] = u8(v % 10) + '0'
				v /= 10
				pos -= 1
			}
		}
		if neg {
			ibuf[pos] = '-'
			pos -= 1
		}
		start := pos + 1
		for i in start ..< len(ibuf) {
			if n^ >= len(buf) do return
			buf[n^] = ibuf[i]
			n^ += 1
		}
	}
	// Helper: append u64.
	append_u64 :: proc(buf: []u8, n: ^int, val: u64) {
		ibuf: [24]u8
		v := val
		pos := len(ibuf) - 1
		if v == 0 {
			ibuf[pos] = '0'
			pos -= 1
		} else {
			for v > 0 {
				ibuf[pos] = u8(v % 10) + '0'
				v /= 10
				pos -= 1
			}
		}
		start := pos + 1
		for i in start ..< len(ibuf) {
			if n^ >= len(buf) do return
			buf[n^] = ibuf[i]
			n^ += 1
		}
	}
	// Helper: pipe separator.
	pipe :: proc(buf: []u8, n: ^int) {
		if n^ < len(buf) { buf[n^] = '|'; n^ += 1 }
	}
	// Helper: newline.
	nl :: proc(buf: []u8, n: ^int) {
		if n^ < len(buf) { buf[n^] = '\n'; n^ += 1 }
	}

	// Header line: SNAP<version>|capture_ts|active_sid|active_tf|slot_count|cell_count
	append_str(buf, &n, "SNAP")
	append_int(buf, &n, i64(snap.version))
	pipe(buf, &n)
	append_int(buf, &n, snap.capture_ts_ms)
	pipe(buf, &n)
	append_u64(buf, &n, snap.active_subject_id)
	pipe(buf, &n)
	append_int(buf, &n, i64(snap.active_tf_idx))
	pipe(buf, &n)
	append_int(buf, &n, i64(snap.slot_count))
	pipe(buf, &n)
	append_int(buf, &n, i64(snap.cell_count))
	pipe(buf, &n)
	// S80: Active route.
	append_int(buf, &n, i64(snap.active_route))
	nl(buf, &n)

	// Active apply state line.
	append_str(buf, &n, "AS|")
	serialize_apply_state(buf, &n, &snap.active_apply_state)
	nl(buf, &n)

	// Per-slot lines: SL|idx|used|sid|venue|symbol|ch|tf_ms|<apply_state>
	for si in 0 ..< snap.slot_count {
		slot := &snap.slots[si]
		if !slot.used do continue
		append_str(buf, &n, "SL|")
		append_int(buf, &n, i64(si))
		pipe(buf, &n)
		append_u64(buf, &n, slot.subject_id)
		pipe(buf, &n)
		vl := int(slot.venue_len)
		if vl > len(slot.venue) do vl = len(slot.venue)
		append_str(buf, &n, string(slot.venue[:vl]))
		pipe(buf, &n)
		sl := int(slot.symbol_len)
		if sl > len(slot.symbol) do sl = len(slot.symbol)
		append_str(buf, &n, string(slot.symbol[:sl]))
		pipe(buf, &n)
		append_int(buf, &n, i64(slot.channel))
		pipe(buf, &n)
		append_int(buf, &n, slot.timeframe_ms)
		pipe(buf, &n)
		serialize_apply_state(buf, &n, &slot.apply_state)
		nl(buf, &n)
	}

	// Per-cell lines: CL|idx|widget|stream_idx|has_bind|venue|symbol|tf_idx|chart_display|indicator_flags
	for ci in 0 ..< snap.cell_count {
		cell := &snap.cells[ci]
		append_str(buf, &n, "CL|")
		append_int(buf, &n, i64(ci))
		pipe(buf, &n)
		append_int(buf, &n, i64(cell.widget_kind))
		pipe(buf, &n)
		append_int(buf, &n, i64(cell.stream_idx))
		pipe(buf, &n)
		append_int(buf, &n, cell.has_binding ? 1 : 0)
		pipe(buf, &n)
		cvl := int(cell.bind_venue_len)
		if cvl > len(cell.bind_venue) do cvl = len(cell.bind_venue)
		append_str(buf, &n, string(cell.bind_venue[:cvl]))
		pipe(buf, &n)
		csl := int(cell.bind_symbol_len)
		if csl > len(cell.bind_symbol) do csl = len(cell.bind_symbol)
		append_str(buf, &n, string(cell.bind_symbol[:csl]))
		pipe(buf, &n)
		append_int(buf, &n, i64(cell.tf_idx))
		// S80: Chart display + indicator flags.
		pipe(buf, &n)
		append_int(buf, &n, i64(cell.chart_display))
		pipe(buf, &n)
		append_int(buf, &n, i64(cell.indicator_flags))
		nl(buf, &n)
	}

	// Compare line: CM|active|count|widget_idx|focused|s0|s1|s2|s3|tf0|tf1|tf2|tf3
	cmp := &snap.compare
	append_str(buf, &n, "CM|")
	append_int(buf, &n, cmp.active ? 1 : 0)
	pipe(buf, &n)
	append_int(buf, &n, i64(cmp.count))
	pipe(buf, &n)
	append_int(buf, &n, i64(cmp.widget_idx))
	pipe(buf, &n)
	append_int(buf, &n, i64(cmp.focused_pane))
	for pi in 0 ..< SNAPSHOT_MAX_COMPARE_PANES {
		pipe(buf, &n)
		append_u64(buf, &n, cmp.slots[pi])
	}
	for pi in 0 ..< SNAPSHOT_MAX_COMPARE_PANES {
		pipe(buf, &n)
		append_int(buf, &n, i64(cmp.tf_idx[pi]))
	}
	for pi in 0 ..< SNAPSHOT_MAX_COMPARE_PANES {
		gr := &cmp.getranges[pi]
		pipe(buf, &n)
		append_int(buf, &n, gr.pending ? 1 : 0)
		pipe(buf, &n)
		append_int(buf, &n, gr.seeded ? 1 : 0)
		pipe(buf, &n)
		append_int(buf, &n, gr.oldest_ts)
		pipe(buf, &n)
		append_u64(buf, &n, gr.sent_frame)
	}
	nl(buf, &n)

	// Recovery log: RL|count|head then RE|kind|ts|att|slot per event
	append_str(buf, &n, "RL|")
	append_int(buf, &n, i64(snap.recovery_log.count))
	pipe(buf, &n)
	append_int(buf, &n, i64(snap.recovery_log.head))
	nl(buf, &n)
	rev_count := min(snap.recovery_log.count, RECOVERY_EVENT_LOG_CAP)
	for ri in 0 ..< rev_count {
		rev, rok := recovery_event_log_get(&snap.recovery_log, ri)
		if !rok do break
		append_str(buf, &n, "RE|")
		append_int(buf, &n, i64(rev.kind))
		pipe(buf, &n)
		append_int(buf, &n, rev.timestamp)
		pipe(buf, &n)
		append_int(buf, &n, i64(rev.attempts))
		pipe(buf, &n)
		append_int(buf, &n, i64(rev.slot_id))
		nl(buf, &n)
	}

	// Aggregate health summary: AH|health|slots|composed|live|pending|empty|recovering|exhausted|stale|aging|events
	agg := &snap.aggregate_health
	append_str(buf, &n, "AH|")
	append_int(buf, &n, i64(agg.health_level))
	pipe(buf, &n)
	append_int(buf, &n, i64(agg.slot_count))
	pipe(buf, &n)
	append_int(buf, &n, i64(agg.slots_composed))
	pipe(buf, &n)
	append_int(buf, &n, i64(agg.slots_live_only))
	pipe(buf, &n)
	append_int(buf, &n, i64(agg.slots_pending))
	pipe(buf, &n)
	append_int(buf, &n, i64(agg.slots_empty))
	pipe(buf, &n)
	append_int(buf, &n, i64(agg.slots_recovering))
	pipe(buf, &n)
	append_int(buf, &n, i64(agg.slots_exhausted))
	pipe(buf, &n)
	append_int(buf, &n, i64(agg.total_stale))
	pipe(buf, &n)
	append_int(buf, &n, i64(agg.total_aging))
	pipe(buf, &n)
	append_u64(buf, &n, agg.total_event_count)
	nl(buf, &n)

	return n
}

// serialize_apply_state writes Stream_Apply_State fields as pipe-delimited values.
// Format: snapshot_seen_mask|has_live_mask|synthetic_mask|recv_ms...|evt_counts...|gr_fields|recovery
@(private = "file")
serialize_apply_state :: proc(buf: []u8, n: ^int, s: ^Stream_Apply_State) {
	append_int :: proc(buf: []u8, n: ^int, val: i64) {
		ibuf: [24]u8
		neg := val < 0
		v := val
		if neg do v = -v
		pos := len(ibuf) - 1
		if v == 0 {
			ibuf[pos] = '0'
			pos -= 1
		} else {
			for v > 0 {
				ibuf[pos] = u8(v % 10) + '0'
				v /= 10
				pos -= 1
			}
		}
		if neg {
			ibuf[pos] = '-'
			pos -= 1
		}
		start := pos + 1
		for i in start ..< len(ibuf) {
			if n^ >= len(buf) do return
			buf[n^] = ibuf[i]
			n^ += 1
		}
	}
	append_u64 :: proc(buf: []u8, n: ^int, val: u64) {
		ibuf: [24]u8
		v := val
		pos := len(ibuf) - 1
		if v == 0 {
			ibuf[pos] = '0'
			pos -= 1
		} else {
			for v > 0 {
				ibuf[pos] = u8(v % 10) + '0'
				v /= 10
				pos -= 1
			}
		}
		start := pos + 1
		for i in start ..< len(ibuf) {
			if n^ >= len(buf) do return
			buf[n^] = ibuf[i]
			n^ += 1
		}
	}
	pipe :: proc(buf: []u8, n: ^int) {
		if n^ < len(buf) { buf[n^] = '|'; n^ += 1 }
	}

	// Pack boolean arrays as bitmask.
	snap_mask: u16
	live_mask: u16
	synth_mask: u16
	for kind in Artifact_Kind {
		bit := u16(1) << u16(kind)
		if s.snapshot_seen[kind] do snap_mask |= bit
		if s.has_live[kind] do live_mask |= bit
		if s.using_synthetic[kind] do synth_mask |= bit
	}
	append_int(buf, n, i64(snap_mask))
	pipe(buf, n)
	append_int(buf, n, i64(live_mask))
	pipe(buf, n)
	append_int(buf, n, i64(synth_mask))

	// Per-artifact last_recv_ms
	for kind in Artifact_Kind {
		pipe(buf, n)
		append_int(buf, n, s.last_recv_ms[kind])
	}
	// Per-artifact event count
	for kind in Artifact_Kind {
		pipe(buf, n)
		append_u64(buf, n, s.artifact_event_count[kind])
	}

	// GetRange fields
	pipe(buf, n)
	append_int(buf, n, s.getrange_seeded ? 1 : 0)
	pipe(buf, n)
	append_int(buf, n, s.getrange_pending ? 1 : 0)
	pipe(buf, n)
	append_int(buf, n, s.getrange_oldest_ts)
	pipe(buf, n)
	append_u64(buf, n, s.getrange_sent_frame)
	pipe(buf, n)
	append_u64(buf, n, s.range_candle_subject_id)
	pipe(buf, n)
	append_u64(buf, n, s.getrange_request_id)

	// Recovery
	pipe(buf, n)
	append_int(buf, n, s.recovery_last_ms)
	pipe(buf, n)
	append_int(buf, n, i64(s.recovery_attempts))

	// Total event count + heatmap dedup
	pipe(buf, n)
	append_u64(buf, n, s.event_count)
	pipe(buf, n)
	append_int(buf, n, s.synth_heatmap_last_window)
}

// runtime_snapshot_apply_states_equal compares two apply states for deterministic equality.
// Pure function — useful for testing snapshot capture consistency.
runtime_snapshot_apply_states_equal :: proc(a, b: Stream_Apply_State) -> bool {
	for kind in Artifact_Kind {
		if a.snapshot_seen[kind] != b.snapshot_seen[kind] do return false
		if a.has_live[kind] != b.has_live[kind] do return false
		if a.using_synthetic[kind] != b.using_synthetic[kind] do return false
		if a.last_recv_ms[kind] != b.last_recv_ms[kind] do return false
		if a.artifact_event_count[kind] != b.artifact_event_count[kind] do return false
	}
	if a.getrange_seeded != b.getrange_seeded do return false
	if a.getrange_pending != b.getrange_pending do return false
	if a.getrange_oldest_ts != b.getrange_oldest_ts do return false
	if a.getrange_sent_frame != b.getrange_sent_frame do return false
	if a.range_candle_subject_id != b.range_candle_subject_id do return false
	if a.getrange_request_id != b.getrange_request_id do return false
	if a.recovery_last_ms != b.recovery_last_ms do return false
	if a.recovery_attempts != b.recovery_attempts do return false
	if a.event_count != b.event_count do return false
	if a.synth_heatmap_last_window != b.synth_heatmap_last_window do return false
	return true
}
