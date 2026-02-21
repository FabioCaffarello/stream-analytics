package main

// Fase 1: Canvas2D renderer via foreign procs mapped to JS.
// Fase 3+: replace with full odin-wasm Canvas2D bindings.

import "mr:ui"

foreign import odin_env "odin_env"

@(default_calling_convention = "contextless")
foreign odin_env {
	canvas_clear     :: proc(r, g, b, a: f32) ---
	canvas_fill_rect :: proc(x, y, w, h, r, g, b, a: f32) ---
	canvas_fill_text :: proc(ptr: [^]u8, text_len: i32, x, y, size, r, g, b, a: f32) ---
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
			// Fase 3+
		case ui.Cmd_Clip_Push:
			// Fase 3+
		case ui.Cmd_Clip_Pop:
			// Fase 3+
		}
	}
}
