package widgets

import "mr:ui"

hello :: proc(buf: ^ui.Command_Buffer) {
	ui.push(buf, ui.Cmd_Text{
		pos   = {20, 40},
		text  = "Hello, Market Raccoon!",
		color = ui.COL_WHITE,
		size  = ui.FONT_SIZE_XL,
	})
}
