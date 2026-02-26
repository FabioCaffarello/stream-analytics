package ports

// Input port — platform-injected per-frame input state.
// Native: populated from GLFW callbacks/polling.
// Web:    populated from JS DOM events.

import "mr:ui"

Mouse_Button :: enum u8 {
	Left,
	Right,
	Middle,
}

Key :: enum u8 {
	Up,
	Down,
	Left,
	Right,
	Enter,
	Escape,
	Tab,
	Space,
	Num_1,
	Num_2,
	Num_3,
	Num_4,
	Num_5,
	Num_6,
}

Mouse :: struct {
	pos:     ui.Vec2,
	buttons: [Mouse_Button]bool,
	scroll:  ui.Vec2, // x = horizontal, y = vertical (positive = up)
}

Keys :: struct {
	pressed: bit_set[Key],
}

Input_State :: struct {
	mouse:         Mouse,
	keys:          Keys,
	viewport_size: ui.Vec2, // render surface size in pixels (canvas/framebuffer)
}
