package main

// Canvas2D renderer via foreign procs mapped to JS.

import "mr:ui"

foreign import odin_env "odin_env"

@(default_calling_convention = "contextless")
foreign odin_env {
	canvas_clear     :: proc(r, g, b, a: f32) ---
	canvas_fill_rect :: proc(x, y, w, h, r, g, b, a: f32) ---
	canvas_fill_text :: proc(ptr: [^]u8, text_len: i32, x, y, size, r, g, b, a: f32) ---
	canvas_line      :: proc(x1, y1, x2, y2, r, g, b, a, thickness: f32) ---
	canvas_clip_push :: proc(x, y, w, h: f32) ---
	canvas_clip_pop  :: proc() ---
}

render_commands :: proc(buf: ^ui.Command_Buffer) {
	for cmd in buf.commands {
		switch c in cmd {
		case ui.Cmd_Clear:
			canvas_clear(c.color.r, c.color.g, c.color.b, c.color.a)
		case ui.Cmd_Rect_Filled:
			canvas_fill_rect(
				c.rect.pos.x, c.rect.pos.y, c.rect.size.x, c.rect.size.y,
				c.color.r, c.color.g, c.color.b, c.color.a,
			)
		case ui.Cmd_Text:
			ptr, tlen := ui.resolve_text(buf, c)
			canvas_fill_text(
				ptr, tlen,
				c.pos.x, c.pos.y, c.size,
				c.color.r, c.color.g, c.color.b, c.color.a,
			)
		case ui.Cmd_Line:
			canvas_line(
				c.from.x, c.from.y, c.to.x, c.to.y,
				c.color.r, c.color.g, c.color.b, c.color.a, c.thickness,
			)
		case ui.Cmd_Clip_Push:
			canvas_clip_push(c.rect.pos.x, c.rect.pos.y, c.rect.size.x, c.rect.size.y)
		case ui.Cmd_Clip_Pop:
			canvas_clip_pop()
		}
	}
}
