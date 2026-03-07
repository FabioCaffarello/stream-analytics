package app

import "core:fmt"
import "mr:layers"
import "mr:ports"
import "mr:services"
import "mr:ui"

// Dashboard detail panel: structured sections with collapsible groups.
@(private = "package")
draw_dashboard_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	y := rect.pos.y

	// ===================================================
	// Section 1: Market Info (always visible)
	// ===================================================
	reg := state.stream_views
	active_name := "---"
	if reg != nil && reg.has_active {
		if slot := stream_view_active_slot(reg); slot != nil {
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			if slot.has_stream_info {
				info := slot.stream_info
				if len(info.venue) > 0 && len(info.symbol) > 0 {
					name_buf: [64]u8
					active_name = fmt.bprintf(name_buf[:], "%s:%s", info.venue, info.symbol)
				}
			}
		}
	}

	// Venue:Symbol (bold).
	ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 14},
		active_name, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_SM, .Bold)
	y += 20

	// Connection dot + stream count.
	conn_disp := current_conn_status_display(state)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {rect.pos.x + 4, y + 4}, size = {6, 6}},
		color = conn_disp.dot_color,
	})
	ui.push_text(&state.cmd_buf, {rect.pos.x + 14, y + 10},
		conn_disp.label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	y += 16

	// TF label.
	tf_opts := TF_OPTIONS
	tf_buf: [16]u8
	tf_str := fmt.bprintf(tf_buf[:], "TF: %s", tf_opts[state.active_tf_idx])
	ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10},
		tf_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	y += 20

	// ===================================================
	// Section 2: Streams (collapsible, with price + click-to-switch)
	// ===================================================
	remaining_h := (rect.pos.y + rect.size.y) - y
	stream_hdr_buf: [24]u8
	stream_count := 0
	if reg != nil { stream_count = reg.count }
	stream_hdr_label := fmt.bprintf(stream_hdr_buf[:], "STREAMS (%d)", stream_count)
	stream_sec := ui.collapsible_section(&state.cmd_buf,
		ui.Rect{pos = {rect.pos.x, y}, size = {rect.size.x, remaining_h}},
		stream_hdr_label, &state.chrome.section_streams, pointer, state.text.measure)
	y += 22 // header height
	if state.chrome.section_streams.expanded && reg != nil && reg.count > 0 {
		item_h := f32(24)
		for i in 0 ..< STREAM_VIEW_CAP {
			if !reg.slots[i].used do continue
			slot := &reg.slots[i]
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			is_active := reg.has_active && slot.subject_id == reg.active_subject_id
			label := "---"
			if slot.has_stream_info && len(slot.stream_info.venue) > 0 {
				sl_buf: [64]u8
				label = fmt.bprintf(sl_buf[:], "%s:%s", slot.stream_info.venue, slot.stream_info.symbol)
			}

			item_rect := ui.Rect{pos = {rect.pos.x + 4, y}, size = {rect.size.x - 8, item_h}}
			item_hovered := ui.rect_contains(item_rect, pointer.pos)
			if is_active {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_BLUE, 0.15)})
			} else if item_hovered {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05)})
			}

			// Click to switch active stream.
			if item_hovered && pointer.left_pressed && !is_active {
				queue_ui_action(state, UI_Action{kind = .Pick_Stream, subject_id = slot.subject_id})
			}

			// Activity dot: green if seen within last 120 frames (~2s), dim otherwise.
			fresh := state.frame > 0 && slot.last_seen_frame > 0 && (state.frame - slot.last_seen_frame) < 120
			dot_color := fresh ? ui.COL_GREEN : ui.with_alpha(ui.COL_WHITE, 0.2)
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect = {pos = {item_rect.pos.x + 2, y + 6}, size = {5, 5}},
				color = dot_color,
			})

			// Venue:Symbol label.
			text_color := is_active ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY
			ui.push_text(&state.cmd_buf, {item_rect.pos.x + 10, y + 10},
				label, text_color, ui.FONT_SIZE_XS, .Mono)

			// Latest price + change from candle store.
			latest := services.get_candle_newest(&slot.candle_store, 0)
			if latest.close > 0 {
				price_buf: [20]u8
				price_str := fmt.bprintf(price_buf[:], "%.2f", latest.close)
				price_w := state.text.measure(ui.FONT_SIZE_XS, price_str).x
				ui.push_text(&state.cmd_buf,
					{item_rect.pos.x + item_rect.size.x - price_w - 2, y + 10},
					price_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

				// Percent change (close vs open of latest candle).
				if latest.open > 0 {
					pct := (latest.close - latest.open) / latest.open * 100
					pct_buf: [12]u8
					pct_str := pct >= 0 ? fmt.bprintf(pct_buf[:], "+%.2f%%", pct) : fmt.bprintf(pct_buf[:], "%.2f%%", pct)
					pct_color := pct >= 0 ? ui.COL_GREEN : ui.COL_RED
					pct_w := state.text.measure(ui.FONT_SIZE_XS, pct_str).x
					ui.push_text(&state.cmd_buf,
						{item_rect.pos.x + item_rect.size.x - pct_w - 2, y + 20},
						pct_str, pct_color, ui.FONT_SIZE_XS, .Mono)
				}
			}

			y += item_h
		}
		y += 4
	}

	// ===================================================
	// Section 3: Analytics & Profiles (collapsible, S55)
	// ===================================================
	remaining_h = (rect.pos.y + rect.size.y) - y
	if remaining_h > 22 {
		// Count analytics/profile cells.
		analytics_cell_count := 0
		profile_cell_count := 0
		for ci in 0 ..< state.world.count {
			wk := state.world.widgets[ci].kind
			if wk == .Analytics do analytics_cell_count += 1
			if wk == .Session_VPVR || wk == .TPO do profile_cell_count += 1
		}
		total_ap := analytics_cell_count + profile_cell_count

		if total_ap > 0 {
			ap_hdr_buf: [24]u8
			ap_hdr_label := fmt.bprintf(ap_hdr_buf[:], "ANALYTICS (%d)", total_ap)
			ui.collapsible_section(&state.cmd_buf,
				ui.Rect{pos = {rect.pos.x, y}, size = {rect.size.x, remaining_h}},
				ap_hdr_label, &state.chrome.section_analytics, pointer, state.text.measure)
			y += 22
			if state.chrome.section_analytics.expanded {
				item_h := f32(22)
				for ci in 0 ..< state.world.count {
					if y + item_h > rect.pos.y + rect.size.y do break
					wk := state.world.widgets[ci].kind
					if wk != .Analytics && wk != .Session_VPVR && wk != .TPO do continue

					is_focused := ci == state.world.focused
					item_rect := ui.Rect{pos = {rect.pos.x + 4, y}, size = {rect.size.x - 8, item_h}}
					if is_focused {
						ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_BLUE, 0.12)})
					}

					label_buf: [48]u8
					label: string
					if wk == .Analytics {
						ANALYTICS_NAMES :: [4]string{"OI", "Delta Vol", "CVD", "Bar Stats"}
						analytics_names := ANALYTICS_NAMES
						ak := int(state.world.analytics[ci].analytics_kind)
						aname := analytics_names[ak] if ak >= 0 && ak < 4 else "OI"
						hist := state.world.analytics[ci].show_history ? "H" : "-"
						label = fmt.bprintf(label_buf[:], "[%d] %s  %s", ci, aname, hist)
					} else if wk == .Session_VPVR {
						stores := resolve_stores_for_cell(state, ci)
						if stores.session_vpvr != nil && stores.session_vpvr.count > 0 {
							poc_buf: [16]u8
							poc_str := fmt.bprintf(poc_buf[:], "%.2f", stores.session_vpvr.buckets[stores.session_vpvr.poc_index].price)
							label = fmt.bprintf(label_buf[:], "[%d] SVPVR  POC:%s", ci, poc_str)
						} else {
							label = fmt.bprintf(label_buf[:], "[%d] SVPVR  --", ci)
						}
					} else { // .TPO
						stores := resolve_stores_for_cell(state, ci)
						if stores.tpo != nil && stores.tpo.level_count > 0 {
							poc_buf: [16]u8
							poc_str := fmt.bprintf(poc_buf[:], "%.2f", stores.tpo.poc_price)
							label = fmt.bprintf(label_buf[:], "[%d] TPO  POC:%s  P:%d", ci, poc_str, stores.tpo.period_count)
						} else {
							label = fmt.bprintf(label_buf[:], "[%d] TPO  --", ci)
						}
					}

					ui.push_text(&state.cmd_buf, {item_rect.pos.x + 4, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
						label, is_focused ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
					y += item_h
				}
				y += 4
			}
		}
	}

	// ===================================================
	// Section 4: Chart Layers (collapsible)
	// ===================================================
	remaining_h = (rect.pos.y + rect.size.y) - y
	if remaining_h > 22 {
		ui.collapsible_section(&state.cmd_buf,
			ui.Rect{pos = {rect.pos.x, y}, size = {rect.size.x, remaining_h}},
			"LAYERS", &state.chrome.section_layers, pointer, state.text.measure)
		y += 22
		if state.chrome.section_layers.expanded {
			toggle_w := rect.size.x - 8
			toggle_h := f32(20)
			tx := rect.pos.x + 4
			layer_items := [6]struct{id: layers.Layer_ID, label: string}{
				{id = .Price_Candles, label = " Price/Candles"},
				{id = .Trades_Tape, label = " Trades Tape"},
				{id = .OrderBook_DOM, label = " OrderBook/DOM"},
				{id = .VPVR_Heatmap, label = " VPVR/Heatmap"},
				{id = .Evidence, label = " Evidence"},
				{id = .Signal, label = " Signal"},
			}
			for li in 0 ..< len(layer_items) {
				if y + toggle_h > rect.pos.y + rect.size.y do break
				item := layer_items[li]
				value := layers.layer_registry_is_enabled(&state.layer_registry, item.id)
				tr := ui.toggle(&state.cmd_buf,
					ui.rect_xywh(tx, y, toggle_w, toggle_h),
					item.label, value, pointer, state.text.measure, ui.FONT_SIZE_XS)
				if tr.changed {
					layers.layer_registry_set_enabled(&state.layer_registry, item.id, tr.value)
					layers.layer_registry_save_settings(&state.layer_registry, &state.settings)
					services.settings_flush(&state.settings)
				}
				y += toggle_h + 2
			}
		}
	}

	// ===================================================
	// Section 4: Panel Layout (collapsible)
	// ===================================================
	remaining_h = (rect.pos.y + rect.size.y) - y
	if remaining_h > 22 {
		ui.collapsible_section(&state.cmd_buf,
			ui.Rect{pos = {rect.pos.x, y}, size = {rect.size.x, remaining_h}},
			"PANELS", &state.chrome.section_panels, pointer, state.text.measure)
		y += 22
		if state.chrome.section_panels.expanded {
			// Layout preset selector.
			if y + 18 < rect.pos.y + rect.size.y {
				preset_labels := ui.LAYOUT_PRESET_LABELS
				seg_w := min(rect.size.x - 8, f32(180))
				seg_rect := ui.rect_xywh(rect.pos.x + 4, y, seg_w, f32(16))
				seg_res := ui.segmented_control(&state.cmd_buf, seg_rect,
					preset_labels[:], state.layout_preset,
					pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
				if seg_res.changed {
					queue_ui_action(state, UI_Action{kind = .Set_Layout_Preset, layout_preset = seg_res.index})
				}
				y += 22
			}

			// Custom preset slots (C1-C4): click to load, shift+click to save.
			if y + 22 < rect.pos.y + rect.size.y {
				ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10},
					"Custom:", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
				cp_x := rect.pos.x + 4 + state.text.measure(ui.FONT_SIZE_XS, "Custom:").x + 6
				cp_btn_w := f32(24)
				cp_btn_h := f32(16)
				CP_LABELS :: [4]string{"C1", "C2", "C3", "C4"}
				cp_labels := CP_LABELS
				for ci in 0 ..< 4 {
					valid := custom_preset_valid(state, ci)
					btn_rect := ui.rect_xywh(cp_x + f32(ci) * (cp_btn_w + 3), y, cp_btn_w, cp_btn_h)
					btn_res := ui.button(&state.cmd_buf, btn_rect, cp_labels[ci], pointer,
						state.text.measure, ui.FONT_SIZE_XS, .Mono, true)
					if valid {
						// Blue dot indicator for saved presets.
						ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
							rect = {pos = {btn_rect.pos.x + btn_rect.size.x - 5, btn_rect.pos.y + 2}, size = {3, 3}},
							color = ui.COL_BLUE,
						})
					}
					if btn_res.clicked {
						if valid {
							load_custom_preset(state, ci)
						} else {
							save_custom_preset(state, ci)
						}
					}
				}
				// Save button.
				save_x := cp_x + 4 * (cp_btn_w + 3) + 4
				save_w := f32(32)
				save_res := ui.button(&state.cmd_buf,
					ui.rect_xywh(save_x, y, save_w, cp_btn_h),
					"Sav", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
				if save_res.clicked {
					// Save to first empty slot, or overwrite last.
					saved := false
					for ci in 0 ..< 4 {
						if !custom_preset_valid(state, ci) {
							save_custom_preset(state, ci)
							saved = true
							break
						}
					}
					if !saved {
						save_custom_preset(state, 3)
					}
				}
				y += 22
			}

			toggle_rect := ui.Rect{
				pos  = {rect.pos.x, y},
				size = {rect.size.x, rect.pos.y + rect.size.y - y},
			}
			sidebar_res := ui.draw_sidebar(&state.cmd_buf, toggle_rect, &state.chrome.sidebar, pointer, state.text.measure)
			if sidebar_res.toggled_panel >= 0 {
				queue_ui_action(state, UI_Action{kind = .Toggle_Panel, panel_idx = sidebar_res.toggled_panel})
			}
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// S52: Dashboard grid workspace — extracted from build_ui.odin
// ═══════════════════════════════════════════════════════════════════════════

@(private = "package")
col_weight_sum :: proc(state: ^App_State, col_count: int) -> f32 {
	s := f32(0)
	for c in 0 ..< col_count {
		s += state.custom_grid_def.col_weights[c]
	}
	if s <= 0 do s = 1
	return s
}

@(private = "package")
row_weight_sum :: proc(state: ^App_State, row_count: int) -> f32 {
	s := f32(0)
	for r in 0 ..< row_count {
		s += state.custom_grid_def.row_weights[r]
	}
	if s <= 0 do s = 1
	return s
}

// Normal-mode grid layout: compute grid, render cells, context menu, resize handles.
@(private = "package")
build_dashboard_grid :: proc(
	state: ^App_State,
	workspace: ui.Rect,
	pointer: ui.Pointer_Input,
	workspace_input: ports.Input_State,
	workspace_pointer: ui.Pointer_Input,
	gap: f32,
	viewport_w, viewport_h: f32,
	mobile: bool,
) {
	// Compute grid layout.
	grid_def: ui.Grid_Def
	if state.layout_mode == .Custom {
		// Free-form: auto-reflow based on cell_count, apply per-cell spans.
		grid_def = ui.build_auto_grid(state.world.count, gap)
		for ci in 0 ..< state.world.count {
			sp := state.world.spans[ci]
			cs := sp.col_span > 1 ? sp.col_span : 1
			rs := sp.row_span > 1 ? sp.row_span : 1
			if ci < grid_def.cell_count {
				grid_def.cells[ci].col_span = cs
				grid_def.cells[ci].row_span = rs
			}
		}
	} else {
		base_grid_def: ui.Grid_Def
		if mobile {
			base_grid_def = ui.build_mobile_grid(gap)
		} else {
			base_grid_def = state.custom_grid_def
		}
		grid_def = ui.build_filtered_grid(base_grid_def, state.chrome.panel_visible, gap)
	}
	grid := ui.compute_grid(grid_def, workspace)

	// Drag-drop panel swap.
	if !mobile {
		swapped, swap_a, swap_b := ui.update_drag(
			&state.panel_drag, grid.rects, state.chrome.panel_visible,
			workspace_pointer, current_now_ms(state), f32(26))
		if swapped {
			ui.apply_panel_swap(&state.custom_grid_def, swap_a, swap_b)
		}
	}

	// Ensure focused_candle_cell_idx points to a valid candle cell.
	if state.world.focused < 0 || state.world.focused >= state.world.count ||
		state.world.widgets[state.world.focused].kind != .Candle {
		state.world.focused = -1
		for fi in 0 ..< state.world.count {
			if state.world.widgets[fi].kind == .Candle {
				state.world.focused = fi
				break
			}
		}
	}

	// Scan for active crosshair (from previous frame) for sync across charts.
	sync_price := f64(0)
	sync_active := false
	for si in 0 ..< state.world.count {
		if state.world.widgets[si].kind != .Candle do continue
		if state.world.views[si].crosshair.active {
			sync_price = state.world.views[si].crosshair.price_at_y
			sync_active = true
			break
		}
	}

	// Dispatch widgets from ECS world components (see build_cell.odin).
	for ci in 0 ..< state.world.count {
		if ci >= grid_def.cell_count do break
		render_cell_widget(state, ci, grid.rects[ci], workspace_pointer, workspace_input, sync_price, sync_active)
	}

	// --- Drag feedback (rendered after widgets for z-order) ---
	if !mobile && state.panel_drag.phase != .Idle {
		ui.draw_drag_feedback(&state.cmd_buf, &state.panel_drag, grid.rects, state.chrome.panel_visible)
	}

	// --- Right-click on cell → open context menu ---
	if !mobile && workspace_input.mouse.pressed[.Right] {
		for ci in 0 ..< state.world.count {
			if ci >= grid_def.cell_count do break
			cell_vp := grid.rects[ci]
			if ui.rect_contains(cell_vp, workspace_pointer.pos) {
				state.cell_context_menu = ui.Context_Menu_State{
					open = true,
					pos  = workspace_pointer.pos,
				}
				state.cell_context_cell_idx = ci
				break
			}
		}
	}

	// S53: Context menu and resize handles delegated to extracted procs.
	build_cell_context_menu(state, workspace_pointer, viewport_w, viewport_h)
	if !mobile {
		update_grid_col_resize(state, workspace, pointer, grid_def, gap)
		update_grid_row_resize(state, workspace, pointer, grid_def, grid, gap)
	}
}
