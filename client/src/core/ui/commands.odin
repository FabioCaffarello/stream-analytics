package ui

// Render Command List (RCL) — the portability core.
//
// The core never draws directly; it emits commands into a CommandBuffer.
// Platform renderers consume the buffer:
//   native: renderer_imgui.odin  -> ImGui drawlist
//   web:    renderer_canvas2d.odin -> Canvas2D (via odin-wasm)
//
// Minimal command set (Fase 1):
//   Clear, RectFilled, Line, Text, ClipPush, ClipPop
