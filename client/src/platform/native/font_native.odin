package main

// Native font port — ImGui font atlas backed.
// Loads TTF fonts into ImGui atlas. Provides push/pop for font selection.
// DPI scale from GLFW/SDL2 content scale.

import "core:c"
import "vendor:glfw"
import imgui "deps:imgui"
import "mr:ports"
import "mr:ui"

// --- File-private state ---

@(private = "file")
g_font_table: [ui.Font_Id]^imgui.Font

@(private = "file")
g_font_loaded: [ui.Font_Id]bool

@(private = "file")
g_dpi_scale: f32 = 1.0

// --- Public API ---

make_font_port :: proc() -> ports.Font_Port {
	// Store default ImGui font as the Default slot.
	g_font_table[.Default] = imgui.GetFont()
	g_font_loaded[.Default] = true

	return ports.Font_Port{
		load      = font_load,
		push_font = font_push,
		pop_font  = font_pop,
		dpi_scale = font_get_dpi_scale,
	}
}

// Call after backend init and before first frame to set DPI scale.
// For GLFW: pass the window handle. For SDL2: call with nil and set scale directly.
set_dpi_scale :: proc(scale: f32) {
	g_dpi_scale = scale > 0 ? scale : 1.0
}

// Get the ImGui font for a given id. Returns default if not loaded.
get_font_for_id :: proc(id: ui.Font_Id) -> ^imgui.Font {
	if g_font_loaded[id] do return g_font_table[id]
	return g_font_table[.Default]
}

// --- Port implementation ---

@(private = "file")
font_load :: proc(id: ui.Font_Id, ttf_data: []u8, size_px: f32) -> bool {
	if len(ttf_data) == 0 do return false

	io := imgui.GetIO()
	if io == nil || io.fonts == nil do return false

	// ImGui takes ownership of font_data, so we must provide a copy it can free.
	// However, cimgui's AddFontFromMemoryTTF with font_cfg=nil copies internally.
	scaled_size := size_px * g_dpi_scale

	font := imgui.FontAtlas_AddFontFromMemoryTTF(
		io.fonts,
		raw_data(ttf_data),
		c.int(len(ttf_data)),
		scaled_size,
	)
	if font == nil do return false

	g_font_table[id] = font
	g_font_loaded[id] = true

	// Rebuild atlas so new font is available.
	imgui.FontAtlas_Build(io.fonts)

	return true
}

@(private = "file")
font_push :: proc(id: ui.Font_Id) {
	font := get_font_for_id(id)
	if font != nil {
		imgui.PushFont(font)
	}
}

@(private = "file")
font_pop :: proc() {
	imgui.PopFont()
}

@(private = "file")
font_get_dpi_scale :: proc() -> f32 {
	return g_dpi_scale
}
