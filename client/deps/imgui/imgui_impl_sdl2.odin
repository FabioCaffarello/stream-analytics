package imgui

// SDL2 backend bindings (4 procs).
// Links against libcimgui.a which includes imgui_impl_sdl2.cpp.

import sdl "vendor:sdl2"

foreign import cimgui_sdl2 {
	"lib/libcimgui.a",
}

@(default_calling_convention = "c")
foreign cimgui_sdl2 {
	@(link_name = "cImGui_ImplSDL2_InitForOpenGL")
	ImplSDL2_InitForOpenGL :: proc(window: ^sdl.Window, gl_context: sdl.GLContext) -> bool ---

	@(link_name = "cImGui_ImplSDL2_Shutdown")
	ImplSDL2_Shutdown :: proc() ---

	@(link_name = "cImGui_ImplSDL2_NewFrame")
	ImplSDL2_NewFrame :: proc() ---

	@(link_name = "cImGui_ImplSDL2_ProcessEvent")
	ImplSDL2_ProcessEvent :: proc(event: ^sdl.Event) -> bool ---
}
