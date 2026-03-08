package app

import "core:fmt"
import "mr:layers"
import "mr:ports"
import "mr:services"
import "mr:ui"

// Compare mode rendering — side-by-side layer canvas comparison.

build_compare_mode :: proc(
	state: ^App_State,
	input: ports.Input_State,
	workspace: ui.Rect,
	pointer: ui.Pointer_Input,
	gap: f32,
) {
	_ = input
	if state == nil do return
	workspace := workspace

	ctrl_h := f32(22)
	ctrl_rect := ui.rect_cut_top(&workspace, ctrl_h)
	ui.rect_cut_top(&workspace, 4)

	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ctrl_rect, color = ui.COL_SURFACE_1})

	cr := ui.rect_pad_xy(ctrl_rect, 8, 2)
	cmp_label := "COMPARE"
	ui.push_text(&state.cmd_buf, {cr.pos.x, cr.pos.y + ctrl_h * 0.5 + ui.FONT_SIZE_XS * 0.25},
		cmp_label, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
	cmp_cursor := cr.pos.x + state.text.measure(ui.FONT_SIZE_XS, cmp_label).x + 10

	cmp_opts := COMPARE_WIDGET_OPTIONS
	seg_w := f32(200)
	seg_rect := ui.rect_xywh(cmp_cursor, cr.pos.y, seg_w, cr.size.y)
	seg_res := ui.segmented_control(&state.cmd_buf, seg_rect, cmp_opts[:], state.compare.widget_idx,
		pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if seg_res.changed {
		state.compare.widget_idx = seg_res.index
	}
	cmp_cursor += seg_w + 10

	count_buf: [48]u8
	count_str := fmt.bprintf(count_buf[:], "%d streams  Tab:add  Esc:exit", state.compare.count)
	ui.push_text(&state.cmd_buf, {cmp_cursor, cr.pos.y + ctrl_h * 0.5 + ui.FONT_SIZE_XS * 0.25},
		count_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	cmp_grid := ui.build_compare_grid(state.compare.count, gap)
	cmp_result := ui.compute_grid(cmp_grid, workspace)

	// S62: Use canonical compare_widget_kind_for_idx (widget_channels.odin).
	render_kind := compare_widget_kind_for_idx(state.compare.widget_idx)

	for ci in 0 ..< state.compare.count {
		cell_rect := cmp_result.rects[ci]
		if state.compare.slots[ci] == 0 do continue

		// S39: Click-to-focus — if pointer is inside this pane and mouse pressed, focus it.
		is_focused := ci == state.compare.focused_pane
		if ui.rect_contains(cell_rect, pointer.pos) && pointer.left_pressed {
			state.compare.focused_pane = ci
			is_focused = true
		}

		// S38: Resolve effective subject_id (per-pane TF aware) for rendering.
		sid := compare_pane_resolve_subject_id(state, ci)
		if sid == 0 do continue

		// S38: Surface view uses per-pane TF for health/staleness.
		sv := resolve_compare_surface_view(state, ci)

		venue_label := "---"
		vl_buf: [64]u8
		if len(sv.venue) > 0 {
			venue_label = fmt.bprintf(vl_buf[:], "%s:%s", sv.venue, sv.symbol)
		}

		header_h := f32(18)
		header_rect := ui.rect_cut_top(&cell_rect, header_h)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = header_rect, color = ui.with_alpha(ui.COL_SURFACE_1, 0.9)})

		// S37: Venue:Symbol label.
		cursor_x := header_rect.pos.x + 6
		text_y := header_rect.pos.y + header_h * 0.5 + ui.FONT_SIZE_XS * 0.35
		ui.push_text(&state.cmd_buf, {cursor_x, text_y},
			venue_label, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
		cursor_x += state.text.measure(ui.FONT_SIZE_XS, venue_label).x + 6

		// S38: Per-pane TF badge.
		tf_opts := TF_OPTIONS
		eff_tf := compare_pane_effective_tf_idx(state, ci)
		tf_str := tf_opts[eff_tf] if eff_tf >= 0 && eff_tf < len(tf_opts) else tf_opts[0]
		is_per_pane_tf := state.compare.tf_idx[ci] >= 0
		tf_color := is_per_pane_tf ? ui.COL_BLUE : ui.COL_YELLOW_ACCENT
		ui.push_text(&state.cmd_buf, {cursor_x, text_y},
			tf_str, tf_color, ui.FONT_SIZE_XS, .Mono)
		cursor_x += state.text.measure(ui.FONT_SIZE_XS, tf_str).x + 4

		// S84: Per-pane analytics kind badge (clickable, cycles OI→DV→CVD→BS→OI).
		if render_kind == .Analytics {
			ANALYTICS_SHORT :: [4]string{"OI", "DV", "CVD", "BS"}
			analytics_short := ANALYTICS_SHORT
			ak := int(state.compare.analytics_kind[ci])
			ak_label := analytics_short[ak] if ak >= 0 && ak < len(analytics_short) else "OI"
			ak_w := state.text.measure(ui.FONT_SIZE_XS, ak_label).x + 6
			ak_rect := ui.rect_xywh(cursor_x, header_rect.pos.y + 1, ak_w, header_h - 2)
			ak_hov := ui.rect_contains(ak_rect, pointer.pos)
			ak_bg := ak_hov ? ui.with_alpha(ui.COL_ACCENT_CYAN, 0.2) : ui.with_alpha(ui.COL_ACCENT_CYAN, 0.08)
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ak_rect, color = ak_bg})
			ui.push_text(&state.cmd_buf, {cursor_x + 3, text_y},
				ak_label, ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Mono)
			if ak_hov && pointer.left_pressed {
				next_ak := services.Analytics_Kind((ak + 1) % 4)
				queue_ui_action(state, UI_Action{
					kind           = .Set_Compare_Analytics_Kind,
					pane_idx       = ci,
					analytics_kind = next_ak,
				})
			}
			cursor_x += ak_w + 4
		}

		// S95: Per-pane subplot indicator pills (C/D/O) — only for Candle widget.
		if render_kind == .Candle {
			SUBPLOT_PILLS :: [3]struct{label: string, color: ui.Color}{
				{"C", {0.3, 0.9, 0.5, 1}},   // CVD — green
				{"D", {0.9, 0.4, 0.3, 1}},   // Delta Vol — red
				{"O", {0.4, 0.7, 0.95, 1}},  // OI — cyan
			}
			subplot_pills := SUBPLOT_PILLS
			subplot_active := [3]bool{
				state.compare.show_cvd[ci],
				state.compare.show_delta_vol[ci],
				state.compare.show_oi[ci],
			}
			for pi in 0 ..< 3 {
				pill := subplot_pills[pi]
				pill_w := state.text.measure(ui.FONT_SIZE_XS, pill.label).x + 6
				pill_rect := ui.rect_xywh(cursor_x, header_rect.pos.y + 1, pill_w, header_h - 2)
				pill_hov := ui.rect_contains(pill_rect, pointer.pos)
				pill_on := subplot_active[pi]
				pill_alpha := pill_on ? f32(0.25) : f32(0.08)
				pill_alpha = pill_hov ? pill_alpha + 0.15 : pill_alpha
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = pill_rect, color = ui.with_alpha(pill.color, pill_alpha)})
				pill_text_col := pill_on ? pill.color : ui.with_alpha(pill.color, 0.5)
				ui.push_text(&state.cmd_buf, {cursor_x + 3, text_y},
					pill.label, pill_text_col, ui.FONT_SIZE_XS, .Mono)
				if pill_hov && pointer.left_pressed {
					queue_ui_action(state, UI_Action{
						kind        = .Toggle_Compare_Subplot,
						pane_idx    = ci,
						subplot_idx = pi,
					})
				}
				cursor_x += pill_w + 2
			}
			cursor_x += 2
		}

		// S37/S53: Composition badge (shared proc).
		cursor_x += draw_composition_badge(&state.cmd_buf, cursor_x, text_y, sv.composition, state.text.measure)

		// S42: Recovery badge (RCVR/XHST) — surfaces per-pane recovery status.
		switch sv.recovery_status {
		case .Recovering:
			rcvr_label :: "RCVR"
			ui.push_text(&state.cmd_buf, {cursor_x, text_y},
				rcvr_label, ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
			cursor_x += state.text.measure(ui.FONT_SIZE_XS, rcvr_label).x + 4
		case .Exhausted:
			xhst_label :: "XHST"
			ui.push_text(&state.cmd_buf, {cursor_x, text_y},
				xhst_label, ui.COL_RED, ui.FONT_SIZE_XS, .Mono)
			cursor_x += state.text.measure(ui.FONT_SIZE_XS, xhst_label).x + 4
		case .None:
		}

		// S37/S53: Health dot (shared proc).
		draw_health_dot(&state.cmd_buf, cursor_x, header_rect.pos.y + header_h * 0.5, 6, sv.health_level, sv.has_live_data, sv.composition)

		// S84: Analytics panes route through analytics-filtered layer canvas.
		if render_kind == .Analytics {
			pane_ak := state.compare.analytics_kind[ci]
			render_subject_layer_canvas_with_analytics(state, sid, .Analytics, cell_rect, pane_ak, true)
		} else if render_kind == .Candle {
			// S95: Route candle panes through subplot-aware renderer.
			sf := layers.Subplot_Flags{
				show_cvd       = state.compare.show_cvd[ci],
				show_delta_vol = state.compare.show_delta_vol[ci],
				show_oi        = state.compare.show_oi[ci],
			}
			n_subplots := layers.subplot_flags_count(sf)
			if n_subplots > 0 {
				render_compare_pane_with_subplots(state, sid, cell_rect, sf, n_subplots)
			} else {
				render_subject_layer_canvas(state, sid, .Candle, cell_rect)
			}
		} else {
			render_subject_layer_canvas(state, sid, render_kind, cell_rect)
		}

		state.telemetry.compare_pane_count += 1 // S97

		// S39: Focused pane border highlight.
		if is_focused {
			border_color := ui.with_alpha(ui.COL_BLUE, 0.6)
			b := f32(1)
			full_rect := cmp_result.rects[ci]
			// Top
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(full_rect.pos.x, full_rect.pos.y, full_rect.size.x, b), color = border_color})
			// Bottom
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(full_rect.pos.x, full_rect.pos.y + full_rect.size.y - b, full_rect.size.x, b), color = border_color})
			// Left
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(full_rect.pos.x, full_rect.pos.y, b, full_rect.size.y), color = border_color})
			// Right
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(full_rect.pos.x + full_rect.size.x - b, full_rect.pos.y, b, full_rect.size.y), color = border_color})
		}
	}
}
