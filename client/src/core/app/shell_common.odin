package app

import "mr:ports"
import "mr:ui"

// S52: Shared shell primitives — canonical connection status display.
// Eliminates 5x duplicated conn_status → (label, dot_color, text_color) mapping.

Conn_Status_Display :: struct {
	label:      string,
	dot_color:  ui.Color,
	text_color: ui.Color,
}

resolve_conn_status_display :: proc(status: ports.MD_Conn_Status) -> Conn_Status_Display {
	switch status {
	case .Connected:
		return {"LIVE", ui.COL_GREEN, ui.COL_GREEN}
	case .Connecting:
		return {"CONNECTING", ui.COL_YELLOW_ACCENT, ui.COL_YELLOW_ACCENT}
	case .Reconnecting:
		return {"RECONNECTING", ui.COL_WARNING, ui.COL_WARNING}
	case .Offline:
		return {"OFFLINE", ui.with_alpha(ui.COL_WHITE, 0.35), ui.COL_TEXT_MUTED}
	}
	return {"OFFLINE", ui.with_alpha(ui.COL_WHITE, 0.35), ui.COL_TEXT_MUTED}
}

current_conn_status_display :: proc(state: ^App_State) -> Conn_Status_Display {
	return resolve_conn_status_display(current_conn_status(state))
}

// Shared modal backdrop: semi-transparent overlay at Z_MODAL.
modal_backdrop :: proc(cmd_buf: ^ui.Command_Buffer, viewport_w, viewport_h: f32, alpha: f32 = 0.75) {
	prev_z := cmd_buf.current_z_layer
	cmd_buf.current_z_layer = ui.Z_MODAL
	ui.push(cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, 0}, size = {viewport_w, viewport_h}},
		color = {0, 0, 0, alpha},
	})
	cmd_buf.current_z_layer = prev_z
}

// S52: Overlay dispatch — renders all global overlays/modals in z-order.
// Z-order (back to front): health panel, help, exchange manager,
// cell stream picker, widget catalog, stream picker, toast/OSD.
@(private = "package")
draw_shell_overlays :: proc(state: ^App_State, viewport_w, viewport_h: f32, pointer: ui.Pointer_Input) {
	// Health panel (floating, shown when telemetry HUD is active).
	if state.telemetry.hud_enabled {
		build_health_panel(state, viewport_w, viewport_h, pointer)
	}

	// Help overlay.
	if state.overlays.show_help {
		draw_help_overlay(state, viewport_w, viewport_h)
	}

	// Exchange manager.
	if state.overlays.show_exchange_manager {
		draw_exchange_manager(state, viewport_w, viewport_h, pointer)
	}

	// Cell stream picker.
	if state.overlays.cell_stream_picker_open >= 0 && state.overlays.cell_stream_picker_open < state.world.count {
		anchor_y := TOP_BAR_H + 20
		anchor_x := f32(80)
		draw_cell_stream_picker(state, {anchor_x, anchor_y}, state.overlays.cell_stream_picker_open,
			viewport_w, viewport_h, pointer)
	}

	// Widget catalog.
	if state.overlays.show_widget_catalog {
		draw_widget_catalog(state, viewport_w, viewport_h, pointer)
	}

	// Stream picker (topmost modal).
	if state.overlays.show_stream_picker {
		draw_stream_picker(state, viewport_w, viewport_h, pointer)
	}

	// Toast notification + TF OSD (always on top).
	draw_toast_osd(state, viewport_w, viewport_h)
}
