package main

// Stub font port for WASM/web. CSS fonts handle rendering natively.

import "mr:ports"
import "mr:ui"

stub_font_port :: proc() -> ports.Font_Port {
	return {
		load      = stub_font_load,
		push_font = stub_font_push,
		pop_font  = stub_font_pop,
		dpi_scale = stub_font_dpi_scale,
	}
}

@(private = "file")
stub_font_load :: proc(id: ui.Font_Id, ttf_data: []u8, size_px: f32) -> bool {
	return true // web uses CSS fonts
}

@(private = "file")
stub_font_push :: proc(id: ui.Font_Id) {}

@(private = "file")
stub_font_pop :: proc() {}

@(private = "file")
stub_font_dpi_scale :: proc() -> f32 {
	return 1.0
}
