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
//
// Z-layer: each command is tagged with a z_layer (u8). Renderers call
// sort_commands_by_z_layer() before iterating to ensure correct stacking.

import "core:slice"

Color :: [4]f32
Vec2  :: [2]f32

Rect :: struct {
	pos:  Vec2,
	size: Vec2,
}

// --- Z-Layer constants ---

Z_BASE    :: u8(0)  // backgrounds, grid
Z_WIDGET  :: u8(1)  // chart, orderbook, trades content
Z_PANEL   :: u8(2)  // sidebar, detail panel
Z_OVERLAY :: u8(3)  // dropdown lists, cell pickers
Z_MODAL   :: u8(4)  // help overlay, exchange manager, widget catalog
Z_TOOLTIP :: u8(5)  // crosshair tooltips, hover info

// Sentinel: when passed to push/push_text, use buf.current_z_layer instead.
Z_CURRENT :: u8(255)

// --- Commands ---

Command :: union {
	Cmd_Clear,
	Cmd_Rect_Filled,
	Cmd_Line,
	Cmd_Text,
	Cmd_Clip_Push,
	Cmd_Clip_Pop,
}

Render_Command :: struct {
	z_layer: u8,
	cmd:     Command,
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
	commands:        [dynamic]Render_Command,
	frame_arena:     Frame_Arena,
	current_z_layer: u8,  // default z_layer for push/push_text (init = Z_WIDGET)
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
		commands        = make([dynamic]Render_Command, allocator),
		frame_arena     = make_frame_arena(allocator),
		current_z_layer = Z_WIDGET,
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

push :: proc(buf: ^Command_Buffer, cmd: Command, z_layer: u8 = Z_CURRENT) {
	layer := z_layer == Z_CURRENT ? buf.current_z_layer : z_layer
	append(&buf.commands, Render_Command{z_layer = layer, cmd = cmd})
}

// Intern text into the per-frame arena and push a Cmd_Text command.
push_text :: proc(
	buf: ^Command_Buffer, pos: Vec2, text: string, color: Color, size: f32,
	font_id: Font_Id = .Default, z_layer: u8 = Z_CURRENT,
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
	}, z_layer)
}

// Stable sort commands by z_layer. Call before rendering.
// Preserves insertion order within the same z_layer.
sort_commands_by_z_layer :: proc(buf: ^Command_Buffer) {
	if len(buf.commands) <= 1 do return
	slice.stable_sort_by(buf.commands[:], proc(a, b: Render_Command) -> bool {
		return a.z_layer < b.z_layer
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
