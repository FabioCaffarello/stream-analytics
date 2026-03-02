package services

// Fixed-capacity heatmap store. Ring buffer of snapshots, zero allocation after init.
// Each snapshot holds up to HEATMAP_LEVEL_CAP price levels with sizes.

import "core:math"

HEATMAP_SNAP_CAP  :: 128
HEATMAP_LEVEL_CAP :: 64

Heatmap_Level :: struct {
	price: f64,
	size:  f64,
}

Heatmap_Snapshot :: struct {
	levels:      [HEATMAP_LEVEL_CAP]Heatmap_Level,
	level_count: int,
	unix:        i64,
	price_group: f64,
	min_price:   f64,
	max_price:   f64,
	max_size:    f64,
}

Heatmap_Store :: struct {
	snapshots:        [HEATMAP_SNAP_CAP]Heatmap_Snapshot,
	head:             int,
	count:            int,
	global_min_price: f64,
	global_max_price: f64,
	global_max_size:  f64,
}

// Push a new snapshot into the ring buffer.
push_heatmap_snapshot :: proc(store: ^Heatmap_Store, snap: Heatmap_Snapshot) {
	store.snapshots[store.head] = snap
	store.head = (store.head + 1) % HEATMAP_SNAP_CAP
	if store.count < HEATMAP_SNAP_CAP {
		store.count += 1
	}

	// Recompute global bounds from all current snapshots.
	// Prevents monotonic drift when old snapshots wrap out of the ring buffer.
	store.global_min_price = snap.min_price
	store.global_max_price = snap.max_price
	store.global_max_size  = snap.max_size
	for i in 0 ..< store.count {
		s := get_heatmap_snapshot(store, i)
		if s == nil do continue
		store.global_min_price = math.min(store.global_min_price, s.min_price)
		store.global_max_price = math.max(store.global_max_price, s.max_price)
		store.global_max_size  = math.max(store.global_max_size, s.max_size)
	}
}

// Get snapshot at logical index (0 = oldest visible).
get_heatmap_snapshot :: proc(store: ^Heatmap_Store, i: int) -> ^Heatmap_Snapshot {
	if i >= store.count do return nil
	oldest := (store.head - store.count + HEATMAP_SNAP_CAP) % HEATMAP_SNAP_CAP
	idx := (oldest + i) % HEATMAP_SNAP_CAP
	return &store.snapshots[idx]
}

// Fill with deterministic demo data.
fill_demo_heatmaps :: proc(store: ^Heatmap_Store) {
	base_price := 42000.0
	price_group := 50.0
	num_levels :: 32
	num_snaps :: 64

	for s in 0 ..< num_snaps {
		snap: Heatmap_Snapshot
		snap.unix = i64(1708000000 + s * 60)
		snap.price_group = price_group
		snap.level_count = num_levels
		snap.min_price = base_price
		snap.max_price = base_price + f64(num_levels - 1) * price_group
		snap.max_size = 0

		for l in 0 ..< num_levels {
			price := base_price + f64(l) * price_group
			// LCG-based pseudo-random size.
			seed := u32(s * 100 + l + 1) * 2654435761
			size := f64(seed % 1000) * 0.1

			snap.levels[l] = Heatmap_Level{price = price, size = size}
			snap.max_size = math.max(snap.max_size, size)
		}

		push_heatmap_snapshot(store, snap)
	}
}
