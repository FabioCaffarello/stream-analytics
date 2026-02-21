package main

// Native text measurement — ImGui-backed via igCalcTextSize.
// Font-aware: pushes the target font before measurement, pops after.

import "core:strings"
import imgui "deps:imgui"
import "mr:ports"
import "mr:ui"

make_text_port :: proc() -> ports.Text_Port {
	return {
		measure     = measure_text_imgui,
		line_height = line_height_imgui,
	}
}

measure_text_imgui :: proc(size: f32, text: string) -> ui.Vec2 {
	cs := strings.clone_to_cstring(text, context.temp_allocator)
	out: imgui.Vec2
	imgui.CalcTextSize(&out, cs)
	return ui.Vec2(out)
}

line_height_imgui :: proc(size: f32) -> f32 {
	return imgui.GetTextLineHeight()
}
