package md_common

// S51: Scene Snapshot — extends Runtime_Snapshot (S46) with data context for
// reproducible incident bundles. Includes data digests, scrubber tail, workspace
// fingerprint, and build info.
//
// Pure structs + serialization, no app dependency. Zero allocations.

SCENE_SNAPSHOT_VERSION :: 1
SCENE_SCRUBBER_TAIL_CAP :: 64

// Store_Digest captures the minimal data signature for one stream slot's stores.
Store_Digest :: struct {
	candle_count:     int,
	candle_newest_ts: i64,
	candle_oldest_ts: i64,
	analytics_count:  int,
	heatmap_seq_hi:   u64,
}

// Scene_Snapshot captures a full runtime scene for incident reproduction.
// Extends Runtime_Snapshot with data context.
Scene_Snapshot :: struct {
	// Embedded runtime state (S46).
	runtime:              Runtime_Snapshot,

	// Last N scrubber events (newest first) for event context.
	scrubber_tail:        [SCENE_SCRUBBER_TAIL_CAP]Scrubber_Entry,
	scrubber_tail_count:  int,

	// Per-slot data store digest.
	store_digests:        [SNAPSHOT_MAX_SLOTS]Store_Digest,

	// Workspace governance.
	workspace_fingerprint: u64,
	schema_version:       int,

	// Build identification.
	build_tag:            [32]u8,
	build_tag_len:        u8,

	// Scene version for forward compatibility.
	scene_version:        int,
}

// scene_snapshot_copy_scrubber_tail copies the last N scrubber events into
// the scene snapshot scrubber_tail array.
scene_snapshot_copy_scrubber_tail :: proc(snap: ^Scene_Snapshot, scrubber: ^Replay_Scrubber) {
	if snap == nil || scrubber == nil do return
	n := min(scrubber.count, SCENE_SCRUBBER_TAIL_CAP)
	snap.scrubber_tail_count = n
	for i in 0 ..< n {
		entry, ok := scrubber_get(scrubber, i)
		if !ok do break
		snap.scrubber_tail[i] = entry
	}
}

// scene_snapshot_set_build_tag sets the build identification tag (truncated to 32 bytes).
scene_snapshot_set_build_tag :: proc(snap: ^Scene_Snapshot, tag: string) {
	if snap == nil do return
	n := min(len(tag), len(snap.build_tag))
	for i in 0 ..< n {
		snap.build_tag[i] = tag[i]
	}
	snap.build_tag_len = u8(n)
}

// scene_snapshot_serialize writes a scene snapshot to a fixed buffer.
// Extends SNAP format with SC| prefix lines. Returns bytes written (0 on failure).
scene_snapshot_serialize :: proc(snap: ^Scene_Snapshot, buf: []u8) -> int {
	if snap == nil || len(buf) < 128 do return 0
	n := 0

	// First, serialize the embedded runtime snapshot.
	rn := runtime_snapshot_serialize(&snap.runtime, buf)
	if rn <= 0 do return 0
	n = rn

	// Helper procs (same pattern as runtime_snapshot_serialize).
	append_str :: proc(buf: []u8, n: ^int, s: string) {
		for c in s {
			if n^ >= len(buf) do return
			buf[n^] = u8(c)
			n^ += 1
		}
	}
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
	nl :: proc(buf: []u8, n: ^int) {
		if n^ < len(buf) { buf[n^] = '\n'; n^ += 1 }
	}

	// Scene header: SC|version|fingerprint|schema|build_tag|scrubber_count
	append_str(buf, &n, "SC|")
	append_int(buf, &n, i64(snap.scene_version))
	pipe(buf, &n)
	append_u64(buf, &n, snap.workspace_fingerprint)
	pipe(buf, &n)
	append_int(buf, &n, i64(snap.schema_version))
	pipe(buf, &n)
	bl := int(snap.build_tag_len)
	if bl > len(snap.build_tag) do bl = len(snap.build_tag)
	append_str(buf, &n, string(snap.build_tag[:bl]))
	pipe(buf, &n)
	append_int(buf, &n, i64(snap.scrubber_tail_count))
	nl(buf, &n)

	// Per-slot store digest: SD|idx|candle_count|newest|oldest|analytics|heatmap_seq
	for si in 0 ..< snap.runtime.slot_count {
		if !snap.runtime.slots[si].used do continue
		d := &snap.store_digests[si]
		append_str(buf, &n, "SD|")
		append_int(buf, &n, i64(si))
		pipe(buf, &n)
		append_int(buf, &n, i64(d.candle_count))
		pipe(buf, &n)
		append_int(buf, &n, d.candle_newest_ts)
		pipe(buf, &n)
		append_int(buf, &n, d.candle_oldest_ts)
		pipe(buf, &n)
		append_int(buf, &n, i64(d.analytics_count))
		pipe(buf, &n)
		append_u64(buf, &n, d.heatmap_seq_hi)
		nl(buf, &n)
	}

	// Scrubber tail: ST|idx|seq|ts_server|ts_local|kind|bytes|slot|integrity
	for i in 0 ..< snap.scrubber_tail_count {
		e := &snap.scrubber_tail[i]
		append_str(buf, &n, "ST|")
		append_int(buf, &n, i64(i))
		pipe(buf, &n)
		append_u64(buf, &n, e.seq)
		pipe(buf, &n)
		append_int(buf, &n, e.ts_server)
		pipe(buf, &n)
		append_int(buf, &n, e.ts_local)
		pipe(buf, &n)
		append_int(buf, &n, i64(e.artifact_kind))
		pipe(buf, &n)
		append_int(buf, &n, i64(e.payload_bytes))
		pipe(buf, &n)
		append_int(buf, &n, i64(e.slot_idx))
		pipe(buf, &n)
		append_int(buf, &n, i64(e.integrity))
		nl(buf, &n)
	}

	return n
}
