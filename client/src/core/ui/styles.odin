package ui

// Palette, typography tokens, and color utilities.
// Colors migrated from MarketMonkey settings; pure color procs from color.odin.

// --- Color palette (RGBA 0-1) ---

COL_SUCCESS         :: Color{0.0, 0.746, 0.179, 1.0}
COL_CROSS_HAIR      :: Color{0.7, 0.7, 0.7, 1.0}
COL_BLUE            :: Color{0.157, 0.384, 1.0, 1.0}
COL_RED             :: Color{0.965, 0.278, 0.365, 1.0}
COL_GREEN           :: Color{0.176, 0.741, 0.522, 1.0}
COL_PURPLE          :: Color{0.6, 0.0, 1.0, 1.0}
COL_BLACK           :: Color{0.047, 0.055, 0.067, 1.0}
COL_PRIMARY         :: Color{0.153, 0.153, 0.2, 1.0}
COL_PRIMARY_DIMMED  :: Color{0.09, 0.102, 0.157, 1.0}
COL_PANEL_BG        :: Color{0.09, 0.102, 0.125, 1.0}
COL_ORDERBOOK_RED   :: Color{0.965, 0.278, 0.365, 0.588}
COL_ORDERBOOK_GREEN :: Color{0.176, 0.741, 0.522, 0.588}
COL_YELLOW_ACCENT   :: Color{0.98, 1.0, 0.412, 1.0}
COL_WHITE           :: Color{1.0, 1.0, 1.0, 1.0}
COL_TRANSPARENT     :: Color{1.0, 1.0, 1.0, 0}

// --- Typography tokens ---

FONT_SIZE_BASE :: f32(16)
FONT_SIZE_LG   :: f32(20)
FONT_SIZE_XL   :: f32(24)

// --- Color utilities (pure, from MM color.odin) ---

with_alpha :: proc(col: Color, a: f32) -> Color {
	return {col.r, col.g, col.b, a}
}

adjust_brightness :: proc(col: Color, factor: f32) -> Color {
	return {
		clamp(col.r * factor, 0, 1),
		clamp(col.g * factor, 0, 1),
		clamp(col.b * factor, 0, 1),
		col.a,
	}
}
