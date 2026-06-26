package services

import "core:testing"

// --- explorer_resolve_venues ---

@(test)
test_explorer_resolve_venues_empty :: proc(t: ^testing.T) {
	store: Markets_Store
	view: Explorer_View
	explorer_resolve_venues(&store, nil, &view)
	testing.expect_value(t, view.venue_count, 0)
	testing.expect_value(t, view.total_rows, 0)
}

@(test)
test_explorer_resolve_venues_groups_by_venue :: proc(t: ^testing.T) {
	store: Markets_Store
	store.entries[0] = {"binance-spot", "BTCUSDT", 0.01, "SPOT"}
	store.entries[1] = {"binance-spot", "ETHUSDT", 0.01, "SPOT"}
	store.entries[2] = {"bybit", "BTCUSDT", 0.01, "USD_M_FUTURES"}
	store.count = 3

	view: Explorer_View
	explorer_resolve_venues(&store, nil, &view)
	testing.expect_value(t, view.venue_count, 2)
	testing.expect_value(t, view.total_rows, 3)
	testing.expect_value(t, view.venues[0].venue, "binance-spot")
	testing.expect_value(t, view.venues[0].total, 2)
	testing.expect_value(t, view.venues[0].spot_count, 2)
	testing.expect_value(t, view.venues[1].venue, "bybit")
	testing.expect_value(t, view.venues[1].total, 1)
	testing.expect_value(t, view.venues[1].perp_count, 1)
}

@(test)
test_explorer_resolve_venues_counts_active :: proc(t: ^testing.T) {
	store: Markets_Store
	store.entries[0] = {"binance-spot", "BTCUSDT", 0.01, "SPOT"}
	store.entries[1] = {"binance-spot", "ETHUSDT", 0.01, "SPOT"}
	store.entries[2] = {"bybit", "BTCUSDT", 0.01, "USD_M_FUTURES"}
	store.count = 3

	is_sub :: proc(idx: int) -> bool { return idx == 0 || idx == 2 }

	view: Explorer_View
	explorer_resolve_venues(&store, is_sub, &view)
	testing.expect_value(t, view.active_total, 2)
	testing.expect_value(t, view.venues[0].active_count, 1)
	testing.expect_value(t, view.venues[1].active_count, 1)
}

@(test)
test_explorer_resolve_venues_type_totals :: proc(t: ^testing.T) {
	store: Markets_Store
	store.entries[0] = {"binance-spot", "BTCUSDT", 0.01, "SPOT"}
	store.entries[1] = {"binance-futures", "BTCUSDT", 0.01, "USD_M_FUTURES"}
	store.entries[2] = {"bybit", "ETHUSDT", 0.01, "USD_M_FUTURES"}
	store.count = 3

	view: Explorer_View
	explorer_resolve_venues(&store, nil, &view)
	testing.expect_value(t, view.spot_total, 1)
	testing.expect_value(t, view.perp_total, 2)
}

// --- explorer_entry_matches ---

@(test)
test_entry_matches_all_filter :: proc(t: ^testing.T) {
	entry := Market_Entry{"binance-spot", "BTCUSDT", 0.01, "SPOT"}
	testing.expect(t, explorer_entry_matches(entry, .All, ""))
}

@(test)
test_entry_matches_spot_filter :: proc(t: ^testing.T) {
	spot := Market_Entry{"binance-spot", "BTCUSDT", 0.01, "SPOT"}
	perp := Market_Entry{"bybit", "BTCUSDT", 0.01, "USD_M_FUTURES"}
	testing.expect(t, explorer_entry_matches(spot, .Spot, ""))
	testing.expect(t, !explorer_entry_matches(perp, .Spot, ""))
}

@(test)
test_entry_matches_perp_filter :: proc(t: ^testing.T) {
	spot := Market_Entry{"binance-spot", "BTCUSDT", 0.01, "SPOT"}
	perp := Market_Entry{"bybit", "BTCUSDT", 0.01, "USD_M_FUTURES"}
	testing.expect(t, !explorer_entry_matches(spot, .Perp, ""))
	testing.expect(t, explorer_entry_matches(perp, .Perp, ""))
}

@(test)
test_entry_matches_search_ticker :: proc(t: ^testing.T) {
	entry := Market_Entry{"binance-spot", "BTCUSDT", 0.01, "SPOT"}
	testing.expect(t, explorer_entry_matches(entry, .All, "btc"))
	testing.expect(t, explorer_entry_matches(entry, .All, "BTC"))
	testing.expect(t, !explorer_entry_matches(entry, .All, "SOL"))
}

@(test)
test_entry_matches_search_venue :: proc(t: ^testing.T) {
	entry := Market_Entry{"binance-spot", "BTCUSDT", 0.01, "SPOT"}
	testing.expect(t, explorer_entry_matches(entry, .All, "binance"))
	testing.expect(t, !explorer_entry_matches(entry, .All, "bybit"))
}

@(test)
test_entry_matches_combined_filter :: proc(t: ^testing.T) {
	entry := Market_Entry{"binance-spot", "BTCUSDT", 0.01, "SPOT"}
	// Type filter + search
	testing.expect(t, explorer_entry_matches(entry, .Spot, "btc"))
	testing.expect(t, !explorer_entry_matches(entry, .Perp, "btc"))
	testing.expect(t, !explorer_entry_matches(entry, .Spot, "sol"))
}

// --- contains_ci ---

@(test)
test_contains_ci_basic :: proc(t: ^testing.T) {
	testing.expect(t, contains_ci("BTCUSDT", "btc"))
	testing.expect(t, contains_ci("btcusdt", "BTC"))
	testing.expect(t, contains_ci("binance-spot", "BINANCE"))
	testing.expect(t, !contains_ci("btc", "btcusdt"))
	testing.expect(t, contains_ci("anything", ""))
}

@(test)
test_explorer_resolve_venues_nil_store :: proc(t: ^testing.T) {
	view: Explorer_View
	explorer_resolve_venues(nil, nil, &view)
	testing.expect_value(t, view.venue_count, 0)
}

@(test)
test_explorer_first_idx :: proc(t: ^testing.T) {
	store: Markets_Store
	store.entries[0] = {"binance-spot", "BTCUSDT", 0.01, "SPOT"}
	store.entries[1] = {"binance-spot", "ETHUSDT", 0.01, "SPOT"}
	store.entries[2] = {"bybit", "BTCUSDT", 0.01, "USD_M_FUTURES"}
	store.count = 3

	view: Explorer_View
	explorer_resolve_venues(&store, nil, &view)
	testing.expect_value(t, view.venues[0].first_idx, 0)
	testing.expect_value(t, view.venues[1].first_idx, 2)
}
