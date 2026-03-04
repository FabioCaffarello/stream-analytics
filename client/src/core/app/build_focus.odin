package app

import "core:fmt"
import "mr:ports"
import "mr:streams"
import "mr:ui"
import "mr:widgets"

// Focus mode rendering — scalper cockpit (candle 75% + orderbook 25%).
// Extracted from build_ui.odin for cohesion.

build_focus_mode :: proc(
	state: ^App_State,
	input: ports.Input_State,
	workspace: ui.Rect,
	pointer: ui.Pointer_Input,
) {
	focus_gap := f32(4)
	candle_w := (workspace.size.x - focus_gap) * 0.75
	ob_w := workspace.size.x - candle_w - focus_gap

	candle_rect := ui.rect_xywh(workspace.pos.x, workspace.pos.y, candle_w, workspace.size.y)
	ob_rect := ui.rect_xywh(workspace.pos.x + candle_w + focus_gap, workspace.pos.y, ob_w, workspace.size.y)

	// Use cell 0 ECS state for focus mode (per-cell stores, indicators, scroll).
	vw := &state.world.views[0]
	ch := &state.world.charts[0]
	ind := &state.world.indicators[0]
	indp := &state.world.ind_params[0]
	stores := resolve_stores_for_cell(state, 0)

	// Candle widget (active stream).
	cell_now_ms := current_now_ms(state)
	cell_recv_ms := stores.candle == &state.stores.candle ? state.candle_last_recv_local_ms : cell_now_ms
	signal_subject_id := u64(0)
	if reg := state.stream_views; reg != nil {
		if active := stream_view_active_slot(reg); active != nil {
			if !active.has_stream_info { refresh_stream_info_for_slot(state, active) }
			if active.has_stream_info {
				signal_subject_id = build_signal_subject_id(active.stream_info.venue, active.stream_info.symbol, cell_effective_tf_string(state, 0))
			}
		}
	}
	candle_health_label, candle_health_detail, candle_health_color := build_candle_health_ui_for_store(
		stores.candle, cell_recv_ms, cell_effective_tf_ms(state, 0), cell_now_ms)
	widgets.candle_widget(&state.cmd_buf, widgets.Candle_Widget_Data{
		store                 = stores.candle,
		heatmap_store         = stores.heatmap,
		vpvr_store            = stores.vpvr,
		viewport              = candle_rect,
		text                  = state.text,
		input                 = input,
		scroll_x              = &vw.candle_scroll_x,
		zoom_level            = &vw.candle_zoom,
		health_label          = candle_health_label,
		health_detail         = candle_health_detail,
		health_color          = candle_health_color,
		tf_label              = cell_effective_tf_string(state, 0),
		heatmap_live          = state.active_metrics.has_live_heatmap,
		heatmap_synth         = !state.active_metrics.has_live_heatmap && stores.heatmap != nil && stores.heatmap.count > 0,
		vpvr_live             = state.active_metrics.has_live_vpvr,
		vpvr_synth            = !state.active_metrics.has_live_vpvr && stores.vpvr != nil && stores.vpvr.count > 0,
		show_volume           = &ch.show_vol,
		show_heatmap_overlay  = &ch.show_heatmap,
		show_vpvr_overlay     = &ch.show_vpvr,
		heatmap_intensity_idx = &ch.heatmap_intensity_idx,
		crosshair             = &vw.crosshair,
		chart_type            = &ch.chart_type,
		show_ma               = ind.show_ma,
		show_bbands           = ind.show_bbands,
		show_vwap             = ind.show_vwap,
		show_rsi              = ind.show_rsi,
		show_macd             = ind.show_macd,
		show_funding          = ind.show_funding,
		show_liq              = ind.show_liq,
		show_trade_counter    = ind.show_trade_counter,
		stats_store           = stores.stats,
		draw_tools            = &state.draw_tools,
		footprint_store       = &state.stores.footprint,
		ma_periods            = indp.ma_periods,
		bb_period             = indp.bb_period,
		bb_sigma              = indp.bb_sigma,
		rsi_period            = indp.rsi_period,
		macd_fast             = indp.macd_fast,
		macd_slow             = indp.macd_slow,
		macd_signal           = indp.macd_signal,
		pointer               = pointer,
		now_ms                = cell_now_ms,
		timeframe_ms          = cell_effective_tf_ms(state, 0),
		signal_store          = &state.stores.signals,
		signal_subject_id     = signal_subject_id,
		indicator_probe       = &state.telemetry.last_indicator_probe,
	})

	// Orderbook widget (active stream).
	ob_max_rows := 20
	if ob_rect.size.y < 170 {
		ob_max_rows = 12
	} else if ob_rect.size.y < 230 {
		ob_max_rows = 16
	}
	ob_price_group := f64(10.0)
	focus_ob_group_options: [5]f64
	focus_ob_group_labels: [5][12]u8
	focus_ob_group_count := 0
	if stores.orderbook != nil && stores.orderbook.last_price > 0 {
		base := orderbook_auto_price_group(stores.orderbook.last_price)
		focus_ob_group_options = {base * 0.1, base, base * 10, base * 100, base * 1000}
		focus_ob_group_count = 5
		for i in 0 ..< 5 {
			lbuf := &focus_ob_group_labels[i]
			lbuf^ = {}
			if focus_ob_group_options[i] >= 1 {
				_ = fmt.bprintf(lbuf[:], "%.0f", focus_ob_group_options[i])
			} else {
				_ = fmt.bprintf(lbuf[:], "%g", focus_ob_group_options[i])
			}
		}
		idx := clamp(ch.ob_group_idx, 0, 4)
		ob_price_group = focus_ob_group_options[idx]
	}
	ob_label_strs: [5]string
	for i in 0 ..< focus_ob_group_count {
		ob_label_strs[i] = string(focus_ob_group_labels[i][:cstring_len(&focus_ob_group_labels[i])])
	}
	active_snapshot_ts_ms := i64(0)
	if active := streams.registry_active(&state.stream_registry); active != nil {
		active_snapshot_ts_ms = active.status.last_snapshot_ts_ms
	}
	active_orderbook_empty_reason := orderbook_wait_message(
		state.active_metrics.state,
		state.active_metrics.desync_reason,
		state.active_metrics.subscribe_acks,
		active_snapshot_ts_ms,
		state.active_metrics.last_orderbook_ts_ms,
	)
	widgets.orderbook_widget(&state.cmd_buf, widgets.Orderbook_Widget_Data{
		store                = stores.orderbook,
		viewport             = ob_rect,
		text                 = state.text,
		scroll_y             = &vw.ob_scroll_y,
		input                = input,
		price_group          = ob_price_group,
		max_rows             = ob_max_rows,
		group_options        = ob_label_strs[:focus_ob_group_count],
		group_idx            = &ch.ob_group_idx,
		pointer              = pointer,
		stream_id            = streams.registry_active_stream_id(&state.stream_registry),
		stream_state         = state.active_metrics.state,
		stream_desync_reason = state.active_metrics.desync_reason,
		empty_reason         = active_orderbook_empty_reason,
	})

	// Focus mode label.
	focus_label := "FOCUS  Esc:exit"
	flw := state.text.measure(ui.FONT_SIZE_XS, focus_label).x
	ui.push_text(&state.cmd_buf, {workspace.pos.x + workspace.size.x - flw - 4, workspace.pos.y + 12},
		focus_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
}
