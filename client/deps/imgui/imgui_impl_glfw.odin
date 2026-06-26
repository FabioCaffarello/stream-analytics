package imgui

// GLFW backend bindings (3 procs).
// Links against libcimgui.a which includes imgui_impl_glfw.cpp.

import "vendor:glfw"

foreign import cimgui_glfw {
	"lib/libcimgui.a",
}

@(default_calling_convention = "c")
foreign cimgui_glfw {
	@(link_name = "cImGui_ImplGlfw_InitForOpenGL")
	ImplGlfw_InitForOpenGL :: proc(window: glfw.WindowHandle, install_callbacks: bool) -> bool ---

	@(link_name = "cImGui_ImplGlfw_Shutdown")
	ImplGlfw_Shutdown :: proc() ---

	@(link_name = "cImGui_ImplGlfw_NewFrame")
	ImplGlfw_NewFrame :: proc() ---
}
