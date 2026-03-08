package app

import "mr:ports"
import "mr:ui"

// S57: Shell orchestrator — delegates to page modules, chrome procs, and overlays.
// This file should remain thin (~100 lines). All page/overlay/chrome logic lives elsewhere.

build_ui :: proc(state: ^App_State, input: ports.Input_State) -> ^ui.Command_Buffer {
	ui.reset(&state.cmd_buf)
	state.telemetry.last_indicator_probe = {}

	ui.push(&state.cmd_buf, ui.Cmd_Clear{color = ui.COL_SURFACE_0})

	viewport_w := input.viewport_size.x
	viewport_h := input.viewport_size.y
	if viewport_w <= 0 do viewport_w = 800
	if viewport_h <= 0 do viewport_h = 600

	// --- Zen mode fade (pure state mutation) ---
	zen_update_fade(&state.zen, input.mouse.pos.x, input.mouse.pos.y, viewport_h)

	// --- Top bar ---
	zen_skip_chrome := state.zen.active && state.zen.top_alpha <= 0
	zen_compact := state.zen.active && !zen_skip_chrome
	if !zen_skip_chrome {
		draw_top_bar(state, input, viewport_w, zen_compact)
	}

	pad := f32(2)
	if viewport_w < 420 do pad = 1
	gap := f32(2)

	// --- Workspace rect (between top bar and status bar) ---
	workspace: ui.Rect
	if state.zen.active {
		workspace = ui.rect_xywh(pad, 1, viewport_w - pad * 2, viewport_h - 2)
	} else {
		workspace = ui.rect_xywh(
			pad, TOP_BAR_H + 1,
			viewport_w - pad * 2,
			viewport_h - (TOP_BAR_H + 1) - SHELL_STATUS_BAR_H,
		)
	}
	if workspace.size.x < 1 do workspace.size.x = 1
	if workspace.size.y < 1 do workspace.size.y = 1

	mobile := viewport_w < 700

	// --- Pointer input ---
	pointer := ui.Pointer_Input{
		pos           = input.mouse.pos,
		left_down     = input.mouse.buttons[.Left],
		left_pressed  = input.mouse.pressed[.Left],
		left_released = input.mouse.released[.Left],
	}
	workspace_input := input
	workspace_pointer := pointer
	// Zen mode: prevent click-through from compact top bar into workspace.
	if state.zen.active && state.zen.top_alpha > 0 && pointer.pos.y <= TOP_BAR_H_COMPACT {
		workspace_input.mouse.buttons[.Left] = false
		workspace_input.mouse.pressed[.Left] = false
		workspace_input.mouse.released[.Left] = false
		workspace_pointer.left_down = false
		workspace_pointer.left_pressed = false
		workspace_pointer.left_released = false
	}

	// --- Sidebar: nav rail + detail panel ---
	sidebar_layout := ui.compute_sidebar_layout(workspace, state.chrome.detail_expanded, mobile, state.chrome.detail_w)

	zen_skip_sidebar := state.zen.active && state.zen.left_alpha <= 0
	if !mobile && !zen_skip_sidebar {
		// Nav rail (route selector).
		NAV_ITEMS :: [5]ui.Nav_Rail_Item{
			{icon = "D", label = "Dashboard"},
			{icon = "V", label = "Venues"},
			{icon = "P", label = "Portfolio"},
			{icon = "G", label = "Settings"},
			{icon = "H", label = "Health"},
		}
		// Nav rail index → Route mapping (skips Instrument_Overview which has no nav entry).
		nav_items := NAV_ITEMS
		nav_routes := [5]Route{.Dashboard, .Markets, .Portfolio, .Settings, .Session_Health}
		active_idx := -1
		for ni in 0 ..< len(nav_routes) {
			if nav_routes[ni] == state.chrome.active_route {
				active_idx = ni
				break
			}
		}
		nav_res := ui.draw_nav_rail(&state.cmd_buf, sidebar_layout.nav_rail_rect,
			nav_items[:], active_idx, pointer, state.text.measure)
		if nav_res.clicked_route_idx >= 0 && nav_res.clicked_route_idx < len(NAV_ITEMS) {
			new_route := nav_routes[nav_res.clicked_route_idx]
			if new_route != state.chrome.active_route {
				queue_ui_action(state, UI_Action{kind = .Navigate_Route, route = new_route})
			}
		}

		// Detail panel (route-specific content via page module).
		if state.chrome.detail_expanded && sidebar_layout.detail_rect.size.x > 0 {
			detail_res := ui.draw_detail_panel_frame(&state.cmd_buf, sidebar_layout.detail_rect)
			page_render_detail(state, state.chrome.active_route, detail_res.content_rect, pointer)
			update_detail_resize(state, sidebar_layout.detail_rect, sidebar_layout.nav_rail_rect.pos.x, pointer)
		}
	}

	if !state.zen.active {
		workspace = sidebar_layout.workspace_rect
	}

	// --- Page content dispatch ---
	// Dashboard has sub-modes (focus, compare, grid) that are viewport-layout concerns.
	// All other pages delegate to their Page_Module.render_page.
	if state.chrome.active_route == .Dashboard {
		if state.focus_mode {
			build_focus_mode(state, workspace_input, workspace, workspace_pointer)
		} else if state.compare.active && state.compare.count >= 2 {
			build_compare_mode(state, workspace_input, workspace, workspace_pointer, gap)
		} else {
			build_dashboard_grid(state, workspace, pointer, workspace_input, workspace_pointer,
				gap, viewport_w, viewport_h, mobile)
		}
	} else {
		page_render(state, state.chrome.active_route, workspace, pointer)
	}

	// --- Status bar ---
	zen_skip_status := state.zen.active && state.zen.bottom_alpha <= 0
	if !zen_skip_status {
		draw_status_bar(state, viewport_w, viewport_h, pointer)
	}

	// --- Overlays, modals, toast ---
	draw_shell_overlays(state, viewport_w, viewport_h, pointer)

	return &state.cmd_buf
}
