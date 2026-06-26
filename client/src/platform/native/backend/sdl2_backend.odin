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

@(private = "file")
g_sdl_prev_mouse_pos: [2]f32

@(private = "file")
g_sdl_prev_mouse_buttons: [ports.Mouse_Button]bool

@(private = "file")
g_sdl_prev_keys_down: bit_set[ports.Key]

@(private = "file")
g_sdl_prev_input_time_s: f64

@(private = "file")
g_sdl_has_prev_mouse_pos: bool

@(private = "file")
g_sdl_has_prev_input_time: bool

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
mark_sdl_key_edges :: proc(input: ^ports.Input_State, curr, prev: bit_set[ports.Key], key: ports.Key) {
	down := key in curr
	was_down := key in prev
	if down && !was_down do input.keys.just_pressed += {key}
	if !down && was_down do input.keys.just_released += {key}
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
	fbw, fbh := sdl2_framebuffer_size()
	input: ports.Input_State

	input.mouse.pos = {f32(mx), f32(my)}
	if g_sdl_has_prev_mouse_pos {
		input.mouse.delta = {input.mouse.pos.x - g_sdl_prev_mouse_pos.x, input.mouse.pos.y - g_sdl_prev_mouse_pos.y}
	}
	g_sdl_prev_mouse_pos = input.mouse.pos
	g_sdl_has_prev_mouse_pos = true

	input.viewport_size = {f32(fbw), f32(fbh)}
	left_down := (buttons & sdl.BUTTON_LMASK) != 0
	right_down := (buttons & sdl.BUTTON_RMASK) != 0
	middle_down := (buttons & sdl.BUTTON_MMASK) != 0
	input.mouse.buttons[.Left] = left_down
	input.mouse.buttons[.Right] = right_down
	input.mouse.buttons[.Middle] = middle_down
	input.mouse.pressed[.Left] = left_down && !g_sdl_prev_mouse_buttons[.Left]
	input.mouse.pressed[.Right] = right_down && !g_sdl_prev_mouse_buttons[.Right]
	input.mouse.pressed[.Middle] = middle_down && !g_sdl_prev_mouse_buttons[.Middle]
	input.mouse.released[.Left] = !left_down && g_sdl_prev_mouse_buttons[.Left]
	input.mouse.released[.Right] = !right_down && g_sdl_prev_mouse_buttons[.Right]
	input.mouse.released[.Middle] = !middle_down && g_sdl_prev_mouse_buttons[.Middle]
	g_sdl_prev_mouse_buttons[.Left] = left_down
	g_sdl_prev_mouse_buttons[.Right] = right_down
	g_sdl_prev_mouse_buttons[.Middle] = middle_down

	input.mouse.scroll = g_sdl_scroll_accum

	keyboard := sdl.GetKeyboardState(nil)
	curr_keys: bit_set[ports.Key]
	if keyboard[sdl.SCANCODE_UP] != 0 do curr_keys += {.Up}
	if keyboard[sdl.SCANCODE_DOWN] != 0 do curr_keys += {.Down}
	if keyboard[sdl.SCANCODE_LEFT] != 0 do curr_keys += {.Left}
	if keyboard[sdl.SCANCODE_RIGHT] != 0 do curr_keys += {.Right}
	if keyboard[sdl.SCANCODE_RETURN] != 0 do curr_keys += {.Enter}
	if keyboard[sdl.SCANCODE_ESCAPE] != 0 do curr_keys += {.Escape}
	if keyboard[sdl.SCANCODE_TAB] != 0 do curr_keys += {.Tab}
	if keyboard[sdl.SCANCODE_SPACE] != 0 do curr_keys += {.Space}
	if keyboard[sdl.SCANCODE_1] != 0 do curr_keys += {.Num_1}
	if keyboard[sdl.SCANCODE_2] != 0 do curr_keys += {.Num_2}
	if keyboard[sdl.SCANCODE_3] != 0 do curr_keys += {.Num_3}
	if keyboard[sdl.SCANCODE_4] != 0 do curr_keys += {.Num_4}
	if keyboard[sdl.SCANCODE_5] != 0 do curr_keys += {.Num_5}
	if keyboard[sdl.SCANCODE_6] != 0 do curr_keys += {.Num_6}
	if keyboard[sdl.SCANCODE_7] != 0 do curr_keys += {.Num_7}
	if keyboard[sdl.SCANCODE_8] != 0 do curr_keys += {.Num_8}
	if keyboard[sdl.SCANCODE_9] != 0 do curr_keys += {.Num_9}
	if keyboard[sdl.SCANCODE_S] != 0 do curr_keys += {.S}
	if keyboard[sdl.SCANCODE_SLASH] != 0 do curr_keys += {.Slash}
	if keyboard[sdl.SCANCODE_C] != 0 do curr_keys += {.C}
	if keyboard[sdl.SCANCODE_G] != 0 do curr_keys += {.G}
	if keyboard[sdl.SCANCODE_F] != 0 do curr_keys += {.F}
	if keyboard[sdl.SCANCODE_M] != 0 do curr_keys += {.M}
	if keyboard[sdl.SCANCODE_B] != 0 do curr_keys += {.B}
	if keyboard[sdl.SCANCODE_V] != 0 do curr_keys += {.V}
	if keyboard[sdl.SCANCODE_R] != 0 do curr_keys += {.R}
	if keyboard[sdl.SCANCODE_I] != 0 do curr_keys += {.I}
	if keyboard[sdl.SCANCODE_H] != 0 do curr_keys += {.H}
	if keyboard[sdl.SCANCODE_J] != 0 do curr_keys += {.J}
	if keyboard[sdl.SCANCODE_K] != 0 do curr_keys += {.K}
	if keyboard[sdl.SCANCODE_Z] != 0 do curr_keys += {.Z}
	if keyboard[sdl.SCANCODE_D] != 0 do curr_keys += {.D}
	if keyboard[sdl.SCANCODE_DELETE] != 0 || keyboard[sdl.SCANCODE_BACKSPACE] != 0 do curr_keys += {.Delete}
	input.keys.pressed = curr_keys
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Up)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Down)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Left)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Right)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Enter)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Escape)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Tab)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Space)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Num_1)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Num_2)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Num_3)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Num_4)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Num_5)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Num_6)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Num_7)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Num_8)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Num_9)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .S)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Slash)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .C)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .G)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .F)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .M)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .B)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .V)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .R)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .I)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .H)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .J)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .K)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Z)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .D)
	mark_sdl_key_edges(&input, curr_keys, g_sdl_prev_keys_down, .Delete)
	g_sdl_prev_keys_down = curr_keys

	mod_state := sdl.GetModState()
	input.modifiers.shift = (mod_state & (sdl.KMOD_LSHIFT | sdl.KMOD_RSHIFT)) != sdl.KMOD_NONE
	input.modifiers.ctrl = (mod_state & (sdl.KMOD_LCTRL | sdl.KMOD_RCTRL)) != sdl.KMOD_NONE
	input.modifiers.alt = (mod_state & (sdl.KMOD_LALT | sdl.KMOD_RALT)) != sdl.KMOD_NONE
	input.modifiers.super = (mod_state & (sdl.KMOD_LGUI | sdl.KMOD_RGUI)) != sdl.KMOD_NONE

	now_s := sdl2_time_now()
	if g_sdl_has_prev_input_time {
		dt := now_s - g_sdl_prev_input_time_s
		if dt > 0 do input.delta_time = f32(dt)
	}
	g_sdl_prev_input_time_s = now_s
	g_sdl_has_prev_input_time = true

	return input
}
