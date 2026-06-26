package app

import "mr:ports"
import "mr:ui"

// S57/S113: Shell orchestrator — delegates to page modules, chrome procs, and overlays.
// S113: Three context levels:
//   1. Top bar (global app context): logo, connection, quick actions
//   2. Workspace toolbar (workspace context): instrument, TF, presets, indicators
//   3. Context stack (active pane context): tabbed Stats/Trades/OB/Counter/Instrument

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

	// --- Top bar (global context) ---
	zen_skip_chrome := state.zen.active && state.zen.top_alpha <= 0
	zen_compact := state.zen.active && !zen_skip_chrome
	if !zen_skip_chrome {
		draw_top_bar(state, input, viewport_w, zen_compact)
	}

	pad := f32(2)
	if viewport_w < 420 do pad = 1
	gap := f32(2)

	// --- S113: Workspace toolbar (workspace context, Dashboard only) ---
	is_dashboard := state.chrome.active_route == .Dashboard
	toolbar_y := TOP_BAR_H + 1
	show_toolbar := is_dashboard && !state.zen.active
	if show_toolbar {
		draw_workspace_toolbar(state, input, toolbar_y, viewport_w)
	}

	// --- Workspace rect (between chrome bars) ---
	workspace: ui.Rect
	if state.zen.active {
		workspace = ui.rect_xywh(pad, 1, viewport_w - pad * 2, viewport_h - 2)
	} else {
		ws_top := show_toolbar ? (toolbar_y + WORKSPACE_TOOLBAR_H) : (TOP_BAR_H + 1)
		workspace = ui.rect_xywh(
			pad, ws_top,
			viewport_w - pad * 2,
			viewport_h - ws_top - SHELL_STATUS_BAR_H,
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
			{icon = "A", label = "Analytics"},
			{icon = "G", label = "Settings"},
			{icon = "H", label = "Health"},
		}
		nav_items := NAV_ITEMS
		nav_routes := [5]Route{.Dashboard, .Markets, .Portfolio, .Settings, .Delivery_Health}
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

	// --- S113: Context stack (right panel, Dashboard only) ---
	context_stack_rect: ui.Rect
	if is_dashboard && state.chrome.context_stack.expanded && !state.zen.active && !mobile {
		cs_w := clamp(state.chrome.context_stack.width, CONTEXT_STACK_W_MIN, CONTEXT_STACK_W_MAX)
		if cs_w > workspace.size.x * 0.4 {
			cs_w = workspace.size.x * 0.4 // Never consume more than 40% of workspace
		}
		context_stack_rect = ui.rect_cut_right(&workspace, cs_w)
		draw_context_stack(state, context_stack_rect, pointer)
		update_context_stack_resize(state, context_stack_rect, pointer)
	}

	// --- Page content dispatch ---
	if is_dashboard {
		if state.focus_mode {
			build_focus_mode(state, workspace_input, workspace, workspace_pointer)
		} else if state.compare.active && state.compare.count >= 2 {
			build_compare_mode(state, workspace_input, workspace, workspace_pointer, gap)
		} else {
			// S106: Workspace tree runtime replaces grid layout.
			build_workspace_dashboard(state, workspace, pointer, workspace_input, workspace_pointer,
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
