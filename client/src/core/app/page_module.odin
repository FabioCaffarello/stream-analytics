package app

import "mr:ports"
import "mr:ui"

// S57: Page Module contract — formal lifecycle for UI pages.
//
// Each route maps to a Page_Module that provides:
//   render_page:   draw the page content into the workspace area
//   render_detail: draw the detail panel content (sidebar), nil if none
//   on_enter:      called when route becomes active (cleanup, init per-page state)
//   on_leave:      called when route is being left (teardown, close page-local overlays)
//
// This replaces the inline switch statements in build_ui.odin and apply_ui_actions
// with a dispatch table, making the shell route-agnostic and enabling new pages
// without touching the shell orchestrator.

Page_Render_Proc :: #type proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input)

Page_Render_Detail_Proc :: #type proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input)

Page_Lifecycle_Proc :: #type proc(state: ^App_State)

Page_Module :: struct {
	render_page:   Page_Render_Proc,
	render_detail: Page_Render_Detail_Proc, // nil = no detail panel content for this route
	on_enter:      Page_Lifecycle_Proc,     // nil = no-op
	on_leave:      Page_Lifecycle_Proc,     // nil = no-op
}

// --- Page table: Route → Page_Module (compile-time, zero alloc) ---

PAGE_MODULES :: [Route]Page_Module{
	.Dashboard = {
		render_page   = page_dashboard_render,
		render_detail = page_dashboard_render_detail,
		on_enter      = nil,
		on_leave      = nil,
	},
	.Markets = {
		render_page   = page_markets_render,
		render_detail = page_markets_render_detail,
		on_enter      = page_explorer_enter,
		on_leave      = page_explorer_leave,
	},
	.Settings = {
		render_page   = page_settings_render,
		render_detail = page_settings_render_detail,
		on_enter      = nil,
		on_leave      = nil,
	},
	.Instrument_Overview = {
		render_page   = page_overview_render,
		render_detail = page_overview_render_detail,
		on_enter      = page_instrument_overview_enter,
		on_leave      = page_instrument_overview_leave,
	},
	.Delivery_Health = {
		render_page   = page_delivery_health_render,
		render_detail = page_delivery_health_render_detail,
		on_enter      = page_delivery_health_enter,
		on_leave      = page_delivery_health_leave,
	},
	.Portfolio = {
		render_page   = page_portfolio_render,
		render_detail = page_portfolio_render_detail,
		on_enter      = page_portfolio_enter,
		on_leave      = page_portfolio_leave,
	},
}

// --- Shell-facing dispatch procs ---

// Render the active page into the workspace area.
@(private = "package")
page_render :: proc(state: ^App_State, route: Route, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	modules := PAGE_MODULES
	mod := modules[route]
	if mod.render_page != nil {
		mod.render_page(state, workspace, pointer)
	}
}

// Render the detail panel content for the active route. Returns false if route has no detail.
@(private = "package")
page_render_detail :: proc(state: ^App_State, route: Route, rect: ui.Rect, pointer: ui.Pointer_Input) -> bool {
	modules := PAGE_MODULES
	mod := modules[route]
	if mod.render_detail != nil {
		mod.render_detail(state, rect, pointer)
		return true
	}
	return false
}

// Route lifecycle: call on_leave for old route, close overlays, call on_enter for new route.
@(private = "package")
page_navigate :: proc(state: ^App_State, from: Route, to: Route) {
	if from == to do return
	modules := PAGE_MODULES
	// Leave old page.
	if modules[from].on_leave != nil {
		modules[from].on_leave(state)
	}
	// Close all modal overlays on route change (prevents stale overlay leaks).
	close_all_overlays(state)
	// Set new route.
	state.chrome.active_route = to
	// Enter new page.
	if modules[to].on_enter != nil {
		modules[to].on_enter(state)
	}
}

// --- Page render adapters (thin wrappers to existing procs) ---

// Dashboard: delegates to existing build_dashboard_grid / build_focus_mode / build_compare_mode
// via the shell. The shell still handles the Dashboard sub-modes (focus, compare, grid) because
// those are viewport-layout modes, not separate pages. The page_render_page for Dashboard is
// therefore a pass-through that the shell calls after resolving the mode.

@(private = "file")
page_dashboard_render :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	// Dashboard rendering is handled by the shell's mode dispatch (focus/compare/grid).
	// This proc is a sentinel — the shell checks for .Dashboard explicitly to apply
	// mode-specific rendering. This exists to satisfy the page contract.
	// Actual rendering: see build_ui.odin Dashboard branch.
}

@(private = "file")
page_dashboard_render_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	draw_dashboard_detail(state, rect, pointer)
}

@(private = "file")
page_markets_render :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	build_markets_page(state, workspace, pointer)
}

@(private = "file")
page_markets_render_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	draw_markets_detail(state, rect, pointer)
}

@(private = "file")
page_settings_render :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	build_settings_page(state, workspace, pointer)
}

@(private = "file")
page_settings_render_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	draw_settings_detail(state, rect, pointer)
}
