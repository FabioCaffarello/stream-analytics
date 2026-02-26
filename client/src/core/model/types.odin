package model

// Domain types used by the MR client UI layer.
// Legacy MarketMonkey types removed — only actively referenced types remain.

// Stats snapshot used by trade_counter widget and app build_ui.
Stat :: struct {
	unix:       i64,
	mark_price: f64,
	funding:    f64,
	tbuy:       f64,
	tsell:      f64,
}
