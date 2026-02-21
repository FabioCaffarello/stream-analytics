package main

// Fase 1: printf renderer (reads RCL → stdout).
// Fase 3+: replace with ImGui drawlist calls.

import "core:fmt"
import "mr:ui"

render_commands :: proc(buf: ^ui.Command_Buffer) {
	for cmd in buf.commands {
		switch c in cmd {
		case ui.Cmd_Clear:
			fmt.printf("  Clear(%.2f, %.2f, %.2f, %.2f)\n",
				c.color.r, c.color.g, c.color.b, c.color.a)
		case ui.Cmd_Rect_Filled:
			fmt.printf("  RectFilled(%.0f, %.0f, %.0f, %.0f)\n",
				c.rect.pos.x, c.rect.pos.y, c.rect.size.x, c.rect.size.y)
		case ui.Cmd_Line:
			fmt.printf("  Line(%.0f,%.0f -> %.0f,%.0f)\n",
				c.from.x, c.from.y, c.to.x, c.to.y)
		case ui.Cmd_Text:
			fmt.printf("  Text(\"%s\" at %.0f,%.0f size=%.0f)\n",
				c.text, c.pos.x, c.pos.y, c.size)
		case ui.Cmd_Clip_Push:
			fmt.printf("  ClipPush(%.0f, %.0f, %.0f, %.0f)\n",
				c.rect.pos.x, c.rect.pos.y, c.rect.size.x, c.rect.size.y)
		case ui.Cmd_Clip_Pop:
			fmt.println("  ClipPop")
		}
	}
}
