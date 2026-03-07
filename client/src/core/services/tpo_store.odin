package services

// S49: TPO (Time-Price Opportunity) profile store.
// Fixed-capacity, zero allocation after init.

TPO_PERIOD_CAP :: 24
TPO_LEVEL_CAP  :: 200
TPO_LETTERS_PER_LEVEL :: 24

TPO_Period :: struct {
	letter:     u8,
	high_price: f64,
	low_price:  f64,
}

TPO_Level :: struct {
	price_low:  f64,
	price_high: f64,
	letters:    [TPO_LETTERS_PER_LEVEL]u8,
	count:      int,
}

TPO_Store :: struct {
	periods:       [TPO_PERIOD_CAP]TPO_Period,
	period_count:  int,
	levels:        [TPO_LEVEL_CAP]TPO_Level,
	level_count:   int,
	poc_price:     f64,
	poc_level_idx: int,
	vah_price:     f64,
	val_price:     f64,
	ib_high:       f64,
	ib_low:        f64,
	range_high:    f64,
	range_low:     f64,
	session_label: [32]u8,
	label_len:     u8,
	dirty:         bool,
}

update_tpo_periods :: proc(store: ^TPO_Store, letters: [^]u8, highs: [^]f64, lows: [^]f64, count: int) {
	n := min(count, TPO_PERIOD_CAP)
	store.period_count = n
	for i in 0 ..< n {
		store.periods[i] = TPO_Period{
			letter     = letters[i],
			high_price = highs[i],
			low_price  = lows[i],
		}
	}
}

update_tpo_levels :: proc(
	store: ^TPO_Store,
	price_lows: [^]f64,
	price_highs: [^]f64,
	level_letters: [^][TPO_LETTERS_PER_LEVEL]u8,
	level_counts: [^]int,
	count: int,
) {
	n := min(count, TPO_LEVEL_CAP)
	store.level_count = n
	for i in 0 ..< n {
		store.levels[i] = TPO_Level{
			price_low  = price_lows[i],
			price_high = price_highs[i],
			letters    = level_letters[i],
			count      = level_counts[i],
		}
	}
}

update_tpo_derived :: proc(
	store: ^TPO_Store,
	poc_price: f64,
	poc_idx: int,
	vah: f64,
	val: f64,
	ib_high: f64,
	ib_low: f64,
	range_high: f64,
	range_low: f64,
) {
	store.poc_price = poc_price
	store.poc_level_idx = clamp(poc_idx, 0, max(store.level_count - 1, 0))
	store.vah_price = vah
	store.val_price = val
	store.ib_high = ib_high
	store.ib_low = ib_low
	store.range_high = range_high
	store.range_low = range_low
	store.dirty = true
}

set_tpo_label :: proc(store: ^TPO_Store, label: string) {
	n := min(len(label), 32)
	for i in 0 ..< n {
		store.session_label[i] = label[i]
	}
	store.label_len = u8(n)
}

get_tpo_label :: proc(store: ^TPO_Store) -> string {
	return string(store.session_label[:store.label_len])
}

clear_tpo :: proc(store: ^TPO_Store) {
	store.period_count = 0
	store.level_count = 0
	store.poc_price = 0
	store.poc_level_idx = 0
	store.vah_price = 0
	store.val_price = 0
	store.ib_high = 0
	store.ib_low = 0
	store.range_high = 0
	store.range_low = 0
	store.dirty = false
}
