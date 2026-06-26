package services

// S60: Market Explorer service — view model for venue-grouped market discovery.
// Provides structured data for the Market Explorer 2.0 page:
//   - Venue grouping with instrument counts
//   - Market type filtering (All / Spot / Perp)
//   - Text search across venue+symbol
//   - Session dashboard summary for global health header

EXPLORER_VENUE_CAP :: EXCHANGE_CAP  // max 16 venues
EXPLORER_ROW_CAP   :: MARKET_CAP   // max 64 instruments

Explorer_Market_Type_Filter :: enum u8 {
	All,
	Spot,
	Perp,
}

// Per-venue summary — assembled from Markets_Store in one pass.
Explorer_Venue_Summary :: struct {
	venue:        string,
	total:        int,    // total instruments in this venue
	spot_count:   int,
	perp_count:   int,
	active_count: int,    // currently subscribed streams
	first_idx:    int,    // first market entry index in Markets_Store
}

// Resolved explorer view — assembled once per frame.
Explorer_View :: struct {
	venues:       [EXPLORER_VENUE_CAP]Explorer_Venue_Summary,
	venue_count:  int,
	total_rows:   int,
	active_total: int,
	spot_total:   int,
	perp_total:   int,
}

// Resolve venue summaries from the markets store.
// active_check_proc is called with market_entry_idx to determine if subscribed.
explorer_resolve_venues :: proc(
	store: ^Markets_Store,
	is_subscribed: proc(idx: int) -> bool,
	out: ^Explorer_View,
) {
	if store == nil || out == nil do return
	out^ = {}

	for mi in 0 ..< store.count {
		entry := store.entries[mi]
		// Find or create venue summary.
		vi := -1
		for v in 0 ..< out.venue_count {
			if out.venues[v].venue == entry.venue {
				vi = v
				break
			}
		}
		if vi < 0 {
			if out.venue_count >= EXPLORER_VENUE_CAP do continue
			vi = out.venue_count
			out.venues[vi] = Explorer_Venue_Summary{
				venue     = entry.venue,
				first_idx = mi,
			}
			out.venue_count += 1
		}
		vs := &out.venues[vi]
		vs.total += 1
		out.total_rows += 1

		is_spot := entry.market_type == "SPOT"
		if is_spot {
			vs.spot_count += 1
			out.spot_total += 1
		} else {
			vs.perp_count += 1
			out.perp_total += 1
		}

		if is_subscribed != nil && is_subscribed(mi) {
			vs.active_count += 1
			out.active_total += 1
		}
	}
}

// Check if a market entry matches the current filters.
explorer_entry_matches :: proc(
	entry: Market_Entry,
	type_filter: Explorer_Market_Type_Filter,
	search: string,
) -> bool {
	// Market type filter.
	if type_filter == .Spot && entry.market_type != "SPOT" do return false
	if type_filter == .Perp && entry.market_type == "SPOT" do return false

	// Search filter — match against venue or ticker (case-insensitive substring).
	if len(search) > 0 {
		if !contains_ci(entry.venue, search) && !contains_ci(entry.ticker, search) {
			return false
		}
	}
	return true
}

// Case-insensitive substring match (ASCII only, no allocation).
contains_ci :: proc(haystack, needle: string) -> bool {
	if len(needle) == 0 do return true
	if len(needle) > len(haystack) do return false
	for i in 0 ..= len(haystack) - len(needle) {
		match := true
		for j in 0 ..< len(needle) {
			a := haystack[i + j]
			b := needle[j]
			if a >= 'A' && a <= 'Z' do a += 32
			if b >= 'A' && b <= 'Z' do b += 32
			if a != b {
				match = false
				break
			}
		}
		if match do return true
	}
	return false
}
