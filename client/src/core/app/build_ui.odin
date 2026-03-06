package app

import "core:fmt"
import "mr:ports"
import "mr:streams"
import "mr:ui"

@(private = "file")
col_weight_sum :: proc(state: ^App_State, col_count: int) -> f32 {
	s := f32(0)
	for c in 0 ..< col_count {
		s += state.custom_grid_def.col_weights[c]
	}
	if s <= 0 do s = 1
	return s
}

@(private = "file")
row_weight_sum :: proc(state: ^App_State, row_count: int) -> f32 {
	s := f32(0)
	for r in 0 ..< row_count {
		s += state.custom_grid_def.row_weights[r]
	}
	if s <= 0 do s = 1
	return s
}

@(private = "file")
cache_string :: proc(buf: []u8, n: int) -> string {
	m := n
	if m <= 0 do return ""
	if m > len(buf) do m = len(buf)
	return string(buf[:m])
}

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

	STATUS_BAR_H :: f32(16)
	top_bar_h := TOP_BAR_H
	workspace: ui.Rect
	if state.zen.active {
		// Full viewport in zen mode (no top/bottom/pad margins).
		workspace = ui.rect_xywh(pad, 1, viewport_w - pad * 2, viewport_h - 2)
	} else {
		workspace = ui.rect_xywh(
			pad, top_bar_h + 1,
			viewport_w - pad * 2,
			viewport_h - (top_bar_h + 1) - STATUS_BAR_H,
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
			// ═══════════════════════════════════════════════════════════
			// NORMAL MODE — grid layout (preset or free-form)
			// ═══════════════════════════════════════════════════════════

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

			// --- Cell context menu rendering (simplified: widget type + add/remove) ---
			if state.cell_context_menu.open {
				cci := state.cell_context_cell_idx
				current_widget := Widget_Kind.Empty
				if cci >= 0 && cci < state.world.count {
					current_widget = state.world.widgets[cci].kind
				}
				WIDGET_LABELS :: [9]string{"Candle", "Stats", "Counter", "Heatmap", "VPVR", "Trades", "Orderbook", "DOM", "Empty"}
				labels := WIDGET_LABELS
				menu_items: [ui.CONTEXT_MENU_MAX_ITEMS]ui.Context_Menu_Item
				menu_count := 0
				for i in 0 ..< 9 {
					menu_items[menu_count] = ui.Context_Menu_Item{
						label    = labels[i],
						selected = Widget_Kind(i) == current_widget,
					}
					menu_count += 1
				}
				// Add Cell + Remove Cell.
				menu_items[menu_count] = {label = "+ Add Cell", divider = true}
				add_cell_idx := menu_count
				menu_count += 1
				menu_items[menu_count] = {label = "- Remove", divider = false}
				remove_cell_idx := menu_count
				menu_count += 1
				// Span controls (PRD-0007 M2).
				expand_right_idx := -1
				expand_down_idx := -1
				reset_size_idx := -1
				clear_all_idx := -1
				if state.layout_mode == .Custom {
					menu_items[menu_count] = {label = "Expand ->", divider = true}
					expand_right_idx = menu_count
					menu_count += 1
					menu_items[menu_count] = {label = "Expand v", divider = false}
					expand_down_idx = menu_count
					menu_count += 1
					has_span := cci >= 0 && cci < state.world.count &&
						(state.world.spans[cci].col_span > 1 || state.world.spans[cci].row_span > 1)
					if has_span {
						menu_items[menu_count] = {label = "Reset Size", divider = false}
						reset_size_idx = menu_count
						menu_count += 1
					}
					menu_items[menu_count] = {label = "Clear All", divider = true}
					clear_all_idx = menu_count
					menu_count += 1
				}

				menu_res := ui.context_menu(&state.cmd_buf, &state.cell_context_menu,
					menu_items[:menu_count], workspace_pointer, state.text.measure,
					ui.Rect{pos = {0, 0}, size = {viewport_w, viewport_h}})
				if menu_res.clicked_idx >= 0 {
					if menu_res.clicked_idx < 9 {
						queue_ui_action(state, UI_Action{
							kind        = .Set_Cell_Widget,
							cell_idx    = cci,
							widget_kind = Widget_Kind(menu_res.clicked_idx),
						})
					} else if menu_res.clicked_idx == add_cell_idx {
						queue_ui_action(state, UI_Action{kind = .Add_Cell})
					} else if menu_res.clicked_idx == remove_cell_idx {
						queue_ui_action(state, UI_Action{kind = .Remove_Cell, cell_idx = cci})
					} else if menu_res.clicked_idx == expand_right_idx && cci >= 0 && cci < state.world.count {
						cs := state.world.spans[cci].col_span
						if cs < 1 do cs = 1
						if cs < 4 { state.world.spans[cci].col_span = cs + 1 }
						persist_layout_v4(state)
					} else if menu_res.clicked_idx == expand_down_idx && cci >= 0 && cci < state.world.count {
						rs := state.world.spans[cci].row_span
						if rs < 1 do rs = 1
						if rs < 4 { state.world.spans[cci].row_span = rs + 1 }
						persist_layout_v4(state)
					} else if menu_res.clicked_idx == reset_size_idx && cci >= 0 && cci < state.world.count {
						state.world.spans[cci].col_span = 1
						state.world.spans[cci].row_span = 1
						persist_layout_v4(state)
					} else if menu_res.clicked_idx == clear_all_idx {
						state.world.count = 0
						state.overlays.show_widget_catalog = true
						state.overlays.catalog_step = 0
						persist_layout_v4(state)
					}
				}
			}

			// --- Grid column resize handles ---
			if !mobile && grid_def.col_count >= 2 {
				RESIZE_HIT_W :: f32(6)
				// Detect hover/drag on column borders.
				if state.grid_col_resize >= 0 {
					// Active resize drag.
					if pointer.left_down {
						ci := state.grid_col_resize
						// Convert pointer X to weight adjustment.
						total_w := workspace.size.x - gap * f32(grid_def.col_count - 1)
						if total_w > 0 {
							left_x := workspace.pos.x
							for c in 0 ..< ci {
								left_x += total_w * (state.custom_grid_def.col_weights[c] / col_weight_sum(state, grid_def.col_count)) + gap
							}
							new_left_w := pointer.pos.x - left_x
							right_edge := left_x + total_w * (state.custom_grid_def.col_weights[ci] / col_weight_sum(state, grid_def.col_count)) + gap + total_w * (state.custom_grid_def.col_weights[ci + 1] / col_weight_sum(state, grid_def.col_count))
							new_right_w := right_edge - pointer.pos.x - gap
							min_w := total_w * 0.08
							if new_left_w >= min_w && new_right_w >= min_w {
								s := col_weight_sum(state, grid_def.col_count)
								state.custom_grid_def.col_weights[ci]     = (new_left_w / total_w) * s
								state.custom_grid_def.col_weights[ci + 1] = (new_right_w / total_w) * s
							}
						}
					} else {
						state.grid_col_resize = -1
						// Persist column weights on drag release.
						persist_col_weights(state, grid_def.col_count)
					}
				} else {
					// Detect hover on column borders.
					for ci in 0 ..< grid_def.col_count - 1 {
						// BUG-20: Compute border_x from accumulated weights (handles spanned cells).
						total_w_detect := workspace.size.x - gap * f32(grid_def.col_count - 1)
						cw_sum_detect := col_weight_sum(state, grid_def.col_count)
						border_x := workspace.pos.x
						for c in 0 ..= ci {
							if c > 0 do border_x += gap
							border_x += total_w_detect * (state.custom_grid_def.col_weights[c] / cw_sum_detect)
						}
						hit := ui.Rect{pos = {border_x - RESIZE_HIT_W * 0.5, workspace.pos.y}, size = {RESIZE_HIT_W, workspace.size.y}}
						if ui.rect_contains(hit, pointer.pos) {
							// Visual hint.
							ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
								rect = {pos = {border_x - 1, workspace.pos.y}, size = {2, workspace.size.y}},
								color = ui.with_alpha(ui.COL_BLUE, 0.35),
							})
							if pointer.left_pressed {
								state.grid_col_resize = ci
							}
							break
						}
					}
				}
			}

			// --- Grid row resize handles (PRD-0007 M0) ---
			if !mobile && grid_def.row_count >= 2 {
				RESIZE_HIT_H :: f32(6)
				if state.grid_row_resize >= 0 {
					// Active resize drag.
					if pointer.left_down {
						ri := state.grid_row_resize
						total_h := workspace.size.y - gap * f32(grid_def.row_count - 1)
						if total_h > 0 {
							top_y := workspace.pos.y
							for r in 0 ..< ri {
								top_y += total_h * (state.custom_grid_def.row_weights[r] / row_weight_sum(state, grid_def.row_count)) + gap
							}
							new_top_h := pointer.pos.y - top_y
							bottom_edge := top_y + total_h * (state.custom_grid_def.row_weights[ri] / row_weight_sum(state, grid_def.row_count)) + gap + total_h * (state.custom_grid_def.row_weights[ri + 1] / row_weight_sum(state, grid_def.row_count))
							new_bottom_h := bottom_edge - pointer.pos.y - gap
							min_h := total_h * 0.06
							if new_top_h >= min_h && new_bottom_h >= min_h {
								s := row_weight_sum(state, grid_def.row_count)
								state.custom_grid_def.row_weights[ri]     = (new_top_h / total_h) * s
								state.custom_grid_def.row_weights[ri + 1] = (new_bottom_h / total_h) * s
							}
						}
					} else {
						state.grid_row_resize = -1
						// Persist row weights on drag release.
						persist_row_weights(state, grid_def.row_count)
					}
				} else {
					// Detect hover on row borders.
					for ri in 0 ..< grid_def.row_count - 1 {
						border_y := f32(0)
						found_border := false
						for gi in 0 ..< grid_def.cell_count {
							gc := grid_def.cells[gi]
							if gc.row == ri && gc.row_span == 1 {
								border_y = ui.rect_bottom(grid.rects[gi])
								found_border = true
								break
							}
						}
						if !found_border do continue
						hit := ui.Rect{pos = {workspace.pos.x, border_y - RESIZE_HIT_H * 0.5}, size = {workspace.size.x, RESIZE_HIT_H}}
						if ui.rect_contains(hit, pointer.pos) {
							// Visual hint: horizontal blue line.
							ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
								rect = {pos = {workspace.pos.x, border_y - 1}, size = {workspace.size.x, 2}},
								color = ui.with_alpha(ui.COL_BLUE, 0.35),
							})
							if pointer.left_pressed {
								state.grid_row_resize = ri
							}
							break
						}
					}
				}
			}
		}

	case .Markets:
		build_markets_page(state, workspace, pointer)

	case .Settings:
		build_settings_page(state, workspace, pointer)
	}

	// --- Status bar (bottom 20px) — hidden in zen mode unless mouse near bottom ---
	zen_skip_status := state.zen.active && state.zen.bottom_alpha <= 0
	if !zen_skip_status {
		bar_y := viewport_h - STATUS_BAR_H
		bar_rect := ui.Rect{pos = {0, bar_y}, size = {viewport_w, STATUS_BAR_H}}
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = bar_rect, color = ui.COL_SURFACE_2})
		ui.push(&state.cmd_buf, ui.Cmd_Line{
			from = {0, bar_y}, to = {viewport_w, bar_y},
			color = ui.COL_DIVIDER, thickness = 1,
		})

		sx := f32(8)
		sy := bar_y + STATUS_BAR_H * 0.5 + ui.FONT_SIZE_XS * 0.35

		// Strong stream health status (LIVE / LAG / DESYNC).
		health_label := "OFFLINE"
		health_color := ui.COL_TEXT_MUTED
		waiting_primary := active_stream_waiting_primary_data(state)
		switch state.active_metrics.state {
		case .Live:
			if waiting_primary {
				health_label = "LIVE (no data)"
				health_color = ui.COL_WARNING
			} else {
				health_label = "LIVE"
				health_color = ui.COL_GREEN
			}
		case .Lag:
			health_label = "LAG"
			health_color = ui.COL_WARNING
		case .Desync:
			health_label = "DESYNC"
			health_color = ui.COL_RED
		case .Offline:
		}
		ui.push_text(&state.cmd_buf, {sx, sy}, health_label, health_color, ui.FONT_SIZE_XS, .Bold)
		sx += state.text.measure(ui.FONT_SIZE_XS, health_label).x + 10
		reason_short := active_stream_reason_short(state)
		if len(reason_short) > 0 {
			reason_color := state.active_metrics.state == .Desync ? ui.COL_RED : ui.COL_TEXT_MUTED
			reason_buf: [48]u8
			reason_str := fmt.bprintf(reason_buf[:], "[%s]", reason_short)
			ui.push_text(&state.cmd_buf, {sx, sy}, reason_str, reason_color, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, reason_str).x + 8
		}
		hud_label := state.telemetry.hud_enabled ? "HUD*" : "HUD"
		hud_rect := ui.rect_xywh(sx, bar_y + 1, 38, STATUS_BAR_H - 2)
		hud_btn := ui.button(&state.cmd_buf, hud_rect, hud_label, pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
		if hud_btn.clicked {
			queue_ui_action(state, UI_Action{kind = .Toggle_Telemetry_HUD})
		}
		sx += hud_rect.size.x + 8
		if state.active_metrics.state == .Desync || waiting_primary {
			rs_rect := ui.rect_xywh(sx, bar_y + 1, 48, STATUS_BAR_H - 2)
			rs_btn := ui.button(&state.cmd_buf, rs_rect, "Resync", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
			if rs_btn.clicked {
				queue_ui_action(state, UI_Action{kind = .Resync_Active_Stream})
			}
			sx += rs_rect.size.x + 8
		}

		rtt_buf: [24]u8
		rtt_str := fmt.bprintf(rtt_buf[:], "RTT:%dms", max(state.active_metrics.rtt_ms, 0))
		ui.push_text(&state.cmd_buf, {sx, sy}, rtt_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, rtt_str).x + 8

		lag_buf: [24]u8
		lag_str := fmt.bprintf(lag_buf[:], "LAG:%dms", max(state.active_metrics.lag_ms, 0))
		lag_color := state.active_metrics.lag_ms > 4_000 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {sx, sy}, lag_str, lag_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, lag_str).x + 8

		last_age_ms := i64(0)
		if now_ms := current_now_ms(state); now_ms > 0 && state.active_metrics.last_msg_ts_ms > 0 {
			last_age_ms = max(now_ms - state.active_metrics.last_msg_ts_ms, 0)
		}
		last_buf: [24]u8
		last_str := fmt.bprintf(last_buf[:], "LAST:%dms", last_age_ms)
		last_color := last_age_ms > 8_000 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {sx, sy}, last_str, last_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, last_str).x + 8

		ack_buf: [20]u8
		ack_str := fmt.bprintf(ack_buf[:], "ACK:%d", max(state.active_metrics.subscribe_acks, 0))
		ui.push_text(&state.cmd_buf, {sx, sy}, ack_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, ack_str).x + 8

		dr_buf: [24]u8
		dr_str := fmt.bprintf(dr_buf[:], "DROP:%d", max(state.active_metrics.drop_count, 0))
		dr_color := state.active_metrics.drop_count > 0 ? ui.COL_RED : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {sx, sy}, dr_str, dr_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, dr_str).x + 8

		rc_buf: [24]u8
		rc_str := fmt.bprintf(rc_buf[:], "RC:%d", max(state.active_metrics.reconnect_count, 0))
		rc_color := state.active_metrics.reconnect_count > 0 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {sx, sy}, rc_str, rc_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, rc_str).x + 8

		// Queue fill + drops + reconnects from metrics history.
		if ok, qmax, drop_delta, rc_delta := metrics_history_summary(state); ok {
			q_buf: [32]u8
			q_str := fmt.bprintf(q_buf[:], "Q:%d", qmax)
			q_color := qmax > 100 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
			ui.push_text(&state.cmd_buf, {sx, sy}, q_str, q_color, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, q_str).x + 10

			d_buf: [32]u8
			d_str := fmt.bprintf(d_buf[:], "D:%d", drop_delta)
			d_color := drop_delta > 0 ? ui.COL_RED : ui.COL_TEXT_MUTED
			ui.push_text(&state.cmd_buf, {sx, sy}, d_str, d_color, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, d_str).x + 10

			r_buf: [32]u8
			r_str := fmt.bprintf(r_buf[:], "RC:%d", rc_delta)
			r_color := rc_delta > 0 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
			ui.push_text(&state.cmd_buf, {sx, sy}, r_str, r_color, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, r_str).x + 10
		}
		if state.telemetry.hud_enabled {
			refresh_telemetry_hud_cache(state)

			mps_str := cache_string(state.telemetry.hud_cache.mps_buf[:], state.telemetry.hud_cache.mps_len)
			ui.push_text(&state.cmd_buf, {sx, sy}, mps_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, mps_str).x + 8

			bps_str := cache_string(state.telemetry.hud_cache.bps_buf[:], state.telemetry.hud_cache.bps_len)
			ui.push_text(&state.cmd_buf, {sx, sy}, bps_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, bps_str).x + 8

			cb_str := cache_string(state.telemetry.hud_cache.cb_buf[:], state.telemetry.hud_cache.cb_len)
			cb_color := state.active_metrics.candle_backlog > 0 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
			ui.push_text(&state.cmd_buf, {sx, sy}, cb_str, cb_color, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, cb_str).x + 8

			arena_str := cache_string(state.telemetry.hud_cache.arena_buf[:], state.telemetry.hud_cache.arena_len)
			ui.push_text(&state.cmd_buf, {sx, sy}, arena_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, arena_str).x + 8

			pm_str := cache_string(state.telemetry.hud_cache.pm_buf[:], state.telemetry.hud_cache.pm_len)
			ui.push_text(&state.cmd_buf, {sx, sy}, pm_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, pm_str).x + 8

			pr_str := cache_string(state.telemetry.hud_cache.pr_buf[:], state.telemetry.hud_cache.pr_len)
			ui.push_text(&state.cmd_buf, {sx, sy}, pr_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, pr_str).x + 8

			pb_str := cache_string(state.telemetry.hud_cache.pb_buf[:], state.telemetry.hud_cache.pb_len)
			ui.push_text(&state.cmd_buf, {sx, sy}, pb_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, pb_str).x + 8

			phase_str := cache_string(state.telemetry.hud_cache.phase_buf[:], state.telemetry.hud_cache.phase_len)
			if len(phase_str) > 0 {
				ui.push_text(&state.cmd_buf, {sx, sy}, phase_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
				sx += state.text.measure(ui.FONT_SIZE_XS, phase_str).x + 8
			}

			// S27: Apply state summary (composition + active artifacts).
			apply_str := cache_string(state.telemetry.hud_cache.apply_buf[:], state.telemetry.hud_cache.apply_len)
			if len(apply_str) > 0 {
				apply_color := ui.COL_TEXT_MUTED
				switch state.active_metrics.context_stage {
				case .Composed:      apply_color = ui.COL_GREEN
				case .Live_Only:     apply_color = ui.COL_YELLOW_ACCENT
				case .Backfilled, .Range_Pending: apply_color = ui.COL_WARNING
				case .Empty:
				}
				ui.push_text(&state.cmd_buf, {sx, sy}, apply_str, apply_color, ui.FONT_SIZE_XS, .Mono)
				sx += state.text.measure(ui.FONT_SIZE_XS, apply_str).x + 8
			}

			// S28: Per-artifact age.
			age_str := cache_string(state.telemetry.hud_cache.age_buf[:], state.telemetry.hud_cache.age_len)
			if len(age_str) > 0 {
				ui.push_text(&state.cmd_buf, {sx, sy}, age_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
				sx += state.text.measure(ui.FONT_SIZE_XS, age_str).x + 8
			}

			// S31: Aggregate health badge.
			agg_str := cache_string(state.telemetry.hud_cache.agg_buf[:], state.telemetry.hud_cache.agg_len)
			if len(agg_str) > 0 {
				ui.push_text(&state.cmd_buf, {sx, sy}, agg_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
				sx += state.text.measure(ui.FONT_SIZE_XS, agg_str).x + 8
			}
		}

		// Frame time p50.
		if state.telemetry.frame_time_count > 0 {
			p50, _, _ := frame_time_percentiles(state)
			fps_approx := p50 > 0 ? 1_000_000 / p50 : 0
			f_buf: [32]u8
			f_str := fmt.bprintf(f_buf[:], "FPS:%d", fps_approx)
			fps_color := fps_approx < 30 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
			ui.push_text(&state.cmd_buf, {sx, sy}, f_str, fps_color, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, f_str).x + 10
		}

		// Data source badges: HM, VP, CD.
		hm_live := state.active_metrics.has_live_heatmap
		hm_synth := !hm_live && state.stores.heatmap.count > 0
		hm_label := hm_live ? "HM:LIVE" : (hm_synth ? "HM:SYNTH" : "HM:--")
		hm_color := hm_live ? ui.COL_GREEN : (hm_synth ? ui.COL_WARNING : ui.COL_TEXT_MUTED)
		ui.push_text(&state.cmd_buf, {sx, sy}, hm_label, hm_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, hm_label).x + 8

		vp_live := state.active_metrics.has_live_vpvr
		vp_synth := !vp_live && state.stores.vpvr.count > 0
		vp_label := vp_live ? "VP:LIVE" : (vp_synth ? "VP:SYNTH" : "VP:--")
		vp_color := vp_live ? ui.COL_GREEN : (vp_synth ? ui.COL_WARNING : ui.COL_TEXT_MUTED)
		ui.push_text(&state.cmd_buf, {sx, sy}, vp_label, vp_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, vp_label).x + 8

		cd_live := state.active_metrics.has_live_candle
		cd_label := cd_live ? "CD:LIVE" : "CD:--"
			cd_color := cd_live ? ui.COL_GREEN : ui.COL_TEXT_MUTED
			ui.push_text(&state.cmd_buf, {sx, sy}, cd_label, cd_color, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, cd_label).x + 12
			ctx_label := "CTX:EMPTY"
			ctx_color := ui.COL_TEXT_MUTED
			switch state.active_metrics.context_stage {
			case .Range_Pending:
				ctx_label = "CTX:PENDING"
				ctx_color = ui.COL_WARNING
			case .Backfilled:
				ctx_label = "CTX:BACKFILLED"
				ctx_color = ui.COL_WARNING
			case .Live_Only:
				ctx_label = "CTX:LIVE_ONLY"
				ctx_color = ui.COL_YELLOW_ACCENT
			case .Composed:
				ctx_label = "CTX:COMPOSED"
				ctx_color = ui.COL_GREEN
			case .Empty:
			}
			ui.push_text(&state.cmd_buf, {sx, sy}, ctx_label, ctx_color, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, ctx_label).x + 12
			if len(reason_short) > 0 {
			rsn_buf: [64]u8
			rsn_label := fmt.bprintf(rsn_buf[:], "RSN:%s", reason_short)
			rsn_color := state.active_metrics.state == .Desync ? ui.COL_RED : ui.COL_WARNING
			ui.push_text(&state.cmd_buf, {sx, sy}, rsn_label, rsn_color, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, rsn_label).x + 8
		}

		// Whale alert flash (visible for ~2 sec = 120 frames at 60fps).
		// Only render if there's enough space before right-aligned elements.
		if state.whale.frame > 0 && state.frame > 0 && state.frame - state.whale.frame < 120 {
			whale_side := state.whale.buy ? "BUY" : "SELL"
			whale_color := state.whale.buy ? ui.COL_GREEN : ui.COL_RED
			w_buf: [48]u8
			w_str := fmt.bprintf(w_buf[:], "WHALE %s %.2f @ %.2f", whale_side, state.whale.qty, state.whale.price)
			pulse_w := state.text.measure(ui.FONT_SIZE_XS, w_str).x + 8
			if sx + pulse_w < viewport_w - 80 {
				alpha := f32(0.25) - f32(state.frame - state.whale.frame) * 0.002
				if alpha > 0 {
					ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
						rect = ui.rect_xywh(sx - 4, bar_y + 1, pulse_w, STATUS_BAR_H - 2),
						color = ui.with_alpha(whale_color, alpha),
					})
				}
				ui.push_text(&state.cmd_buf, {sx, sy}, w_str, whale_color, ui.FONT_SIZE_XS, .Bold)
			}
		}

		// Error state: persistent last-error indicator (visible ~10 sec = 600 frames).
		ERROR_DISPLAY_FRAMES :: u64(600)
		if state.error_state.len > 0 && state.error_state.frame > 0 &&
			state.frame - state.error_state.frame < ERROR_DISPLAY_FRAMES {
			err_str := string(state.error_state.text[:state.error_state.len])
			err_w := state.text.measure(ui.FONT_SIZE_XS, err_str).x + 8
			if sx + err_w < viewport_w - 120 {
				age := state.frame - state.error_state.frame
				err_alpha := age < ERROR_DISPLAY_FRAMES - 120 ? f32(1.0) : f32(ERROR_DISPLAY_FRAMES - age) / 120.0
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = ui.rect_xywh(sx - 2, bar_y + 1, err_w, STATUS_BAR_H - 2),
					color = ui.with_alpha(ui.COL_RED, 0.15 * err_alpha),
				})
				ui.push_text(&state.cmd_buf, {sx, sy}, err_str,
					ui.with_alpha(ui.COL_RED, 0.9 * err_alpha), ui.FONT_SIZE_XS, .Mono)
				sx += err_w + 8
			}
		}

		// Right-aligned: active stream_id + TF.
		right_x := viewport_w - 8
		tf_opts := TF_OPTIONS
		tf_str := tf_opts[state.active_tf_idx]
		tf_w := state.text.measure(ui.FONT_SIZE_XS, tf_str).x
		right_x -= tf_w
		ui.push_text(&state.cmd_buf, {right_x, sy}, tf_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		right_x -= 8

		active_stream_id := streams.registry_active_stream_id(&state.stream_registry)
		if len(active_stream_id) > 0 {
			id_w := state.text.measure(ui.FONT_SIZE_XS, active_stream_id).x
			right_x -= id_w
			ui.push_text(&state.cmd_buf, {right_x, sy}, active_stream_id, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		}
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

	// --- Toast notification (brief feedback, fades after ~90 frames / 1.5s) ---
	if state.toast.len > 0 && state.frame > 0 && state.toast.frame > 0 {
		elapsed := state.frame - state.toast.frame
		TOAST_DURATION :: u64(90)
		if elapsed < TOAST_DURATION {
			toast_str := string(state.toast.text[:state.toast.len])
			tw := state.text.measure(ui.FONT_SIZE_SM, toast_str).x
			pill_w := tw + 20
			pill_h := f32(24)
			px := (viewport_w - pill_w) * 0.5
			py := viewport_h - 60
			alpha := f32(1.0)
			if elapsed > 60 {
				alpha = 1.0 - f32(elapsed - 60) / f32(TOAST_DURATION - 60)
			}
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect  = ui.rect_xywh(px, py, pill_w, pill_h),
				color = ui.with_alpha(ui.COL_SURFACE_2, alpha * 0.9),
			})
			ui.push_text(&state.cmd_buf,
				{px + 10, py + pill_h * 0.5 + ui.FONT_SIZE_SM * 0.35},
				toast_str, ui.with_alpha(ui.COL_TEXT_PRIMARY, alpha), ui.FONT_SIZE_SM, .Mono)
		}
	}

	// --- TF OSD: large overlay text when TF changes in zen mode (PRD-0007 M4.3) ---
	if state.zen.active && state.zen.tf_osd_frame > 0 && state.frame > state.zen.tf_osd_frame {
		osd_elapsed := state.frame - state.zen.tf_osd_frame
		OSD_DURATION :: u64(90) // ~1.5s at 60fps
		if osd_elapsed < OSD_DURATION {
			tf_opts := TF_OPTIONS
			osd_str_buf: [16]u8
			osd_str := fmt.bprintf(osd_str_buf[:], "TF: %s", tf_opts[state.active_tf_idx])
			osd_w := state.text.measure(ui.FONT_SIZE_LG, osd_str).x
			osd_x := (viewport_w - osd_w) * 0.5
			osd_y := viewport_h * 0.45
			alpha := f32(1.0)
			if osd_elapsed > 60 {
				alpha = 1.0 - f32(osd_elapsed - 60) / f32(OSD_DURATION - 60)
			}
			ui.push_text(&state.cmd_buf, {osd_x, osd_y},
				osd_str, ui.with_alpha(ui.COL_TEXT_PRIMARY, alpha), ui.FONT_SIZE_LG, .Bold)
		}
	}

	return &state.cmd_buf
}
