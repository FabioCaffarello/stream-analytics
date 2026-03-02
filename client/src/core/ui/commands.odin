package ui

// Render Command List (RCL) — the portability core.
//
// core never draws directly; it emits commands into a Command_Buffer.
// Platform renderers consume the buffer each frame:
//   native → renderer_imgui.odin  (ImGui drawlist)
//   web    → renderer_canvas2d.odin (Canvas2D via odin-wasm)
//
// Text is interned into a per-frame byte arena. Widgets call push_text()
// which copies text + NUL into the arena and stores an offset in Cmd_Text.
// Renderers resolve offsets via resolve_cstr / resolve_text before reset().

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
	pos:      Vec2,
	text_off: u32,      // byte offset into Command_Buffer.frame_arena.bytes
	text_len: u32,      // byte length (not including NUL)
	color:    Color,
	size:     f32,
	font_id:  Font_Id,  // .Default when omitted
}

Cmd_Clip_Push :: struct {
	rect: Rect,
}

Cmd_Clip_Pop :: struct {}

// --- Command_Buffer ---

Frame_Arena :: struct {
	bytes: [dynamic]u8,
	high_water_bytes: int,
	grow_events:      u64,
	reset_count:      u64,
}

Command_Buffer :: struct {
	commands:    [dynamic]Command,
	frame_arena: Frame_Arena,
}

make_frame_arena :: proc(allocator := context.allocator) -> Frame_Arena {
	arena := make([dynamic]u8, allocator)
	reserve(&arena, 8 * 1024)
	return Frame_Arena{
		bytes = arena,
	}
}

destroy_frame_arena :: proc(arena: ^Frame_Arena) {
	if arena == nil do return
	delete(arena.bytes)
	arena^ = {}
}

reset_frame_arena :: proc(arena: ^Frame_Arena) {
	if arena == nil do return
	clear(&arena.bytes)
	arena.reset_count += 1
}

frame_arena_usage :: proc(buf: ^Command_Buffer) -> int {
	if buf == nil do return 0
	return len(buf.frame_arena.bytes)
}

frame_arena_capacity :: proc(buf: ^Command_Buffer) -> int {
	if buf == nil do return 0
	return cap(buf.frame_arena.bytes)
}

make_buffer :: proc(allocator := context.allocator) -> Command_Buffer {
	return {
		commands    = make([dynamic]Command, allocator),
		frame_arena = make_frame_arena(allocator),
	}
}

destroy_buffer :: proc(buf: ^Command_Buffer) {
	delete(buf.commands)
	destroy_frame_arena(&buf.frame_arena)
}

reset :: proc(buf: ^Command_Buffer) {
	clear(&buf.commands)
	reset_frame_arena(&buf.frame_arena)
}

push :: proc(buf: ^Command_Buffer, cmd: Command) {
	append(&buf.commands, cmd)
}

// Intern text into the per-frame arena and push a Cmd_Text command.
push_text :: proc(
	buf: ^Command_Buffer, pos: Vec2, text: string, color: Color, size: f32,
	font_id: Font_Id = .Default,
) {
	off := u32(len(buf.frame_arena.bytes))
	text_bytes := transmute([]u8)text
	prev_cap := cap(buf.frame_arena.bytes)
	append(&buf.frame_arena.bytes, ..text_bytes)
	append(&buf.frame_arena.bytes, 0) // NUL terminator
	if cap(buf.frame_arena.bytes) > prev_cap {
		buf.frame_arena.grow_events += 1
	}
	if len(buf.frame_arena.bytes) > buf.frame_arena.high_water_bytes {
		buf.frame_arena.high_water_bytes = len(buf.frame_arena.bytes)
	}
	push(buf, Cmd_Text{
		pos      = pos,
		text_off = off,
		text_len = u32(len(text)),
		color    = color,
		size     = size,
		font_id  = font_id,
	})
}

// Resolve interned text as cstring (NUL-terminated). Only valid before reset().
resolve_cstr :: proc(buf: ^Command_Buffer, cmd: Cmd_Text) -> cstring {
	return transmute(cstring)raw_data(buf.frame_arena.bytes[cmd.text_off:])
}

// Resolve interned text as raw pointer + length. Only valid before reset().
resolve_text :: proc(buf: ^Command_Buffer, cmd: Cmd_Text) -> ([^]u8, i32) {
	return raw_data(buf.frame_arena.bytes[cmd.text_off:]), i32(cmd.text_len)
}
