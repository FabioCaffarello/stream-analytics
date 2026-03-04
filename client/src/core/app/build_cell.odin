package app

// Cell rendering — extracted from build_ui.odin for cohesion.
// Each grid cell is rendered by render_cell_widget, dispatching
// to the appropriate widget based on Widget_Kind.

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:ui"
import "mr:widgets"

CELL_HDR_H :: f32(20)

render_cell_widget :: proc(
	state: ^App_State,
	ci: int,
	cell_vp_in: ui.Rect,
	pointer: ui.Pointer_Input,
	input: ports.Input_State,
	sync_price: f64,
	sync_active: bool,
) {
	bind := &state.world.bindings[ci]
	vw := &state.world.views[ci]
	ch := &state.world.charts[ci]
	ind := &state.world.indicators[ci]
	indp := &state.world.ind_params[ci]
	sub := &state.world.subplots[ci]
	wid := state.world.widgets[ci].kind
	tf_comp := &state.world.timeframes[ci]
	cell_vp := cell_vp_in
	if cell_vp.size.x <= 0 || cell_vp.size.y <= 0 do return

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
	badge_label := "~ Active"  // "~" = follow-active indicator
	badge_buf: [40]u8
	if bind.stream_idx >= 0 && bind.stream_idx < STREAM_VIEW_CAP {
		if reg := state.stream_views; reg != nil && reg.slots[bind.stream_idx].used {
			slot := &reg.slots[bind.stream_idx]
			if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
			if slot.has_stream_info && len(slot.stream_info.venue) > 0 {
				badge_label = fmt.bprintf(badge_buf[:], "%s:%s", slot.stream_info.venue, slot.stream_info.symbol)
			}
		}
	}
	badge_w := min(state.text.measure(ui.FONT_SIZE_XS, badge_label).x + 12, hdr_rect.size.x * 0.5)
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
	if state.world.count > 1 {
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
	// Skip TF badge in very narrow cells to prevent header overlap.
	tf_inset := f32(0)
	if wid == .Candle && cell_vp.size.x >= 120 {
		tf_opts := TF_OPTIONS
		eff_tf := cell_effective_tf_idx(state, ci)
		tf_str := tf_opts[eff_tf] if eff_tf >= 0 && eff_tf < len(tf_opts) else tf_opts[0]
		is_per_cell_tf := tf_comp.tf_idx >= 0
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
		// Underline when per-cell TF override is active (visual cue even when TF matches global).
		if is_per_cell_tf {
			ui.push(&state.cmd_buf, ui.Cmd_Line{
				from = {tf_rect.pos.x, ui.rect_bottom(tf_rect)},
				to   = {tf_rect.pos.x + tf_w, ui.rect_bottom(tf_rect)},
				color = tf_color, thickness = 1,
			})
		}
		tf_inset = tf_w + 4
		// Click TF badge → cycle through TF options for this cell.
		// Guard: only process if mouse is within this cell to prevent cross-cell misclicks.
		if is_cell_focused && pointer.left_pressed && ui.rect_contains(tf_rect, pointer.pos) {
			next_tf: int
			if is_per_cell_tf {
				// Already has per-cell override: cycle tf_idx, wrapping to -1 (revert to global).
				next_tf = tf_comp.tf_idx + 1
				if next_tf >= len(tf_opts) do next_tf = -1
			} else {
				// No override yet: set first per-cell override by advancing from effective TF.
				next_tf = (eff_tf + 1) % len(tf_opts)
			}
			queue_ui_action(state, UI_Action{kind = .Set_Cell_Timeframe, cell_idx = ci, timeframe_idx = next_tf})
		}
	}
	// Widget type label.
	WIDGET_SHORT :: [9]string{"Candle", "Stats", "Counter", "HM", "VPVR", "Trades", "OB", "DOM", "--"}
	ws := WIDGET_SHORT
	wlabel := ws[int(wid)]
	wlabel_w := state.text.measure(ui.FONT_SIZE_XS, wlabel).x
	wlabel_x := ui.rect_right(hdr_rect) - wlabel_w - 4 - close_inset - tf_inset
	ui.push_text(&state.cmd_buf,
		{wlabel_x, hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35},
		wlabel, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	// Indicator count badge for candle cells (e.g., "3i" when 3 indicators active).
	if wid == .Candle {
		ind_count := 0
		if ind.show_ma            do ind_count += 1
		if ind.show_bbands        do ind_count += 1
		if ind.show_vwap          do ind_count += 1
		if ind.show_rsi           do ind_count += 1
		if ind.show_macd          do ind_count += 1
		if ind.show_funding       do ind_count += 1
		if ind.show_liq           do ind_count += 1
		if ind.show_trade_counter do ind_count += 1
		if ind_count > 0 {
			ic_buf: [4]u8
			ic_str := fmt.bprintf(ic_buf[:], "%di", ind_count)
			ic_w := state.text.measure(ui.FONT_SIZE_XS, ic_str).x + 6
			ic_x := wlabel_x - ic_w - 2
			if ic_x > badge_rect.pos.x + badge_w + 4 {
				ic_rect := ui.rect_xywh(ic_x, hdr_rect.pos.y + 2, ic_w, CELL_HDR_H - 4)
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = ic_rect, color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.12),
				})
				ui.push_text(&state.cmd_buf,
					{ic_x + 3, hdr_rect.pos.y + CELL_HDR_H * 0.5 + ui.FONT_SIZE_XS * 0.35},
					ic_str, ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Mono)
			}
		}
	}
	// Header divider line.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {hdr_rect.pos.x, hdr_rect.pos.y + CELL_HDR_H},
		to   = {ui.rect_right(hdr_rect), hdr_rect.pos.y + CELL_HDR_H},
		color = ui.COL_DIVIDER, thickness = 1,
	})

	stores := resolve_stores_for_cell(state, ci)
	cell_stream_id_buf: [streams.STREAM_ID_CAP]u8
	cell_stream_id := streams.registry_active_stream_id(&state.stream_registry)
	active_stream_id := cell_stream_id
	cell_stream_state := state.active_metrics.state
	cell_stream_desync_reason := state.active_metrics.desync_reason
	cell_stream_subscribe_acks := state.active_metrics.subscribe_acks
	cell_stream_last_snapshot_ts_ms := i64(0)
	cell_stream_last_msg_ts_ms := state.active_metrics.last_msg_ts_ms
	if active := streams.registry_active(&state.stream_registry); active != nil {
		cell_stream_last_snapshot_ts_ms = active.status.last_snapshot_ts_ms
		if cell_stream_last_msg_ts_ms <= 0 {
			cell_stream_last_msg_ts_ms = active.status.last_local_ts_ms
		}
	}
	cell_stats_last_ts_ms := state.active_metrics.last_stats_ts_ms
	cell_orderbook_last_ts_ms := state.active_metrics.last_orderbook_ts_ms
	if bind.stream_idx >= 0 && bind.stream_idx < STREAM_VIEW_CAP {
		if reg := state.stream_views; reg != nil && reg.slots[bind.stream_idx].used {
			slot := &reg.slots[bind.stream_idx]
			if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
			if slot.has_stream_info {
				cell_stream_id = build_stream_id_from_market_into(cell_stream_id_buf[:], slot.stream_info.venue, slot.stream_info.symbol)
				if h := streams.registry_get(&state.stream_registry, cell_stream_id); h != nil {
					cell_stream_state = h.status.state
					cell_stream_desync_reason = h.status.desync_reason
					cell_stream_subscribe_acks = h.status.subscribe_acks
					cell_stream_last_snapshot_ts_ms = h.status.last_snapshot_ts_ms
					cell_stream_last_msg_ts_ms = h.status.last_local_ts_ms
				}
			}
		}
	}
	if cell_stream_id != active_stream_id {
		cell_stats_last_ts_ms = 0
		cell_orderbook_last_ts_ms = 0
	}
	stats_empty_reason := stats_wait_message(
		cell_stream_state,
		cell_stream_desync_reason,
		cell_stream_subscribe_acks,
		cell_stats_last_ts_ms,
	)
	orderbook_empty_reason := orderbook_wait_message(
		cell_stream_state,
		cell_stream_desync_reason,
		cell_stream_subscribe_acks,
		cell_stream_last_snapshot_ts_ms,
		cell_orderbook_last_ts_ms,
	)

	switch wid {
	case .Candle:
		// BUG-C fix: per-cell health using the cell's own store + TF.
		cell_now_ms := current_now_ms(state)
		cell_recv_ms := stores.candle == &state.stores.candle ? state.candle_last_recv_local_ms : cell_now_ms
		signal_subject_id := u64(0)
		signal_venue := ""
		signal_symbol := ""
		if bind.stream_idx >= 0 && bind.stream_idx < STREAM_VIEW_CAP {
			if reg := state.stream_views; reg != nil && reg.slots[bind.stream_idx].used {
				slot := &reg.slots[bind.stream_idx]
				if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
				if slot.has_stream_info {
					signal_venue = slot.stream_info.venue
					signal_symbol = slot.stream_info.symbol
				}
			}
		} else if reg := state.stream_views; reg != nil {
			if active := stream_view_active_slot(reg); active != nil {
				if !active.has_stream_info { refresh_stream_info_for_slot(state, active) }
				if active.has_stream_info {
					signal_venue = active.stream_info.venue
					signal_symbol = active.stream_info.symbol
				}
			}
		}
		if len(signal_venue) > 0 && len(signal_symbol) > 0 {
			signal_subject_id = build_signal_subject_id(signal_venue, signal_symbol, cell_effective_tf_string(state, ci))
		}
		candle_health_label, candle_health_detail, candle_health_color := build_candle_health_ui_for_store(
			stores.candle, cell_recv_ms, cell_effective_tf_ms(state, ci), cell_now_ms)
		// BUG-G fix: is_active = follow-active cell, is_active_market = same market as active.
		is_active := bind.stream_idx < 0
		is_active_market := is_active
		if !is_active_market && len(active_stream_id) > 0 && cell_stream_id == active_stream_id {
			is_active_market = true
		}
		prev_show_vol := ch.show_vol
		prev_show_heatmap := ch.show_heatmap
		prev_show_vpvr := ch.show_vpvr
		prev_heatmap_idx := ch.heatmap_intensity_idx
		// Track focused candle cell for keyboard shortcuts.
		if vw.crosshair.active {
			state.world.focused = ci
		}
		cell_indicator_probe := (^widgets.Indicator_Render_Probe)(nil)
		if ci == state.world.focused {
			cell_indicator_probe = &state.telemetry.last_indicator_probe
		}
		widgets.candle_widget(&state.cmd_buf, widgets.Candle_Widget_Data{
			store                 = stores.candle,
			heatmap_store         = stores.heatmap,
			vpvr_store            = stores.vpvr,
			viewport              = cell_vp,
			text                  = state.text,
			input                 = input,
			scroll_x              = &vw.candle_scroll_x,
			zoom_level            = &vw.candle_zoom,
			health_label          = candle_health_label,
			health_detail         = candle_health_detail,
			health_color          = candle_health_color,
			tf_label              = cell_effective_tf_string(state, ci),
			stream_id             = cell_stream_id,
			stream_state          = cell_stream_state,
			heatmap_live          = stores.heatmap_live,
			heatmap_synth         = !stores.heatmap_live && stores.heatmap != nil && stores.heatmap.count > 0,
			vpvr_live             = stores.vpvr_live,
			vpvr_synth            = !stores.vpvr_live && stores.vpvr != nil && stores.vpvr.count > 0,
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
			footprint_store       = is_active ? &state.stores.footprint : nil,
			ma_periods            = indp.ma_periods,
			bb_period             = indp.bb_period,
			bb_sigma              = indp.bb_sigma,
			rsi_period            = indp.rsi_period,
			macd_fast             = indp.macd_fast,
			macd_slow             = indp.macd_slow,
			macd_signal           = indp.macd_signal,
			pointer               = pointer,
			now_ms                = cell_now_ms,
			timeframe_ms          = cell_effective_tf_ms(state, ci),
			signal_store          = &state.stores.signals,
			signal_subject_id     = signal_subject_id,
			sync_price            = sync_active && !vw.crosshair.active ? sync_price : 0,
			sync_active           = sync_active && !vw.crosshair.active,
			indicator_probe       = cell_indicator_probe,
			sub_main_split        = &sub.sub_main_split,
			sub_ratios            = &sub.sub_ratios,
			sub_resize_idx        = &sub.sub_resize_idx,
		})
		// Persist candle toggle changes from the primary (active stream) cell.
		if is_active {
			persisted := false
			if ch.show_vol != prev_show_vol {
				state.chart_display.show_vol = ch.show_vol
				services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_VOL,
					ch.show_vol ? "1" : "0")
				persisted = true
			}
			if ch.show_heatmap != prev_show_heatmap {
				state.chart_display.show_heatmap = ch.show_heatmap
				services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_HEATMAP,
					ch.show_heatmap ? "1" : "0")
				persisted = true
			}
			if ch.show_vpvr != prev_show_vpvr {
				state.chart_display.show_vpvr = ch.show_vpvr
				services.settings_set(&state.settings, services.SETTING_SHOW_CANDLE_VPVR,
					ch.show_vpvr ? "1" : "0")
				persisted = true
			}
			if ch.heatmap_intensity_idx != prev_heatmap_idx {
				state.chart_display.heatmap_intensity_idx = ch.heatmap_intensity_idx
				idx_buf: [4]u8
				services.settings_set(&state.settings, services.SETTING_CANDLE_HEATMAP_INTENSITY_IDX,
					fmt.bprintf(idx_buf[:], "%d", ch.heatmap_intensity_idx))
				persisted = true
			}
			if persisted {
				services.settings_flush(&state.settings)
			}
		}

	case .Stats:
		widgets.stats_widget(&state.cmd_buf, widgets.Stats_Widget_Data{
			store                = stores.stats,
			viewport             = cell_vp,
			text                 = state.text,
			stream_id            = cell_stream_id,
			stream_state         = cell_stream_state,
			stream_desync_reason = cell_stream_desync_reason,
			empty_reason         = stats_empty_reason,
		})

	case .Counter:
		// Use candle store buy_vol/sell_vol (much denser than stats liq data).
		counter_candle := stores.candle
		if counter_candle != nil && counter_candle.count > 0 {
			stats_buf: [services.CANDLE_CAP]widgets.Stat
			sc := 0
			for ki in 0 ..< counter_candle.count {
				c := services.get_candle(counter_candle, ki)
				stats_buf[sc] = widgets.Stat{
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
			cell_tf_sec := cell_effective_tf_ms(state, ci) / 1000
			if cell_tf_sec <= 0 do cell_tf_sec = 60
			widgets.trade_counter(&state.cmd_buf, widgets.Trade_Counter_Data{
				stats         = stats_buf[:sc],
				viewport      = cell_vp,
				timeframe     = cell_tf_sec,
				x_min         = x_min,
				x_max         = x_max,
				bar_width_pct = widgets.CANDLE_WIDTH_PCT,
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
			scroll_y   = &vw.trades_scroll_y,
			input      = input,
			filter_idx = &ch.trade_filter_idx,
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
		ob_group_options: [5]f64
		ob_group_labels: [5][12]u8
		ob_group_count := 0
		if stores.orderbook != nil && stores.orderbook.last_price > 0 {
			base := orderbook_auto_price_group(stores.orderbook.last_price)
			ob_group_options = {base * 0.1, base, base * 10, base * 100, base * 1000}
			ob_group_count = 5
			for i in 0 ..< 5 {
				lbuf := &ob_group_labels[i]
				lbuf^ = {}
				if ob_group_options[i] >= 1 {
					_ = fmt.bprintf(lbuf[:], "%.0f", ob_group_options[i])
				} else {
					_ = fmt.bprintf(lbuf[:], "%g", ob_group_options[i])
				}
			}
			idx := clamp(ch.ob_group_idx, 0, 4)
			ob_price_group = ob_group_options[idx]
		}
		ob_label_strs: [5]string
		for i in 0 ..< ob_group_count {
			ob_label_strs[i] = string(ob_group_labels[i][:cstring_len(&ob_group_labels[i])])
		}
		widgets.orderbook_widget(&state.cmd_buf, widgets.Orderbook_Widget_Data{
			store                = stores.orderbook,
			viewport             = cell_vp,
			text                 = state.text,
			scroll_y             = &vw.ob_scroll_y,
			input                = input,
			price_group          = ob_price_group,
			max_rows             = ob_max_rows,
			group_options        = ob_label_strs[:ob_group_count],
			group_idx            = &ch.ob_group_idx,
			pointer              = pointer,
			stream_id            = cell_stream_id,
			stream_state         = cell_stream_state,
			stream_desync_reason = cell_stream_desync_reason,
			empty_reason         = orderbook_empty_reason,
		})

	case .DOM:
		dom_price_group := f64(10.0)
		dom_group_options: [5]f64
		dom_group_labels: [5][12]u8
		dom_group_count := 0
		if stores.orderbook != nil && stores.orderbook.last_price > 0 {
			base := orderbook_auto_price_group(stores.orderbook.last_price)
			dom_group_options = {base * 0.1, base, base * 10, base * 100, base * 1000}
			dom_group_count = 5
			for i in 0 ..< 5 {
				lbuf := &dom_group_labels[i]
				lbuf^ = {}
				if dom_group_options[i] >= 1 {
					_ = fmt.bprintf(lbuf[:], "%.0f", dom_group_options[i])
				} else {
					_ = fmt.bprintf(lbuf[:], "%g", dom_group_options[i])
				}
			}
			idx := clamp(ch.dom_group_idx, 0, 4)
			dom_price_group = dom_group_options[idx]
		}
		dom_label_strs: [5]string
		for i in 0 ..< dom_group_count {
			dom_label_strs[i] = string(dom_group_labels[i][:cstring_len(&dom_group_labels[i])])
		}
		widgets.dom_widget(&state.cmd_buf, widgets.DOM_Widget_Data{
			orderbook            = stores.orderbook,
			dom                  = &state.stores.dom,
			viewport             = cell_vp,
			text                 = state.text,
			input                = input,
			pointer              = pointer,
			group_options        = dom_label_strs[:dom_group_count],
			group_idx            = &ch.dom_group_idx,
			price_group          = dom_price_group,
			stream_id            = cell_stream_id,
			stream_state         = cell_stream_state,
			stream_desync_reason = cell_stream_desync_reason,
			empty_reason         = orderbook_empty_reason,
		})

	case .Empty:
		// Empty cell — just show background.
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = cell_vp, color = ui.COL_SURFACE_1})
	}
}
