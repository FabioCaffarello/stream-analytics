// Thin C bridge for ImGui SDL2 backend functions.
// Mirrors cimgui_impl_bridge.cpp pattern for GLFW.
// Provides extern "C" wrappers for imgui_impl_sdl2.h C++ symbols.

#include "imgui.h"
#include "imgui_impl_sdl2.h"

struct SDL_Window;
union SDL_Event;
typedef void* SDL_GLContext;

extern "C" {

bool cImGui_ImplSDL2_InitForOpenGL(SDL_Window* window, SDL_GLContext gl_context) {
    return ImGui_ImplSDL2_InitForOpenGL(window, gl_context);
}

void cImGui_ImplSDL2_Shutdown(void) {
    ImGui_ImplSDL2_Shutdown();
}

void cImGui_ImplSDL2_NewFrame(void) {
    ImGui_ImplSDL2_NewFrame();
}

bool cImGui_ImplSDL2_ProcessEvent(const SDL_Event* event) {
    return ImGui_ImplSDL2_ProcessEvent(event);
}

} // extern "C"
