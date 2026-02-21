package widgets

import "mr:ports"
import "mr:ui"

hello :: proc(buf: ^ui.Command_Buffer, text: ports.Text_Port) {
	ui.push_text(buf, {20, 40}, "Hello, Market Raccoon!", ui.COL_WHITE, ui.FONT_SIZE_XL)
}
