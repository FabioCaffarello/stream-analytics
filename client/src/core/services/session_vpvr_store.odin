package services

// S49: Session Volume Profile store.
// Fixed-capacity, zero allocation after init.
// Reuses VPVR_Bucket for bucket representation.

import "core:math"

SESSION_VPVR_BUCKET_CAP :: 200
SESSION_LABEL_CAP :: 32

Session_VPVR_Store :: struct {
	buckets:       [SESSION_VPVR_BUCKET_CAP]VPVR_Bucket,
	count:         int,
	poc_index:     int,
	vah_price:     f64,
	val_price:     f64,
	total_volume:  f64,
	buy_volume:    f64,
	sell_volume:   f64,
	max_volume:    f64,
	session_label: [SESSION_LABEL_CAP]u8,
	label_len:     u8,
	session_start: i64,
	session_end:   i64,
	dirty:         bool,
}

update_session_vpvr :: proc(
	store: ^Session_VPVR_Store,
	prices: [^]f64,
	buys: [^]f64,
	sells: [^]f64,
	count: int,
	poc_idx: int,
	vah: f64,
	val: f64,
	total_vol: f64,
	buy_vol: f64,
	sell_vol: f64,
	session_start: i64,
	session_end: i64,
) {
	n := min(count, SESSION_VPVR_BUCKET_CAP)
	store.count = n
	store.poc_index = clamp(poc_idx, 0, max(n - 1, 0))
	store.vah_price = vah
	store.val_price = val
	store.total_volume = total_vol
	store.buy_volume = buy_vol
	store.sell_volume = sell_vol
	store.session_start = session_start
	store.session_end = session_end
	store.max_volume = 0
	store.dirty = true

	for i in 0 ..< n {
		store.buckets[i] = VPVR_Bucket{
			price      = prices[i],
			buy_volume = buys[i],
			sell_volume = sells[i],
		}
		total := buys[i] + sells[i]
		store.max_volume = math.max(store.max_volume, total)
	}
}

set_session_vpvr_label :: proc(store: ^Session_VPVR_Store, label: string) {
	n := min(len(label), SESSION_LABEL_CAP)
	for i in 0 ..< n {
		store.session_label[i] = label[i]
	}
	store.label_len = u8(n)
}

get_session_vpvr_label :: proc(store: ^Session_VPVR_Store) -> string {
	return string(store.session_label[:store.label_len])
}

clear_session_vpvr :: proc(store: ^Session_VPVR_Store) {
	store.count = 0
	store.poc_index = 0
	store.vah_price = 0
	store.val_price = 0
	store.total_volume = 0
	store.buy_volume = 0
	store.sell_volume = 0
	store.max_volume = 0
	store.session_start = 0
	store.session_end = 0
	store.dirty = false
}
