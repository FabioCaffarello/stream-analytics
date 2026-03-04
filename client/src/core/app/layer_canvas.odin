package app

import "core:fmt"
import "mr:layers"
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
	render_subject_layer_canvas(state, subject_id, kind, cell_vp)
}

render_subject_layer_canvas :: proc(
	state: ^App_State,
	subject_id: u64,
	kind: Widget_Kind,
	cell_vp: ui.Rect,
) {
	if state == nil do return
	if cell_vp.size.x <= 0 || cell_vp.size.y <= 0 do return

	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = cell_vp, color = ui.with_alpha(ui.COL_SURFACE_1, 0.92)})

	bundle_mask := legacy_widget_bundle(kind)
	if bundle_mask == 0 {
		empty_label := "Empty"
		ui.push_text(&state.cmd_buf,
			{cell_vp.pos.x + 6, cell_vp.pos.y + 14},
			empty_label,
			ui.COL_TEXT_MUTED,
			ui.FONT_SIZE_XS,
			.Mono,
		)
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
	}

	layers.layer_outputs_reset(&state.layer_outputs)
	layers.layer_registry_render_bundle(&state.layer_registry, bundle_mask, &ctx, &state.layer_outputs)
	layers.canvas_render_outputs(&state.cmd_buf, &state.layer_outputs)

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
}
