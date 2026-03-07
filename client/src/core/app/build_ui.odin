package app

import "mr:ports"
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
				WIDGET_LABELS :: [12]string{"Candle", "Stats", "Counter", "Heatmap", "VPVR", "Trades", "Orderbook", "DOM", "Empty", "Analytics", "Session VPVR", "TPO"}
				labels := WIDGET_LABELS
				menu_items: [ui.CONTEXT_MENU_MAX_ITEMS]ui.Context_Menu_Item
				menu_count := 0
				for i in 0 ..< 10 {
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
					if menu_res.clicked_idx < 10 {
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
						persist_layout_v6(state)
					} else if menu_res.clicked_idx == expand_down_idx && cci >= 0 && cci < state.world.count {
						rs := state.world.spans[cci].row_span
						if rs < 1 do rs = 1
						if rs < 4 { state.world.spans[cci].row_span = rs + 1 }
						persist_layout_v6(state)
					} else if menu_res.clicked_idx == reset_size_idx && cci >= 0 && cci < state.world.count {
						state.world.spans[cci].col_span = 1
						state.world.spans[cci].row_span = 1
						persist_layout_v6(state)
					} else if menu_res.clicked_idx == clear_all_idx {
						state.world.count = 0
						state.overlays.show_widget_catalog = true
						state.overlays.catalog_step = 0
						persist_layout_v6(state)
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
