package app

import "mr:ports"
import "mr:ui"

build_ui :: proc(state: ^App_State, input: ports.Input_State) -> ^ui.Command_Buffer {
	ui.reset(&state.cmd_buf)
	state.telemetry.last_indicator_probe = {}

	ui.push(&state.cmd_buf, ui.Cmd_Clear{color = ui.COL_SURFACE_0})

	viewport_w := input.viewport_size.x
	viewport_h := input.viewport_size.y
	if viewport_w <= 0 do viewport_w = 800
	if viewport_h <= 0 do viewport_h = 600

	// --- Zen mode fade logic (PRD-0007 M4) ---
	ZEN_TRIGGER_TOP :: f32(12)     // mouse within 12px of top edge → fade in
	ZEN_TRIGGER_BOTTOM :: f32(12)
	ZEN_TRIGGER_LEFT :: f32(12)
	ZEN_FADE_SPEED :: f32(0.08)    // ~12 frames to full fade
	if state.zen.active {
		// Fade top bar.
		if input.mouse.pos.y < ZEN_TRIGGER_TOP {
			state.zen.top_alpha = min(state.zen.top_alpha + ZEN_FADE_SPEED, 1.0)
		} else {
			state.zen.top_alpha = max(state.zen.top_alpha - ZEN_FADE_SPEED, 0.0)
		}
		// Fade bottom status bar.
		if input.mouse.pos.y > viewport_h - ZEN_TRIGGER_BOTTOM {
			state.zen.bottom_alpha = min(state.zen.bottom_alpha + ZEN_FADE_SPEED, 1.0)
		} else {
			state.zen.bottom_alpha = max(state.zen.bottom_alpha - ZEN_FADE_SPEED, 0.0)
		}
		// Fade left nav rail.
		if input.mouse.pos.x < ZEN_TRIGGER_LEFT {
			state.zen.left_alpha = min(state.zen.left_alpha + ZEN_FADE_SPEED, 1.0)
		} else {
			state.zen.left_alpha = max(state.zen.left_alpha - ZEN_FADE_SPEED, 0.0)
		}
	}

	// --- Top bar: title + connection status ---
	zen_skip_chrome := state.zen.active && state.zen.top_alpha <= 0
	zen_compact := state.zen.active && !zen_skip_chrome
	if !zen_skip_chrome {
		draw_top_bar(state, input, viewport_w, zen_compact)
	}

	pad := f32(2)
	if viewport_w < 420 do pad = 1
	gap := f32(2)

	top_bar_h := TOP_BAR_H
	workspace: ui.Rect
	if state.zen.active {
		// Full viewport in zen mode (no top/bottom/pad margins).
		workspace = ui.rect_xywh(pad, 1, viewport_w - pad * 2, viewport_h - 2)
	} else {
		workspace = ui.rect_xywh(
			pad, top_bar_h + 1,
			viewport_w - pad * 2,
			viewport_h - (top_bar_h + 1) - SHELL_STATUS_BAR_H,
		)
	}
	if workspace.size.x < 1 do workspace.size.x = 1
	if workspace.size.y < 1 do workspace.size.y = 1

	mobile := viewport_w < 700

	// Shared pointer input for widget controls.
	pointer := ui.Pointer_Input{
		pos           = input.mouse.pos,
		left_down     = input.mouse.buttons[.Left],
		left_pressed  = input.mouse.pressed[.Left],
		left_released = input.mouse.released[.Left],
	}
	workspace_input := input
	workspace_pointer := pointer
	// In zen mode, top bar is rendered as a compact overlay. Prevent click-through
	// from top controls into the underlying workspace when the bar is visible.
	if state.zen.active && state.zen.top_alpha > 0 && pointer.pos.y <= TOP_BAR_H_COMPACT {
		workspace_input.mouse.buttons[.Left] = false
		workspace_input.mouse.pressed[.Left] = false
		workspace_input.mouse.released[.Left] = false
		workspace_pointer.left_down = false
		workspace_pointer.left_pressed = false
		workspace_pointer.left_released = false
	}

	// --- Two-zone sidebar: nav rail + detail panel ---
	sidebar_layout := ui.compute_sidebar_layout(workspace, state.chrome.detail_expanded, mobile, state.chrome.detail_w)

	// Nav rail (always visible on desktop, hidden in zen mode unless mouse near left).
	zen_skip_sidebar := state.zen.active && state.zen.left_alpha <= 0
	if !mobile && !zen_skip_sidebar {
		NAV_ITEMS :: [3]ui.Nav_Rail_Item{
			{icon = "D", label = "Dashboard"},
			{icon = "V", label = "Venues"},
			{icon = "G", label = "Settings"},
		}
		nav_items := NAV_ITEMS
		active_idx := int(state.chrome.active_route)
		nav_res := ui.draw_nav_rail(&state.cmd_buf, sidebar_layout.nav_rail_rect,
			nav_items[:], active_idx, pointer, state.text.measure)
		if nav_res.clicked_route_idx >= 0 && nav_res.clicked_route_idx < len(NAV_ITEMS) {
			new_route := Route(nav_res.clicked_route_idx)
			if new_route != state.chrome.active_route {
				queue_ui_action(state, UI_Action{kind = .Navigate_Route, route = new_route})
			}
		}

		// Detail panel (collapsible, route-specific content).
		if state.chrome.detail_expanded && sidebar_layout.detail_rect.size.x > 0 {
			detail_res := ui.draw_detail_panel_frame(&state.cmd_buf, sidebar_layout.detail_rect)
			switch state.chrome.active_route {
			case .Dashboard:
				draw_dashboard_detail(state, detail_res.content_rect, pointer)
			case .Markets:
				draw_markets_detail(state, detail_res.content_rect, pointer)
			case .Settings:
				draw_settings_detail(state, detail_res.content_rect)
			}

			// Resize handle on right edge of detail panel.
			// BUG-19: Push at Z_OVERLAY so the handle is always clickable above cell content.
			dr := sidebar_layout.detail_rect
			handle_rect := ui.Rect{
				pos  = {ui.rect_right(dr) - ui.RESIZE_HANDLE_W, dr.pos.y},
				size = {ui.RESIZE_HANDLE_W, dr.size.y},
			}
			handle_hovered := ui.rect_contains(handle_rect, pointer.pos)
			if handle_hovered || state.chrome.detail_resizing {
				prev_z := state.cmd_buf.current_z_layer
				state.cmd_buf.current_z_layer = ui.Z_OVERLAY
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect  = handle_rect,
					color = ui.with_alpha(ui.COL_BLUE, 0.25),
				})
				state.cmd_buf.current_z_layer = prev_z
			}
			if handle_hovered && pointer.left_pressed {
				state.chrome.detail_resizing = true
			}
			if state.chrome.detail_resizing {
				if pointer.left_down {
					state.chrome.detail_w = clamp(
						pointer.pos.x - sidebar_layout.nav_rail_rect.pos.x - ui.NAV_RAIL_W,
						ui.DETAIL_PANEL_W_MIN, ui.DETAIL_PANEL_W_MAX,
					)
				} else {
					state.chrome.detail_resizing = false
				}
			}
		}
	}

	if !state.zen.active {
		workspace = sidebar_layout.workspace_rect
	}

	switch state.chrome.active_route {
	case .Dashboard:
		if state.focus_mode {
			build_focus_mode(state, workspace_input, workspace, workspace_pointer)
		} else if state.compare.active && state.compare.count >= 2 {
			build_compare_mode(state, workspace_input, workspace, workspace_pointer, gap)
		} else {
			build_dashboard_grid(state, workspace, pointer, workspace_input, workspace_pointer,
				gap, viewport_w, viewport_h, mobile)
		}

	case .Markets:
		build_markets_page(state, workspace, pointer)

	case .Settings:
		build_settings_page(state, workspace, pointer)
	}

	// --- Status bar (bottom) — hidden in zen mode unless mouse near bottom ---
	zen_skip_status := state.zen.active && state.zen.bottom_alpha <= 0
	if !zen_skip_status {
		draw_status_bar(state, viewport_w, viewport_h, pointer)
	}

	// --- Health panel (floating overlay, shown when telemetry HUD is active) ---
	if state.telemetry.hud_enabled {
		build_health_panel(state, viewport_w, viewport_h, pointer)
	}

	// --- Help overlay (rendered LAST, on top of everything) ---
	if state.overlays.show_help {
		draw_help_overlay(state, viewport_w, viewport_h)
	}

	// --- Exchange manager (on top of help overlay) ---
	if state.overlays.show_exchange_manager {
		draw_exchange_manager(state, viewport_w, viewport_h, pointer)
	}

	// --- Cell stream picker (on top of exchange manager) ---
	if state.overlays.cell_stream_picker_open >= 0 && state.overlays.cell_stream_picker_open < state.world.count {
		// Anchor below the cell header badge (approximate position).
		anchor_y := TOP_BAR_H + 20
		anchor_x := f32(80)
		draw_cell_stream_picker(state, {anchor_x, anchor_y}, state.overlays.cell_stream_picker_open,
			viewport_w, viewport_h, pointer)
	}

	// --- Widget catalog (on top of cell stream picker) ---
	if state.overlays.show_widget_catalog {
		draw_widget_catalog(state, viewport_w, viewport_h, pointer)
	}

	// --- Stream picker (on top of everything) ---
	if state.overlays.show_stream_picker {
		draw_stream_picker(state, viewport_w, viewport_h, pointer)
	}

	// --- Toast notification + TF OSD ---
	draw_toast_osd(state, viewport_w, viewport_h)

	return &state.cmd_buf
}
