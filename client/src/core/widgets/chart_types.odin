package widgets

import "mr:ui"

// Minimal type definitions referenced by app ECS components.
// These types are stored in View_Component and Telemetry_State but
// are not consumed by the active layers-based rendering pipeline.

Chart_Type :: enum u8 {
	Candlesticks,
	Line,
	Heiken_Ashi,
	Footprint,
	Footprint_Delta,
}

Crosshair_State :: struct {
	active:              bool,
	mouse_pos:           ui.Vec2,
	hovered_idx:         int,
	price_at_y:          f64,
	last_yaxis_click_ms: i64,
}

Indicator_Render_Probe :: struct {
	rsi_enabled:           bool,
	macd_enabled:          bool,
	funding_enabled:       bool,
	liq_enabled:           bool,
	trade_counter_enabled: bool,
	cvd_enabled:           bool,
	delta_vol_enabled:     bool,
	oi_enabled:            bool,   // S94
	rsi_rendered:          bool,
	macd_rendered:         bool,
	funding_rendered:      bool,
	liq_rendered:          bool,
	trade_counter_rendered: bool,
	cvd_rendered:          bool,
	delta_vol_rendered:    bool,
	oi_rendered:           bool,   // S94
}
