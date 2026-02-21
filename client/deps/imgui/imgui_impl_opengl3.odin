package imgui

// OpenGL3 backend bindings (4 procs).
// Links against libcimgui.a which includes imgui_impl_opengl3.cpp.

foreign import cimgui_gl3 {
	"lib/libcimgui.a",
}

@(default_calling_convention = "c")
foreign cimgui_gl3 {
	@(link_name = "cImGui_ImplOpenGL3_Init")
	ImplOpenGL3_Init :: proc(glsl_version: cstring = nil) -> bool ---

	@(link_name = "cImGui_ImplOpenGL3_Shutdown")
	ImplOpenGL3_Shutdown :: proc() ---

	@(link_name = "cImGui_ImplOpenGL3_NewFrame")
	ImplOpenGL3_NewFrame :: proc() ---

	@(link_name = "cImGui_ImplOpenGL3_RenderDrawData")
	ImplOpenGL3_RenderDrawData :: proc(draw_data: ^DrawData) ---
}
