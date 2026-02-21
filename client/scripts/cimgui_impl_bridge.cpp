// Thin C bridge for ImGui backend functions.
// cimgui wraps core ImGui but the backends use C++ linkage.
// This file provides extern "C" wrappers.

#include "imgui.h"
#include "imgui_impl_glfw.h"
#include "imgui_impl_opengl3.h"

extern "C" {

bool cImGui_ImplGlfw_InitForOpenGL(GLFWwindow* window, bool install_callbacks) {
    return ImGui_ImplGlfw_InitForOpenGL(window, install_callbacks);
}

void cImGui_ImplGlfw_Shutdown(void) {
    ImGui_ImplGlfw_Shutdown();
}

void cImGui_ImplGlfw_NewFrame(void) {
    ImGui_ImplGlfw_NewFrame();
}

bool cImGui_ImplOpenGL3_Init(const char* glsl_version) {
    return ImGui_ImplOpenGL3_Init(glsl_version);
}

void cImGui_ImplOpenGL3_Shutdown(void) {
    ImGui_ImplOpenGL3_Shutdown();
}

void cImGui_ImplOpenGL3_NewFrame(void) {
    ImGui_ImplOpenGL3_NewFrame();
}

void cImGui_ImplOpenGL3_RenderDrawData(ImDrawData* draw_data) {
    ImGui_ImplOpenGL3_RenderDrawData(draw_data);
}

} // extern "C"
