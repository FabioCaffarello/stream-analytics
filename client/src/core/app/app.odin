package app

import "mr:ui"
import "mr:widgets"

App_State :: struct {
	cmd_buf: ui.Command_Buffer,
	frame:   u64,
}

init :: proc(state: ^App_State) {
	state.cmd_buf = ui.make_buffer()
}

shutdown :: proc(state: ^App_State) {
	ui.destroy_buffer(&state.cmd_buf)
}

update :: proc(state: ^App_State) -> ^ui.Command_Buffer {
	ui.reset(&state.cmd_buf)
	state.frame += 1

	ui.push(&state.cmd_buf, ui.Cmd_Clear{color = {0.04, 0.04, 0.04, 1.0}})
	widgets.hello(&state.cmd_buf)

	return &state.cmd_buf
}
