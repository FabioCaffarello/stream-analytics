package app

import "mr:md_common"
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

// S53: Shared composition badge — renders PEND/BFILL/LIVE/COMP label.
// Returns the cursor advance (width of label + trailing gap), or 0 if empty.
@(private = "package")
draw_composition_badge :: proc(
	cmd_buf: ^ui.Command_Buffer,
	x, text_y: f32,
	composition: md_common.Composition_Stage,
	measure: proc(size: f32, text: string) -> ui.Vec2,
) -> f32 {
	comp_label: string
	comp_color: ui.Color
	switch composition {
	case .Range_Pending: comp_label = "PEND";  comp_color = ui.COL_WARNING
	case .Backfilled:    comp_label = "BFILL"; comp_color = ui.COL_WARNING
	case .Live_Only:     comp_label = "LIVE";  comp_color = ui.COL_YELLOW_ACCENT
	case .Composed:      comp_label = "COMP";  comp_color = ui.COL_GREEN
	case .Empty:         return 0
	}
	ui.push_text(cmd_buf, {x, text_y}, comp_label, comp_color, ui.FONT_SIZE_XS, .Mono)
	return measure(ui.FONT_SIZE_XS, comp_label).x + 4
}

// S53: Shared health dot — renders green/yellow/red square indicator.
// Returns the cursor advance (dot_size + trailing gap), or 0 if not shown.
@(private = "package")
draw_health_dot :: proc(
	cmd_buf: ^ui.Command_Buffer,
	x, center_y, dot_sz: f32,
	health_level: md_common.System_Health_Level,
	has_live_data: bool,
	composition: md_common.Composition_Stage,
) -> f32 {
	if !has_live_data && composition == .Empty do return 0
	health_color := ui.COL_GREEN
	switch health_level {
	case .Degraded:  health_color = ui.COL_WARNING
	case .Unhealthy: health_color = ui.COL_RED
	case .Critical:  health_color = ui.COL_RED
	case .Healthy:
	}
	dot_y := center_y - dot_sz * 0.5
	ui.push(cmd_buf, ui.Cmd_Rect_Filled{
		rect = ui.rect_xywh(x, dot_y, dot_sz, dot_sz),
		color = health_color,
	})
	return dot_sz + 4
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
