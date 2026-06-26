package services

// Deterministic bounded signal store.
// - Hard cap of SIGNAL_KIND_CAP kinds.
// - Ring of SIGNAL_PER_KIND_CAP entries per kind.

SIGNAL_KIND_CAP     :: 8
SIGNAL_PER_KIND_CAP :: 50

Signal_Entry :: struct {
	kind:            [24]u8,
	kind_len:        u8,
	severity:        [12]u8,
	severity_len:    u8,
	confidence:      f64,
	reason:          [96]u8,
	reason_len:      u8,
	regime:          [24]u8,
	regime_len:      u8,
	regime_strength: f64,
	unix:            i64,
	subject_id:      u64,
	seq:             i64,
}

Signal_Kind_Ring :: struct {
	key:      [24]u8,
	key_len:  u8,
	entries:  [SIGNAL_PER_KIND_CAP]Signal_Entry,
	head:     int,
	count:    int,
	last_unix: i64,
	last_seq:  i64,
}

Signal_Store :: struct {
	kinds:              [SIGNAL_KIND_CAP]Signal_Kind_Ring,
	kind_count:         int,
	overwritten_total:  u64,
	evicted_kind_total: u64,
}

@(private = "file")
kind_equals :: proc(a: [24]u8, a_len: u8, b: [24]u8, b_len: u8) -> bool {
	if a_len != b_len do return false
	n := int(a_len)
	for i in 0 ..< n {
		if a[i] != b[i] do return false
	}
	return true
}

@(private = "file")
kind_compare :: proc(a: [24]u8, a_len: u8, b: [24]u8, b_len: u8) -> int {
	an := int(a_len)
	bn := int(b_len)
	n := min(an, bn)
	for i in 0 ..< n {
		if a[i] < b[i] do return -1
		if a[i] > b[i] do return 1
	}
	if an < bn do return -1
	if an > bn do return 1
	return 0
}

@(private = "file")
signal_is_newer :: proc(a, b: Signal_Entry) -> bool {
	if a.unix != b.unix do return a.unix > b.unix
	if a.seq != b.seq do return a.seq > b.seq
	return kind_compare(a.kind, a.kind_len, b.kind, b.kind_len) > 0
}

@(private = "file")
signal_kind_slot :: proc(store: ^Signal_Store, entry: Signal_Entry) -> ^Signal_Kind_Ring {
	if store == nil do return nil
	if entry.kind_len == 0 do return nil

	for i in 0 ..< store.kind_count {
		slot := &store.kinds[i]
		if kind_equals(slot.key, slot.key_len, entry.kind, entry.kind_len) {
			return slot
		}
	}

	if store.kind_count < SIGNAL_KIND_CAP {
		slot := &store.kinds[store.kind_count]
		slot^ = {}
		slot.key = entry.kind
		slot.key_len = entry.kind_len
		store.kind_count += 1
		return slot
	}

	// Evict the oldest kind deterministically.
	evict := 0
	for i in 1 ..< SIGNAL_KIND_CAP {
		a := store.kinds[i]
		b := store.kinds[evict]
		if a.last_unix < b.last_unix {
			evict = i
			continue
		}
		if a.last_unix > b.last_unix do continue
		if a.last_seq < b.last_seq {
			evict = i
			continue
		}
		if a.last_seq > b.last_seq do continue
		if kind_compare(a.key, a.key_len, b.key, b.key_len) < 0 {
			evict = i
		}
	}
	slot := &store.kinds[evict]
	slot^ = {}
	slot.key = entry.kind
	slot.key_len = entry.kind_len
	store.evicted_kind_total += 1
	return slot
}

signal_store_push :: proc(store: ^Signal_Store, entry: Signal_Entry) {
	slot := signal_kind_slot(store, entry)
	if slot == nil do return
	if slot.count >= SIGNAL_PER_KIND_CAP {
		store.overwritten_total += 1
	}
	slot.entries[slot.head] = entry
	slot.head = (slot.head + 1) % SIGNAL_PER_KIND_CAP
	if slot.count < SIGNAL_PER_KIND_CAP do slot.count += 1
	slot.last_unix = entry.unix
	slot.last_seq = entry.seq
}

signal_store_recent_for_subject :: proc(store: ^Signal_Store, subject_id: u64, out: []Signal_Entry) -> int {
	if store == nil || len(out) == 0 || subject_id == 0 do return 0
	count := 0
	for ki in 0 ..< store.kind_count {
		slot := &store.kinds[ki]
		for si in 0 ..< slot.count {
			raw_idx := (slot.head - 1 - si + SIGNAL_PER_KIND_CAP) % SIGNAL_PER_KIND_CAP
			entry := slot.entries[raw_idx]
			if entry.subject_id != subject_id do continue

			insert_at := count
			if insert_at > len(out) do insert_at = len(out)
			for insert_at > 0 && signal_is_newer(entry, out[insert_at - 1]) {
				if insert_at < len(out) {
					out[insert_at] = out[insert_at - 1]
				}
				insert_at -= 1
			}
			if insert_at < len(out) {
				out[insert_at] = entry
				if count < len(out) do count += 1
			}
		}
	}
	if count > len(out) do count = len(out)
	return count
}

signal_store_latest_for_subject :: proc(store: ^Signal_Store, subject_id: u64) -> (Signal_Entry, bool) {
	one: [1]Signal_Entry
	n := signal_store_recent_for_subject(store, subject_id, one[:])
	if n <= 0 do return {}, false
	return one[0], true
}

signal_entry_kind_string :: proc(entry: ^Signal_Entry) -> string {
	if entry == nil || entry.kind_len == 0 do return ""
	n := min(int(entry.kind_len), len(entry.kind))
	return string(entry.kind[:n])
}

signal_entry_severity_string :: proc(entry: ^Signal_Entry) -> string {
	if entry == nil || entry.severity_len == 0 do return ""
	n := min(int(entry.severity_len), len(entry.severity))
	return string(entry.severity[:n])
}

signal_entry_reason_string :: proc(entry: ^Signal_Entry) -> string {
	if entry == nil || entry.reason_len == 0 do return ""
	n := min(int(entry.reason_len), len(entry.reason))
	return string(entry.reason[:n])
}
