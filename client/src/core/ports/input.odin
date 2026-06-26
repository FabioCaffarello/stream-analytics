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
	Num_7,
	Num_8,
	Num_9,
	S,
	Slash,
	C,
	G,
	F,
	M,
	B,
	V,
	R,
	I,
	H,
	J,
	K,
	Z,
	D,     // S46: Ctrl+D = capture runtime snapshot
	Delete,
	Home,  // S141: jump to live edge
	End,   // S141: jump to oldest candle
}

Mouse :: struct {
	pos:      ui.Vec2,
	delta:    ui.Vec2,
	buttons:  [Mouse_Button]bool, // down
	pressed:  [Mouse_Button]bool, // edge: down this frame
	released: [Mouse_Button]bool, // edge: up this frame
	scroll:   ui.Vec2, // x = horizontal, y = vertical (positive = up)
}

Keys :: struct {
	pressed:       bit_set[Key], // down
	just_pressed:  bit_set[Key], // edge: down this frame
	just_released: bit_set[Key], // edge: up this frame
}

Modifiers :: struct {
	shift: bool,
	ctrl:  bool,
	alt:   bool,
	super: bool,
}

Input_State :: struct {
	mouse:         Mouse,
	keys:          Keys,
	modifiers:     Modifiers,
	delta_time:    f32, // seconds since previous frame
	viewport_size: ui.Vec2, // render surface size in pixels (canvas/framebuffer)
}
