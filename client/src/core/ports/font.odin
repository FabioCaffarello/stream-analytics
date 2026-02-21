package ports

// Font port — platform-injected font management.
// Core widgets use Font_Id tokens; platform maps to concrete fonts.
//
// Native: backed by ImGui font atlas (TTF loading).
// Web:    stub (CSS fonts, no loading needed).

import "mr:ui"

Font_Port :: struct {
	// Load a font from TTF data. Returns false if loading fails.
	load:      proc(id: ui.Font_Id, ttf_data: []u8, size_px: f32) -> bool,
	// Push font for ImGui/platform text rendering.
	push_font: proc(id: ui.Font_Id),
	// Pop font after rendering.
	pop_font:  proc(),
	// Get DPI scale factor (1.0 = standard, 2.0 = Retina).
	dpi_scale: proc() -> f32,
}
