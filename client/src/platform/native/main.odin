package main

import "core:fmt"
import "mr:app"

main :: proc() {
	state: app.App_State
	app.init(&state)
	defer app.shutdown(&state)

	buf := app.update(&state)
	fmt.printf("Frame %v: %v commands\n", state.frame, len(buf.commands))
	render_commands(buf)
}
