package services

import "core:testing"

@(private = "file")
mk_signal :: proc(kind: string, unix: i64, seq: i64, subject_id: u64) -> Signal_Entry {
	e: Signal_Entry
	kn := min(len(kind), len(e.kind))
	for i in 0 ..< kn {
		e.kind[i] = kind[i]
	}
	e.kind_len = u8(kn)
	e.unix = unix
	e.seq = seq
	e.subject_id = subject_id
	e.confidence = 0.75
	return e
}

@(test)
test_signal_store_per_kind_ring_overwrites_oldest :: proc(t: ^testing.T) {
	store: Signal_Store
	subject := u64(0xAA)
	for i in 0 ..< SIGNAL_PER_KIND_CAP + 3 {
		e := mk_signal("trend_breakout", i64(1_700_000_000 + i), i64(i + 1), subject)
		signal_store_push(&store, e)
	}
	testing.expect_value(t, store.kind_count, 1)
	testing.expect_value(t, store.overwritten_total, u64(3))

	out: [SIGNAL_PER_KIND_CAP]Signal_Entry
	n := signal_store_recent_for_subject(&store, subject, out[:])
	testing.expect_value(t, n, SIGNAL_PER_KIND_CAP)
	// Newest first.
	testing.expect_value(t, out[0].seq, i64(SIGNAL_PER_KIND_CAP + 3))
	// Oldest retained after 3 overwrites.
	testing.expect_value(t, out[SIGNAL_PER_KIND_CAP - 1].seq, i64(4))
}

@(test)
test_signal_store_kind_cap_eviction_is_deterministic :: proc(t: ^testing.T) {
	store: Signal_Store
	subject := u64(0xBB)
	kinds := [SIGNAL_KIND_CAP]string{"ka", "kb", "kc", "kd", "ke", "kf", "kg", "kh"}
	for i in 0 ..< SIGNAL_KIND_CAP {
		e := mk_signal(kinds[i], i64(100 + i), i64(i + 1), subject)
		signal_store_push(&store, e)
	}
	testing.expect_value(t, store.kind_count, SIGNAL_KIND_CAP)

	// Push a new kind with newer timestamp; oldest kind should be evicted.
	signal_store_push(&store, mk_signal("kz", 999, 999, subject))
	testing.expect_value(t, store.kind_count, SIGNAL_KIND_CAP)
	testing.expect_value(t, store.evicted_kind_total, u64(1))

	// Verify ka (oldest by unix) was evicted: no recent item with seq=1.
	out: [SIGNAL_KIND_CAP * SIGNAL_PER_KIND_CAP]Signal_Entry
	n := signal_store_recent_for_subject(&store, subject, out[:])
	found_seq1 := false
	for i in 0 ..< n {
		if out[i].seq == 1 {
			found_seq1 = true
			break
		}
	}
	testing.expect_value(t, found_seq1, false)
}
