package main

// RCL -> ImGui DrawList renderer.
// Maps each RCL command to the corresponding ImDrawList call.
// Font-aware: pushes target font for text commands.

import imgui "deps:imgui"
import "mr:ui"

// Convert our [4]f32 RGBA color to ImGui packed u32 (ABGR).
color_to_u32 :: proc(c: ui.Color) -> u32 {
	return imgui.ColorConvertFloat4ToU32({c.r, c.g, c.b, c.a})
}

render_commands :: proc(buf: ^ui.Command_Buffer, display_w, display_h: f32) {
	dl := imgui.GetBackgroundDrawList()

	for cmd in buf.commands {
		switch c in cmd {
		case ui.Cmd_Clear:
			imgui.DrawList_AddRectFilled(dl, {0, 0}, {display_w, display_h}, color_to_u32(c.color))

		case ui.Cmd_Rect_Filled:
			p_min := imgui.Vec2{c.rect.pos.x, c.rect.pos.y}
			p_max := imgui.Vec2{c.rect.pos.x + c.rect.size.x, c.rect.pos.y + c.rect.size.y}
			imgui.DrawList_AddRectFilled(dl, p_min, p_max, color_to_u32(c.color))

		case ui.Cmd_Line:
			imgui.DrawList_AddLine(
				dl,
				{c.from.x, c.from.y},
				{c.to.x, c.to.y},
				color_to_u32(c.color),
				c.thickness,
			)

		case ui.Cmd_Text:
			font := get_font_for_id(c.font_id)
			if font != nil do imgui.PushFont(font)
			cs := ui.resolve_cstr(buf, c)
			imgui.DrawList_AddText(dl, {c.pos.x, c.pos.y}, color_to_u32(c.color), cs)
			if font != nil do imgui.PopFont()

		case ui.Cmd_Clip_Push:
			clip_min := imgui.Vec2{c.rect.pos.x, c.rect.pos.y}
			clip_max := imgui.Vec2{c.rect.pos.x + c.rect.size.x, c.rect.pos.y + c.rect.size.y}
			imgui.DrawList_PushClipRect(dl, clip_min, clip_max, true)

		case ui.Cmd_Clip_Pop:
			imgui.DrawList_PopClipRect(dl)
		}
	}
}
