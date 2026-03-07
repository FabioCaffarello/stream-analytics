package md_common

// S51: Replay Scrubber — ring buffer tracking recent sequence events with
// integrity flags for gap/reorder/duplicate detection. Pure structs + functions.
//
// The scrubber records the last SCRUBBER_RING_CAP events for inspection.
// No allocations, no mutable global state.

SCRUBBER_RING_CAP :: 256

Stream_Integrity_Flag :: enum u8 {
	Ok,
	Gap,         // seq jump > 1
	Reorder,     // seq < prev seq for same slot
	Duplicate,   // seq == prev seq for same slot
}

Scrubber_Entry :: struct {
	seq:           u64,
	ts_server:     i64,
	ts_local:      i64,
	artifact_kind: Artifact_Kind,
	payload_bytes: u32,
	slot_idx:      int,
	integrity:     Stream_Integrity_Flag,
}

Replay_Scrubber :: struct {
	ring:       [SCRUBBER_RING_CAP]Scrubber_Entry,
	head:       int,     // next write position
	count:      int,     // entries in ring (max SCRUBBER_RING_CAP)
	cursor:     int,     // inspection cursor (-1 = live head)
	paused:     bool,    // freeze ring push when user is scrubbing
	// Per-slot last seq for integrity detection.
	last_seq:   [SNAPSHOT_MAX_SLOTS]u64,
	slot_seen:  [SNAPSHOT_MAX_SLOTS]bool,
}

Integrity_Summary :: struct {
	total:      int,
	ok:         int,
	gaps:       int,
	reorders:   int,
	duplicates: int,
}

// scrubber_push appends an entry to the ring buffer. If paused, the push is
// silently discarded. Integrity flag is computed from per-slot last seq.
scrubber_push :: proc(s: ^Replay_Scrubber, entry: Scrubber_Entry) {
	if s == nil || s.paused do return

	e := entry
	slot := entry.slot_idx
	if slot >= 0 && slot < SNAPSHOT_MAX_SLOTS {
		if s.slot_seen[slot] {
			prev := s.last_seq[slot]
			if entry.seq == prev {
				e.integrity = .Duplicate
			} else if entry.seq < prev {
				e.integrity = .Reorder
			} else if entry.seq > prev + 1 {
				e.integrity = .Gap
			} else {
				e.integrity = .Ok
			}
		} else {
			e.integrity = .Ok
			s.slot_seen[slot] = true
		}
		s.last_seq[slot] = entry.seq
	}

	s.ring[s.head] = e
	s.head = (s.head + 1) % SCRUBBER_RING_CAP
	if s.count < SCRUBBER_RING_CAP {
		s.count += 1
	}
}

// scrubber_get retrieves an entry by offset from newest (0 = most recent).
// Returns the entry and true on success, or a zero entry and false if out of range.
scrubber_get :: proc(s: ^Replay_Scrubber, offset: int) -> (Scrubber_Entry, bool) {
	if s == nil || offset < 0 || offset >= s.count do return {}, false
	actual := (s.head - 1 - offset + SCRUBBER_RING_CAP) % SCRUBBER_RING_CAP
	return s.ring[actual], true
}

// scrubber_integrity_summary scans the ring and returns integrity counts.
scrubber_integrity_summary :: proc(s: ^Replay_Scrubber) -> Integrity_Summary {
	if s == nil do return {}
	summary: Integrity_Summary
	summary.total = s.count
	for i in 0 ..< s.count {
		actual := (s.head - 1 - i + SCRUBBER_RING_CAP) % SCRUBBER_RING_CAP
		switch s.ring[actual].integrity {
		case .Ok:        summary.ok += 1
		case .Gap:       summary.gaps += 1
		case .Reorder:   summary.reorders += 1
		case .Duplicate: summary.duplicates += 1
		}
	}
	return summary
}

// scrubber_seek sets the inspection cursor. -1 means live (follow head).
scrubber_seek :: proc(s: ^Replay_Scrubber, cursor: int) {
	if s == nil do return
	if cursor < 0 {
		s.cursor = -1
	} else if cursor >= s.count {
		s.cursor = s.count - 1
	} else {
		s.cursor = cursor
	}
}

// scrubber_pause freezes the ring — new pushes are discarded while paused.
scrubber_pause :: proc(s: ^Replay_Scrubber, paused: bool) {
	if s == nil do return
	s.paused = paused
	if !paused {
		s.cursor = -1 // return to live on unpause
	}
}

// scrubber_reset clears all scrubber state.
scrubber_reset :: proc(s: ^Replay_Scrubber) {
	if s == nil do return
	s^ = {}
}
