package layers

import "mr:ui"

// Unified canvas renderer for layer primitives.
canvas_render_outputs :: proc(buf: ^ui.Command_Buffer, out: ^Layer_Outputs) {
	if buf == nil || out == nil do return
	for i in 0 ..< out.count {
		item := out.items[i]
		switch item.kind {
		case .Line:
			line := item.data.line
			ui.push(buf, ui.Cmd_Line{
				from = line.from,
				to = line.to,
				color = line.color,
				thickness = line.thickness,
			})
		case .Bar:
			bar := item.data.bar
			ui.push(buf, ui.Cmd_Rect_Filled{
				rect = bar.rect,
				color = bar.color,
			})
		case .Text_Badge:
			text := item.data.text
			ui.push_text(buf,
				text.pos,
				text_badge_string(&text),
				text.color,
				text.size,
				.Mono,
			)
		}
	}
}
