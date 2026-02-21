package main

// Web text measurement — Canvas2D-backed via measureText.

import "mr:ports"
import "mr:ui"

foreign import odin_env "odin_env"

@(default_calling_convention = "contextless")
foreign odin_env {
	canvas_measure_text :: proc(ptr: [^]u8, text_len: i32, size: f32) -> f32 ---
}

make_text_port :: proc() -> ports.Text_Port {
	return {
		measure     = measure_text_canvas2d,
		line_height = line_height_canvas2d,
	}
}

measure_text_canvas2d :: proc(size: f32, text: string) -> ui.Vec2 {
	w := canvas_measure_text(raw_data(text), i32(len(text)), size)
	return {w, size * 1.2}
}

line_height_canvas2d :: proc(size: f32) -> f32 {
	return size * 1.2
}
