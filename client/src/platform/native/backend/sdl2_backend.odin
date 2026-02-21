package backend

// SDL2 + OpenGL 3.3 Core + ImGui-SDL2/OpenGL3 backend.
// All SDL2, OpenGL, and ImGui-backend code is encapsulated here.
// main.odin never imports these packages.

import "core:c"
import sdl "vendor:sdl2"
import gl "vendor:OpenGL"
import imgui "deps:imgui"
import "mr:ports"

// --- File-private state ---

@(private = "file")
g_sdl_window: ^sdl.Window

@(private = "file")
g_sdl_gl_ctx: sdl.GLContext

@(private = "file")
g_sdl_scroll_accum: [2]f32

@(private = "file")
g_sdl_should_quit: bool

// --- Constructor ---

make_sdl2_backend :: proc() -> Backend {
	return Backend{
		init             = sdl2_init,
		shutdown         = sdl2_shutdown,
		should_close     = sdl2_should_close,
		poll_events      = sdl2_poll_events,
		begin_frame      = sdl2_begin_frame,
		end_frame        = sdl2_end_frame,
		swap             = sdl2_swap,
		framebuffer_size = sdl2_framebuffer_size,
		time_now         = sdl2_time_now,
		collect_input    = sdl2_collect_input,
	}
}

// --- Lifecycle ---

@(private = "file")
sdl2_init :: proc(title: cstring, width, height: i32) -> bool {
	if sdl.Init({.VIDEO, .EVENTS}) != 0 do return false

	// OpenGL 3.3 Core attributes.
	sdl.GL_SetAttribute(.CONTEXT_MAJOR_VERSION, 3)
	sdl.GL_SetAttribute(.CONTEXT_MINOR_VERSION, 3)
	sdl.GL_SetAttribute(.CONTEXT_PROFILE_MASK, c.int(sdl.GLprofile.CORE))
	sdl.GL_SetAttribute(.CONTEXT_FLAGS, c.int(sdl.GLcontextFlag.FORWARD_COMPATIBLE_FLAG))
	sdl.GL_SetAttribute(.DOUBLEBUFFER, 1)

	g_sdl_window = sdl.CreateWindow(
		title,
		sdl.WINDOWPOS_CENTERED, sdl.WINDOWPOS_CENTERED,
		width, height,
		{.OPENGL, .RESIZABLE, .ALLOW_HIGHDPI},
	)
	if g_sdl_window == nil {
		sdl.Quit()
		return false
	}

	g_sdl_gl_ctx = sdl.GL_CreateContext(g_sdl_window)
	if g_sdl_gl_ctx == nil {
		sdl.DestroyWindow(g_sdl_window)
		sdl.Quit()
		return false
	}

	sdl.GL_MakeCurrent(g_sdl_window, g_sdl_gl_ctx)
	sdl.GL_SetSwapInterval(1) // vsync

	// OpenGL loader.
	gl.load_up_to(3, 3, sdl.gl_set_proc_address)

	// ImGui context + backends.
	imgui.CreateContext()
	io := imgui.GetIO()
	io.ini_filename = nil // disable imgui.ini
	imgui.StyleColorsDark()

	imgui.ImplSDL2_InitForOpenGL(g_sdl_window, g_sdl_gl_ctx)
	imgui.ImplOpenGL3_Init("#version 330")

	return true
}

@(private = "file")
sdl2_shutdown :: proc() {
	imgui.ImplOpenGL3_Shutdown()
	imgui.ImplSDL2_Shutdown()
	imgui.DestroyContext()
	sdl.GL_DeleteContext(g_sdl_gl_ctx)
	sdl.DestroyWindow(g_sdl_window)
	sdl.Quit()
}

// --- Frame loop ---

@(private = "file")
sdl2_should_close :: proc() -> bool {
	return g_sdl_should_quit
}

@(private = "file")
sdl2_poll_events :: proc() {
	g_sdl_scroll_accum = {0, 0}
	event: sdl.Event
	for sdl.PollEvent(&event) {
		imgui.ImplSDL2_ProcessEvent(&event)
		#partial switch event.type {
		case .QUIT:
			g_sdl_should_quit = true
		case .MOUSEWHEEL:
			g_sdl_scroll_accum.x += f32(event.wheel.x)
			g_sdl_scroll_accum.y += f32(event.wheel.y)
		}
	}
}

@(private = "file")
sdl2_begin_frame :: proc() {
	imgui.ImplOpenGL3_NewFrame()
	imgui.ImplSDL2_NewFrame()
	imgui.NewFrame()
}

@(private = "file")
sdl2_end_frame :: proc() {
	imgui.Render()
	w, h: c.int
	sdl.GL_GetDrawableSize(g_sdl_window, &w, &h)
	gl.Viewport(0, 0, w, h)
	gl.ClearColor(0.04, 0.04, 0.04, 1.0)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	imgui.ImplOpenGL3_RenderDrawData(imgui.GetDrawData())
}

@(private = "file")
sdl2_swap :: proc() {
	sdl.GL_SwapWindow(g_sdl_window)
}

// --- Queries ---

@(private = "file")
sdl2_framebuffer_size :: proc() -> (w: i32, h: i32) {
	cw, ch: c.int
	sdl.GL_GetDrawableSize(g_sdl_window, &cw, &ch)
	return i32(cw), i32(ch)
}

@(private = "file")
sdl2_time_now :: proc() -> f64 {
	return f64(sdl.GetPerformanceCounter()) / f64(sdl.GetPerformanceFrequency())
}

// --- Input ---

@(private = "file")
sdl2_collect_input :: proc() -> ports.Input_State {
	mx, my: c.int
	buttons := sdl.GetMouseState(&mx, &my)
	input: ports.Input_State

	input.mouse.pos = {f32(mx), f32(my)}
	input.mouse.buttons[.Left]   = (buttons & sdl.BUTTON_LMASK) != 0
	input.mouse.buttons[.Right]  = (buttons & sdl.BUTTON_RMASK) != 0
	input.mouse.buttons[.Middle] = (buttons & sdl.BUTTON_MMASK) != 0
	input.mouse.scroll = g_sdl_scroll_accum

	keyboard := sdl.GetKeyboardState(nil)
	if keyboard[sdl.SCANCODE_UP]     != 0 do input.keys.pressed += {.Up}
	if keyboard[sdl.SCANCODE_DOWN]   != 0 do input.keys.pressed += {.Down}
	if keyboard[sdl.SCANCODE_LEFT]   != 0 do input.keys.pressed += {.Left}
	if keyboard[sdl.SCANCODE_RIGHT]  != 0 do input.keys.pressed += {.Right}
	if keyboard[sdl.SCANCODE_RETURN] != 0 do input.keys.pressed += {.Enter}
	if keyboard[sdl.SCANCODE_ESCAPE] != 0 do input.keys.pressed += {.Escape}
	if keyboard[sdl.SCANCODE_TAB]    != 0 do input.keys.pressed += {.Tab}
	if keyboard[sdl.SCANCODE_SPACE]  != 0 do input.keys.pressed += {.Space}

	return input
}
