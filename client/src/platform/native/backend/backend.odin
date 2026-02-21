package backend

// Backend — windowing/GL/ImGui-backend abstraction.
//
// Contract: main.odin calls these procs and NEVER imports vendor:glfw,
// vendor:OpenGL, or deps:imgui backend procs directly.
//
// Implementations:
//   glfw_backend.odin  — GLFW 3.3 + OpenGL 3.3 + ImGui-GLFW (current)
//   sdl2_backend.odin  — SDL2 + OpenGL 3.3 + ImGui-SDL2     (future)
//
// Adding a new backend:
//   1. Create <name>_backend.odin in this package
//   2. Implement make_<name>_backend() -> Backend
//   3. Wire it in main.odin (single line change)

import "mr:ports"

Backend :: struct {
	// --- Lifecycle ---

	// Create window, init OpenGL, init ImGui context + platform backend.
	// Returns false on failure (main should exit).
	init:     proc(title: cstring, width, height: i32) -> bool,

	// Tear down ImGui backends, destroy window, terminate windowing library.
	shutdown: proc(),

	// --- Frame loop ---

	// Returns true when the user has requested to close the window.
	should_close: proc() -> bool,

	// Process OS events (keyboard, mouse, resize, etc.).
	poll_events: proc(),

	// Begin a new render frame (ImGui NewFrame for all backends).
	// Call AFTER poll_events, BEFORE app.update / render_commands.
	begin_frame: proc(),

	// Finalize rendering: imgui.Render, GL viewport+clear, draw ImGui data.
	// Call AFTER render_commands, BEFORE swap.
	end_frame: proc(),

	// Present the framebuffer (buffer swap).
	swap: proc(),

	// --- Queries ---

	// Current framebuffer dimensions in pixels (accounts for HiDPI).
	framebuffer_size: proc() -> (w: i32, h: i32),

	// Monotonic time in seconds since init.
	time_now: proc() -> f64,

	// Collect per-frame input state (mouse, keyboard, scroll).
	collect_input: proc() -> ports.Input_State,
}
