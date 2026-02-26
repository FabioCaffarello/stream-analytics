package app

import "core:fmt"
import "mr:model"
import "mr:ports"
import "mr:services"
import "mr:ui"
import "mr:util"
import "mr:widgets"

MD_POLL_CAP :: 64

TF_OPTIONS :: [6]string{"1m", "5m", "15m", "1h", "4h", "1d"}

CANDLE_TF_MS                 :: i64(60_000)
CANDLE_LAG_WARN_CLOSED_MS    :: i64(120_000)
CANDLE_LAG_STALE_CLOSED_MS   :: i64(180_000)
CANDLE_LAG_WARN_OPEN_MS      :: i64(90_000)
CANDLE_LAG_STALE_OPEN_MS     :: i64(120_000)
CANDLE_SILENCE_WARN_OPEN_MS  :: i64(20_000)
CANDLE_SILENCE_STALE_OPEN_MS :: i64(60_000)

Candle_Health :: enum u8 {
	No_Data,
	OK,
	Lagging,
	Stale,
}

STREAM_VIEW_CAP :: 32
MD_METRICS_HISTORY_CAP :: 120

Stream_View_Slot :: struct {
	used:            bool,
	subject_id:      u64,
	last_seen_frame: u64,
	has_channel:     bool,
	channel:         ports.MD_Channel,
	has_timeframe_ms: bool,
	timeframe_ms:     i64,
	has_heatmap_snapshot: bool,
	heatmap_snapshot:     services.Heatmap_Snapshot,
	vpvr_store:           services.VPVR_Store,
	trades_store:    services.Trades_Store,
	orderbook_store: services.Orderbook_Store,
	stats_store:     services.Stats_Store,
	candle_store:    services.Candle_Store,
}

Stream_View_Registry :: struct {
	slots:             [STREAM_VIEW_CAP]Stream_View_Slot,
	count:             int,
	has_active:        bool,
	active_subject_id: u64,
	eviction_count:    u64,
	repair_count:      u64,
}

MD_Metrics_History_Sample :: struct {
	frame:    u64,
	metrics:  ports.MD_Runtime_Metrics,
}

Runtime_Probe :: struct {
	frame:                 u64,
	conn_status:           ports.MD_Conn_Status,
	candle_health:         Candle_Health,
	stream_count:          int,
	has_active_stream:     bool,
	active_subject_id:     u64,
	stream_evictions:      u64,
	stream_repairs:        u64,
	pending_restore:       bool,
	has_md_metrics:        bool,
	md_metrics:            ports.MD_Runtime_Metrics,
	md_qmax_recent:        int,
	md_drop_delta_recent:  int,
	md_rc_delta_recent:    int,
}

App_State :: struct {
	cmd_buf:         ui.Command_Buffer,
	text:            ports.Text_Port,
	fonts:           ports.Font_Port,
	marketdata:      ports.Marketdata_Port,
	trades_store:    services.Trades_Store,
	orderbook_store: services.Orderbook_Store,
	heatmap_store:   services.Heatmap_Store,
	vpvr_store:      services.VPVR_Store,
	stats_store:     services.Stats_Store,
	candle_store:    services.Candle_Store,
	stream_views:    ^Stream_View_Registry,
	settings:        services.Settings_Store,
	scroll_y:        f32,
	ob_scroll_y:     f32,
	candle_scroll_x: f32,
	candle_zoom:     f32,
	frame:           u64,
	last_viewport:   ui.Vec2,
	last_conn:       ports.MD_Conn_Status,
	last_keys_pressed: bit_set[ports.Key],
	has_last_render: bool,
	has_pending_active_subject: bool,
	pending_active_subject_id:  u64,
	md_metrics_history: [MD_METRICS_HISTORY_CAP]MD_Metrics_History_Sample,
	md_metrics_head:    int,
	md_metrics_count:   int,

	candle_last_recv_local_ms: i64,
	candle_health:             Candle_Health,

	active_tf_idx:    int,      // index into TF_OPTIONS
	getrange_pending: bool,     // true while waiting for range response
}

init :: proc(
	state: ^App_State,
	text: ports.Text_Port,
	md: ports.Marketdata_Port,
	fonts: ports.Font_Port = {},
	settings_port: ports.Settings_Port = {},
	offline: bool = true,
) {
	state.cmd_buf = ui.make_buffer()
	state.text = text
	state.fonts = fonts
	state.marketdata = md
	state.stream_views = new(Stream_View_Registry)

	// Only fill demo data in offline mode; real data overwrites stores when live.
	if offline {
		services.fill_demo_trades(&state.trades_store)
		services.fill_demo_orderbook(&state.orderbook_store)
		services.fill_demo_heatmaps(&state.heatmap_store)
		services.fill_demo_vpvr(&state.vpvr_store)
		services.fill_demo_stats(&state.stats_store)
		services.fill_demo_candles(&state.candle_store)
	}

	// Initialize settings store.
	if settings_port.load != nil {
		services.settings_init(&state.settings, settings_port)
		if v, ok := services.settings_get(&state.settings, services.SETTING_ACTIVE_STREAM_SUBJECT_ID); ok {
			if subject_id, ok := parse_subject_id_hex(v); ok && subject_id != 0 {
				state.has_pending_active_subject = true
				state.pending_active_subject_id = subject_id
			}
		}
		venue, ok_venue := services.settings_get(&state.settings, services.SETTING_ACTIVE_STREAM_VENUE)
		symbol, ok_symbol := services.settings_get(&state.settings, services.SETTING_ACTIVE_STREAM_SYMBOL)
		ch_s, ok_channel := services.settings_get(&state.settings, services.SETTING_ACTIVE_STREAM_CHANNEL)
		if ok_venue && ok_symbol && ok_channel {
			if ch, ok := parse_channel_short_label(ch_s); ok {
				state.has_pending_active_subject = true
				state.pending_active_subject_id = util.subject_id64_for_stream(venue, symbol, ch)
			}
		}
	}
}

shutdown :: proc(state: ^App_State) {
	if state.marketdata.shutdown != nil {
		state.marketdata.shutdown()
	}
	if state.stream_views != nil {
		free(state.stream_views)
		state.stream_views = nil
	}
	services.settings_flush(&state.settings)
	ui.destroy_buffer(&state.cmd_buf)
}

runtime_probe :: proc(state: ^App_State) -> Runtime_Probe {
	p: Runtime_Probe
	if state == nil do return p

	p.frame = state.frame
	p.conn_status = current_conn_status(state)
	p.candle_health = state.candle_health
	p.pending_restore = state.has_pending_active_subject

	if reg := state.stream_views; reg != nil {
		p.stream_count = reg.count
		p.has_active_stream = reg.has_active
		p.active_subject_id = reg.active_subject_id
		p.stream_evictions = reg.eviction_count
		p.stream_repairs = reg.repair_count
	}

	if state.marketdata.metrics != nil {
		p.has_md_metrics = state.marketdata.metrics(&p.md_metrics)
	}
	if ok, qmax, drop_delta, rc_delta := metrics_history_summary(state); ok {
		p.md_qmax_recent = qmax
		p.md_drop_delta_recent = drop_delta
		p.md_rc_delta_recent = rc_delta
	}
	return p
}

	update :: proc(state: ^App_State, input: ports.Input_State) -> ^ui.Command_Buffer {
		state.frame += 1

		_ = drain_marketdata(state)
		_ = handle_stream_hotkeys(state, input)
		_ = handle_tf_hotkeys(state, input)
		sample_marketdata_metrics(state)
		observe_candle_health(state)
		cache_render_observations(state, input)
		return build_ui(state, input)
	}

	update_web :: proc(state: ^App_State, input: ports.Input_State) -> (buf: ^ui.Command_Buffer, should_render: bool) {
			state.frame += 1
			events_processed := drain_marketdata(state)
			stream_switched := handle_stream_hotkeys(state, input)
			tf_switched := handle_tf_hotkeys(state, input)
			sample_marketdata_metrics(state)

			conn := current_conn_status(state)
		candle_health_changed := observe_candle_health(state)
	needs_render := !state.has_last_render
	if !needs_render {
		needs_render = events_processed > 0
	}
	if !needs_render {
		needs_render = state.last_viewport.x != input.viewport_size.x || state.last_viewport.y != input.viewport_size.y
	}
	if !needs_render {
		needs_render = state.last_conn != conn
	}
		if !needs_render {
			needs_render = candle_health_changed
		}
		if !needs_render {
			needs_render = stream_switched
		}
		if !needs_render {
			needs_render = tf_switched
		}
		if !needs_render {
			return &state.cmd_buf, false
		}

	cache_render_observations(state, input)
	buf = build_ui(state, input)
	return buf, true
}

@(private = "file")
current_conn_status :: proc(state: ^App_State) -> ports.MD_Conn_Status {
	if state.marketdata.conn_status != nil {
		return state.marketdata.conn_status()
	}
	return .Offline
}

@(private = "file")
current_now_ms :: proc(state: ^App_State) -> i64 {
	if state.marketdata.now_ms != nil {
		return state.marketdata.now_ms()
	}
	return 0
}

@(private = "file")
parse_subject_id_hex :: proc(s: string) -> (u64, bool) {
	if len(s) == 0 do return 0, false
	v := u64(0)
	for c in s {
		digit := u64(0)
		if c >= '0' && c <= '9' {
			digit = u64(c - '0')
		} else if c >= 'a' && c <= 'f' {
			digit = 10 + u64(c - 'a')
		} else if c >= 'A' && c <= 'F' {
			digit = 10 + u64(c - 'A')
		} else {
			return 0, false
		}
		v = (v << 4) | digit
	}
	return v, true
}

@(private = "file")
persist_active_stream_subject :: proc(state: ^App_State) {
	if state == nil do return
	reg := state.stream_views
	if reg == nil || !reg.has_active do return
	if reg.active_subject_id == 0 do return
	buf: [32]u8
	value := fmt.bprintf(buf[:], "%x", reg.active_subject_id)
	services.settings_set(&state.settings, services.SETTING_ACTIVE_STREAM_SUBJECT_ID, value)
	if state.marketdata.describe_stream != nil {
		info: ports.MD_Stream_Info
		if state.marketdata.describe_stream(reg.active_subject_id, &info) {
			if len(info.venue) > 0 {
				services.settings_set(&state.settings, services.SETTING_ACTIVE_STREAM_VENUE, info.venue)
			}
			if len(info.symbol) > 0 {
				services.settings_set(&state.settings, services.SETTING_ACTIVE_STREAM_SYMBOL, info.symbol)
			}
			services.settings_set(&state.settings, services.SETTING_ACTIVE_STREAM_CHANNEL, channel_short_label(info.channel))
		}
	}
}

@(private = "file")
clamp_nonneg_i64 :: proc(v: i64) -> i64 {
	if v < 0 do return 0
	return v
}

@(private = "file")
compute_candle_health :: proc(state: ^App_State) -> Candle_Health {
	if state.candle_store.count <= 0 do return .No_Data

	now_ms := current_now_ms(state)
	if now_ms <= 0 do return .OK

	latest := services.get_candle_newest(&state.candle_store, 0)
	end_lag_ms := clamp_nonneg_i64(now_ms - latest.window_end_ts)
	recv_age_ms := clamp_nonneg_i64(now_ms - state.candle_last_recv_local_ms)

	if latest.is_closed {
		if end_lag_ms >= CANDLE_LAG_STALE_CLOSED_MS do return .Stale
		if end_lag_ms >= CANDLE_LAG_WARN_CLOSED_MS do return .Lagging
		return .OK
	}

	if recv_age_ms >= CANDLE_SILENCE_STALE_OPEN_MS || end_lag_ms >= CANDLE_LAG_STALE_OPEN_MS do return .Stale
	if recv_age_ms >= CANDLE_SILENCE_WARN_OPEN_MS || end_lag_ms >= CANDLE_LAG_WARN_OPEN_MS do return .Lagging
	return .OK
}

@(private = "file")
observe_candle_health :: proc(state: ^App_State) -> bool {
	next := compute_candle_health(state)
	if next == state.candle_health do return false
	state.candle_health = next
	return true
}

@(private = "file")
format_ms_short :: proc(ms: i64) -> string {
	v := ms
	if v < 0 do v = 0
	if v < 1000 do return fmt.tprintf("%dms", v)
	sec := v / 1000
	if sec < 60 do return fmt.tprintf("%ds", sec)
	mins := sec / 60
	secs := sec % 60
	return fmt.tprintf("%dm%02ds", mins, secs)
}

@(private = "file")
build_candle_health_ui :: proc(state: ^App_State) -> (label: string, detail: string, color: ui.Color) {
	if state.candle_store.count <= 0 {
		return "NO DATA", "waiting for first candle", ui.with_alpha(ui.COL_WHITE, 0.6)
	}
	now_ms := current_now_ms(state)
	latest := services.get_candle_newest(&state.candle_store, 0)
	end_lag_ms := i64(0)
	recv_age_ms := i64(0)
	if now_ms > 0 {
		end_lag_ms = clamp_nonneg_i64(now_ms - latest.window_end_ts)
		recv_age_ms = clamp_nonneg_i64(now_ms - state.candle_last_recv_local_ms)
	}
	status_str := "OK"
	status_color := ui.COL_GREEN
	switch state.candle_health {
	case .Lagging:
		status_str = "LAG"
		status_color = ui.COL_YELLOW_ACCENT
	case .Stale:
		status_str = "STALE"
		status_color = ui.COL_RED
	case .No_Data:
		status_str = "NO DATA"
		status_color = ui.with_alpha(ui.COL_WHITE, 0.6)
	case .OK:
	}
	phase := latest.is_closed ? "closed" : "open"
	return status_str,
		fmt.tprintf("%s recv=%s endlag=%s", phase, format_ms_short(recv_age_ms), format_ms_short(end_lag_ms)),
		status_color
}

@(private = "file")
cache_render_observations :: proc(state: ^App_State, input: ports.Input_State) {
	state.last_viewport = input.viewport_size
	state.last_conn = current_conn_status(state)
	state.has_last_render = true
}

@(private = "file")
sample_marketdata_metrics :: proc(state: ^App_State) {
	if state == nil do return
	if state.marketdata.metrics == nil do return
	m: ports.MD_Runtime_Metrics
	if !state.marketdata.metrics(&m) do return
	state.md_metrics_history[state.md_metrics_head] = MD_Metrics_History_Sample{
		frame    = state.frame,
		metrics  = m,
	}
	state.md_metrics_head = (state.md_metrics_head + 1) % MD_METRICS_HISTORY_CAP
	if state.md_metrics_count < MD_METRICS_HISTORY_CAP {
		state.md_metrics_count += 1
	}
}

@(private = "file")
metrics_history_summary :: proc(state: ^App_State) -> (ok: bool, qmax: int, drop_delta: int, rc_delta: int) {
	if state == nil do return false, 0, 0, 0
	if state.md_metrics_count <= 0 do return false, 0, 0, 0

	oldest_idx := (state.md_metrics_head - state.md_metrics_count + MD_METRICS_HISTORY_CAP) % MD_METRICS_HISTORY_CAP
	newest_idx := (state.md_metrics_head - 1 + MD_METRICS_HISTORY_CAP) % MD_METRICS_HISTORY_CAP
	oldest := state.md_metrics_history[oldest_idx].metrics
	newest := state.md_metrics_history[newest_idx].metrics

	qmax = 0
	for i in 0 ..< state.md_metrics_count {
		idx := (oldest_idx + i) % MD_METRICS_HISTORY_CAP
		qmax = max(qmax, state.md_metrics_history[idx].metrics.trade_backlog)
	}
	drop_delta = newest.drop_count - oldest.drop_count
	rc_delta = newest.reconnect_count - oldest.reconnect_count
	if drop_delta < 0 do drop_delta = 0
	if rc_delta < 0 do rc_delta = 0
	return true, qmax, drop_delta, rc_delta
}

@(private = "file")
stream_view_find_slot :: proc(reg: ^Stream_View_Registry, subject_id: u64) -> int {
	if reg == nil do return -1
	if subject_id == 0 do return -1
	for i in 0 ..< len(reg.slots) {
		if reg.slots[i].used && reg.slots[i].subject_id == subject_id do return i
	}
	return -1
}

@(private = "file")
stream_view_repair_invariants :: proc(reg: ^Stream_View_Registry) -> bool {
	if reg == nil do return false
	repaired := false

	used_count := 0
	first_used_subject := u64(0)
	has_first_used := false

	for i in 0 ..< len(reg.slots) {
		if !reg.slots[i].used do continue
		used_count += 1
		if !has_first_used {
			has_first_used = true
			first_used_subject = reg.slots[i].subject_id
		}
	}

	if reg.count != used_count {
		reg.count = used_count
		repaired = true
	}

	if reg.count <= 0 {
		if reg.has_active || reg.active_subject_id != 0 {
			reg.has_active = false
			reg.active_subject_id = 0
			repaired = true
		}
		if repaired { reg.repair_count += 1 }
		return repaired
	}

	if reg.has_active {
		if stream_view_find_slot(reg, reg.active_subject_id) < 0 {
			reg.active_subject_id = first_used_subject
			repaired = true
		}
	} else {
		reg.has_active = true
		reg.active_subject_id = first_used_subject
		repaired = true
	}

	if repaired {
		reg.repair_count += 1
	}
	return repaired
}

@(private = "file")
stream_view_get_or_alloc_slot :: proc(reg: ^Stream_View_Registry, subject_id: u64, frame: u64) -> ^Stream_View_Slot {
	if reg == nil do return nil
	if subject_id == 0 do return nil

	if idx := stream_view_find_slot(reg, subject_id); idx >= 0 {
		reg.slots[idx].last_seen_frame = frame
		return &reg.slots[idx]
	}

	slot_idx := -1
	for i in 0 ..< len(reg.slots) {
		if !reg.slots[i].used {
			slot_idx = i
			break
		}
	}

	if slot_idx < 0 {
		oldest_idx := -1
		oldest_frame := u64(0)
		for i in 0 ..< len(reg.slots) {
			if reg.has_active && reg.slots[i].subject_id == reg.active_subject_id do continue
			if oldest_idx < 0 || reg.slots[i].last_seen_frame < oldest_frame {
				oldest_idx = i
				oldest_frame = reg.slots[i].last_seen_frame
			}
		}
		if oldest_idx < 0 {
			oldest_idx = 0
			oldest_frame = reg.slots[0].last_seen_frame
			for i in 1 ..< len(reg.slots) {
				if reg.slots[i].last_seen_frame < oldest_frame {
					oldest_idx = i
					oldest_frame = reg.slots[i].last_seen_frame
				}
			}
		}
		slot_idx = oldest_idx
		if slot_idx >= 0 && reg.slots[slot_idx].used {
			reg.eviction_count += 1
		}
	} else {
		reg.count += 1
	}

	reg.slots[slot_idx] = Stream_View_Slot{
		used            = true,
		subject_id      = subject_id,
		last_seen_frame = frame,
	}
	if !reg.has_active {
		reg.has_active = true
		reg.active_subject_id = subject_id
	}
	return &reg.slots[slot_idx]
}

@(private = "file")
stream_view_cycle_active :: proc(reg: ^Stream_View_Registry) -> bool {
	if reg == nil || !reg.has_active do return false
	if reg.count <= 1 do return false

	curr_idx := stream_view_find_slot(reg, reg.active_subject_id)
	start := curr_idx
	if start < 0 do start = 0

	for step in 1 ..< len(reg.slots) + 1 {
		idx := (start + step) % len(reg.slots)
		if !reg.slots[idx].used do continue
		if reg.slots[idx].subject_id == reg.active_subject_id do continue
		reg.active_subject_id = reg.slots[idx].subject_id
		return true
	}
	return false
}

@(private = "file")
sync_active_stream_view_to_global_stores :: proc(state: ^App_State) {
	reg := state.stream_views
	if reg == nil do return
	if !reg.has_active do return
	if idx := stream_view_find_slot(reg, reg.active_subject_id); idx >= 0 {
		slot := reg.slots[idx]
		state.trades_store = slot.trades_store
		state.orderbook_store = slot.orderbook_store
		state.heatmap_store = {}
		if slot.has_heatmap_snapshot {
			services.push_heatmap_snapshot(&state.heatmap_store, slot.heatmap_snapshot)
		}
		state.vpvr_store = slot.vpvr_store
		state.stats_store = slot.stats_store
		state.candle_store = slot.candle_store
	}
}

@(private = "file")
handle_stream_hotkeys :: proc(state: ^App_State, input: ports.Input_State) -> bool {
	tab_down := .Tab in input.keys.pressed
	tab_was_down := .Tab in state.last_keys_pressed
	state.last_keys_pressed = input.keys.pressed

	if !(tab_down && !tab_was_down) do return false
	_ = stream_view_repair_invariants(state.stream_views)

	is_offline := current_conn_status(state) == .Offline
	if is_offline {
		// In offline mode with no stream views, re-populate demo data.
		state.trades_store = {}
		state.orderbook_store = {}
		state.heatmap_store = {}
		state.vpvr_store = {}
		state.stats_store = {}
		state.candle_store = {}
		services.fill_demo_trades(&state.trades_store)
		services.fill_demo_orderbook(&state.orderbook_store)
		services.fill_demo_heatmaps(&state.heatmap_store)
		services.fill_demo_vpvr(&state.vpvr_store)
		services.fill_demo_stats(&state.stats_store)
		services.fill_demo_candles(&state.candle_store)
		return true
	}

	if !stream_view_cycle_active(state.stream_views) do return false

	sync_active_stream_view_to_global_stores(state)
	persist_active_stream_subject(state)
	if now_ms := current_now_ms(state); now_ms > 0 {
		state.candle_last_recv_local_ms = now_ms
	}
	return true
}

@(private = "file")
tf_key_index :: proc(input: ports.Input_State, last_keys: bit_set[ports.Key]) -> int {
	// Detect edge (key-down this frame but not last frame).
	if .Num_1 in input.keys.pressed && !(.Num_1 in last_keys) do return 0
	if .Num_2 in input.keys.pressed && !(.Num_2 in last_keys) do return 1
	if .Num_3 in input.keys.pressed && !(.Num_3 in last_keys) do return 2
	if .Num_4 in input.keys.pressed && !(.Num_4 in last_keys) do return 3
	if .Num_5 in input.keys.pressed && !(.Num_5 in last_keys) do return 4
	if .Num_6 in input.keys.pressed && !(.Num_6 in last_keys) do return 5
	return -1
}

@(private = "file")
handle_tf_hotkeys :: proc(state: ^App_State, input: ports.Input_State) -> bool {
	idx := tf_key_index(input, state.last_keys_pressed)
	if idx < 0 || idx >= len(TF_OPTIONS) do return false
	if idx == state.active_tf_idx do return false

	state.active_tf_idx = idx
	opts := TF_OPTIONS
	tf := opts[idx]

	// Update TF filter in the adapter.
	if state.marketdata.set_candle_tf != nil {
		state.marketdata.set_candle_tf(tf)
	}

	// Clear candle store for new TF data.
	state.candle_store.head = 0
	state.candle_store.count = 0

	// Also clear candle store in the active stream view slot.
	if slot := stream_view_active_slot(state.stream_views); slot != nil {
		slot.candle_store.head = 0
		slot.candle_store.count = 0
	}

	// Request historical data for the new TF.
	state.getrange_pending = true
	if state.marketdata.send_getrange != nil {
		// Build candle subject for active venue/symbol.
		if reg := state.stream_views; reg != nil && reg.has_active {
			if state.marketdata.describe_stream != nil {
				info: ports.MD_Stream_Info
				if state.marketdata.describe_stream(reg.active_subject_id, &info) {
					candle_subject := util.build_subject(info.venue, info.symbol, .Candles)
					state.marketdata.send_getrange(candle_subject, services.CANDLE_CAP)
					delete(candle_subject)
				}
			}
		}
	}

	return true
}

@(private = "file")
channel_short_label :: proc(ch: ports.MD_Channel) -> string {
	switch ch {
	case .Trades:
		return "trades"
	case .Orderbook:
		return "orderbook"
	case .Stats:
		return "stats"
	case .Heatmaps:
		return "heatmap"
	case .VPVR:
		return "vpvr"
	case .Candles:
		return "candles"
	}
	return "?"
}

@(private = "file")
parse_channel_short_label :: proc(s: string) -> (ports.MD_Channel, bool) {
	switch s {
	case "trades":
		return .Trades, true
	case "orderbook":
		return .Orderbook, true
	case "stats":
		return .Stats, true
	case "heatmap":
		return .Heatmaps, true
	case "vpvr":
		return .VPVR, true
	case "candles":
		return .Candles, true
	}
	return {}, false
}

@(private = "file")
format_timeframe_short_into :: proc(buf: []u8, tf_ms: i64) -> string {
	if tf_ms <= 0 do return ""
	if tf_ms % 86_400_000 == 0 do return fmt.bprintf(buf, "%dd", tf_ms / 86_400_000)
	if tf_ms % 3_600_000 == 0 do return fmt.bprintf(buf, "%dh", tf_ms / 3_600_000)
	if tf_ms % 60_000 == 0 do return fmt.bprintf(buf, "%dm", tf_ms / 60_000)
	if tf_ms % 1000 == 0 do return fmt.bprintf(buf, "%ds", tf_ms / 1000)
	return fmt.bprintf(buf, "%dms", tf_ms)
}

@(private = "file")
stream_view_active_slot :: proc(reg: ^Stream_View_Registry) -> ^Stream_View_Slot {
	if reg == nil do return nil
	if !reg.has_active do return nil
	if idx := stream_view_find_slot(reg, reg.active_subject_id); idx >= 0 {
		return &reg.slots[idx]
	}
	return nil
}

@(private = "file")
apply_trade_to_store :: proc(store: ^services.Trades_Store, t: ports.MD_Trade_Event) {
	services.push_trade(store, services.Trade_Entry{
		price = t.price,
		qty   = t.qty,
		side  = t.is_buy ? .Buy : .Sell,
		unix  = t.unix,
	})
}

@(private = "file")
apply_orderbook_to_store :: proc(store: ^services.Orderbook_Store, ob: ports.MD_Orderbook_Event) {
	services.update_orderbook(store,
		ob.ask_prices[:ob.ask_count], ob.ask_sizes[:ob.ask_count],
		ob.bid_prices[:ob.bid_count], ob.bid_sizes[:ob.bid_count],
		ob.last_price, ob.unix,
	)
}

@(private = "file")
apply_stats_to_store :: proc(store: ^services.Stats_Store, st: ports.MD_Stats_Event) {
	services.push_stats(store, services.Stats_Entry{
		mark_price = st.mark_price,
		funding    = st.funding,
		liq_buy    = st.tbuy,
		liq_sell   = st.tsell,
		unix       = st.unix,
	})
}

@(private = "file")
build_heatmap_snapshot :: proc(hm: ports.MD_Heatmap_Event) -> services.Heatmap_Snapshot {
	snap: services.Heatmap_Snapshot
	snap.unix = hm.unix
	snap.price_group = hm.price_group
	snap.min_price = hm.min_price
	snap.max_price = hm.max_price
	snap.max_size = hm.max_size
	snap.level_count = min(hm.level_count, services.HEATMAP_LEVEL_CAP)
	for j in 0 ..< snap.level_count {
		snap.levels[j] = services.Heatmap_Level{
			price = hm.prices[j],
			size  = hm.sizes[j],
		}
	}
	return snap
}

@(private = "file")
apply_vpvr_to_store :: proc(store: ^services.VPVR_Store, vpvr: ports.MD_VPVR_Event) {
	count := min(vpvr.level_count, services.VPVR_BUCKET_CAP)
	services.update_vpvr(
		store,
		vpvr.prices, vpvr.buys, vpvr.sells,
		count, vpvr.price_group,
	)
}

@(private = "file")
apply_candle_to_store :: proc(store: ^services.Candle_Store, cd: ports.MD_Candle_Event) {
	services.push_candle(store, services.Candle_Entry{
		open            = cd.open,
		high            = cd.high,
		low             = cd.low,
		close           = cd.close,
		volume          = cd.volume,
		buy_vol         = cd.buy_vol,
		sell_vol        = cd.sell_vol,
		trade_count     = cd.trade_count,
		window_start_ts = cd.window_start_ts,
		window_end_ts   = cd.window_end_ts,
		is_closed       = cd.is_closed,
	})
}

@(private = "file")
drain_marketdata :: proc(state: ^App_State) -> int {
	processed := 0
	// Drain marketdata events (non-blocking).
		if state.marketdata.poll != nil {
		events: [MD_POLL_CAP]ports.MD_Event
		n := state.marketdata.poll(events[:])
			processed = n
			for i in 0 ..< n {
				evt := events[i]
				subject_id := evt.source.subject_id
				slot := stream_view_get_or_alloc_slot(state.stream_views, subject_id, state.frame)
					if slot != nil {
						slot.last_seen_frame = state.frame
						slot.has_channel = true
						slot.channel = evt.source.channel
					}
					if state.has_pending_active_subject && subject_id != 0 && subject_id == state.pending_active_subject_id {
						if state.stream_views != nil {
							state.stream_views.has_active = true
							state.stream_views.active_subject_id = subject_id
							sync_active_stream_view_to_global_stores(state)
							persist_active_stream_subject(state)
						}
						state.has_pending_active_subject = false
						state.pending_active_subject_id = 0
					}
					is_active_stream := subject_id == 0 || state.stream_views == nil || !state.stream_views.has_active || state.stream_views.active_subject_id == subject_id
				switch evt.kind {
				case .Trade:
					t := evt.data.trade
					if slot != nil {
						apply_trade_to_store(&slot.trades_store, t)
					}
					if is_active_stream {
						apply_trade_to_store(&state.trades_store, t)
					}
				case .Orderbook_Snapshot:
					ob := evt.data.ob
					if slot != nil {
						apply_orderbook_to_store(&slot.orderbook_store, ob)
					}
					if is_active_stream {
						apply_orderbook_to_store(&state.orderbook_store, ob)
					}
					case .Stats:
						st := evt.data.stats
						if slot != nil {
							apply_stats_to_store(&slot.stats_store, st)
					}
					if is_active_stream {
						apply_stats_to_store(&state.stats_store, st)
					}
				case .Heatmap:
					hm := evt.data.heatmap
					snap := build_heatmap_snapshot(hm)
					if slot != nil {
						slot.has_heatmap_snapshot = true
						slot.heatmap_snapshot = snap
					}
					if is_active_stream {
						services.push_heatmap_snapshot(&state.heatmap_store, snap)
					}
				case .VPVR:
					vpvr := evt.data.vpvr
					if slot != nil {
						apply_vpvr_to_store(&slot.vpvr_store, vpvr)
					}
					if is_active_stream {
						apply_vpvr_to_store(&state.vpvr_store, vpvr)
					}
					case .Candle:
						cd := evt.data.candle
						if slot != nil {
							tf_ms := cd.window_end_ts - cd.window_start_ts
							if tf_ms > 0 {
								slot.has_timeframe_ms = true
								slot.timeframe_ms = tf_ms
							}
							apply_candle_to_store(&slot.candle_store, cd)
						}
						now_ms := current_now_ms(state)
					if is_active_stream && now_ms > 0 {
						state.candle_last_recv_local_ms = now_ms
					}
					if is_active_stream {
						apply_candle_to_store(&state.candle_store, cd)
					}
				case .Range_Candle_Batch:
					batch := evt.data.range_candles
					for ci in 0 ..< batch.count {
						cd := batch.candles[ci]
						if slot != nil {
							apply_candle_to_store(&slot.candle_store, cd)
						}
						if is_active_stream {
							apply_candle_to_store(&state.candle_store, cd)
						}
					}
					if batch.is_last {
						state.getrange_pending = false
					}
				}
			}
		}
		if state.stream_views != nil && processed > 0 {
			if stream_view_repair_invariants(state.stream_views) {
				sync_active_stream_view_to_global_stores(state)
			}
		}
		return processed
	}

@(private = "file")
build_ui :: proc(state: ^App_State, input: ports.Input_State) -> ^ui.Command_Buffer {
	ui.reset(&state.cmd_buf)

	ui.push(&state.cmd_buf, ui.Cmd_Clear{color = {0.04, 0.04, 0.04, 1.0}})

	viewport_w := input.viewport_size.x
	viewport_h := input.viewport_size.y
	if viewport_w <= 0 do viewport_w = 800
	if viewport_h <= 0 do viewport_h = 600

	// --- Top bar: title + connection status ---
	draw_top_bar(state, viewport_w)

	pad := f32(8)
	if viewport_w < 520 do pad = 6
	if viewport_w < 420 do pad = 4
	gap := f32(6)
	if viewport_w < 420 do gap = 4

	top_bar_h := f32(26)
	content := ui.rect_xywh(
		pad, top_bar_h + 2,
		viewport_w - pad * 2,
		viewport_h - (top_bar_h + 2) - pad,
	)
	if content.size.x < 1 do content.size.x = 1
	if content.size.y < 1 do content.size.y = 1

	mobile := viewport_w < 700

	chart_vp, stats_vp, trades_vp, orderbook_vp: ui.Rect
	orderbook_max_rows := 20

	remaining := content
	chart_h := remaining.size.y * (mobile ? 0.42 : 0.40)
	if mobile {
		if chart_h < 180 do chart_h = min(f32(180), remaining.size.y)
		if chart_h > 340 do chart_h = 340
	} else {
		if chart_h < 220 do chart_h = min(f32(220), remaining.size.y)
		if chart_h > 300 do chart_h = 300
	}
	chart_vp = ui.rect_cut_top(&remaining, chart_h)
	if remaining.size.y > gap do ui.rect_cut_top(&remaining, gap)

	stats_h := remaining.size.y * (mobile ? 0.14 : 0.16)
	if stats_h < 56 do stats_h = min(f32(56), remaining.size.y)
	if stats_h > 96 do stats_h = 96
	stats_vp = ui.rect_cut_top(&remaining, stats_h)
	if remaining.size.y > gap do ui.rect_cut_top(&remaining, gap)

	if mobile {
		top_panel_h := (remaining.size.y - gap) * 0.5
		if top_panel_h < 110 do top_panel_h = min(f32(110), remaining.size.y)
		trades_vp = ui.rect_cut_top(&remaining, top_panel_h)
		if remaining.size.y > gap do ui.rect_cut_top(&remaining, gap)
		orderbook_vp = remaining
		if orderbook_vp.size.y < 160 {
			orderbook_max_rows = 10
		} else if orderbook_vp.size.y < 220 {
			orderbook_max_rows = 14
		}
	} else {
		left_w := (remaining.size.x - gap) * 0.5
		trades_vp = ui.rect_cut_left(&remaining, left_w)
		if remaining.size.x > gap do ui.rect_cut_left(&remaining, gap)
		orderbook_vp = remaining
	}

	// --- Candlestick chart (hero widget) ---
	candle_health_label, candle_health_detail, candle_health_color := build_candle_health_ui(state)
	tf_opts := TF_OPTIONS
	widgets.candle_widget(&state.cmd_buf, widgets.Candle_Widget_Data{
		store         = &state.candle_store,
		viewport      = chart_vp,
		text          = state.text,
		input         = input,
		scroll_x      = &state.candle_scroll_x,
		zoom_level    = &state.candle_zoom,
		health_label  = candle_health_label,
		health_detail = candle_health_detail,
		health_color  = candle_health_color,
		tf_label      = tf_opts[state.active_tf_idx],
	})

	// --- Stats / Liquidation bar chart ---
	stats_buf: [services.STATS_CAP]model.Stat
	sc := 0
	for i in 0 ..< state.stats_store.count {
		e := services.get_stats(&state.stats_store, i)
		stats_buf[sc] = model.Stat{
			unix       = e.unix,
			tbuy       = e.liq_buy,
			tsell      = e.liq_sell,
			mark_price = e.mark_price,
			funding    = e.funding,
		}
		sc += 1
	}

	x_min, x_max: f64
	if sc > 0 {
		oldest := stats_buf[sc - 1].unix
		newest := stats_buf[0].unix
		x_min = f64(oldest) - 60
		x_max = f64(newest) + 60
	}

	widgets.trade_counter(&state.cmd_buf, widgets.Trade_Counter_Data{
		stats         = stats_buf[:sc],
		viewport      = stats_vp,
		timeframe     = 60,
		x_min         = x_min,
		x_max         = x_max,
		bar_width_pct = CANDLE_WIDTH_PCT,
		text          = state.text,
	})

	// --- Bottom row: Trades (left) + Orderbook (right) ---
	widgets.trades_widget(&state.cmd_buf, widgets.Trades_Widget_Data{
		store    = &state.trades_store,
		viewport = trades_vp,
		text     = state.text,
		scroll_y = &state.scroll_y,
		input    = input,
	})

	widgets.orderbook_widget(&state.cmd_buf, widgets.Orderbook_Widget_Data{
		store       = &state.orderbook_store,
		viewport    = orderbook_vp,
		text        = state.text,
		scroll_y    = &state.ob_scroll_y,
		input       = input,
		price_group = 10.0,
		max_rows    = orderbook_max_rows,
	})

	return &state.cmd_buf
}

// --- Top bar: title + connection status ---

@(private = "file")
draw_top_bar :: proc(state: ^App_State, viewport_w: f32) {
	bar_w := viewport_w
	if bar_w <= 0 do bar_w = 800
	// Background bar.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, 0}, size = {bar_w, 26}},
		color = ui.COL_PANEL_BG,
	})

	// Title (left).
	ui.push_text(&state.cmd_buf, {8, 17}, "MARKET RACCOON",
		ui.with_alpha(ui.COL_WHITE, 0.8), ui.FONT_SIZE_SM, .Bold)

	// Active stream summary (center-left).
	if reg := state.stream_views; reg != nil && reg.count > 0 {
		active_id := u64(0)
		active_channel := "?"
		active_name := "unknown"
		active_timeframe := ""
		if slot := stream_view_active_slot(reg); slot != nil {
			active_id = slot.subject_id
			if slot.has_channel {
				active_channel = channel_short_label(slot.channel)
			}
			if state.marketdata.describe_stream != nil {
				info: ports.MD_Stream_Info
				if state.marketdata.describe_stream(slot.subject_id, &info) {
					name_buf: [128]u8
					if len(info.venue) > 0 && len(info.symbol) > 0 {
						active_name = fmt.bprintf(name_buf[:], "%s:%s", info.venue, info.symbol)
					}
					if len(info.timeframe) > 0 {
						active_timeframe = info.timeframe
					}
					if info.channel != slot.channel {
						active_channel = channel_short_label(info.channel)
					}
				}
			}
			if slot.has_timeframe_ms {
				tf_buf: [24]u8
				active_timeframe = format_timeframe_short_into(tf_buf[:], slot.timeframe_ms)
			}
		}

		info_buf: [160]u8
		if len(active_timeframe) > 0 {
			info := fmt.bprintf(info_buf[:], "streams %d  %s@%s  %s  %x  [Tab]", reg.count, active_name, active_timeframe, active_channel, active_id)
			info_x := f32(150)
			info_w := state.text.measure(ui.FONT_SIZE_SM, info).x
			if info_x + info_w < bar_w - 170 {
				ui.push_text(&state.cmd_buf, {info_x, 17}, info,
					ui.with_alpha(ui.COL_WHITE, 0.55), ui.FONT_SIZE_SM, .Mono)
			}
		} else {
			info := fmt.bprintf(info_buf[:], "streams %d  %s  %s  %x  [Tab]", reg.count, active_name, active_channel, active_id)
			info_x := f32(150)
			info_w := state.text.measure(ui.FONT_SIZE_SM, info).x
			if info_x + info_w < bar_w - 170 {
				ui.push_text(&state.cmd_buf, {info_x, 17}, info,
					ui.with_alpha(ui.COL_WHITE, 0.55), ui.FONT_SIZE_SM, .Mono)
			}
		}
	}

	// Connection status (right).
	status: ports.MD_Conn_Status = .Offline
	if state.marketdata.conn_status != nil {
		status = state.marketdata.conn_status()
	}

	label: string
	color: ui.Color
	switch status {
	case .Connected:
		label = "LIVE"
		color = ui.COL_GREEN
	case .Connecting:
		label = "CONNECTING..."
		color = ui.COL_YELLOW_ACCENT
	case .Reconnecting:
		label = "RECONNECTING..."
		color = ui.COL_YELLOW_ACCENT
	case .Offline:
		label = "OFFLINE"
		color = ui.with_alpha(ui.COL_WHITE, 0.4)
	}

	label_w := state.text.measure(ui.FONT_SIZE_SM, label).x
	x := bar_w - 8 - label_w
	if x < 24 do x = 24
	y := f32(17)

	// Compact marketdata runtime metrics (left of connection status).
	if state.marketdata.metrics != nil {
		m: ports.MD_Runtime_Metrics
			if state.marketdata.metrics(&m) {
				metrics_buf: [128]u8
				metrics_text := fmt.bprintf(metrics_buf[:], "subs:%d q:%d drop:%d p:%d rc:%d",
					m.active_subs, m.trade_backlog, m.drop_count, m.latest_pending, m.reconnect_count)
				metrics_w := state.text.measure(ui.FONT_SIZE_SM, metrics_text).x
				metrics_x := x - 18 - metrics_w
				if metrics_x > 150 {
					if ok, qmax, drop_delta, rc_delta := metrics_history_summary(state); ok {
						reg_ev := u64(0)
						reg_fix := u64(0)
						if state.stream_views != nil {
							reg_ev = state.stream_views.eviction_count
							reg_fix = state.stream_views.repair_count
						}
						hist_buf: [160]u8
						hist_text := fmt.bprintf(hist_buf[:], "qmax:%d d+:%d rc+:%d ev:%d fix:%d",
							qmax, drop_delta, rc_delta, reg_ev, reg_fix)
						hist_w := state.text.measure(ui.FONT_SIZE_SM, hist_text).x
						hist_x := metrics_x - 12 - hist_w
						if hist_x > 150 {
							ui.push_text(&state.cmd_buf, {hist_x, y}, hist_text,
								ui.with_alpha(ui.COL_WHITE, 0.40), ui.FONT_SIZE_SM, .Mono)
						}
					}
					ui.push_text(&state.cmd_buf, {metrics_x, y}, metrics_text,
						ui.with_alpha(ui.COL_WHITE, 0.48), ui.FONT_SIZE_SM, .Mono)
				}
			}
		}

	// Dot indicator.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {x - 10, y - 4}, size = {6, 6}},
		color = color,
	})
	ui.push_text(&state.cmd_buf, {x, y}, label, color, ui.FONT_SIZE_SM, .Mono)
}
