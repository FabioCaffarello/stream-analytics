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
glfw_collect_input :: proc() -> ports.Input_State {
	mx, my := glfw.GetCursorPos(g_window)
	fbw, fbh := glfw.GetFramebufferSize(g_window)
	input: ports.Input_State

	input.mouse.pos = {f32(mx), f32(my)}
	input.viewport_size = {f32(fbw), f32(fbh)}
	input.mouse.buttons[.Left]   = glfw.GetMouseButton(g_window, glfw.MOUSE_BUTTON_LEFT) == glfw.PRESS
	input.mouse.buttons[.Right]  = glfw.GetMouseButton(g_window, glfw.MOUSE_BUTTON_RIGHT) == glfw.PRESS
	input.mouse.buttons[.Middle] = glfw.GetMouseButton(g_window, glfw.MOUSE_BUTTON_MIDDLE) == glfw.PRESS
	input.mouse.scroll = g_scroll_accum
	g_scroll_accum = {0, 0}

	if glfw.GetKey(g_window, glfw.KEY_UP)     == glfw.PRESS do input.keys.pressed += {.Up}
	if glfw.GetKey(g_window, glfw.KEY_DOWN)   == glfw.PRESS do input.keys.pressed += {.Down}
	if glfw.GetKey(g_window, glfw.KEY_LEFT)   == glfw.PRESS do input.keys.pressed += {.Left}
	if glfw.GetKey(g_window, glfw.KEY_RIGHT)  == glfw.PRESS do input.keys.pressed += {.Right}
	if glfw.GetKey(g_window, glfw.KEY_ENTER)  == glfw.PRESS do input.keys.pressed += {.Enter}
	if glfw.GetKey(g_window, glfw.KEY_ESCAPE) == glfw.PRESS do input.keys.pressed += {.Escape}
	if glfw.GetKey(g_window, glfw.KEY_TAB)    == glfw.PRESS do input.keys.pressed += {.Tab}
	if glfw.GetKey(g_window, glfw.KEY_SPACE)  == glfw.PRESS do input.keys.pressed += {.Space}

	return input
}
