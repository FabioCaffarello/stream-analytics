package ui

// Palette, typography tokens, color utilities, and shared formatting.
// Colors migrated from MarketMonkey settings; pure color procs from color.odin.

import "core:fmt"

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
COL_PANEL_BG        :: Color{0.07, 0.08, 0.105, 1.0}
COL_ORDERBOOK_RED   :: Color{0.965, 0.278, 0.365, 0.588}
COL_ORDERBOOK_GREEN :: Color{0.176, 0.741, 0.522, 0.588}
COL_YELLOW_ACCENT   :: Color{0.98, 1.0, 0.412, 1.0}
COL_WHITE           :: Color{1.0, 1.0, 1.0, 1.0}
COL_TRANSPARENT     :: Color{1.0, 1.0, 1.0, 0}

// --- Semantic surface tokens (cool-tone elevation) ---
COL_SURFACE_0       :: Color{0.035, 0.035, 0.045, 1.0}  // deepest black (app bg)
COL_SURFACE_1       :: Color{0.07, 0.08, 0.105, 1.0}    // panel backgrounds
COL_SURFACE_2       :: Color{0.11, 0.125, 0.165, 1.0}   // elevated panels / headers
COL_SURFACE_3       :: Color{0.17, 0.185, 0.245, 1.0}   // hover states
COL_TEXT_PRIMARY     :: Color{1.0, 1.0, 1.0, 0.88}
COL_TEXT_SECONDARY   :: Color{1.0, 1.0, 1.0, 0.55}
COL_TEXT_MUTED       :: Color{1.0, 1.0, 1.0, 0.35}
COL_DIVIDER          :: Color{1.0, 1.0, 1.0, 0.12}
COL_BORDER_SUBTLE    :: Color{1.0, 1.0, 1.0, 0.08}     // default panel border
COL_BORDER_STRONG    :: Color{1.0, 1.0, 1.0, 0.20}     // active / focused border

// --- Accent colors ---
COL_ACCENT_ORANGE    :: Color{1.0, 0.65, 0.0, 1.0}     // warnings
COL_ACCENT_CYAN      :: Color{0.0, 0.82, 0.95, 1.0}    // info / highlights
COL_WARNING          :: Color{1.0, 0.76, 0.03, 1.0}   // status bar warnings (amber)

// --- Typography tokens ---

FONT_SIZE_BASE :: f32(16)
FONT_SIZE_LG   :: f32(20)
FONT_SIZE_XL   :: f32(24)
FONT_SIZE_2XL  :: f32(28) // hero prices

// --- Spacing tokens ---

SPACING_XS :: f32(2)
SPACING_SM :: f32(4)
SPACING_MD :: f32(8)
SPACING_LG :: f32(12)
SPACING_XL :: f32(16)

// --- Color utilities (pure, from MM color.odin) ---

with_alpha :: proc(col: Color, a: f32) -> Color {
	return {col.r, col.g, col.b, a}
}

lerp_color :: proc(a, b: Color, t: f32) -> Color {
	tc := clamp(t, 0, 1)
	return {
		a.r + (b.r - a.r) * tc,
		a.g + (b.g - a.g) * tc,
		a.b + (b.b - a.b) * tc,
		a.a + (b.a - a.a) * tc,
	}
}

adjust_brightness :: proc(col: Color, factor: f32) -> Color {
	return {
		clamp(col.r * factor, 0, 1),
		clamp(col.g * factor, 0, 1),
		clamp(col.b * factor, 0, 1),
		col.a,
	}
}

// Viridis 5-stop colormap (canonical matplotlib).
// Piecewise linear interpolation between stops.
viridis_gradient :: proc(t: f32) -> Color {
	tc := clamp(t, 0, 1)

	// Viridis stops: dark navy → deep indigo → mid teal → lime green → bright yellow
	S0_R :: f32(0.267); S0_G :: f32(0.004); S0_B :: f32(0.329)
	S1_R :: f32(0.282); S1_G :: f32(0.140); S1_B :: f32(0.458)
	S2_R :: f32(0.127); S2_G :: f32(0.566); S2_B :: f32(0.551)
	S3_R :: f32(0.544); S3_G :: f32(0.773); S3_B :: f32(0.246)
	S4_R :: f32(0.993); S4_G :: f32(0.906); S4_B :: f32(0.144)

	if tc < 0.25 {
		f := tc * 4
		return {S0_R + (S1_R - S0_R) * f, S0_G + (S1_G - S0_G) * f, S0_B + (S1_B - S0_B) * f, 1.0}
	}
	if tc < 0.50 {
		f := (tc - 0.25) * 4
		return {S1_R + (S2_R - S1_R) * f, S1_G + (S2_G - S1_G) * f, S1_B + (S2_B - S1_B) * f, 1.0}
	}
	if tc < 0.75 {
		f := (tc - 0.50) * 4
		return {S2_R + (S3_R - S2_R) * f, S2_G + (S3_G - S2_G) * f, S2_B + (S3_B - S2_B) * f, 1.0}
	}
	f := (tc - 0.75) * 4
	return {S3_R + (S4_R - S3_R) * f, S3_G + (S4_G - S3_G) * f, S3_B + (S4_B - S3_B) * f, 1.0}
}

// --- Shared price formatting ---

// Auto-detect decimal places from price magnitude.
auto_price_decimals :: proc(price: f64) -> int {
	p := price < 0 ? -price : price
	if p >= 1000 do return 1
	if p >= 1   do return 2
	if p >= 0.01 do return 4
	return 6
}

// Format price with N decimals into a caller-owned buffer.
format_price :: proc(buf: []u8, price: f64, decimals: int) -> string {
	switch decimals {
	case 0:  return fmt.bprintf(buf, "%.0f", price)
	case 1:  return fmt.bprintf(buf, "%.1f", price)
	case 3:  return fmt.bprintf(buf, "%.3f", price)
	case 4:  return fmt.bprintf(buf, "%.4f", price)
	case 5:  return fmt.bprintf(buf, "%.5f", price)
	case 6:  return fmt.bprintf(buf, "%.6f", price)
	case:    return fmt.bprintf(buf, "%.2f", price)
	}
}
