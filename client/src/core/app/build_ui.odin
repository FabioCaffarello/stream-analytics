package app

import "core:fmt"
import "mr:model"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:ui"
import "mr:widgets"

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
grid_rect_for_panel :: proc(
	grid: ui.Grid_Result,
	visible: [ui.PANEL_COUNT]bool,
	panel_idx: int,
) -> (rect: ui.Rect, ok: bool) {
	if panel_idx < 0 || panel_idx >= ui.PANEL_COUNT do return
	if !visible[panel_idx] do return
	cell_idx := 0
	for i in 0 ..< panel_idx {
		if visible[i] do cell_idx += 1
	}
	return grid.rects[cell_idx], true
}

build_ui :: proc(state: ^App_State, input: ports.Input_State) -> ^ui.Command_Buffer {
	ui.reset(&state.cmd_buf)
	state.last_indicator_probe = {}

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
	if state.zen_mode {
		// Fade top bar.
		if input.mouse.pos.y < ZEN_TRIGGER_TOP {
			state.zen_top_alpha = min(state.zen_top_alpha + ZEN_FADE_SPEED, 1.0)
		} else {
			state.zen_top_alpha = max(state.zen_top_alpha - ZEN_FADE_SPEED, 0.0)
		}
		// Fade bottom status bar.
		if input.mouse.pos.y > viewport_h - ZEN_TRIGGER_BOTTOM {
			state.zen_bottom_alpha = min(state.zen_bottom_alpha + ZEN_FADE_SPEED, 1.0)
		} else {
			state.zen_bottom_alpha = max(state.zen_bottom_alpha - ZEN_FADE_SPEED, 0.0)
		}
		// Fade left nav rail.
		if input.mouse.pos.x < ZEN_TRIGGER_LEFT {
			state.zen_left_alpha = min(state.zen_left_alpha + ZEN_FADE_SPEED, 1.0)
		} else {
			state.zen_left_alpha = max(state.zen_left_alpha - ZEN_FADE_SPEED, 0.0)
		}
	}

	// --- Top bar: title + connection status ---
	zen_skip_chrome := state.zen_mode && state.zen_top_alpha <= 0
	zen_compact := state.zen_mode && !zen_skip_chrome
	if !zen_skip_chrome {
		draw_top_bar(state, input, viewport_w, zen_compact)
	}

	pad := f32(2)
	if viewport_w < 420 do pad = 1
	gap := f32(2)

	STATUS_BAR_H :: f32(16)
	top_bar_h := TOP_BAR_H
	workspace: ui.Rect
	if state.zen_mode {
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

	// --- Two-zone sidebar: nav rail + detail panel ---
	sidebar_layout := ui.compute_sidebar_layout(workspace, state.detail_panel_expanded, mobile, state.detail_panel_w)

	// Nav rail (always visible on desktop, hidden in zen mode unless mouse near left).
	zen_skip_sidebar := state.zen_mode && state.zen_left_alpha <= 0
	if !mobile && !zen_skip_sidebar {
		NAV_ITEMS :: [3]ui.Nav_Rail_Item{
			{icon = "D", label = "Dashboard"},
			{icon = "V", label = "Venues"},
			{icon = "G", label = "Settings"},
		}
		nav_items := NAV_ITEMS
		active_idx := int(state.active_route)
		nav_res := ui.draw_nav_rail(&state.cmd_buf, sidebar_layout.nav_rail_rect,
			nav_items[:], active_idx, pointer, state.text.measure)
		if nav_res.clicked_route_idx >= 0 && nav_res.clicked_route_idx < len(NAV_ITEMS) {
			new_route := Route(nav_res.clicked_route_idx)
			if new_route != state.active_route {
				queue_ui_action(state, UI_Action{kind = .Navigate_Route, route = new_route})
			}
		}

		// Detail panel (collapsible, route-specific content).
		if state.detail_panel_expanded && sidebar_layout.detail_rect.size.x > 0 {
			detail_res := ui.draw_detail_panel_frame(&state.cmd_buf, sidebar_layout.detail_rect)
			switch state.active_route {
			case .Dashboard:
				draw_dashboard_detail(state, detail_res.content_rect, pointer)
			case .Markets:
				draw_markets_detail(state, detail_res.content_rect, pointer)
			case .Settings:
				draw_settings_detail(state, detail_res.content_rect)
			}

			// Resize handle on right edge of detail panel.
			dr := sidebar_layout.detail_rect
			handle_rect := ui.Rect{
				pos  = {ui.rect_right(dr) - ui.RESIZE_HANDLE_W, dr.pos.y},
				size = {ui.RESIZE_HANDLE_W, dr.size.y},
			}
			handle_hovered := ui.rect_contains(handle_rect, pointer.pos)
			if handle_hovered || state.detail_resizing {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect  = handle_rect,
					color = ui.with_alpha(ui.COL_BLUE, 0.25),
				})
			}
			if handle_hovered && pointer.left_pressed {
				state.detail_resizing = true
			}
			if state.detail_resizing {
				if pointer.left_down {
					state.detail_panel_w = clamp(
						pointer.pos.x - sidebar_layout.nav_rail_rect.pos.x - ui.NAV_RAIL_W,
						ui.DETAIL_PANEL_W_MIN, ui.DETAIL_PANEL_W_MAX,
					)
				} else {
					state.detail_resizing = false
				}
			}
		}
	}

	if !state.zen_mode {
		workspace = sidebar_layout.workspace_rect
	}

	switch state.active_route {
	case .Dashboard:
		if state.focus_mode {
		// ═══════════════════════════════════════════════════════════
		// FOCUS MODE — scalper cockpit (candle 75% + orderbook 25%)
		// ═══════════════════════════════════════════════════════════
		focus_gap := f32(4)
		candle_w := (workspace.size.x - focus_gap) * 0.75
		ob_w := workspace.size.x - candle_w - focus_gap

		candle_rect := ui.rect_xywh(workspace.pos.x, workspace.pos.y, candle_w, workspace.size.y)
		ob_rect := ui.rect_xywh(workspace.pos.x + candle_w + focus_gap, workspace.pos.y, ob_w, workspace.size.y)

		// Candle widget (active stream).
		candle_health_label, candle_health_detail, candle_health_color := build_candle_health_ui(state)
		tf_opts := TF_OPTIONS
		widgets.candle_widget(&state.cmd_buf, widgets.Candle_Widget_Data{
			store                 = &state.candle_store,
			heatmap_store         = &state.heatmap_store,
			vpvr_store            = &state.vpvr_store,
			viewport              = candle_rect,
			text                  = state.text,
			input                 = input,
			scroll_x              = &state.candle_scroll_x,
			zoom_level            = &state.candle_zoom,
			health_label          = candle_health_label,
			health_detail         = candle_health_detail,
			health_color          = candle_health_color,
			tf_label              = tf_opts[state.active_tf_idx],
			heatmap_live          = state.active_has_live_heatmap,
			heatmap_synth         = !state.active_has_live_heatmap && state.heatmap_store.count > 0,
			vpvr_live             = state.active_has_live_vpvr,
			vpvr_synth            = !state.active_has_live_vpvr && state.vpvr_store.count > 0,
			show_volume           = &state.show_candle_vol,
			show_heatmap_overlay  = &state.show_candle_heatmap,
			show_vpvr_overlay     = &state.show_candle_vpvr,
			heatmap_intensity_idx = &state.candle_heatmap_intensity_idx,
			crosshair             = &state.candle_crosshair,
			chart_type            = &state.candle_chart_type,
			show_ma               = state.show_ma,
			show_bbands           = state.show_bbands,
			show_vwap             = state.show_vwap,
			show_rsi              = state.show_rsi,
			show_macd             = state.show_macd,
			show_funding          = state.show_funding,
			show_liq              = state.show_liq,
			show_trade_counter    = state.show_trade_counter,
			stats_store           = &state.stats_store,
			draw_tools            = &state.draw_tools,
			footprint_store       = &state.footprint_store,
			ma_periods            = state.ma_periods,
			bb_period             = state.bb_period,
			bb_sigma              = state.bb_sigma,
			rsi_period            = state.rsi_period,
			macd_fast             = state.macd_fast,
			macd_slow             = state.macd_slow,
			macd_signal           = state.macd_signal,
			pointer               = pointer,
			now_ms                = current_now_ms(state),
			timeframe_ms          = active_timeframe_ms(state),
			indicator_probe       = &state.last_indicator_probe,
		})

		// Orderbook widget (active stream).
		ob_max_rows := 20
		if ob_rect.size.y < 170 {
			ob_max_rows = 12
		} else if ob_rect.size.y < 230 {
			ob_max_rows = 16
		}
		ob_price_group := f64(10.0)
		if state.orderbook_store.last_price > 0 {
			base := orderbook_auto_price_group(state.orderbook_store.last_price)
			state.ob_group_options = {base * 0.1, base, base * 10, base * 100, base * 1000}
			state.ob_group_count = 5
			for i in 0 ..< 5 {
				lbuf := &state.ob_group_labels[i]
				lbuf^ = {}
				if state.ob_group_options[i] >= 1 {
					_ = fmt.bprintf(lbuf[:], "%.0f", state.ob_group_options[i])
				} else {
					_ = fmt.bprintf(lbuf[:], "%g", state.ob_group_options[i])
				}
			}
			idx := clamp(state.ob_group_idx, 0, 4)
			ob_price_group = state.ob_group_options[idx]
		}
		ob_label_strs: [5]string
		for i in 0 ..< state.ob_group_count {
			ob_label_strs[i] = string(state.ob_group_labels[i][:cstring_len(&state.ob_group_labels[i])])
		}
		widgets.orderbook_widget(&state.cmd_buf, widgets.Orderbook_Widget_Data{
			store         = &state.orderbook_store,
			viewport      = ob_rect,
			text          = state.text,
			scroll_y      = &state.ob_scroll_y,
			input         = input,
			price_group   = ob_price_group,
			max_rows      = ob_max_rows,
			group_options = ob_label_strs[:state.ob_group_count],
			group_idx     = &state.ob_group_idx,
			pointer       = pointer,
		})

		// Focus mode label.
		focus_label := "FOCUS  Esc:exit"
		flw := state.text.measure(ui.FONT_SIZE_XS, focus_label).x
		ui.push_text(&state.cmd_buf, {workspace.pos.x + workspace.size.x - flw - 4, workspace.pos.y + 12},
			focus_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

		} else if state.compare_mode && state.compare_count >= 2 {
		// ═══════════════════════════════════════════════════════════
		// COMPARE MODE — side-by-side widget comparison
		// ═══════════════════════════════════════════════════════════

		// Control bar: widget type selector + info.
		ctrl_h := f32(22)
		ctrl_rect := ui.rect_cut_top(&workspace, ctrl_h)
		ui.rect_cut_top(&workspace, 4) // gap

		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect = ctrl_rect, color = ui.COL_SURFACE_1,
		})

		cr := ui.rect_pad_xy(ctrl_rect, 8, 2)
		// Compare mode label.
		cmp_label := "COMPARE"
		ui.push_text(&state.cmd_buf, {cr.pos.x, cr.pos.y + ctrl_h * 0.5 + ui.FONT_SIZE_XS * 0.25},
			cmp_label, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
		cmp_cursor := cr.pos.x + state.text.measure(ui.FONT_SIZE_XS, cmp_label).x + 10

		// Widget type segmented control.
		cmp_opts := COMPARE_WIDGET_OPTIONS
		seg_w := f32(150)
		seg_rect := ui.rect_xywh(cmp_cursor, cr.pos.y, seg_w, cr.size.y)
		seg_res := ui.segmented_control(&state.cmd_buf, seg_rect, cmp_opts[:], state.compare_widget_idx,
			pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
		if seg_res.changed {
			state.compare_widget_idx = seg_res.index
		}
		cmp_cursor += seg_w + 10

		// Stream count.
		count_buf: [16]u8
		count_str := fmt.bprintf(count_buf[:], "%d streams  Tab:add  Esc:exit", state.compare_count)
		ui.push_text(&state.cmd_buf, {cmp_cursor, cr.pos.y + ctrl_h * 0.5 + ui.FONT_SIZE_XS * 0.25},
			count_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

		// Compare grid.
		cmp_grid := ui.build_compare_grid(state.compare_count, gap)
		cmp_result := ui.compute_grid(cmp_grid, workspace)

		// Render a panel for each compare slot.
		for ci in 0 ..< state.compare_count {
			cell_rect := cmp_result.rects[ci]
			sid := state.compare_slots[ci]
			reg := state.stream_views
			slot_idx := stream_view_find_slot(reg, sid)
			if slot_idx < 0 do continue
			slot := &reg.slots[slot_idx]

			// Ensure stream info.
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}

			// Mini-header with venue:symbol.
			header_h := f32(18)
			header_rect := ui.rect_cut_top(&cell_rect, header_h)
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = header_rect, color = ui.with_alpha(ui.COL_SURFACE_1, 0.9)})

			venue_label := "---"
			if slot.has_stream_info && len(slot.stream_info.venue) > 0 {
				vl_buf: [64]u8
				venue_label = fmt.bprintf(vl_buf[:], "%s:%s", slot.stream_info.venue, slot.stream_info.symbol)
			}
			ui.push_text(&state.cmd_buf, {header_rect.pos.x + 6, header_rect.pos.y + header_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				venue_label, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)

			// Render selected widget type.
			switch state.compare_widget_idx {
			case 0: // Orderbook
				ob_pg := f64(10.0)
				if slot.orderbook_store.last_price > 0 {
					ob_pg = orderbook_auto_price_group(slot.orderbook_store.last_price)
				}
				ob_max := 16
				if cell_rect.size.y < 170 do ob_max = 10
				widgets.orderbook_widget(&state.cmd_buf, widgets.Orderbook_Widget_Data{
					store       = &slot.orderbook_store,
					viewport    = cell_rect,
					text        = state.text,
					scroll_y    = &state.compare_ob_scroll[ci],
					input       = input,
					price_group = ob_pg,
					max_rows    = ob_max,
					group_idx   = &state.compare_ob_grp_idx[ci],
					pointer     = pointer,
				})
			case 1: // Trades
				widgets.trades_widget(&state.cmd_buf, widgets.Trades_Widget_Data{
					store      = &slot.trades_store,
					viewport   = cell_rect,
					text       = state.text,
					scroll_y   = &state.compare_trade_scroll[ci],
					input      = input,
					filter_idx = &state.compare_trade_filter[ci],
					pointer    = pointer,
					now_ms     = current_now_ms(state),
				})
				case 2: // Candles
					tf_opts := TF_OPTIONS
					widgets.candle_widget(&state.cmd_buf, widgets.Candle_Widget_Data{
						store         = &slot.candle_store,
						heatmap_store = &slot.heatmap_store,
						vpvr_store    = &slot.vpvr_store,
						viewport     = cell_rect,
						text         = state.text,
						input        = input,
						scroll_x     = &state.compare_candle_scroll_x[ci],
						zoom_level   = &state.compare_candle_zoom[ci],
						health_label = "---",
						health_color = ui.COL_TEXT_MUTED,
						tf_label     = tf_opts[state.active_tf_idx],
						heatmap_live  = false,
						heatmap_synth = false,
						vpvr_live     = false,
						vpvr_synth    = false,
						show_volume  = &state.compare_show_candle_vol[ci],
						show_heatmap_overlay = &state.compare_show_heatmap[ci],
						show_vpvr_overlay    = &state.compare_show_vpvr[ci],
						heatmap_intensity_idx = &state.compare_heatmap_intensity_idx[ci],
						show_funding = state.show_funding,
						show_liq     = state.show_liq,
						show_trade_counter = state.show_trade_counter,
						footprint_store = nil,
						pointer      = pointer,
						now_ms       = current_now_ms(state),
						timeframe_ms = active_timeframe_ms(state),
					})
				}
		}
	} else {
		// ═══════════════════════════════════════════════════════════
		// NORMAL MODE — grid layout (preset or free-form)
		// ═══════════════════════════════════════════════════════════

			// Compute grid layout.
			grid_def: ui.Grid_Def
			if state.layout_mode == .Custom {
				// Free-form: auto-reflow based on cell_count, apply per-cell spans.
				grid_def = ui.build_auto_grid(state.cell_count, gap)
				for ci in 0 ..< state.cell_count {
					ca := state.cell_assignments[ci]
					cs := ca.col_span > 1 ? ca.col_span : 1
					rs := ca.row_span > 1 ? ca.row_span : 1
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
				grid_def = ui.build_filtered_grid(base_grid_def, state.panel_visible, gap)
			}
			grid := ui.compute_grid(grid_def, workspace)

			// Drag-drop panel swap.
			if !mobile {
				swapped, swap_a, swap_b := ui.update_drag(
					&state.panel_drag, grid.rects, state.panel_visible,
					pointer, current_now_ms(state), f32(26))
				if swapped {
					ui.apply_panel_swap(&state.custom_grid_def, swap_a, swap_b)
				}
			}

			// Ensure focused_candle_cell_idx points to a valid candle cell.
			if state.focused_candle_cell_idx < 0 || state.focused_candle_cell_idx >= state.cell_count ||
				state.cell_assignments[state.focused_candle_cell_idx].widget != .Candle {
				state.focused_candle_cell_idx = -1
				for fi in 0 ..< state.cell_count {
					if state.cell_assignments[fi].widget == .Candle {
						state.focused_candle_cell_idx = fi
						break
					}
				}
			}

			// Scan for active crosshair (from previous frame) for sync across charts.
			sync_price := f64(0)
			sync_active := false
			for si in 0 ..< state.cell_count {
				if state.cell_assignments[si].widget != .Candle do continue
				if state.cell_assignments[si].crosshair.active {
					sync_price = state.cell_assignments[si].crosshair.price_at_y
					sync_active = true
					break
				}
			}

			// Dispatch widgets from cell_assignments.
			CELL_HDR_H :: f32(16)
			for ci in 0 ..< state.cell_count {
				if ci >= grid_def.cell_count do break
				cell := &state.cell_assignments[ci]
				cell_vp := grid.rects[ci]
				if cell_vp.size.x <= 0 || cell_vp.size.y <= 0 do continue

				// Active cell focus border: highlight the cell under the mouse.
				is_cell_focused := ui.rect_contains(cell_vp, input.mouse.pos)
				cell_border_color := is_cell_focused ? ui.COL_BORDER_STRONG : ui.COL_BORDER_SUBTLE
				// Top edge.
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = {pos = cell_vp.pos, size = {cell_vp.size.x, 1}}, color = cell_border_color,
				})
				// Bottom edge.
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = {pos = {cell_vp.pos.x, cell_vp.pos.y + cell_vp.size.y - 1}, size = {cell_vp.size.x, 1}}, color = cell_border_color,
				})
				// Left edge.
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = {pos = cell_vp.pos, size = {1, cell_vp.size.y}}, color = cell_border_color,
				})
				// Right edge.
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = {pos = {cell_vp.pos.x + cell_vp.size.x - 1, cell_vp.pos.y}, size = {1, cell_vp.size.y}}, color = cell_border_color,
				})

				// Per-cell stream header bar (PRD-0006-B M2).
				hdr_rect := ui.rect_cut_top(&cell_vp, CELL_HDR_H)
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = hdr_rect, color = ui.with_alpha(ui.COL_SURFACE_2, 0.7)})
				// Left: stream badge (clickable).
				badge_label := "Active"
				badge_buf: [40]u8
				if cell.stream_idx >= 0 && cell.stream_idx < STREAM_VIEW_CAP {
					if reg := state.stream_views; reg != nil && reg.slots[cell.stream_idx].used {
						slot := &reg.slots[cell.stream_idx]
						if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
						if slot.has_stream_info && len(slot.stream_info.venue) > 0 {
							badge_label = fmt.bprintf(badge_buf[:], "%s:%s", slot.stream_info.venue, slot.stream_info.symbol)
						}
					}
				}
				badge_w := state.text.measure(ui.FONT_SIZE_XS, badge_label).x + 12
				badge_rect := ui.rect_xywh(hdr_rect.pos.x + 2, hdr_rect.pos.y + 1, badge_w, CELL_HDR_H - 2)
				badge_hovered := ui.rect_contains(badge_rect, pointer.pos)
				badge_bg := badge_hovered ? ui.with_alpha(ui.COL_BLUE, 0.2) : ui.with_alpha(ui.COL_BLUE, 0.1)
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = badge_rect, color = badge_bg})
				ui.push_text(&state.cmd_buf,
					{badge_rect.pos.x + 6, hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35},
					badge_label, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
				if badge_hovered && pointer.left_pressed {
					queue_ui_action(state, UI_Action{kind = .Open_Cell_Stream_Picker, cell_idx = ci})
				}
				// Right: close button (only when 2+ cells).
				close_inset := f32(0)
				if state.cell_count > 1 {
					close_sz := f32(14)
					close_x := ui.rect_right(hdr_rect) - close_sz - 2
					close_y := hdr_rect.pos.y + (CELL_HDR_H - close_sz) * 0.5
					close_res := ui.icon_button(&state.cmd_buf,
						ui.rect_xywh(close_x, close_y, close_sz, close_sz),
						"x", pointer, state.text.measure, ui.FONT_SIZE_XS)
					if close_res.clicked {
						queue_ui_action(state, UI_Action{kind = .Remove_Cell, cell_idx = ci})
					}
					close_inset = close_sz + 4
				}
				// TF badge for candle cells (positioned before widget label).
				tf_inset := f32(0)
				if cell.widget == .Candle {
					tf_opts := TF_OPTIONS
					eff_tf := cell_effective_tf_idx(state, cell)
					tf_str := tf_opts[eff_tf] if eff_tf >= 0 && eff_tf < len(tf_opts) else tf_opts[0]
					is_per_cell_tf := cell.tf_idx >= 0
					tf_color := is_per_cell_tf ? ui.COL_BLUE : ui.COL_YELLOW_ACCENT
					tf_w := state.text.measure(ui.FONT_SIZE_XS, tf_str).x + 8
					tf_x := ui.rect_right(hdr_rect) - tf_w - 4 - close_inset
					tf_rect := ui.rect_xywh(tf_x, hdr_rect.pos.y + 1, tf_w, CELL_HDR_H - 2)
					ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
						rect = tf_rect, color = ui.with_alpha(tf_color, 0.12),
					})
					ui.push_text(&state.cmd_buf,
						{tf_x + 4, hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35},
						tf_str, tf_color, ui.FONT_SIZE_XS, .Mono)
					tf_inset = tf_w + 4
					// Click TF badge → cycle through TF options for this cell.
					if pointer.left_pressed && ui.rect_contains(tf_rect, pointer.pos) {
						next_tf := (eff_tf + 1) % len(tf_opts)
						queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = ci, timeframe_idx = next_tf})
					}
				}
				// Widget type label.
				WIDGET_SHORT :: [9]string{"Candle", "Stats", "Counter", "HM", "VPVR", "Trades", "OB", "DOM", "--"}
				ws := WIDGET_SHORT
				wlabel := ws[int(cell.widget)]
				wlabel_w := state.text.measure(ui.FONT_SIZE_XS, wlabel).x
				ui.push_text(&state.cmd_buf,
					{ui.rect_right(hdr_rect) - wlabel_w - 4 - close_inset - tf_inset, hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35},
					wlabel, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

				stores := resolve_stores_for_cell(state, cell, ci)
				cell_stream_id_buf: [streams.STREAM_ID_CAP]u8
				cell_stream_id := streams.registry_active_stream_id(&state.stream_registry)
				cell_stream_state := state.active_stream_state
				if cell.stream_idx >= 0 && cell.stream_idx < STREAM_VIEW_CAP {
					if reg := state.stream_views; reg != nil && reg.slots[cell.stream_idx].used {
						slot := &reg.slots[cell.stream_idx]
						if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
						if slot.has_stream_info {
							cell_stream_id = build_stream_id_from_market_into(cell_stream_id_buf[:], slot.stream_info.venue, slot.stream_info.symbol)
							if h := streams.registry_get(&state.stream_registry, cell_stream_id); h != nil {
								cell_stream_state = h.status.state
							}
						}
					}
				}

				switch cell.widget {
				case .Candle:
					candle_health_label, candle_health_detail, candle_health_color := build_candle_health_ui(state)
					tf_opts := TF_OPTIONS
					is_active := cell.stream_idx < 0
					prev_show_vol := cell.show_vol
					prev_show_heatmap := cell.show_heatmap
					prev_show_vpvr := cell.show_vpvr
					prev_heatmap_idx := cell.heatmap_intensity_idx
					// Track focused candle cell for keyboard shortcuts.
					if cell.crosshair.active {
						state.focused_candle_cell_idx = ci
					}
					cell_indicator_probe := (^widgets.Indicator_Render_Probe)(nil)
					if ci == state.focused_candle_cell_idx {
						cell_indicator_probe = &state.last_indicator_probe
					}
					widgets.candle_widget(&state.cmd_buf, widgets.Candle_Widget_Data{
						store                 = stores.candle,
						heatmap_store         = stores.heatmap,
						vpvr_store            = stores.vpvr,
						viewport              = cell_vp,
						text                  = state.text,
						input                 = input,
						scroll_x              = &cell.candle_scroll_x,
						zoom_level            = &cell.candle_zoom,
						health_label          = candle_health_label,
						health_detail         = candle_health_detail,
						health_color          = candle_health_color,
						tf_label              = tf_opts[cell_effective_tf_idx(state, cell)],
						stream_id             = cell_stream_id,
						stream_state          = cell_stream_state,
						heatmap_live          = is_active && state.active_has_live_heatmap,
						heatmap_synth         = is_active && !state.active_has_live_heatmap && stores.heatmap != nil && stores.heatmap.count > 0,
						vpvr_live             = is_active && state.active_has_live_vpvr,
						vpvr_synth            = is_active && !state.active_has_live_vpvr && stores.vpvr != nil && stores.vpvr.count > 0,
						show_volume           = &cell.show_vol,
						show_heatmap_overlay  = &cell.show_heatmap,
						show_vpvr_overlay     = &cell.show_vpvr,
						heatmap_intensity_idx = &cell.heatmap_intensity_idx,
						crosshair             = &cell.crosshair,
						chart_type            = &cell.chart_type,
						show_ma               = cell.show_ma,
						show_bbands           = cell.show_bbands,
						show_vwap             = cell.show_vwap,
						show_rsi              = cell.show_rsi,
						show_macd             = cell.show_macd,
						show_funding          = cell.show_funding,
						show_liq              = cell.show_liq,
						show_trade_counter    = cell.show_trade_counter,
						stats_store           = stores.stats,
						draw_tools            = &state.draw_tools,
						footprint_store       = is_active ? &state.footprint_store : nil,
						ma_periods            = cell.ma_periods,
						bb_period             = cell.bb_period,
						bb_sigma              = cell.bb_sigma,
						rsi_period            = cell.rsi_period,
						macd_fast             = cell.macd_fast,
						macd_slow             = cell.macd_slow,
						macd_signal           = cell.macd_signal,
						pointer               = pointer,
						now_ms                = current_now_ms(state),
						timeframe_ms          = active_timeframe_ms(state),
						sync_price            = sync_active && !cell.crosshair.active ? sync_price : 0,
						sync_active           = sync_active && !cell.crosshair.active,
						indicator_probe       = cell_indicator_probe,
						sub_main_split        = &cell.sub_main_split,
						sub_ratios            = &cell.sub_ratios,
						sub_resize_idx        = &cell.sub_resize_idx,
					})
					// Persist candle toggle changes from the primary (active stream) cell.
					if is_active {
						persisted := false
						if cell.show_vol != prev_show_vol {
							state.show_candle_vol = cell.show_vol
							services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_VOL,
								cell.show_vol ? "1" : "0")
							persisted = true
						}
						if cell.show_heatmap != prev_show_heatmap {
							state.show_candle_heatmap = cell.show_heatmap
							services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_HEATMAP,
								cell.show_heatmap ? "1" : "0")
							persisted = true
						}
						if cell.show_vpvr != prev_show_vpvr {
							state.show_candle_vpvr = cell.show_vpvr
							services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_VPVR,
								cell.show_vpvr ? "1" : "0")
							persisted = true
						}
						if cell.heatmap_intensity_idx != prev_heatmap_idx {
							state.candle_heatmap_intensity_idx = cell.heatmap_intensity_idx
							idx_buf: [4]u8
							services.settings_set(&state.settings, services.SETTING_CANDLE_HEATMAP_INTENSITY_IDX,
								fmt.bprintf(idx_buf[:], "%d", cell.heatmap_intensity_idx))
							persisted = true
						}
						if persisted {
							services.settings_flush(&state.settings)
						}
					}

				case .Stats:
					widgets.stats_widget(&state.cmd_buf, widgets.Stats_Widget_Data{
						store    = stores.stats,
						viewport = cell_vp,
						text     = state.text,
						stream_id = cell_stream_id,
						stream_state = cell_stream_state,
					})

				case .Counter:
					// Use candle store buy_vol/sell_vol (much denser than stats liq data).
					counter_candle := stores.candle
					if counter_candle != nil && counter_candle.count > 0 {
						stats_buf: [services.CANDLE_CAP]model.Stat
						sc := 0
						for ci in 0 ..< counter_candle.count {
							c := services.get_candle(counter_candle, ci)
							stats_buf[sc] = model.Stat{
								unix       = c.window_start_ts / 1000,
								tbuy       = c.buy_vol,
								tsell      = c.sell_vol,
								mark_price = c.close,
							}
							sc += 1
						}
						x_min, x_max: f64
						if sc > 0 {
							x_min = f64(stats_buf[0].unix) - 60
							x_max = f64(stats_buf[sc - 1].unix) + 60
						}
						widgets.trade_counter(&state.cmd_buf, widgets.Trade_Counter_Data{
							stats         = stats_buf[:sc],
							viewport      = cell_vp,
							timeframe     = 60,
							x_min         = x_min,
							x_max         = x_max,
							bar_width_pct = CANDLE_WIDTH_PCT,
							text          = state.text,
						})
					}

				case .Heatmap:
					widgets.heatmap_widget(&state.cmd_buf, widgets.Heatmap_Widget_Data{
						store    = stores.heatmap,
						viewport = cell_vp,
						text     = state.text,
						input    = input,
						pointer  = pointer,
					})

				case .VPVR:
					widgets.vpvr_widget(&state.cmd_buf, widgets.VPVR_Widget_Data{
						store    = stores.vpvr,
						viewport = cell_vp,
						text     = state.text,
						input    = input,
					})

				case .Trades:
					widgets.trades_widget(&state.cmd_buf, widgets.Trades_Widget_Data{
						store      = stores.trades,
						viewport   = cell_vp,
						text       = state.text,
						scroll_y   = &cell.trades_scroll_y,
						input      = input,
						filter_idx = &cell.trade_filter_idx,
						pointer    = pointer,
						now_ms     = current_now_ms(state),
						stream_id  = cell_stream_id,
						stream_state = cell_stream_state,
					})

				case .Orderbook:
					ob_max_rows := 20
					if cell_vp.size.y < 170 {
						ob_max_rows = 12
					} else if cell_vp.size.y < 230 {
						ob_max_rows = 16
					}
					ob_price_group := f64(10.0)
					if stores.orderbook != nil && stores.orderbook.last_price > 0 {
						base := orderbook_auto_price_group(stores.orderbook.last_price)
						state.ob_group_options = {base * 0.1, base, base * 10, base * 100, base * 1000}
						state.ob_group_count = 5
						for i in 0 ..< 5 {
							lbuf := &state.ob_group_labels[i]
							lbuf^ = {}
							if state.ob_group_options[i] >= 1 {
								_ = fmt.bprintf(lbuf[:], "%.0f", state.ob_group_options[i])
							} else {
								_ = fmt.bprintf(lbuf[:], "%g", state.ob_group_options[i])
							}
						}
						idx := clamp(cell.ob_group_idx, 0, 4)
						ob_price_group = state.ob_group_options[idx]
					}
					ob_label_strs: [5]string
					for i in 0 ..< state.ob_group_count {
						ob_label_strs[i] = string(state.ob_group_labels[i][:cstring_len(&state.ob_group_labels[i])])
					}
					widgets.orderbook_widget(&state.cmd_buf, widgets.Orderbook_Widget_Data{
						store         = stores.orderbook,
						viewport      = cell_vp,
						text          = state.text,
						scroll_y      = &cell.ob_scroll_y,
						input         = input,
						price_group   = ob_price_group,
						max_rows      = ob_max_rows,
						group_options = ob_label_strs[:state.ob_group_count],
						group_idx     = &cell.ob_group_idx,
						pointer       = pointer,
						stream_id     = cell_stream_id,
						stream_state  = cell_stream_state,
					})

				case .DOM:
					dom_price_group := f64(10.0)
					if stores.orderbook != nil && stores.orderbook.last_price > 0 {
						base := orderbook_auto_price_group(stores.orderbook.last_price)
						state.ob_group_options = {base * 0.1, base, base * 10, base * 100, base * 1000}
						state.ob_group_count = 5
						for i in 0 ..< 5 {
							lbuf := &state.ob_group_labels[i]
							lbuf^ = {}
							if state.ob_group_options[i] >= 1 {
								_ = fmt.bprintf(lbuf[:], "%.0f", state.ob_group_options[i])
							} else {
								_ = fmt.bprintf(lbuf[:], "%g", state.ob_group_options[i])
							}
						}
						idx := clamp(cell.dom_group_idx, 0, 4)
						dom_price_group = state.ob_group_options[idx]
					}
					dom_label_strs: [5]string
					for i in 0 ..< state.ob_group_count {
						dom_label_strs[i] = string(state.ob_group_labels[i][:cstring_len(&state.ob_group_labels[i])])
					}
					widgets.dom_widget(&state.cmd_buf, widgets.DOM_Widget_Data{
						orderbook     = stores.orderbook,
						dom           = &state.dom_store,
						viewport      = cell_vp,
						text          = state.text,
						input         = input,
						pointer       = pointer,
						group_options = dom_label_strs[:state.ob_group_count],
						group_idx     = &cell.dom_group_idx,
						price_group   = dom_price_group,
					})

				case .Empty:
					// Empty cell — just show background.
					ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = cell_vp, color = ui.COL_SURFACE_1})
				}
			}

			// --- Drag feedback (rendered after widgets for z-order) ---
			if !mobile && state.panel_drag.phase != .Idle {
				ui.draw_drag_feedback(&state.cmd_buf, &state.panel_drag, grid.rects, state.panel_visible)
			}

			// --- Right-click on cell → open context menu ---
			if !mobile && input.mouse.pressed[.Right] {
				for ci in 0 ..< state.cell_count {
					if ci >= grid_def.cell_count do break
					cell_vp := grid.rects[ci]
					if ui.rect_contains(cell_vp, pointer.pos) {
						state.cell_context_menu = ui.Context_Menu_State{
							open = true,
							pos  = pointer.pos,
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
				if cci >= 0 && cci < state.cell_count {
					current_widget = state.cell_assignments[cci].widget
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
					has_span := cci >= 0 && cci < state.cell_count &&
						(state.cell_assignments[cci].col_span > 1 || state.cell_assignments[cci].row_span > 1)
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
					menu_items[:menu_count], pointer, state.text.measure,
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
					} else if menu_res.clicked_idx == expand_right_idx && cci >= 0 && cci < state.cell_count {
						cs := state.cell_assignments[cci].col_span
						if cs < 1 do cs = 1
						if cs < 4 { state.cell_assignments[cci].col_span = cs + 1 }
						persist_layout_v4(state)
					} else if menu_res.clicked_idx == expand_down_idx && cci >= 0 && cci < state.cell_count {
						rs := state.cell_assignments[cci].row_span
						if rs < 1 do rs = 1
						if rs < 4 { state.cell_assignments[cci].row_span = rs + 1 }
						persist_layout_v4(state)
					} else if menu_res.clicked_idx == reset_size_idx && cci >= 0 && cci < state.cell_count {
						state.cell_assignments[cci].col_span = 1
						state.cell_assignments[cci].row_span = 1
						persist_layout_v4(state)
					} else if menu_res.clicked_idx == clear_all_idx {
						state.cell_count = 0
						state.show_widget_catalog = true
						state.catalog_step = 0
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
						border_x := ui.rect_right(grid.rects[0]) // approximate — use first row
						// Find any cell in column ci to get its right edge.
						for gi in 0 ..< grid_def.cell_count {
							gc := grid_def.cells[gi]
							if gc.col == ci && gc.col_span == 1 {
								border_x = ui.rect_right(grid.rects[gi])
								break
							}
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

		// --- Venue dropdown (rendered after widgets for z-order) ---
		if !state.compare_mode {
			draw_venue_dropdown(state, pointer, viewport_w)
		}

	case .Markets:
		build_markets_page(state, workspace, pointer)

	case .Settings:
		build_settings_page(state, workspace, pointer)
	}

	// --- Status bar (bottom 20px) — hidden in zen mode unless mouse near bottom ---
	zen_skip_status := state.zen_mode && state.zen_bottom_alpha <= 0
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
		switch state.active_stream_state {
		case .Live:
			health_label = "LIVE"
			health_color = ui.COL_GREEN
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
		hud_label := state.telemetry_hud_enabled ? "HUD*" : "HUD"
		hud_rect := ui.rect_xywh(sx, bar_y + 1, 38, STATUS_BAR_H - 2)
		hud_btn := ui.button(&state.cmd_buf, hud_rect, hud_label, pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
		if hud_btn.clicked {
			queue_ui_action(state, UI_Action{kind = .Toggle_Telemetry_HUD})
		}
		sx += hud_rect.size.x + 8
		if state.active_stream_state == .Desync {
			rs_rect := ui.rect_xywh(sx, bar_y + 1, 48, STATUS_BAR_H - 2)
			rs_btn := ui.button(&state.cmd_buf, rs_rect, "Resync", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
			if rs_btn.clicked {
				queue_ui_action(state, UI_Action{kind = .Resync_Active_Stream})
			}
			sx += rs_rect.size.x + 8
		}

		rtt_buf: [24]u8
		rtt_str := fmt.bprintf(rtt_buf[:], "RTT:%dms", max(state.active_stream_rtt_ms, 0))
		ui.push_text(&state.cmd_buf, {sx, sy}, rtt_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, rtt_str).x + 8

		lag_buf: [24]u8
		lag_str := fmt.bprintf(lag_buf[:], "LAG:%dms", max(state.active_stream_lag_ms, 0))
		lag_color := state.active_stream_lag_ms > 4_000 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {sx, sy}, lag_str, lag_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, lag_str).x + 8

		last_age_ms := i64(0)
		if now_ms := current_now_ms(state); now_ms > 0 && state.active_stream_last_msg_ts_ms > 0 {
			last_age_ms = max(now_ms - state.active_stream_last_msg_ts_ms, 0)
		}
		last_buf: [24]u8
		last_str := fmt.bprintf(last_buf[:], "LAST:%dms", last_age_ms)
		last_color := last_age_ms > 8_000 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {sx, sy}, last_str, last_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, last_str).x + 8

		ack_buf: [20]u8
		ack_str := fmt.bprintf(ack_buf[:], "ACK:%d", max(state.active_stream_subscribe_acks, 0))
		ui.push_text(&state.cmd_buf, {sx, sy}, ack_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, ack_str).x + 8

		dr_buf: [24]u8
		dr_str := fmt.bprintf(dr_buf[:], "DROP:%d", max(state.active_stream_drop_count, 0))
		dr_color := state.active_stream_drop_count > 0 ? ui.COL_RED : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {sx, sy}, dr_str, dr_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, dr_str).x + 8

		rc_buf: [24]u8
		rc_str := fmt.bprintf(rc_buf[:], "RC:%d", max(state.active_stream_reconnect_count, 0))
		rc_color := state.active_stream_reconnect_count > 0 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
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
		if state.telemetry_hud_enabled {
			mps_buf: [32]u8
			mps_str := fmt.bprintf(mps_buf[:], "MPS:%.1f", state.active_stream_msg_rate)
			ui.push_text(&state.cmd_buf, {sx, sy}, mps_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, mps_str).x + 8

			bytes_per_sec := i64(state.active_stream_bytes_rate)
			bps_buf: [32]u8
			bps_str := ""
			if bytes_per_sec >= 1024 * 1024 {
				bps_str = fmt.bprintf(bps_buf[:], "BPS:%dMB/s", bytes_per_sec / (1024 * 1024))
			} else {
				bps_str = fmt.bprintf(bps_buf[:], "BPS:%dKB/s", bytes_per_sec / 1024)
			}
			ui.push_text(&state.cmd_buf, {sx, sy}, bps_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, bps_str).x + 8

			cb_buf: [20]u8
			cb_str := fmt.bprintf(cb_buf[:], "CB:%d", max(state.active_stream_candle_backlog, 0))
			cb_color := state.active_stream_candle_backlog > 0 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
			ui.push_text(&state.cmd_buf, {sx, sy}, cb_str, cb_color, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, cb_str).x + 8

			arena_buf: [40]u8
			arena_str := fmt.bprintf(arena_buf[:], "Arena:%d/%d", ui.frame_arena_usage(&state.cmd_buf), ui.frame_arena_capacity(&state.cmd_buf))
			ui.push_text(&state.cmd_buf, {sx, sy}, arena_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, arena_str).x + 8

			pm_buf: [32]u8
			pm_str := fmt.bprintf(pm_buf[:], "PM:%d", state.active_stream_parsed_msgs_total)
			ui.push_text(&state.cmd_buf, {sx, sy}, pm_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, pm_str).x + 8

			pb_buf: [32]u8
			pb_mb := i64(state.active_stream_parsed_bytes_total / u64(1024 * 1024))
			pb_str := fmt.bprintf(pb_buf[:], "PB:%dMB", pb_mb)
			ui.push_text(&state.cmd_buf, {sx, sy}, pb_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, pb_str).x + 8
		}

		// Frame time p50.
		if state.frame_time_count > 0 {
			p50, _, _ := frame_time_percentiles(state)
			fps_approx := p50 > 0 ? 1_000_000 / p50 : 0
			f_buf: [32]u8
			f_str := fmt.bprintf(f_buf[:], "FPS:%d", fps_approx)
			fps_color := fps_approx < 30 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
			ui.push_text(&state.cmd_buf, {sx, sy}, f_str, fps_color, ui.FONT_SIZE_XS, .Mono)
			sx += state.text.measure(ui.FONT_SIZE_XS, f_str).x + 10
		}

		// Data source badges: HM, VP, CD.
		hm_live := state.active_has_live_heatmap
		hm_synth := !hm_live && state.heatmap_store.count > 0
		hm_label := hm_live ? "HM:LIVE" : (hm_synth ? "HM:SYNTH" : "HM:--")
		hm_color := hm_live ? ui.COL_GREEN : (hm_synth ? ui.COL_WARNING : ui.COL_TEXT_MUTED)
		ui.push_text(&state.cmd_buf, {sx, sy}, hm_label, hm_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, hm_label).x + 8

		vp_live := state.active_has_live_vpvr
		vp_synth := !vp_live && state.vpvr_store.count > 0
		vp_label := vp_live ? "VP:LIVE" : (vp_synth ? "VP:SYNTH" : "VP:--")
		vp_color := vp_live ? ui.COL_GREEN : (vp_synth ? ui.COL_WARNING : ui.COL_TEXT_MUTED)
		ui.push_text(&state.cmd_buf, {sx, sy}, vp_label, vp_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, vp_label).x + 8

		cd_live := state.active_has_live_candle
		cd_label := cd_live ? "CD:LIVE" : "CD:--"
		cd_color := cd_live ? ui.COL_GREEN : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {sx, sy}, cd_label, cd_color, ui.FONT_SIZE_XS, .Mono)
		sx += state.text.measure(ui.FONT_SIZE_XS, cd_label).x + 12

		// Whale alert flash (visible for ~2 sec = 120 frames at 60fps).
		if state.whale_alert_frame > 0 && state.frame > 0 && state.frame - state.whale_alert_frame < 120 {
			whale_side := state.whale_alert_buy ? "BUY" : "SELL"
			whale_color := state.whale_alert_buy ? ui.COL_GREEN : ui.COL_RED
			w_buf: [48]u8
			w_str := fmt.bprintf(w_buf[:], "WHALE %s %.2f @ %.2f", whale_side, state.whale_alert_qty, state.whale_alert_price)
			// Pulsing background for visibility.
			pulse_w := state.text.measure(ui.FONT_SIZE_XS, w_str).x + 8
			alpha := f32(0.25) - f32(state.frame - state.whale_alert_frame) * 0.002
			if alpha > 0 {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = ui.rect_xywh(sx - 4, bar_y + 1, pulse_w, STATUS_BAR_H - 2),
					color = ui.with_alpha(whale_color, alpha),
				})
			}
			ui.push_text(&state.cmd_buf, {sx, sy}, w_str, whale_color, ui.FONT_SIZE_XS, .Bold)
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

	// --- Help overlay (rendered LAST, on top of everything) ---
	if state.show_help_overlay {
		draw_help_overlay(state, viewport_w, viewport_h)
	}

	// --- Exchange manager (on top of help overlay) ---
	if state.show_exchange_manager {
		draw_exchange_manager(state, viewport_w, viewport_h, pointer)
	}

	// --- Cell stream picker (on top of exchange manager) ---
	if state.cell_stream_picker_open >= 0 && state.cell_stream_picker_open < state.cell_count {
		// Anchor below the cell header badge (approximate position).
		anchor_y := TOP_BAR_H + 20
		anchor_x := f32(80)
		draw_cell_stream_picker(state, {anchor_x, anchor_y}, state.cell_stream_picker_open,
			viewport_w, viewport_h, pointer)
	}

	// --- Widget catalog (on top of cell stream picker) ---
	if state.show_widget_catalog {
		draw_widget_catalog(state, viewport_w, viewport_h, pointer)
	}

	// --- Stream picker (on top of everything) ---
	if state.show_stream_picker {
		draw_stream_picker(state, viewport_w, viewport_h, pointer)
	}

	// --- Toast notification (brief feedback, fades after ~90 frames / 1.5s) ---
	if state.toast_len > 0 && state.frame > 0 && state.toast_frame > 0 {
		elapsed := state.frame - state.toast_frame
		TOAST_DURATION :: u64(90)
		if elapsed < TOAST_DURATION {
			toast_str := string(state.toast_text[:state.toast_len])
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
	if state.zen_mode && state.tf_osd_frame > 0 && state.frame > state.tf_osd_frame {
		osd_elapsed := state.frame - state.tf_osd_frame
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

// Dashboard detail panel: structured sections with collapsible groups.
@(private = "file")
draw_dashboard_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	y := rect.pos.y

	// ═══════════════════════════════════════════════
	// Section 1: Market Info (always visible)
	// ═══════════════════════════════════════════════
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
	conn_status: ports.MD_Conn_Status = .Offline
	if state.marketdata.conn_status != nil {
		conn_status = state.marketdata.conn_status()
	}
	dot_color := conn_status == .Connected ? ui.COL_GREEN : ui.with_alpha(ui.COL_WHITE, 0.35)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {rect.pos.x + 4, y + 4}, size = {6, 6}},
		color = dot_color,
	})
	status_label := conn_status == .Connected ? "LIVE" : "OFFLINE"
	ui.push_text(&state.cmd_buf, {rect.pos.x + 14, y + 10},
		status_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	y += 16

	// TF label.
	tf_opts := TF_OPTIONS
	tf_buf: [16]u8
	tf_str := fmt.bprintf(tf_buf[:], "TF: %s", tf_opts[state.active_tf_idx])
	ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10},
		tf_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	y += 20

	// ═══════════════════════════════════════════════
	// Section 2: Streams (collapsible, with price + click-to-switch)
	// ═══════════════════════════════════════════════
	remaining_h := (rect.pos.y + rect.size.y) - y
	stream_hdr_buf: [24]u8
	stream_count := 0
	if reg != nil { stream_count = reg.count }
	stream_hdr_label := fmt.bprintf(stream_hdr_buf[:], "STREAMS (%d)", stream_count)
	stream_sec := ui.collapsible_section(&state.cmd_buf,
		ui.Rect{pos = {rect.pos.x, y}, size = {rect.size.x, remaining_h}},
		stream_hdr_label, &state.section_streams, pointer, state.text.measure)
	y += 22 // header height
	if state.section_streams.expanded && reg != nil && reg.count > 0 {
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

	// ═══════════════════════════════════════════════
	// Section 3: Chart Layers (collapsible)
	// ═══════════════════════════════════════════════
	remaining_h = (rect.pos.y + rect.size.y) - y
	if remaining_h > 22 {
		ui.collapsible_section(&state.cmd_buf,
			ui.Rect{pos = {rect.pos.x, y}, size = {rect.size.x, remaining_h}},
			"LAYERS", &state.section_layers, pointer, state.text.measure)
		y += 22
		if state.section_layers.expanded {
			toggle_w := rect.size.x - 8
			toggle_h := f32(20)
			tx := rect.pos.x + 4

			LAYER_TOGGLES :: [11]struct{label: string, field_offset: int}{
				{label = "  Volume",    field_offset = 0},
				{label = "  Heatmap",   field_offset = 1},
				{label = "  VPVR",      field_offset = 2},
				{label = "  EMA/SMA",   field_offset = 3},
				{label = "  Bollinger", field_offset = 4},
				{label = "  VWAP",      field_offset = 5},
				{label = "  RSI",       field_offset = 6},
				{label = "  MACD",      field_offset = 7},
				{label = "  Funding",   field_offset = 8},
				{label = "  Liq Vol",   field_offset = 9},
				{label = "  Trade Cnt", field_offset = 10},
			}
			// Point toggles at focused candle cell when available, else global.
			fci := state.focused_candle_cell_idx
			has_focus := fci >= 0 && fci < state.cell_count && state.cell_assignments[fci].widget == .Candle
			layer_ptrs: [11]^bool
			if has_focus {
				fc := &state.cell_assignments[fci]
				layer_ptrs = {
					&fc.show_vol, &fc.show_heatmap, &fc.show_vpvr,
					&fc.show_ma, &fc.show_bbands, &fc.show_vwap,
					&fc.show_rsi, &fc.show_macd, &fc.show_funding,
					&fc.show_liq, &fc.show_trade_counter,
				}
			} else {
				layer_ptrs = {
					&state.show_candle_vol, &state.show_candle_heatmap, &state.show_candle_vpvr,
					&state.show_ma, &state.show_bbands, &state.show_vwap,
					&state.show_rsi, &state.show_macd, &state.show_funding,
					&state.show_liq, &state.show_trade_counter,
				}
			}
			layer_labels := LAYER_TOGGLES
			step_h := f32(16)
			for li in 0 ..< 11 {
				if y + toggle_h > rect.pos.y + rect.size.y do break
				tr := ui.toggle(&state.cmd_buf,
					ui.rect_xywh(tx, y, toggle_w, toggle_h),
					layer_labels[li].label, layer_ptrs[li]^,
					pointer, state.text.measure, ui.FONT_SIZE_XS)
				if tr.changed {
					layer_ptrs[li]^ = tr.value
					// Sync to global defaults for persistence.
					sync_layer_to_global(state, li, tr.value)
				}
				y += toggle_h + 2

				// Inline settings when layer is active.
				if !layer_ptrs[li]^ do continue
				sx := tx + 14
				sw := toggle_w - 18

				// Get parameter pointers (focused cell or global).
				ma_per := has_focus ? &state.cell_assignments[fci].ma_periods : &state.ma_periods
				bb_per := has_focus ? &state.cell_assignments[fci].bb_period  : &state.bb_period
				bb_sig := has_focus ? &state.cell_assignments[fci].bb_sigma   : &state.bb_sigma
				rsi_per := has_focus ? &state.cell_assignments[fci].rsi_period : &state.rsi_period
				macd_f := has_focus ? &state.cell_assignments[fci].macd_fast   : &state.macd_fast
				macd_s := has_focus ? &state.cell_assignments[fci].macd_slow   : &state.macd_slow
				macd_sig := has_focus ? &state.cell_assignments[fci].macd_signal : &state.macd_signal

				switch li {
				case 3: // EMA/SMA
					ma_keys := [3]string{services.SETTING_MA_PERIOD_0, services.SETTING_MA_PERIOD_1, services.SETTING_MA_PERIOD_2}
					ma_labels := [3]string{"MA1", "MA2", "MA3"}
					for mi in 0 ..< 3 {
						if y + step_h > rect.pos.y + rect.size.y do break
						sr := ui.stepper(&state.cmd_buf, ui.rect_xywh(sx, y, sw, step_h),
							ma_labels[mi], ma_per[mi], 2, 200,
							pointer, state.text.measure)
						if sr.changed {
							ma_per[mi] = sr.value
							state.ma_periods[mi] = sr.value
							buf: [4]u8
							services.settings_set(&state.settings, ma_keys[mi], fmt.bprintf(buf[:], "%d", sr.value))
						}
						y += step_h + 1
					}
				case 4: // Bollinger
					if y + step_h > rect.pos.y + rect.size.y do continue
					sr := ui.stepper(&state.cmd_buf, ui.rect_xywh(sx, y, sw, step_h),
						"Per", bb_per^, 2, 200,
						pointer, state.text.measure)
					if sr.changed {
						bb_per^ = sr.value
						state.bb_period = sr.value
						buf: [4]u8
						services.settings_set(&state.settings, services.SETTING_BB_PERIOD, fmt.bprintf(buf[:], "%d", sr.value))
					}
					y += step_h + 1
					if y + step_h > rect.pos.y + rect.size.y do continue
					sf := ui.stepper_float(&state.cmd_buf, ui.rect_xywh(sx, y, sw, step_h),
						"Sig", bb_sig^, 0.5, 5.0, 0.5,
						pointer, state.text.measure)
					if sf.changed {
						bb_sig^ = sf.value
						state.bb_sigma = sf.value
						buf: [4]u8
						services.settings_set(&state.settings, services.SETTING_BB_SIGMA, fmt.bprintf(buf[:], "%.1f", sf.value))
					}
					y += step_h + 1
				case 6: // RSI
					if y + step_h > rect.pos.y + rect.size.y do continue
					sr := ui.stepper(&state.cmd_buf, ui.rect_xywh(sx, y, sw, step_h),
						"Per", rsi_per^, 2, 100,
						pointer, state.text.measure)
					if sr.changed {
						rsi_per^ = sr.value
						state.rsi_period = sr.value
						buf: [4]u8
						services.settings_set(&state.settings, services.SETTING_RSI_PERIOD, fmt.bprintf(buf[:], "%d", sr.value))
					}
					y += step_h + 1
				case 7: // MACD
					macd_params := [3]struct{label: string, ptr: ^int, global_ptr: ^int, key: string, lo, hi: int}{
						{"Fst", macd_f, &state.macd_fast, services.SETTING_MACD_FAST, 2, 100},
						{"Slw", macd_s, &state.macd_slow, services.SETTING_MACD_SLOW, 2, 200},
						{"Sig", macd_sig, &state.macd_signal, services.SETTING_MACD_SIGNAL, 2, 100},
					}
					for &mp in macd_params {
						if y + step_h > rect.pos.y + rect.size.y do break
						sr := ui.stepper(&state.cmd_buf, ui.rect_xywh(sx, y, sw, step_h),
							mp.label, mp.ptr^, mp.lo, mp.hi,
							pointer, state.text.measure)
						if sr.changed {
							mp.ptr^ = sr.value
							mp.global_ptr^ = sr.value
							buf: [4]u8
							services.settings_set(&state.settings, mp.key, fmt.bprintf(buf[:], "%d", sr.value))
						}
						y += step_h + 1
					}
				}
			}
			y += 4
		}
	}

	// ═══════════════════════════════════════════════
	// Section 4: Panel Layout (collapsible)
	// ═══════════════════════════════════════════════
	remaining_h = (rect.pos.y + rect.size.y) - y
	if remaining_h > 22 {
		ui.collapsible_section(&state.cmd_buf,
			ui.Rect{pos = {rect.pos.x, y}, size = {rect.size.x, remaining_h}},
			"PANELS", &state.section_panels, pointer, state.text.measure)
		y += 22
		if state.section_panels.expanded {
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
			sidebar_res := ui.draw_sidebar(&state.cmd_buf, toggle_rect, &state.sidebar, pointer, state.text.measure)
			if sidebar_res.toggled_panel >= 0 {
				queue_ui_action(state, UI_Action{kind = .Toggle_Panel, panel_idx = sidebar_res.toggled_panel})
			}
		}
	}
}

// Venues detail panel — collapsible venue sections with subscribe/unsubscribe.
@(private = "file")
draw_markets_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	y := rect.pos.y
	bottom := rect.pos.y + rect.size.y

	// Header: "VENUES" + connection status badge.
	ui.push_text(&state.cmd_buf, {rect.pos.x + 2, y + 14}, "VENUES",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

	conn_status: ports.MD_Conn_Status = .Offline
	if state.marketdata.conn_status != nil {
		conn_status = state.marketdata.conn_status()
	}
	conn_label: string
	conn_dot_color: ui.Color
	conn_text_color: ui.Color
	switch conn_status {
	case .Connected:    conn_label = "LIVE";         conn_dot_color = ui.COL_GREEN;          conn_text_color = ui.COL_GREEN
	case .Connecting:   conn_label = "CONNECTING";   conn_dot_color = ui.COL_YELLOW_ACCENT;  conn_text_color = ui.COL_YELLOW_ACCENT
	case .Reconnecting: conn_label = "RECONNECTING"; conn_dot_color = ui.COL_YELLOW_ACCENT;  conn_text_color = ui.COL_YELLOW_ACCENT
	case .Offline:      conn_label = "OFFLINE";      conn_dot_color = ui.with_alpha(ui.COL_WHITE, 0.35); conn_text_color = ui.COL_TEXT_MUTED
	}
	badge_w := ui.status_badge_width(conn_label, state.text.measure, ui.FONT_SIZE_XS)
	badge_x := rect.pos.x + rect.size.x - badge_w - 4
	ui.status_badge(&state.cmd_buf,
		ui.rect_xywh(badge_x, y + 2, badge_w, f32(16)),
		conn_label, conn_dot_color, conn_text_color, state.text.measure, ui.FONT_SIZE_XS)
	y += 22

	// Stream count summary.
	reg := state.stream_views
	stream_count := 0
	if reg != nil { stream_count = reg.count }
	sc_buf: [24]u8
	sc_str := fmt.bprintf(sc_buf[:], "%d streams", stream_count)
	ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10},
		sc_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	y += 16

	// Divider.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {rect.pos.x + 4, y}, to = {rect.pos.x + rect.size.x - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 4

	// Collapsible venue sections.
	vc := services.markets_venue_count(&state.markets_store)
	item_h := f32(20)

	for vi in 0 ..< vc {
		if y + 20 > bottom do break
		venue := services.markets_venue_at(&state.markets_store, vi)

		// Venue header (collapsible).
		sec := &state.exchange_sections[vi]
		hdr_rect := ui.rect_xywh(rect.pos.x + 2, y, rect.size.x - 4, f32(20))
		hdr_hovered := ui.rect_contains(hdr_rect, pointer.pos)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect = hdr_rect,
			color = hdr_hovered ? ui.with_alpha(ui.COL_SURFACE_2, 0.9) : ui.with_alpha(ui.COL_SURFACE_2, 0.5),
		})
		arrow := sec.expanded ? "v " : "> "
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 13}, arrow,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 16, y + 13}, venue,
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
		if hdr_hovered && pointer.left_pressed {
			sec.expanded = !sec.expanded
		}
		y += 22

		if !sec.expanded do continue

		// Symbols under this venue.
		sc := services.markets_symbol_count(&state.markets_store, venue)
		for si in 0 ..< sc {
			if y + item_h > bottom do break
			entry := services.markets_symbol_at(&state.markets_store, venue, si)
			if entry == nil do continue

			is_sub := markets_is_subscribed(state, entry.venue, entry.ticker)

			sym_rect := ui.rect_xywh(rect.pos.x + 4, y, rect.size.x - 8, item_h)
			sym_hovered := ui.rect_contains(sym_rect, pointer.pos)
			if sym_hovered {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = sym_rect, color = ui.with_alpha(ui.COL_WHITE, 0.04),
				})
			}

			// Green dot if subscribed.
			if is_sub {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = {pos = {rect.pos.x + 8, y + item_h * 0.5 - 3}, size = {6, 6}},
					color = ui.COL_GREEN,
				})
			}

			ui.push_text(&state.cmd_buf,
				{rect.pos.x + 18, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				entry.ticker, is_sub ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY,
				ui.FONT_SIZE_XS, .Mono)

			// Subscribe/unsubscribe button.
			btn_label := is_sub ? "-" : "+"
			btn_w := f32(18)
			btn_x := rect.pos.x + rect.size.x - btn_w - 6
			btn_rect := ui.rect_xywh(btn_x, y + 2, btn_w, item_h - 4)
			btn_color := is_sub ? ui.with_alpha(ui.COL_RED, 0.15) : ui.with_alpha(ui.COL_GREEN, 0.15)
			btn_hovered := ui.rect_contains(btn_rect, pointer.pos)
			if btn_hovered {
				btn_color = is_sub ? ui.with_alpha(ui.COL_RED, 0.3) : ui.with_alpha(ui.COL_GREEN, 0.3)
			}
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = btn_rect, color = btn_color})
			ui.push_text(&state.cmd_buf,
				{btn_x + btn_w * 0.5 - 3, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				btn_label, is_sub ? ui.COL_RED : ui.COL_GREEN, ui.FONT_SIZE_XS, .Bold)

			if btn_hovered && pointer.left_pressed {
				for mi in 0 ..< state.markets_store.count {
					me := state.markets_store.entries[mi]
					if me.venue == entry.venue && me.ticker == entry.ticker {
						if is_sub {
							queue_ui_action(state, UI_Action{kind = .Unsubscribe_Market, market_entry_idx = mi})
						} else {
							queue_ui_action(state, UI_Action{kind = .Subscribe_Market, market_entry_idx = mi})
						}
						break
					}
				}
			}

			y += item_h
		}
		y += 2
	}

	// "Manage..." link at bottom → opens full exchange manager overlay.
	if y + 22 < bottom {
		y += 2
		manage_rect := ui.rect_xywh(rect.pos.x + 4, y, rect.size.x - 8, f32(20))
		manage_hovered := ui.rect_contains(manage_rect, pointer.pos)
		if manage_hovered {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect = manage_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05),
			})
		}
		ui.push_text(&state.cmd_buf,
			{rect.pos.x + 8, y + 13},
			"Manage...", manage_hovered ? ui.COL_ACCENT_CYAN : ui.COL_TEXT_MUTED,
			ui.FONT_SIZE_XS, .Mono)
		if manage_hovered && pointer.left_pressed {
			queue_ui_action(state, UI_Action{kind = .Toggle_Connection_Modal})
		}
	}
}
