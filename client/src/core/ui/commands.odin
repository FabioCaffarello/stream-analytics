package ui

// Render Command List (RCL) — the portability core.
//
// core never draws directly; it emits commands into a Command_Buffer.
// Platform renderers consume the buffer each frame:
//   native → renderer_imgui.odin  (ImGui drawlist)
//   web    → renderer_canvas2d.odin (Canvas2D via odin-wasm)

Color :: [4]f32
Vec2  :: [2]f32

Rect :: struct {
	pos:  Vec2,
	size: Vec2,
}

Command :: union {
	Cmd_Clear,
	Cmd_Rect_Filled,
	Cmd_Line,
	Cmd_Text,
	Cmd_Clip_Push,
	Cmd_Clip_Pop,
}

Cmd_Clear :: struct {
	color: Color,
}

Cmd_Rect_Filled :: struct {
	rect:  Rect,
	color: Color,
}

Cmd_Line :: struct {
	from:      Vec2,
	to:        Vec2,
	color:     Color,
	thickness: f32,
}

Cmd_Text :: struct {
	pos:   Vec2,
	text:  string,
	color: Color,
	size:  f32,
}

Cmd_Clip_Push :: struct {
	rect: Rect,
}

Cmd_Clip_Pop :: struct {}

// --- Command_Buffer ---

Command_Buffer :: struct {
	commands: [dynamic]Command,
}

make_buffer :: proc(allocator := context.allocator) -> Command_Buffer {
	return {commands = make([dynamic]Command, allocator)}
}

destroy_buffer :: proc(buf: ^Command_Buffer) {
	delete(buf.commands)
}

reset :: proc(buf: ^Command_Buffer) {
	clear(&buf.commands)
}

push :: proc(buf: ^Command_Buffer, cmd: Command) {
	append(&buf.commands, cmd)
}
