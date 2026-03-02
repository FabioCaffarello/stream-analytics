package app

import "core:fmt"
import "mr:ports"
import "mr:streams"
import "mr:ui"
import "mr:widgets"

// Compare mode rendering — side-by-side widget comparison.
// Extracted from build_ui.odin for cohesion.

build_compare_mode :: proc(
	state: ^App_State,
	input: ports.Input_State,
	workspace: ui.Rect,
	pointer: ui.Pointer_Input,
	gap: f32,
) {
	workspace := workspace

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
	seg_res := ui.segmented_control(&state.cmd_buf, seg_rect, cmp_opts[:], state.compare.widget_idx,
		pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if seg_res.changed {
		state.compare.widget_idx = seg_res.index
	}
	cmp_cursor += seg_w + 10

	// Stream count.
	count_buf: [16]u8
	count_str := fmt.bprintf(count_buf[:], "%d streams  Tab:add  Esc:exit", state.compare.count)
	ui.push_text(&state.cmd_buf, {cmp_cursor, cr.pos.y + ctrl_h * 0.5 + ui.FONT_SIZE_XS * 0.25},
		count_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	// Compare grid.
	cmp_grid := ui.build_compare_grid(state.compare.count, gap)
	cmp_result := ui.compute_grid(cmp_grid, workspace)

	// Render a panel for each compare slot.
	for ci in 0 ..< state.compare.count {
		cell_rect := cmp_result.rects[ci]
		sid := state.compare.slots[ci]
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
		switch state.compare.widget_idx {
		case 0: // Orderbook
			ob_pg := f64(10.0)
			if slot.orderbook_store.last_price > 0 {
				ob_pg = orderbook_auto_price_group(slot.orderbook_store.last_price)
			}
			ob_max := 16
			if cell_rect.size.y < 170 do ob_max = 10
			cmp_stream_id_buf: [streams.STREAM_ID_CAP]u8
			cmp_stream_id := ""
			cmp_stream_state := streams.Stream_State.Live
			cmp_desync_reason := streams.Stream_Desync_Reason.None
			cmp_subscribe_acks := 0
			cmp_snapshot_ts_ms := i64(0)
			if slot.has_stream_info {
				cmp_stream_id = build_stream_id_from_market_into(cmp_stream_id_buf[:], slot.stream_info.venue, slot.stream_info.symbol)
				if h := streams.registry_get(&state.stream_registry, cmp_stream_id); h != nil {
					cmp_stream_state = h.status.state
					cmp_desync_reason = h.status.desync_reason
					cmp_subscribe_acks = h.status.subscribe_acks
					cmp_snapshot_ts_ms = h.status.last_snapshot_ts_ms
				}
			}
			cmp_orderbook_empty_reason := orderbook_wait_message(
				cmp_stream_state,
				cmp_desync_reason,
				cmp_subscribe_acks,
				cmp_snapshot_ts_ms,
				0,
			)
			widgets.orderbook_widget(&state.cmd_buf, widgets.Orderbook_Widget_Data{
				store                = &slot.orderbook_store,
				viewport             = cell_rect,
				text                 = state.text,
				scroll_y             = &state.compare.ob_scroll[ci],
				input                = input,
				price_group          = ob_pg,
				max_rows             = ob_max,
				group_idx            = &state.compare.ob_grp[ci],
				pointer              = pointer,
				stream_id            = cmp_stream_id,
				stream_state         = cmp_stream_state,
				stream_desync_reason = cmp_desync_reason,
				empty_reason         = cmp_orderbook_empty_reason,
			})
		case 1: // Trades
			widgets.trades_widget(&state.cmd_buf, widgets.Trades_Widget_Data{
				store      = &slot.trades_store,
				viewport   = cell_rect,
				text       = state.text,
				scroll_y   = &state.compare.trade_scroll[ci],
				input      = input,
				filter_idx = &state.compare.trade_filter[ci],
				pointer    = pointer,
				now_ms     = current_now_ms(state),
			})
		case 2: // Candles — synchronized scroll/zoom across all compare panels.
			widgets.candle_widget(&state.cmd_buf, widgets.Candle_Widget_Data{
				store         = &slot.candle_store,
				heatmap_store = &slot.heatmap_store,
				vpvr_store    = &slot.vpvr_store,
				viewport     = cell_rect,
				text         = state.text,
				input        = input,
				scroll_x     = &state.compare.scroll_x[0],
				zoom_level   = &state.compare.zoom[0],
				health_label = "---",
				health_color = ui.COL_TEXT_MUTED,
				tf_label     = cell_effective_tf_string(state, 0),
				heatmap_live  = false,
				heatmap_synth = false,
				vpvr_live     = false,
				vpvr_synth    = false,
				show_volume  = &state.compare.show_vol[ci],
				show_heatmap_overlay = &state.compare.show_heatmap[ci],
				show_vpvr_overlay    = &state.compare.show_vpvr[ci],
				heatmap_intensity_idx = &state.compare.heatmap_idx[ci],
				show_funding = state.indicators.show_funding,
				show_liq     = state.indicators.show_liq,
				show_trade_counter = state.indicators.show_trade_counter,
				footprint_store = nil,
				pointer      = pointer,
				now_ms       = current_now_ms(state),
				timeframe_ms = cell_effective_tf_ms(state, 0),
			})
		}
	}
}
