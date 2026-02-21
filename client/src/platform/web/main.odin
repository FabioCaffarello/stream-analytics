package main

import "core:fmt"
import "mr:app"
import "mr:ports"

main :: proc() {
	text_port := make_text_port()
	md_port := stub_marketdata_port()
	font_port := stub_font_port()
	settings_port := stub_settings_port()

	state: app.App_State
	app.init(&state, text_port, md_port, font_port, settings_port)
	defer app.shutdown(&state)

	input: ports.Input_State // zero-initialized (no events yet)
	buf := app.update(&state, input)
	fmt.printf("Frame %v: %v commands\n", state.frame, len(buf.commands))
	render_commands(buf)
}
