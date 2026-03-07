package backend

// GLFW 3.3 + OpenGL 3.3 Core + ImGui-GLFW/OpenGL3 backend.
// All GLFW, OpenGL, and ImGui-backend code is encapsulated here.
// main.odin never imports these packages.

import "core:c"
import "vendor:glfw"
import gl "vendor:OpenGL"
import imgui "deps:imgui"
import "mr:ports"

// --- File-private state ---

@(private = "file")
g_window: glfw.WindowHandle

@(private = "file")
g_scroll_accum: [2]f32

@(private = "file")
g_prev_mouse_pos: [2]f32

@(private = "file")
g_prev_mouse_buttons: [ports.Mouse_Button]bool

@(private = "file")
g_prev_keys_down: bit_set[ports.Key]

@(private = "file")
g_prev_input_time_s: f64

@(private = "file")
g_has_prev_mouse_pos: bool

@(private = "file")
g_has_prev_input_time: bool

// --- Constructor ---

make_glfw_backend :: proc() -> Backend {
	return Backend{
		init             = glfw_init,
		shutdown         = glfw_shutdown,
		should_close     = glfw_should_close,
		poll_events      = glfw_poll_events,
		begin_frame      = glfw_begin_frame,
		end_frame        = glfw_end_frame,
		swap             = glfw_swap,
		framebuffer_size = glfw_framebuffer_size,
		time_now         = glfw_time_now,
		collect_input    = glfw_collect_input,
	}
}

// --- Lifecycle ---

@(private = "file")
glfw_init :: proc(title: cstring, width, height: i32) -> bool {
	if !glfw.Init() do return false

	glfw.WindowHint(glfw.CONTEXT_VERSION_MAJOR, 3)
	glfw.WindowHint(glfw.CONTEXT_VERSION_MINOR, 3)
	glfw.WindowHint(glfw.OPENGL_PROFILE, glfw.OPENGL_CORE_PROFILE)
	glfw.WindowHint(glfw.OPENGL_FORWARD_COMPAT, c.int(1)) // macOS required

	g_window = glfw.CreateWindow(width, height, title, nil, nil)
	if g_window == nil {
		glfw.Terminate()
		return false
	}

	glfw.MakeContextCurrent(g_window)
	glfw.SwapInterval(1) // vsync

	// Register scroll callback BEFORE ImGui so it chains ours.
	glfw.SetScrollCallback(g_window, glfw_scroll_callback)

	// OpenGL loader.
	gl.load_up_to(3, 3, glfw.gl_set_proc_address)

	// ImGui context + backends.
	imgui.CreateContext()
	io := imgui.GetIO()
	io.ini_filename = nil // disable imgui.ini
	imgui.StyleColorsDark()

	imgui.ImplGlfw_InitForOpenGL(g_window, true)
	imgui.ImplOpenGL3_Init("#version 330")

	return true
}

@(private = "file")
glfw_shutdown :: proc() {
	imgui.ImplOpenGL3_Shutdown()
	imgui.ImplGlfw_Shutdown()
	imgui.DestroyContext()
	glfw.DestroyWindow(g_window)
	glfw.Terminate()
}

// --- Frame loop ---

@(private = "file")
glfw_should_close :: proc() -> bool {
	return bool(glfw.WindowShouldClose(g_window))
}

@(private = "file")
glfw_poll_events :: proc() {
	glfw.PollEvents()
}

@(private = "file")
glfw_begin_frame :: proc() {
	imgui.ImplOpenGL3_NewFrame()
	imgui.ImplGlfw_NewFrame()
	imgui.NewFrame()
}

@(private = "file")
glfw_end_frame :: proc() {
	imgui.Render()
	w, h := glfw.GetFramebufferSize(g_window)
	gl.Viewport(0, 0, w, h)
	gl.ClearColor(0.04, 0.04, 0.04, 1.0)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	imgui.ImplOpenGL3_RenderDrawData(imgui.GetDrawData())
}

@(private = "file")
glfw_swap :: proc() {
	glfw.SwapBuffers(g_window)
}

// --- Queries ---

@(private = "file")
glfw_framebuffer_size :: proc() -> (w: i32, h: i32) {
	return glfw.GetFramebufferSize(g_window)
}

@(private = "file")
glfw_time_now :: proc() -> f64 {
	return glfw.GetTime()
}

// --- Input ---

@(private = "file")
glfw_scroll_callback :: proc "c" (window: glfw.WindowHandle, xoff, yoff: f64) {
	g_scroll_accum.x += f32(xoff)
	g_scroll_accum.y += f32(yoff)
}

@(private = "file")
mark_key_edges :: proc(input: ^ports.Input_State, curr, prev: bit_set[ports.Key], key: ports.Key) {
	down := key in curr
	was_down := key in prev
	if down && !was_down do input.keys.just_pressed += {key}
	if !down && was_down do input.keys.just_released += {key}
}

@(private = "file")
glfw_collect_input :: proc() -> ports.Input_State {
	mx, my := glfw.GetCursorPos(g_window)
	fbw, fbh := glfw.GetFramebufferSize(g_window)
	input: ports.Input_State

	input.mouse.pos = {f32(mx), f32(my)}
	if g_has_prev_mouse_pos {
		input.mouse.delta = {input.mouse.pos.x - g_prev_mouse_pos.x, input.mouse.pos.y - g_prev_mouse_pos.y}
	}
	g_prev_mouse_pos = input.mouse.pos
	g_has_prev_mouse_pos = true

	input.viewport_size = {f32(fbw), f32(fbh)}
	left_down := glfw.GetMouseButton(g_window, glfw.MOUSE_BUTTON_LEFT) == glfw.PRESS
	right_down := glfw.GetMouseButton(g_window, glfw.MOUSE_BUTTON_RIGHT) == glfw.PRESS
	middle_down := glfw.GetMouseButton(g_window, glfw.MOUSE_BUTTON_MIDDLE) == glfw.PRESS
	input.mouse.buttons[.Left] = left_down
	input.mouse.buttons[.Right] = right_down
	input.mouse.buttons[.Middle] = middle_down
	input.mouse.pressed[.Left] = left_down && !g_prev_mouse_buttons[.Left]
	input.mouse.pressed[.Right] = right_down && !g_prev_mouse_buttons[.Right]
	input.mouse.pressed[.Middle] = middle_down && !g_prev_mouse_buttons[.Middle]
	input.mouse.released[.Left] = !left_down && g_prev_mouse_buttons[.Left]
	input.mouse.released[.Right] = !right_down && g_prev_mouse_buttons[.Right]
	input.mouse.released[.Middle] = !middle_down && g_prev_mouse_buttons[.Middle]
	g_prev_mouse_buttons[.Left] = left_down
	g_prev_mouse_buttons[.Right] = right_down
	g_prev_mouse_buttons[.Middle] = middle_down

	input.mouse.scroll = g_scroll_accum
	g_scroll_accum = {0, 0}

	curr_keys: bit_set[ports.Key]
	if glfw.GetKey(g_window, glfw.KEY_UP) == glfw.PRESS do curr_keys += {.Up}
	if glfw.GetKey(g_window, glfw.KEY_DOWN) == glfw.PRESS do curr_keys += {.Down}
	if glfw.GetKey(g_window, glfw.KEY_LEFT) == glfw.PRESS do curr_keys += {.Left}
	if glfw.GetKey(g_window, glfw.KEY_RIGHT) == glfw.PRESS do curr_keys += {.Right}
	if glfw.GetKey(g_window, glfw.KEY_ENTER) == glfw.PRESS do curr_keys += {.Enter}
	if glfw.GetKey(g_window, glfw.KEY_ESCAPE) == glfw.PRESS do curr_keys += {.Escape}
	if glfw.GetKey(g_window, glfw.KEY_TAB) == glfw.PRESS do curr_keys += {.Tab}
	if glfw.GetKey(g_window, glfw.KEY_SPACE) == glfw.PRESS do curr_keys += {.Space}
	if glfw.GetKey(g_window, glfw.KEY_1) == glfw.PRESS do curr_keys += {.Num_1}
	if glfw.GetKey(g_window, glfw.KEY_2) == glfw.PRESS do curr_keys += {.Num_2}
	if glfw.GetKey(g_window, glfw.KEY_3) == glfw.PRESS do curr_keys += {.Num_3}
	if glfw.GetKey(g_window, glfw.KEY_4) == glfw.PRESS do curr_keys += {.Num_4}
	if glfw.GetKey(g_window, glfw.KEY_5) == glfw.PRESS do curr_keys += {.Num_5}
	if glfw.GetKey(g_window, glfw.KEY_6) == glfw.PRESS do curr_keys += {.Num_6}
	if glfw.GetKey(g_window, glfw.KEY_7) == glfw.PRESS do curr_keys += {.Num_7}
	if glfw.GetKey(g_window, glfw.KEY_8) == glfw.PRESS do curr_keys += {.Num_8}
	if glfw.GetKey(g_window, glfw.KEY_9) == glfw.PRESS do curr_keys += {.Num_9}
	if glfw.GetKey(g_window, glfw.KEY_S) == glfw.PRESS do curr_keys += {.S}
	if glfw.GetKey(g_window, glfw.KEY_SLASH) == glfw.PRESS do curr_keys += {.Slash}
	if glfw.GetKey(g_window, glfw.KEY_C) == glfw.PRESS do curr_keys += {.C}
	if glfw.GetKey(g_window, glfw.KEY_G) == glfw.PRESS do curr_keys += {.G}
	if glfw.GetKey(g_window, glfw.KEY_F) == glfw.PRESS do curr_keys += {.F}
	if glfw.GetKey(g_window, glfw.KEY_M) == glfw.PRESS do curr_keys += {.M}
	if glfw.GetKey(g_window, glfw.KEY_B) == glfw.PRESS do curr_keys += {.B}
	if glfw.GetKey(g_window, glfw.KEY_V) == glfw.PRESS do curr_keys += {.V}
	if glfw.GetKey(g_window, glfw.KEY_R) == glfw.PRESS do curr_keys += {.R}
	if glfw.GetKey(g_window, glfw.KEY_I) == glfw.PRESS do curr_keys += {.I}
	if glfw.GetKey(g_window, glfw.KEY_H) == glfw.PRESS do curr_keys += {.H}
	if glfw.GetKey(g_window, glfw.KEY_J) == glfw.PRESS do curr_keys += {.J}
	if glfw.GetKey(g_window, glfw.KEY_K) == glfw.PRESS do curr_keys += {.K}
	if glfw.GetKey(g_window, glfw.KEY_Z) == glfw.PRESS do curr_keys += {.Z}
	if glfw.GetKey(g_window, glfw.KEY_D) == glfw.PRESS do curr_keys += {.D}
	if glfw.GetKey(g_window, glfw.KEY_DELETE) == glfw.PRESS || glfw.GetKey(g_window, glfw.KEY_BACKSPACE) == glfw.PRESS do curr_keys += {.Delete}
	input.keys.pressed = curr_keys
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Up)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Down)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Left)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Right)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Enter)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Escape)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Tab)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Space)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Num_1)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Num_2)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Num_3)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Num_4)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Num_5)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Num_6)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Num_7)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Num_8)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Num_9)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .S)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Slash)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .C)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .G)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .F)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .M)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .B)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .V)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .R)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .I)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .H)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .J)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .K)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Z)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .D)
	mark_key_edges(&input, curr_keys, g_prev_keys_down, .Delete)
	g_prev_keys_down = curr_keys

	input.modifiers.shift =
		glfw.GetKey(g_window, glfw.KEY_LEFT_SHIFT) == glfw.PRESS ||
		glfw.GetKey(g_window, glfw.KEY_RIGHT_SHIFT) == glfw.PRESS
	input.modifiers.ctrl =
		glfw.GetKey(g_window, glfw.KEY_LEFT_CONTROL) == glfw.PRESS ||
		glfw.GetKey(g_window, glfw.KEY_RIGHT_CONTROL) == glfw.PRESS
	input.modifiers.alt =
		glfw.GetKey(g_window, glfw.KEY_LEFT_ALT) == glfw.PRESS ||
		glfw.GetKey(g_window, glfw.KEY_RIGHT_ALT) == glfw.PRESS
	input.modifiers.super =
		glfw.GetKey(g_window, glfw.KEY_LEFT_SUPER) == glfw.PRESS ||
		glfw.GetKey(g_window, glfw.KEY_RIGHT_SUPER) == glfw.PRESS

	now_s := glfw.GetTime()
	if g_has_prev_input_time {
		dt := now_s - g_prev_input_time_s
		if dt > 0 do input.delta_time = f32(dt)
	}
	g_prev_input_time_s = now_s
	g_has_prev_input_time = true

	return input
}
