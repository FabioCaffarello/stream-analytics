package app

import "mr:md_common"
import "mr:ports"
import "mr:streams"
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

// S53/S127: Shared composition badge — renders PEND/BFILL/LIVE/COMP as a tinted pill.
// Returns the cursor advance (width of pill + trailing gap), or 0 if empty.
@(private = "package")
draw_composition_badge :: proc(
	cmd_buf: ^ui.Command_Buffer,
	x, text_y: f32,
	composition: md_common.Composition_Stage,
	measure: proc(size: f32, text: string) -> ui.Vec2,
) -> f32 {
	comp_label: string
	comp_color: ui.Color
	// S134: Hide COMP badge in steady state — only show transitional stages.
	switch composition {
	case .Range_Pending: comp_label = "PEND";  comp_color = ui.COL_WARNING
	case .Backfilled:    comp_label = "BFILL"; comp_color = ui.COL_WARNING
	case .Live_Only:     comp_label = "LIVE";  comp_color = ui.COL_YELLOW_ACCENT
	case .Composed:      return 0  // steady state — no badge noise
	case .Empty:         return 0
	}
	label_w := measure(ui.FONT_SIZE_XS, comp_label).x
	pill_w := label_w + 6
	pill_h := f32(14)
	// S127: Tinted background pill for readability.
	pill_y := text_y - ui.FONT_SIZE_XS * 0.35 - pill_h * 0.5
	ui.push(cmd_buf, ui.Cmd_Rect_Filled{
		rect  = ui.rect_xywh(x, pill_y, pill_w, pill_h),
		color = ui.with_alpha(comp_color, 0.12),
	})
	ui.push_text(cmd_buf, {x + 3, text_y}, comp_label, comp_color, ui.FONT_SIZE_XS, .Mono)
	return pill_w + 4
}

// S53/S127: Shared health dot — renders green/yellow/red indicator with background.
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
	// S127: Background ring for contrast against dark headers.
	ring_pad := f32(2)
	ui.push(cmd_buf, ui.Cmd_Rect_Filled{
		rect  = ui.rect_xywh(x - ring_pad, dot_y - ring_pad, dot_sz + ring_pad * 2, dot_sz + ring_pad * 2),
		color = ui.with_alpha(health_color, 0.10),
	})
	ui.push(cmd_buf, ui.Cmd_Rect_Filled{
		rect = ui.rect_xywh(x, dot_y, dot_sz, dot_sz),
		color = health_color,
	})
	return dot_sz + ring_pad * 2 + 4
}

// ═══════════════════════════════════════════════════════════════
// S107/S120: Pane Visual State — unified state overlay system.
// S120 expands with Snapshot_Pending + No_History for granular UX.
// ═══════════════════════════════════════════════════════════════

Pane_Visual_State :: enum u8 {
	Active,            // normal rendering — no overlay
	Loading,           // connected, composition Range_Pending
	Seeding,           // connected, composition Live_Only or Backfilled (candle-centric)
	Snapshot_Pending,  // S120: live data flowing but widget awaits initial snapshot (OB, Stats, DOM)
	Empty,             // no stream bound or composition Empty
	No_History,        // S120: live data flowing but no historical backfill (candle widgets)
	Offline,           // connection offline
	Error,             // desync or critical health
}

// Resolve the visual state for a pane given its surface view, connection context,
// and widget kind. S136: Policy-driven via widget_data_readiness — replaces
// per-widget inline switch with unified readiness assessment.
@(private = "package")
resolve_pane_visual_state :: proc(
	sv: Cell_Surface_View,
	conn_status: ports.MD_Conn_Status,
	stream_state: streams.Stream_State,
	widget_kind: Widget_Kind = .Candle,
	stores: Cell_Stores = {},
) -> Pane_Visual_State {
	// Universal gates — these always override widget readiness.
	if conn_status == .Offline do return .Offline
	if stream_state == .Desync do return .Error
	if sv.health_level == .Critical do return .Error
	if sv.composition == .Empty && !sv.stream_bound do return .Empty

	// S136: Unified policy-driven readiness. The widget_data_readiness function
	// consults the compile-time policy table to assess readiness from store state,
	// artifact liveness, and stream composition. Composition badges (PEND/BFILL/LIVE)
	// communicate transitional state — the overlay only blocks rendering when there
	// is truly no data to show.
	readiness := widget_data_readiness(widget_kind, sv, stores)
	return readiness_to_visual_state(readiness)
}

// S114/S120: Draw an informative state overlay on a pane body for non-Active states.
// S120: Richer visuals — widget glyph, progress bar, animated dots, better messaging.
@(private = "package")
draw_pane_state_overlay :: proc(
	cmd_buf: ^ui.Command_Buffer,
	rect: ui.Rect,
	visual_state: Pane_Visual_State,
	widget_kind: Widget_Kind,
	measure: proc(size: f32, text: string) -> ui.Vec2,
	frame_seq: u64 = 0,
	tf_ms: i64 = 60_000,  // S130: TF context for bootstrap hints
) {
	if visual_state == .Active do return
	if rect.size.x <= 0 || rect.size.y <= 0 do return

	title: string
	title_color: ui.Color
	sub_label: string
	hint: string
	show_progress: bool // S120: animated progress bar for loading/seeding states

	switch visual_state {
	case .Loading:
		title = "Loading"
		title_color = ui.COL_STATE_LOADING
		sub_label = _state_sub_label_loading(widget_kind)
		bh := bootstrap_hint_for_widget(widget_kind, tf_ms)
		hint = bh.hint_label
		show_progress = true
	case .Seeding:
		title = "Seeding"
		title_color = ui.COL_STATE_SEEDING
		sub_label = _state_sub_label_seeding(widget_kind)
		bh := bootstrap_hint_for_widget(widget_kind, tf_ms)
		hint = bh.hint_label
		show_progress = true
	case .Snapshot_Pending:
		title = "Awaiting Snapshot"
		title_color = ui.COL_STATE_SEEDING
		sub_label = _state_sub_label_snapshot_pending(widget_kind)
		bh := bootstrap_hint_for_widget(widget_kind, tf_ms)
		hint = bh.hint_label
		show_progress = true
	case .Empty:
		title = "No Data"
		title_color = ui.COL_STATE_EMPTY
		sub_label = _state_sub_label_empty(widget_kind)
		hint = "Click stream badge to bind"
	case .No_History:
		title = "Live Only"
		title_color = ui.COL_YELLOW_ACCENT
		sub_label = _state_sub_label_no_history(widget_kind)
		hint = "Historical backfill not available"
	case .Offline:
		title = "Offline"
		title_color = ui.COL_STATE_OFFLINE
		sub_label = "Server connection lost"
		hint = "Reconnecting automatically..."
	case .Error:
		title = "Error"
		title_color = ui.COL_STATE_ERROR
		sub_label = "Stream desync or critical health"
		hint = "Recovery in progress"
	case .Active:
		return
	}

	// Semi-transparent backdrop.
	ui.push(cmd_buf, ui.Cmd_Rect_Filled{
		rect  = rect,
		color = ui.with_alpha(ui.COL_SURFACE_0, 0.72),
	})

	// Compact mode: small panes only get the title + optional glyph.
	is_compact := rect.size.y < 60 || rect.size.x < 120
	if is_compact {
		title_sz := measure(ui.FONT_SIZE_SM, title)
		cx := rect.pos.x + (rect.size.x - title_sz.x) * 0.5
		cy := rect.pos.y + rect.size.y * 0.5 + ui.FONT_SIZE_SM * 0.35
		ui.push_text(cmd_buf, {cx, cy}, title, title_color, ui.FONT_SIZE_SM, .Bold)
		return
	}

	// S120: Widget glyph — centered icon letter above the text group.
	glyph := _widget_state_glyph(widget_kind)
	glyph_h := f32(0)
	glyph_gap := f32(0)
	if len(glyph) > 0 && rect.size.y >= 100 {
		glyph_h = ui.FONT_SIZE_LG
		glyph_gap = 6
	}

	// Full mode: glyph + title + sub-label + hint + optional progress bar.
	line_gap := f32(4)
	title_h := ui.FONT_SIZE_SM
	sub_h := ui.FONT_SIZE_XS
	hint_h := ui.FONT_SIZE_XS
	progress_h := show_progress ? f32(6) : f32(0)
	progress_gap := show_progress ? f32(6) : f32(0)
	total_h := glyph_h + glyph_gap + title_h + line_gap + sub_h + line_gap + hint_h + progress_gap + progress_h
	group_top := rect.pos.y + (rect.size.y - total_h) * 0.5
	cursor_y := group_top

	// S120: Widget glyph (large, muted letter).
	if glyph_h > 0 {
		glyph_sz := measure(ui.FONT_SIZE_LG, glyph)
		glyph_x := rect.pos.x + (rect.size.x - glyph_sz.x) * 0.5
		glyph_color := ui.with_alpha(title_color, 0.3)
		ui.push_text(cmd_buf, {glyph_x, cursor_y + glyph_h * 0.5 + ui.FONT_SIZE_LG * 0.35},
			glyph, glyph_color, ui.FONT_SIZE_LG, .Bold)
		cursor_y += glyph_h + glyph_gap
	}

	// Title.
	title_sz := measure(ui.FONT_SIZE_SM, title)
	ui.push_text(cmd_buf,
		{rect.pos.x + (rect.size.x - title_sz.x) * 0.5, cursor_y + title_h * 0.5 + ui.FONT_SIZE_SM * 0.35},
		title, title_color, ui.FONT_SIZE_SM, .Bold)
	cursor_y += title_h + line_gap

	// Sub-label.
	if len(sub_label) > 0 {
		sub_sz := measure(ui.FONT_SIZE_XS, sub_label)
		ui.push_text(cmd_buf,
			{rect.pos.x + (rect.size.x - sub_sz.x) * 0.5, cursor_y + sub_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			sub_label, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	}
	cursor_y += sub_h + line_gap

	// Hint.
	if len(hint) > 0 {
		hint_sz := measure(ui.FONT_SIZE_XS, hint)
		ui.push_text(cmd_buf,
			{rect.pos.x + (rect.size.x - hint_sz.x) * 0.5, cursor_y + hint_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			hint, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}
	cursor_y += hint_h

	// S120: Animated progress bar — subtle moving indicator for loading/seeding states.
	if show_progress && rect.size.x >= 80 {
		cursor_y += progress_gap
		bar_w := min(rect.size.x * 0.5, 120)
		bar_x := rect.pos.x + (rect.size.x - bar_w) * 0.5
		bar_h := f32(2)
		bar_y := cursor_y

		// Track background.
		ui.push(cmd_buf, ui.Cmd_Rect_Filled{
			rect  = ui.rect_xywh(bar_x, bar_y, bar_w, bar_h),
			color = ui.with_alpha(ui.COL_WHITE, 0.06),
		})

		// Moving indicator — oscillates across the track.
		// Use frame_seq to animate: period of ~120 frames (2 seconds at 60fps).
		phase := f32(frame_seq % 120) / 120.0
		// Ping-pong: 0→1→0 over 120 frames.
		t := phase < 0.5 ? phase * 2.0 : (1.0 - phase) * 2.0
		indicator_w := bar_w * 0.3
		indicator_x := bar_x + t * (bar_w - indicator_w)
		ui.push(cmd_buf, ui.Cmd_Rect_Filled{
			rect  = ui.rect_xywh(indicator_x, bar_y, indicator_w, bar_h),
			color = ui.with_alpha(title_color, 0.5),
		})
	}
}

// S120: Widget glyph — representative letter for each widget kind.
// Shown centered above the state title in large panes.
@(private = "package")
_widget_state_glyph :: proc(wk: Widget_Kind) -> string {
	switch wk {
	case .Candle:       return "C"
	case .Stats:        return "S"
	case .Counter:      return "#"
	case .Heatmap:      return "H"
	case .VPVR:         return "V"
	case .Trades:       return "T"
	case .Orderbook:    return "B"
	case .DOM:          return "D"
	case .Analytics:    return "A"
	case .Session_VPVR: return "P"
	case .TPO:          return "P"
	case .Empty:        return ""
	}
	return ""
}

// S114/S120: Widget-specific sub-labels for Loading state.
@(private = "package")
_state_sub_label_loading :: proc(wk: Widget_Kind) -> string {
	switch wk {
	case .Candle:       return "Requesting candle history"
	case .Stats:        return "Requesting market stats"
	case .Counter:      return "Requesting trade counters"
	case .Heatmap:      return "Requesting heatmap data"
	case .VPVR:         return "Requesting volume profile"
	case .Trades:       return "Requesting recent trades"
	case .Orderbook:    return "Requesting order book"
	case .DOM:          return "Requesting depth of market"
	case .Analytics:    return "Requesting analytics range"
	case .Session_VPVR: return "Requesting session profile"
	case .TPO:          return "Requesting TPO profile"
	case .Empty:        return "No widget selected"
	}
	return "Requesting data"
}

// S114/S120: Widget-specific sub-labels for Seeding state.
@(private = "package")
_state_sub_label_seeding :: proc(wk: Widget_Kind) -> string {
	switch wk {
	case .Candle:       return "Candles arriving, building chart"
	case .Stats:        return "Stats accumulating"
	case .Counter:      return "Accumulating trade counts"
	case .Heatmap:      return "Building heatmap grid"
	case .VPVR:         return "Accumulating volume levels"
	case .Trades:       return "Trade feed starting"
	case .Orderbook:    return "Book levels populating"
	case .DOM:          return "Depth levels populating"
	case .Analytics:    return "Analytics feed starting"
	case .Session_VPVR: return "Session profile building"
	case .TPO:          return "TPO blocks accumulating"
	case .Empty:        return ""
	}
	return "Receiving initial data"
}

// S120: Widget-specific sub-labels for Snapshot_Pending state.
@(private = "package")
_state_sub_label_snapshot_pending :: proc(wk: Widget_Kind) -> string {
	switch wk {
	case .Stats:        return "Waiting for first stats snapshot"
	case .Orderbook:    return "Waiting for order book snapshot"
	case .DOM:          return "Waiting for depth snapshot"
	case .Candle:       return "Waiting for initial candle"
	case .Counter:      return "Waiting for first trade tick"
	case .Heatmap:      return "Waiting for heatmap frame"
	case .VPVR:         return "Waiting for volume snapshot"
	case .Trades:       return "Waiting for first trade"
	case .Analytics:    return "Waiting for analytics snapshot"
	case .Session_VPVR: return "Waiting for session profile"
	case .TPO:          return "Waiting for TPO snapshot"
	case .Empty:        return ""
	}
	return "Waiting for snapshot"
}

// S120: Widget-specific sub-labels for No_History state.
@(private = "package")
_state_sub_label_no_history :: proc(wk: Widget_Kind) -> string {
	switch wk {
	case .Candle:       return "Live candles only, no historical backfill"
	case .Stats:        return "Live stats only"
	case .Counter:      return "Live counts only"
	case .Heatmap:      return "Live heatmap only"
	case .VPVR:         return "Live profile only"
	case .Trades:       return "Live trades only"
	case .Orderbook:    return "Live book only"
	case .DOM:          return "Live depth only"
	case .Analytics:    return "Live analytics only"
	case .Session_VPVR: return "Live session profile only"
	case .TPO:          return "Live TPO only"
	case .Empty:        return ""
	}
	return "No historical data"
}

// S114/S120: Widget-specific sub-labels for Empty state.
@(private = "package")
_state_sub_label_empty :: proc(wk: Widget_Kind) -> string {
	switch wk {
	case .Candle:       return "Bind a market stream to see candles"
	case .Stats:        return "Bind a market stream to see stats"
	case .Counter:      return "Bind a market stream to see counters"
	case .Heatmap:      return "Bind a market stream to see heatmap"
	case .VPVR:         return "Bind a market stream to see volume"
	case .Trades:       return "Bind a market stream to see trades"
	case .Orderbook:    return "Bind a market stream to see book"
	case .DOM:          return "Bind a market stream to see depth"
	case .Analytics:    return "Bind a market stream to see analytics"
	case .Session_VPVR: return "Bind a market stream to see profile"
	case .TPO:          return "Bind a market stream to see TPO"
	case .Empty:        return "Select a widget type from the catalog"
	}
	return "No data source"
}

// S130/S136: Resolve TF-aware bootstrap hint for a widget.
// Uses the widget readiness policy table to determine the primary artifact.
// Pure function — no mutation, no allocation.
@(private = "package")
bootstrap_hint_for_widget :: proc(wk: Widget_Kind, tf_ms: i64) -> md_common.Bootstrap_Hint {
	policy := widget_readiness_policies[wk]
	return md_common.bootstrap_hint_for_artifact(policy.primary_artifact, tf_ms)
}

// S64: Shared status → color mapping. Consolidates identical helpers across pages.
@(private = "package")
status_color :: proc(status: string) -> ui.Color {
	if status == "ready" do return ui.COL_GREEN
	if status == "degraded" do return ui.COL_WARNING
	if status == "not_ready" do return ui.COL_RED
	if status == "inactive" do return ui.COL_TEXT_MUTED
	return ui.COL_TEXT_MUTED
}

@(private = "package")
freshness_color :: proc(status: string) -> ui.Color {
	if status == "flowing" do return ui.COL_GREEN
	if status == "partial" do return ui.COL_WARNING
	if status == "stale" do return ui.COL_WARNING
	if status == "inactive" do return ui.COL_TEXT_MUTED
	return ui.COL_TEXT_MUTED
}

@(private = "package")
resync_color :: proc(status: string) -> ui.Color {
	if status == "stable" do return ui.COL_GREEN
	if status == "recovering" do return ui.COL_WARNING
	if status == "degraded" do return ui.COL_RED
	return ui.COL_TEXT_MUTED
}

@(private = "package")
coverage_color :: proc(status: string) -> ui.Color {
	if status == "available" do return ui.COL_GREEN
	if status == "partial" do return ui.COL_WARNING
	if status == "empty" do return ui.COL_WARNING
	if status == "unavailable" do return ui.COL_TEXT_MUTED
	return ui.COL_TEXT_MUTED
}

// S64: Standard backdrop alpha for all modals.
MODAL_BACKDROP_ALPHA :: f32(0.75)

// S57: Zen mode fade — updates alpha values for top bar, bottom status, left nav rail.
// Pure state mutation, no rendering. Called once per frame in build_ui.
@(private = "package")
zen_update_fade :: proc(zen: ^Zen_State, mouse_x, mouse_y, viewport_h: f32) {
	if !zen.active do return
	ZEN_TRIGGER :: f32(12)     // mouse within 12px of edge → fade in
	ZEN_FADE_SPEED :: f32(0.08) // ~12 frames to full fade

	// Top bar.
	if mouse_y < ZEN_TRIGGER {
		zen.top_alpha = min(zen.top_alpha + ZEN_FADE_SPEED, 1.0)
	} else {
		zen.top_alpha = max(zen.top_alpha - ZEN_FADE_SPEED, 0.0)
	}
	// Bottom status bar.
	if mouse_y > viewport_h - ZEN_TRIGGER {
		zen.bottom_alpha = min(zen.bottom_alpha + ZEN_FADE_SPEED, 1.0)
	} else {
		zen.bottom_alpha = max(zen.bottom_alpha - ZEN_FADE_SPEED, 0.0)
	}
	// Left nav rail.
	if mouse_x < ZEN_TRIGGER {
		zen.left_alpha = min(zen.left_alpha + ZEN_FADE_SPEED, 1.0)
	} else {
		zen.left_alpha = max(zen.left_alpha - ZEN_FADE_SPEED, 0.0)
	}
}

// S57: Detail panel resize handle — interactive drag handle on the right edge.
@(private = "package")
update_detail_resize :: proc(state: ^App_State, detail_rect: ui.Rect, nav_rail_x: f32, pointer: ui.Pointer_Input) {
	dr := detail_rect
	handle_rect := ui.Rect{
		pos  = {ui.rect_right(dr) - ui.RESIZE_HANDLE_W, dr.pos.y},
		size = {ui.RESIZE_HANDLE_W, dr.size.y},
	}
	handle_hovered := ui.rect_contains(handle_rect, pointer.pos)
	if handle_hovered || state.chrome.detail_resizing {
		// BUG-19: Push at Z_OVERLAY so the handle is always clickable above cell content.
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
				pointer.pos.x - nav_rail_x - ui.NAV_RAIL_W,
				ui.DETAIL_PANEL_W_MIN, ui.DETAIL_PANEL_W_MAX,
			)
		} else {
			state.chrome.detail_resizing = false
		}
	}
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
