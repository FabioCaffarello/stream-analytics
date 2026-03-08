package app

import "core:fmt"
import "mr:layers"
import "mr:services"
import "mr:ui"

resolve_cell_subject_id :: proc(state: ^App_State, ci: int) -> u64 {
	if state == nil do return 0
	if ci < 0 || ci >= state.world.count do return state.layer_store.active_subject_id

	bind := &state.world.bindings[ci]
	if bind.stream_idx >= 0 && bind.stream_idx < STREAM_VIEW_CAP {
		if state.stream_views != nil && state.stream_views.slots[bind.stream_idx].used {
			return state.stream_views.slots[bind.stream_idx].subject_id
		}
	}
	if state.stream_views != nil && state.stream_views.has_active {
		return state.stream_views.active_subject_id
	}
	return state.layer_store.active_subject_id
}

render_cell_layer_canvas :: proc(
	state: ^App_State,
	ci: int,
	kind: Widget_Kind,
	cell_vp: ui.Rect,
) {
	subject_id := resolve_cell_subject_id(state, ci)

	// S94: For candle cells, check if any analytics subplots are active.
	if kind == .Candle && ci >= 0 && ci < state.world.count {
		ind := &state.world.indicators[ci]
		sf := layers.Subplot_Flags{
			show_cvd       = ind.show_cvd,
			show_delta_vol = ind.show_delta_vol,
			show_oi        = ind.show_oi,
		}
		n_subplots := layers.subplot_flags_count(sf)
		if n_subplots > 0 {
			render_cell_layer_canvas_with_subplots(state, subject_id, kind, cell_vp, sf, n_subplots)
			return
		}
	}
	render_subject_layer_canvas(state, subject_id, kind, cell_vp)
}

// S9: Analytics cells use filtered Layer_Context to render only the selected kind.
render_cell_layer_canvas_analytics :: proc(
	state: ^App_State,
	ci: int,
	cell_vp: ui.Rect,
	analytics_kind: services.Analytics_Kind,
) {
	subject_id := resolve_cell_subject_id(state, ci)
	render_subject_layer_canvas_with_analytics(state, subject_id, .Analytics, cell_vp, analytics_kind, true)
}

// S94: Candle cell with analytics subplots — split viewport into main chart + subplot strips.
@(private = "file")
render_cell_layer_canvas_with_subplots :: proc(
	state: ^App_State,
	subject_id: u64,
	kind: Widget_Kind,
	cell_vp: ui.Rect,
	sf: layers.Subplot_Flags,
	n_subplots: int,
) {
	if state == nil do return
	if cell_vp.size.x <= 0 || cell_vp.size.y <= 0 do return

	// Subplot height: each subplot gets a fixed fraction, capped.
	// Default: 20% of cell height per subplot, min 30px, max 80px.
	SUBPLOT_MIN_H :: f32(30)
	SUBPLOT_MAX_H :: f32(80)
	subplot_h := clamp(cell_vp.size.y * 0.20, SUBPLOT_MIN_H, SUBPLOT_MAX_H)
	total_subplot_h := subplot_h * f32(n_subplots)

	// Ensure main chart gets at least 40% of the viewport.
	if total_subplot_h > cell_vp.size.y * 0.6 {
		total_subplot_h = cell_vp.size.y * 0.6
		subplot_h = total_subplot_h / f32(n_subplots)
	}

	main_h := cell_vp.size.y - total_subplot_h
	main_vp := ui.Rect{pos = cell_vp.pos, size = {cell_vp.size.x, main_h}}

	// Render main chart in the upper viewport.
	render_subject_layer_canvas(state, subject_id, kind, main_vp)

	// Resolve the stream for subplot rendering.
	stream := layers.market_store_stream_for_subject(&state.layer_store, subject_id)
	if stream == nil do return

	ctx := layers.Layer_Context{
		store        = &state.layer_store,
		stream       = stream,
		subject_id   = subject_id,
		now_ms       = current_now_ms(state),
		frame_seq    = state.frame,
		text         = state.text,
		capabilities = layers.layer_capabilities_from_stream(stream),
		subplot_flags = sf,
	}

	// Render each active subplot in order: Delta Vol, CVD, OI.
	subplot_y := cell_vp.pos.y + main_h
	subplot_idx := 0

	if sf.show_delta_vol {
		vp := ui.Rect{pos = {cell_vp.pos.x, subplot_y + f32(subplot_idx) * subplot_h}, size = {cell_vp.size.x, subplot_h}}
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Push{rect = vp})
		layers.layer_outputs_reset(&state.layer_outputs)
		layers.subplot_delta_vol_render(&ctx, &state.layer_outputs, vp)
		layers.canvas_render_outputs(&state.cmd_buf, &state.layer_outputs)
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Pop{})
		state.telemetry.subplot_count += 1 // S97
		subplot_idx += 1
	}

	if sf.show_cvd {
		vp := ui.Rect{pos = {cell_vp.pos.x, subplot_y + f32(subplot_idx) * subplot_h}, size = {cell_vp.size.x, subplot_h}}
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Push{rect = vp})
		layers.layer_outputs_reset(&state.layer_outputs)
		layers.subplot_cvd_render(&ctx, &state.layer_outputs, vp)
		layers.canvas_render_outputs(&state.cmd_buf, &state.layer_outputs)
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Pop{})
		state.telemetry.subplot_count += 1 // S97
		subplot_idx += 1
	}

	if sf.show_oi {
		vp := ui.Rect{pos = {cell_vp.pos.x, subplot_y + f32(subplot_idx) * subplot_h}, size = {cell_vp.size.x, subplot_h}}
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Push{rect = vp})
		layers.layer_outputs_reset(&state.layer_outputs)
		layers.subplot_oi_render(&ctx, &state.layer_outputs, vp)
		layers.canvas_render_outputs(&state.cmd_buf, &state.layer_outputs)
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Pop{})
		state.telemetry.subplot_count += 1 // S97
	}
}

// S95: Compare pane with analytics subplots — same logic as cell subplots
// but takes subject_id directly (no ECS cell lookup).
render_compare_pane_with_subplots :: proc(
	state: ^App_State,
	subject_id: u64,
	cell_vp: ui.Rect,
	sf: layers.Subplot_Flags,
	n_subplots: int,
) {
	if state == nil do return
	if cell_vp.size.x <= 0 || cell_vp.size.y <= 0 do return

	SUBPLOT_MIN_H :: f32(30)
	SUBPLOT_MAX_H :: f32(80)
	subplot_h := clamp(cell_vp.size.y * 0.20, SUBPLOT_MIN_H, SUBPLOT_MAX_H)
	total_subplot_h := subplot_h * f32(n_subplots)

	if total_subplot_h > cell_vp.size.y * 0.6 {
		total_subplot_h = cell_vp.size.y * 0.6
		subplot_h = total_subplot_h / f32(n_subplots)
	}

	main_h := cell_vp.size.y - total_subplot_h
	main_vp := ui.Rect{pos = cell_vp.pos, size = {cell_vp.size.x, main_h}}

	render_subject_layer_canvas(state, subject_id, .Candle, main_vp)

	stream := layers.market_store_stream_for_subject(&state.layer_store, subject_id)
	if stream == nil do return

	ctx := layers.Layer_Context{
		store        = &state.layer_store,
		stream       = stream,
		subject_id   = subject_id,
		now_ms       = current_now_ms(state),
		frame_seq    = state.frame,
		text         = state.text,
		capabilities = layers.layer_capabilities_from_stream(stream),
		subplot_flags = sf,
	}

	subplot_y := cell_vp.pos.y + main_h
	subplot_idx := 0

	if sf.show_delta_vol {
		vp := ui.Rect{pos = {cell_vp.pos.x, subplot_y + f32(subplot_idx) * subplot_h}, size = {cell_vp.size.x, subplot_h}}
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Push{rect = vp})
		layers.layer_outputs_reset(&state.layer_outputs)
		layers.subplot_delta_vol_render(&ctx, &state.layer_outputs, vp)
		layers.canvas_render_outputs(&state.cmd_buf, &state.layer_outputs)
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Pop{})
		state.telemetry.subplot_count += 1 // S97
		subplot_idx += 1
	}

	if sf.show_cvd {
		vp := ui.Rect{pos = {cell_vp.pos.x, subplot_y + f32(subplot_idx) * subplot_h}, size = {cell_vp.size.x, subplot_h}}
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Push{rect = vp})
		layers.layer_outputs_reset(&state.layer_outputs)
		layers.subplot_cvd_render(&ctx, &state.layer_outputs, vp)
		layers.canvas_render_outputs(&state.cmd_buf, &state.layer_outputs)
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Pop{})
		state.telemetry.subplot_count += 1 // S97
		subplot_idx += 1
	}

	if sf.show_oi {
		vp := ui.Rect{pos = {cell_vp.pos.x, subplot_y + f32(subplot_idx) * subplot_h}, size = {cell_vp.size.x, subplot_h}}
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Push{rect = vp})
		layers.layer_outputs_reset(&state.layer_outputs)
		layers.subplot_oi_render(&ctx, &state.layer_outputs, vp)
		layers.canvas_render_outputs(&state.cmd_buf, &state.layer_outputs)
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Pop{})
		state.telemetry.subplot_count += 1 // S97
	}
}

render_subject_layer_canvas :: proc(
	state: ^App_State,
	subject_id: u64,
	kind: Widget_Kind,
	cell_vp: ui.Rect,
) {
	render_subject_layer_canvas_with_analytics(state, subject_id, kind, cell_vp, .Open_Interest, false)
}

// S9: Unified layer canvas render with optional analytics kind filter.
render_subject_layer_canvas_with_analytics :: proc(
	state: ^App_State,
	subject_id: u64,
	kind: Widget_Kind,
	cell_vp: ui.Rect,
	analytics_kind: services.Analytics_Kind,
	analytics_filter: bool,
) {
	if state == nil do return
	if cell_vp.size.x <= 0 || cell_vp.size.y <= 0 do return

	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = cell_vp, color = ui.with_alpha(ui.COL_SURFACE_1, 0.92)})

	// S86: Viewport isolation — clip all layer output to the cell rect.
	ui.push(&state.cmd_buf, ui.Cmd_Clip_Push{rect = cell_vp})

	bundle_mask := layer_bundle_for_widget(kind)
	if bundle_mask == 0 {
		empty_label := "Empty"
		ui.push_text(&state.cmd_buf,
			{cell_vp.pos.x + 6, cell_vp.pos.y + 14},
			empty_label,
			ui.COL_TEXT_MUTED,
			ui.FONT_SIZE_XS,
			.Mono,
		)
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Pop{})
		return
	}

	stream := layers.market_store_stream_for_subject(&state.layer_store, subject_id)
	if stream == nil {
		wait_buf: [80]u8
		wait := fmt.bprintf(wait_buf[:], "Waiting stream %x", subject_id)
		ui.push_text(&state.cmd_buf,
			{cell_vp.pos.x + 6, cell_vp.pos.y + 14},
			wait,
			ui.COL_TEXT_MUTED,
			ui.FONT_SIZE_XS,
			.Mono,
		)
		ui.push(&state.cmd_buf, ui.Cmd_Clip_Pop{})
		return
	}

	ctx := layers.Layer_Context{
		store        = &state.layer_store,
		stream       = stream,
		subject_id   = subject_id,
		now_ms       = current_now_ms(state),
		frame_seq    = state.frame,
		viewport     = cell_vp,
		text         = state.text,
		capabilities = layers.layer_capabilities_from_stream(stream),
		signal_evidence_link_enabled = state.signal_evidence_link_enabled,
		analytics_kind   = analytics_kind,
		analytics_filter = analytics_filter,
		active_bundle    = bundle_mask,
	}

	layers.layer_outputs_reset(&state.layer_outputs)
	layers.layer_registry_render_bundle(&state.layer_registry, bundle_mask, &ctx, &state.layer_outputs)
	layers.canvas_render_outputs(&state.cmd_buf, &state.layer_outputs)
	state.telemetry.layer_render_count += 1 // S97

	if state.layer_outputs.overflowed > 0 {
		drop_buf: [48]u8
		drop := fmt.bprintf(drop_buf[:], "Dropped %d", state.layer_outputs.overflowed)
		ui.push_text(&state.cmd_buf,
			{cell_vp.pos.x + 6, cell_vp.pos.y + cell_vp.size.y - 4},
			drop,
			ui.COL_WARNING,
			ui.FONT_SIZE_XS,
			.Mono,
		)
	}

	// S86: Close viewport clip — matches Cmd_Clip_Push above.
	ui.push(&state.cmd_buf, ui.Cmd_Clip_Pop{})
}
