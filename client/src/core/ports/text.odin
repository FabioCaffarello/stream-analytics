package ports

// Text measurement port — platform-injected capability.
// Core widgets use this for dynamic layout instead of hardcoded pixel offsets.
//
// Native: backed by ImGui (igCalcTextSize).
// Web:    backed by Canvas2D (measureText).

import "mr:ui"

Text_Port :: struct {
	measure:     proc(size: f32, text: string) -> ui.Vec2,
	line_height: proc(size: f32) -> f32,
}
