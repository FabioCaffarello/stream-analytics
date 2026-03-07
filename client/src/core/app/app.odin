package app

import "core:fmt"
import "core:time"
import "mr:layers"
import "mr:md_common"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:ui"
import "mr:util"
import "mr:widgets"

MD_POLL_CAP :: 64

TF_OPTIONS :: [9]string{"1s", "5s", "1m", "5m", "15m", "30m", "1h", "4h", "1d"}
TF_OPTION_MS :: [9]i64{1_000, 5_000, 60_000, 300_000, 900_000, 1_800_000, 3_600_000, 14_400_000, 86_400_000}

COMPARE_WIDGET_OPTIONS :: [3]string{"OB", "Trades", "Candles"}

// --- Widget Instance Model (PRD-0005-B M0) ---

CELL_MAX :: 12

Widget_Kind :: enum u8 {
	Candle,
	Stats,
	Counter,
	Heatmap,
	VPVR,
	Trades,
	Orderbook,
	DOM,
	Empty,
}

TOP_BAR_H :: f32(32)
TOP_BAR_H_COMPACT :: f32(28)

Route :: enum u8 {
	Dashboard,
	Markets,
	Settings,
}

Layout_Mode :: enum u8 {
	Preset,
	Custom,
}

Candle_Health :: enum u8 {
	No_Data,
	OK,
	Lagging,
	Stale,
}

STREAM_VIEW_CAP :: 32
MD_METRICS_HISTORY_CAP :: 120
UI_ACTION_CAP :: 8

UI_Action_Kind :: enum u8 {
	Cycle_Stream_Next,
	Cycle_Stream_Prev,
	Set_Timeframe,
	Toggle_Sidebar,
	Toggle_Panel,
	Toggle_Help,
	Toggle_Telemetry_HUD,
	Toggle_Compare,
	Add_Compare_Stream,
	Exit_Compare,
	Navigate_Route,
	Toggle_Detail_Panel,
	Set_Layout_Preset,
	Toggle_Connection_Modal,
	Set_Cell_Widget,
	Set_Cell_Stream,
	Add_Cell,
	Remove_Cell,
	Toggle_Focus_Mode,
	Toggle_Stream_Picker,
	Pick_Stream,
	Toggle_MA,
	Toggle_BBands,
	Toggle_VWAP,
	Toggle_RSI,
	Toggle_MACD,
	Toggle_Funding,
	Toggle_Liq,
	Toggle_Trade_Counter,
	Delete_Draw_Tool,
	Subscribe_Market,
	Unsubscribe_Market,
	Toggle_Widget_Catalog,
	Open_Cell_Stream_Picker,
	Close_Cell_Stream_Picker,
	Toggle_Zen_Mode,
	Set_Cell_Timeframe,
	Set_Compare_Pane_Timeframe, // S39: TF change directed at a compare pane
	Focus_Compare_Pane,         // S39: set focused pane in compare mode
	Resync_Active_Stream,
	Select_Profile,
	Add_Profile,
	Remove_Profile,
	Apply_Profile,
	Connect_Profile,
	Disconnect_Profile,
	Capture_Runtime_Snapshot, // S46: deterministic snapshot to clipboard
}

UI_Action :: struct {
	kind:           UI_Action_Kind,
	timeframe_idx:  int,
	panel_idx:      int,
	route:          Route,
	layout_preset:  int,
	cell_idx:       int,
	profile_idx:    int,
	widget_kind:    Widget_Kind,
	stream_idx:     int,
	subject_id:     u64,        // for Pick_Stream
	market_entry_idx: int,      // for Subscribe_Market
	pane_idx:       int,        // S39: for Set_Compare_Pane_Timeframe / Focus_Compare_Pane
	// Intent binding transport (PRD-0009): venue/symbol for Set_Cell_Stream / Add_Cell.
	bind_venue:     string,
	bind_symbol:    string,
}

Stream_View_Slot :: struct {
	used:            bool,
	subject_id:      u64,
	last_seen_frame: u64,
	has_stream_info: bool,
	stream_info:     ports.MD_Stream_Info,
	has_channel:     bool,
	channel:         ports.MD_Channel,
	has_timeframe_ms: bool,
	timeframe_ms:     i64,
	heatmap_snapshot:     services.Heatmap_Snapshot,
	heatmap_store:        services.Heatmap_Store,
	vpvr_store:           services.VPVR_Store,
	trades_store:    services.Trades_Store,
	orderbook_store: services.Orderbook_Store,
	stats_store:     services.Stats_Store,
	candle_store:    services.Candle_Store,
	// S23: Canonical per-stream apply state (replaces scattered booleans above).
	apply_state:     md_common.Stream_Apply_State,
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
	active_tf_idx:         int,
	stream_count:          int,
	has_active_stream:     bool,
	active_subject_id:     u64,
	stream_evictions:      u64,
	stream_repairs:        u64,
	layer_stream_entries:  int,
	layer_stream_evictions: u64,
	pending_restore:       bool,
	has_md_metrics:        bool,
	md_metrics:            ports.MD_Runtime_Metrics,
	md_qmax_recent:        int,
	md_drop_delta_recent:  int,
	md_rc_delta_recent:    int,
	md_candle_backlog_recent: int,
	md_msg_rate:          f64,
	md_bytes_rate:        f64,
	md_parsed_msgs_total: u64,
	md_parsed_bytes_total: u64,
	md_parse_arena_resets_total: u64,
	md_alloc_estimate_total: u64,
	md_alloc_estimate_frame: i64,
	md_canonical_stats_frames: u64,
	md_stats_fallback_frames:  u64,
	md_canonical_evidence_frames: u64,
	md_legacy_evidence_frames:    u64,
	md_evidence_fallback_frames:  u64,
	md_canonical_signal_frames:   u64,
	md_legacy_signal_frames:      u64,
	md_signal_fallback_frames:    u64,
	md_legacy_evidence_rejected:  u64,
	md_legacy_signal_rejected:    u64,
	ui_actions_enqueued_total: u64,
	ui_action_drops:       u64,
	stream_switches_total: u64,
	timeframe_switches_total: u64,
	active_live_stats:   bool,
	active_live_heatmap: bool,
	active_live_vpvr:    bool,
	active_live_candle:  bool,
	active_synth_heatmap: bool,
	active_synth_vpvr:    bool,
	compare_mode:        bool,
	compare_widget_idx:  int,
	compare_count:       int,
	w_trades_count:        int,
	w_orderbook_asks:      int,
	w_orderbook_bids:      int,
	w_stats_count:         int,
	w_stats_parse_total:   u64,
	w_stats_fallback_total: u64,
	w_stats_drop_total:    u64,
	w_stats_drop_capacity_total: u64,
	w_stats_drop_render_overflow_total: u64,
	w_stats_entries:       int,
	w_stats_max_entries:   int,
	w_stats_evicted_total: u64,
	w_stats_render_p95_us: i64,
	w_stats_render_p99_us: i64,
	w_stats_render_budget_us: i64,
	w_stats_render_over_budget: u64,
	w_heatmap_snaps:       int,
	w_vpvr_levels:         int,
	w_candle_count:        int,
	w_evidence_count:      int,
	w_signal_count:        int,
	w_signal_link_total:   u64,
	w_signal_link_evidence_seq: i64,
	w_dom_parse_total:     u64,
	w_dom_fallback_total:  u64,
	w_dom_drop_total:      u64,
	w_dom_drop_capacity_total: u64,
	w_dom_drop_render_overflow_total: u64,
	w_dom_entries:         int,
	w_dom_max_entries:     int,
	w_dom_evicted_total:   u64,
	w_dom_render_p95_us:   i64,
	w_dom_render_p99_us:   i64,
	w_dom_render_budget_us: i64,
	w_dom_render_over_budget: u64,
	w_tape_parse_total:    u64,
	w_tape_fallback_total: u64,
	w_tape_drop_total:     u64,
	w_tape_drop_capacity_total: u64,
	w_tape_drop_render_overflow_total: u64,
	w_tape_entries:        int,
	w_tape_max_entries:    int,
	w_tape_evicted_total:  u64,
	w_tape_render_p95_us:  i64,
	w_tape_render_p99_us:  i64,
	w_tape_render_budget_us: i64,
	w_tape_render_over_budget: u64,
	w_evidence_parse_total:    u64,
	w_evidence_fallback_total: u64,
	w_evidence_drop_total:     u64,
	w_evidence_drop_capacity_total: u64,
	w_evidence_drop_render_overflow_total: u64,
	w_evidence_entries:     int,
	w_evidence_max_entries: int,
	w_evidence_evicted_total: u64,
	w_evidence_render_p95_us:  i64,
	w_evidence_render_p99_us:  i64,
	w_evidence_render_budget_us: i64,
	w_evidence_render_over_budget: u64,
	w_signal_parse_total:    u64,
	w_signal_fallback_total: u64,
	w_signal_drop_total:     u64,
	w_signal_drop_capacity_total: u64,
	w_signal_drop_render_overflow_total: u64,
	w_signal_entries:       int,
	w_signal_max_entries:   int,
	w_signal_evicted_total: u64,
	w_signal_render_p95_us:  i64,
	w_signal_render_p99_us:  i64,
	w_signal_render_budget_us: i64,
	w_signal_render_over_budget: u64,
	w_stats_state:          layers.Layer_Widget_State,
	w_signal_state:         layers.Layer_Widget_State,
	w_evidence_state:       layers.Layer_Widget_State,
	layout_version:         int,
	layout_migrated:        bool,
	layout_link_enabled:    bool,
	ind_rsi_enabled:           bool,
	ind_macd_enabled:          bool,
	ind_funding_enabled:       bool,
	ind_liq_enabled:           bool,
	ind_trade_counter_enabled: bool,
	ind_rsi_rendered:          bool,
	ind_macd_rendered:         bool,
	ind_funding_rendered:      bool,
	ind_liq_rendered:          bool,
	ind_trade_counter_rendered: bool,
	// Performance metrics (PRD-0005-B M5).
	frame_time_p50_us:     i64,
	frame_time_p95_us:     i64,
	frame_time_p99_us:     i64,
	cmd_buf_count:           int,
	cmd_frame_arena_bytes:   int,
	cmd_frame_arena_capacity: int,
	cell_count:            int,
	cell_tf_idxs:          [CELL_MAX]int,  // per-cell tf_idx (-1 = global)
}

Telemetry_HUD_Cache :: struct {
	last_update_ms: i64,
	mps_buf: [32]u8,
	mps_len: int,
	bps_buf: [32]u8,
	bps_len: int,
	cb_buf: [24]u8,
	cb_len: int,
	arena_buf: [40]u8,
	arena_len: int,
	pm_buf: [32]u8,
	pm_len: int,
	pr_buf: [32]u8,
	pr_len: int,
	pb_buf: [32]u8,
	pb_len: int,
	// Sub-phase timing cache.
	phase_buf: [128]u8,
	phase_len: int,
	// S27: Per-artifact event count cache.
	artifact_buf: [128]u8,
	artifact_len: int,
	// S27: Composition + apply state summary cache.
	apply_buf: [96]u8,
	apply_len: int,
	// S28: Per-artifact age cache.
	age_buf: [128]u8,
	age_len: int,
	// S31: Aggregate health summary cache.
	agg_buf: [96]u8,
	agg_len: int,
}

App_State :: struct {
	cmd_buf:         ui.Command_Buffer,
	text:            ports.Text_Port,
	fonts:           ports.Font_Port,
	marketdata:      ports.Marketdata_Port,

	// Entity-Component System (M2).
	world: Entity_World,

	// Sub-states (M2).
	conn:           Connection_State,
	telemetry:      Telemetry_State,
	compare:        Compare_State,
	overlays:       Overlay_State,
	chrome:         UI_Chrome_State,
	zen:            Zen_State,
	whale:          Whale_Alert_State,
	toast:          Toast_State,
	error_state:    Error_State,
	log_state:      Log_State,
	evidence:       Evidence_State,
	active_metrics: Active_Stream_Metrics,
	bp_assist:      Backpressure_Assist_State,

	stores:          Global_Stores,
	stream_views:    ^Stream_View_Registry,
	stream_registry: streams.Stream_Registry,
	stream_controller: streams.Stream_Controller,
	settings:        services.Settings_Store,
	layer_store:     layers.Market_Store,
	layer_registry:  layers.Layer_Registry,
	layer_datasource: layers.Data_Source,
	layer_outputs:   layers.Layer_Outputs,
	layer_hard_cutover: bool,
	profiles:        services.Profile_Store,
	connection_manager_selected_profile: int,
	frame:           u64,
	last_viewport:   ui.Vec2,
	last_keys_pressed: bit_set[ports.Key],
	last_mouse_press_frame: [ports.Mouse_Button]u64,
	ui_actions:      [UI_ACTION_CAP]UI_Action,
	ui_action_count: int,
	ui_actions_enqueued_total: u64,
	ui_action_drops: u64,
	stream_switches_total: u64,
	timeframe_switches_total: u64,
	tf_flash_frame: u64,
	has_last_render: bool,
	has_pending_active_subject: bool,
	pending_active_subject_id:  u64,
	candle_health:             Candle_Health,

	active_tf_idx:    int,      // index into TF_OPTIONS
	getrange:         GetRange_Global_State,

	chart_display:   Chart_Display_State,
	indicators:      Global_Indicator_State,

	// Draw tools state.
	draw_tools: widgets.Draw_Tools_State,

	// Drag-drop layout (Phase 3).
	panel_drag:      ui.Panel_Drag_State,
	layout_preset:   int,        // 0=Default, 1=Chart, 2=Analysis, 3=Compact
	custom_grid_def: ui.Grid_Def, // custom grid after swaps (updated from preset base)

	// Cell context menu (PRD-0005-B M4).
	cell_context_menu:     ui.Context_Menu_State,
	cell_context_cell_idx: int,

	// Grid column/row resize (PRD-0005-B M4, PRD-0007 M0).
	grid_col_resize: int, // -1 = not resizing, else col index of left column being resized
	grid_row_resize: int, // -1 = not resizing, else row index of top row being resized

	// Focus mode (MVP-2): F key → scalper cockpit (candle 75% + orderbook 25%).
	focus_mode: bool,

	// Layout mode (PRD-0007 M2).
	layout_mode: Layout_Mode,
	signal_evidence_link_enabled: bool,

	// Manual resync counter (app-side, not overwritten by transport metrics).
	manual_resync_count: int,

	// Subscription reconcile: previous wanted set for diff-aware unsubscribe (BUG-1 fix).
	prev_subs:       [SUB_WANT_CAP]Prev_Sub_Entry,
	prev_subs_count: int,

	// S20: HTTP bootstrap state from GET /api/v1/session.
	bootstrap: Bootstrap_State,
	freshness: Freshness_State,
	timeline:  Timeline_State,

	// S21: Unified bootstrap lifecycle state.
	lifecycle:          md_common.Bootstrap_Lifecycle,
	was_ever_connected: bool, // True once WS has connected at least once (distinguishes Ready vs Offline).

	// S23/S25: Canonical active-stream apply state. Single source of truth for
	// has_live_*, snapshot_seen, synthetic fallback, getrange, and composition stage.
	// Synced to active_metrics + getrange via apply_state_sync_all each frame.
	active_apply_state: md_common.Stream_Apply_State,

	// S31: Recovery event log — canonical ring buffer for recovery observability.
	recovery_log:       md_common.Recovery_Event_Log,
}

// S20: Bootstrap state populated from GET /api/v1/session.
Bootstrap_State :: struct {
	server_time_ms: i64,
	ready:          bool,
	has_session:    bool,
}

// S20: Per-channel freshness from GET /api/v1/freshness.
FRESHNESS_CHANNEL_CAP :: 9

Channel_Freshness :: struct {
	flowing: bool,
	lag_ms:  i64,
}

Freshness_State :: struct {
	active:     bool,
	channels:   [FRESHNESS_CHANNEL_CAP]Channel_Freshness,
	checked_at: i64,
	loaded:     bool,
}

// S20: Data availability from GET /api/v1/timeline.
Timeline_State :: struct {
	first_ts: i64,
	last_ts:  i64,
	loaded:   bool,
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
	streams.registry_init(&state.stream_registry, true)
	streams.controller_init(&state.stream_controller)
	layers.market_store_reset(&state.layer_store)
	layers.layer_registry_init(&state.layer_registry, &state.layer_store)
	state.layer_hard_cutover = true
	state.active_metrics.state = .Offline
	state.bp_assist.getrange_divisor = 1
	state.bp_assist.user_enabled = false
	state.chart_display.show_vol = true
	state.chart_display.show_heatmap = true
	state.chart_display.show_vpvr = true
	state.chart_display.heatmap_intensity_idx = 1
	for i in 0 ..< len(state.compare.show_vol) {
		state.compare.show_vol[i] = true
		state.compare.show_heatmap[i] = true
		state.compare.show_vpvr[i] = true
		state.compare.heatmap_idx[i] = 1
	}
	state.active_tf_idx = 2 // default to "1m"
	state.signal_evidence_link_enabled = true
	// All panels visible by default.
	for i in 0 ..< ui.PANEL_COUNT {
		state.chrome.panel_visible[i] = true
	}
	// Overlay-first default inspired by MarketMonkey: heatmap/VPVR on candle chart.
	state.chrome.panel_visible[ui.PANEL_HEATMAP] = false
	state.chrome.panel_visible[ui.PANEL_VPVR] = false
	ui.init_sidebar(&state.chrome.sidebar, &state.chrome.panel_visible)

	state.chrome.detail_w = ui.DETAIL_PANEL_W
	ui.init_drag_state(&state.panel_drag)
	state.custom_grid_def = ui.build_default_grid(6)

	state.grid_col_resize = -1
	state.grid_row_resize = -1
	state.overlays.cell_stream_picker_open = -1

	// Initialize cell assignments from default panel visibility.
	layout_from_panels(state)

	// Sidebar sections: panels expanded by default, others collapsed.
	state.chrome.section_streams = {expanded = false}
	state.chrome.section_layers  = {expanded = false}
	state.chrome.section_panels  = {expanded = true}

	// Indicator parameter defaults.
	state.indicators.ma_periods = {9, 21, 50}
	state.indicators.bb_period = 20
	state.indicators.bb_sigma = 2.0
	state.indicators.rsi_period = 14
	state.indicators.macd_fast = 12
	state.indicators.macd_slow = 26
	state.indicators.macd_signal = 9

	// Only fill demo data in offline mode; real data overwrites stores when live.
	if offline {
		services.fill_demo_trades(&state.stores.trades)
		services.fill_demo_orderbook(&state.stores.orderbook)
		services.fill_demo_heatmaps(&state.stores.heatmap)
		services.fill_demo_vpvr(&state.stores.vpvr)
		services.fill_demo_stats(&state.stores.stats)
		services.fill_demo_candles(&state.stores.candle)
		layers.market_store_seed_demo(&state.layer_store, 1)
	}

	// S20: Load market discovery (defaults + session bootstrap + markets fetch).
	services.markets_load_defaults(&state.stores.markets)

	// Try session bootstrap first — gives readiness + server_time + markets overview.
	if state.marketdata.fetch_session != nil {
		session_buf: [8192]u8
		sn := state.marketdata.fetch_session(raw_data(session_buf[:]), i32(len(session_buf)))
		if sn > 0 {
			bootstrap_out: services.Session_Bootstrap
			if services.session_parse_json(&state.stores.markets, session_buf[:int(sn)], &bootstrap_out) {
				state.bootstrap.has_session = true
				state.bootstrap.server_time_ms = bootstrap_out.server_time_ms
				state.bootstrap.ready = bootstrap_out.ready
			}
		}
	}
	// S21: Derive lifecycle after session parse.
	state.lifecycle = md_common.derive_lifecycle(
		state.bootstrap.has_session, state.bootstrap.ready, state.stores.markets.loaded,
		false, false, false, false, false,
	)

	// Full market details (tick_size, market_type) — still needed since session lacks them.
	if state.marketdata.fetch_markets != nil {
		markets_buf: [4096]u8
		n := state.marketdata.fetch_markets(raw_data(markets_buf[:]), i32(len(markets_buf)))
		if n > 0 {
			services.markets_parse_json(&state.stores.markets, markets_buf[:int(n)])
		}
	}
	// S21: Re-derive lifecycle after markets fetch.
	state.lifecycle = md_common.derive_lifecycle(
		state.bootstrap.has_session, state.bootstrap.ready, state.stores.markets.loaded,
		false, false, false, false, false,
	)

	// Initialize settings store.
	if settings_port.load != nil {
		services.settings_init(&state.settings, settings_port)
		services.profile_store_ensure_default(
			&state.profiles,
			&state.settings,
			"Default",
			"ws://127.0.0.1:8080/ws",
			"binance",
			"BTCUSDT:SPOT",
			"SPOT",
		)
		state.connection_manager_selected_profile = state.profiles.active_idx
		if v, ok := services.settings_get(&state.settings, services.SETTING_ACTIVE_STREAM_SUBJECT_ID); ok {
			if subject_id, ok := parse_subject_id_hex(v); ok && subject_id != 0 {
				state.has_pending_active_subject = true
				state.pending_active_subject_id = subject_id
			}
		}
		venue, ok_venue := services.settings_get(&state.settings, services.SETTING_ACTIVE_STREAM_VENUE)
		symbol, ok_symbol := services.settings_get(&state.settings, services.SETTING_ACTIVE_STREAM_SYMBOL)
		if ok_venue && ok_symbol {
			mid := util.market_id64(venue, symbol)
			if mid != 0 {
				state.has_pending_active_subject = true
				state.pending_active_subject_id = mid
			}
		}

		// Restore UI widget state.
		if v, ok := services.settings_get(&state.settings, services.SETTING_SIDEBAR_EXPANDED); ok {
			state.chrome.detail_expanded = v == "1"
			state.chrome.sidebar.expanded = true // sidebar items always shown when detail panel is up
		}
		if v, ok := services.settings_get(&state.settings, services.SETTING_ACTIVE_TF_IDX); ok {
			state.active_tf_idx = parse_int_clamped(v, 0, len(TF_OPTIONS) - 1, 0)
		}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_CANDLE_VOL); ok {
				state.chart_display.show_vol = v != "0"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_CANDLE_HEATMAP); ok {
				state.chart_display.show_heatmap = v != "0"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_CANDLE_VPVR); ok {
				state.chart_display.show_vpvr = v != "0"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_CANDLE_HEATMAP_INTENSITY_IDX); ok {
				state.chart_display.heatmap_intensity_idx = parse_int_clamped(v, 0, 2, 0)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_MA); ok {
				state.indicators.show_ma = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_BBANDS); ok {
				state.indicators.show_bbands = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_VWAP); ok {
				state.indicators.show_vwap = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_RSI); ok {
				state.indicators.show_rsi = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_MACD); ok {
				state.indicators.show_macd = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_FUNDING); ok {
				state.indicators.show_funding = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_LIQ); ok {
				state.indicators.show_liq = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_TRADE_COUNTER); ok {
				state.indicators.show_trade_counter = v == "1"
			}
			// Restore indicator parameters.
			if v, ok := services.settings_get(&state.settings, services.SETTING_MA_PERIOD_0); ok {
				state.indicators.ma_periods[0] = parse_int_clamped(v, 2, 200, 9)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_MA_PERIOD_1); ok {
				state.indicators.ma_periods[1] = parse_int_clamped(v, 2, 200, 21)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_MA_PERIOD_2); ok {
				state.indicators.ma_periods[2] = parse_int_clamped(v, 2, 200, 50)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_BB_PERIOD); ok {
				state.indicators.bb_period = parse_int_clamped(v, 2, 200, 20)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_BB_SIGMA); ok {
				state.indicators.bb_sigma = parse_float_clamped(v, 0.5, 5.0, 2.0)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_RSI_PERIOD); ok {
				state.indicators.rsi_period = parse_int_clamped(v, 2, 100, 14)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_MACD_FAST); ok {
				state.indicators.macd_fast = parse_int_clamped(v, 2, 100, 12)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_MACD_SLOW); ok {
				state.indicators.macd_slow = parse_int_clamped(v, 2, 200, 26)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_MACD_SIGNAL); ok {
				state.indicators.macd_signal = parse_int_clamped(v, 2, 100, 9)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_PANEL_VISIBLE_MASK); ok {
				if panel_visibility_mask_decode(v, &state.chrome.panel_visible) {
					ui.sync_sidebar_visibility(&state.chrome.sidebar, state.chrome.panel_visible)
				}
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_ASSIST_MODE); ok {
				state.bp_assist.user_enabled = v == "1"
			}
			layers.layer_registry_load_settings(&state.layer_registry, &state.settings)
			// Restore layout preset.
			if v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_PRESET); ok {
				state.layout_preset = parse_int_clamped(v, 0, ui.LAYOUT_PRESET_COUNT - 1, 0)
				grid_def, vis := ui.get_layout_preset(state.layout_preset, 6)
				state.custom_grid_def = grid_def
				state.chrome.panel_visible = vis
				ui.sync_sidebar_visibility(&state.chrome.sidebar, state.chrome.panel_visible)
			}
			// Restore cell layout (V6 -> V5 -> V4 -> V3 -> V2 -> V1 chain).
			layout_from_panels(state) // rebuild from panel_visible
			if !restore_layout_v6(state) {
				if !restore_layout_v5(state) {
					if !restore_layout_v4(state) {
						if !restore_layout_v3(state) {
							if !restore_layout_v2(state) {
								restore_layout(state) // V1 fallback
							}
						}
					}
				}
			}
			// Restore layout mode (PRD-0007 M2).
			if v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_MODE); ok {
				state.layout_mode = v == "C" ? .Custom : .Preset
			}
			// Restore grid weights (PRD-0007 M0) — only if V3 didn't set them.
			restore_col_weights(state)
			restore_row_weights(state)
			// Restore draw tools.
			if v, ok := services.settings_get(&state.settings, services.SETTING_DRAW_TOOLS); ok {
				widgets.draw_tools_deserialize(&state.draw_tools, v)
			}
		}
		for i in 0 ..< len(state.compare.show_vol) {
			state.compare.show_vol[i] = state.chart_display.show_vol
			state.compare.show_heatmap[i] = state.chart_display.show_heatmap
			state.compare.show_vpvr[i] = state.chart_display.show_vpvr
			state.compare.heatmap_idx[i] = state.chart_display.heatmap_intensity_idx
		}

	// PRD-0009: If no cells have bindings (first-ever startup), set default binding on cell 0.
	any_bound := false
	for ci in 0 ..< state.world.count {
		if binding_has(&state.world.bindings[ci]) {
			any_bound = true
			break
		}
	}
	if !any_bound && state.world.count > 0 {
		binding_set(&state.world.bindings[0], "binance", "BTCUSDT:SPOT")
	}

	// Auto-connect: if auto_connect=1 and we have an active profile with URL, reconnect.
	// S20: Gate on bootstrap readiness — skip WS connect if backend reported not_ready.
	if !offline {
		backend_ready := !state.bootstrap.has_session || state.bootstrap.ready
		auto_connect_val, _ := services.settings_get(&state.settings, services.SETTING_AUTO_CONNECT)
		if auto_connect_val == "1" && backend_ready {
			if profile := services.profile_store_active(&state.profiles); profile != nil {
				ws_url := services.profile_ws_url(profile)
				api_key := services.profile_api_key_ref(profile)
				if len(ws_url) > 0 && state.marketdata.reconnect_transport != nil {
					_ = state.marketdata.reconnect_transport(ws_url, api_key)
				}
			}
		}
	}

	// Reconcile subscriptions after layout restore so cells bound to
	// non-default venues/symbols get their data channels subscribed on startup.
	if !offline {
		reconcile_subscriptions(state)

		// S20 Slice 3: Fetch timeline bounds before requesting historical candles.
		fetch_timeline_for_active(state)

		// Prime historical candles even before first live event, so high TF startup
		// does not stay empty waiting for a subject event to create an active slot.
		if state.stores.candle.count <= 0 {
			request_active_stream_candle_range(state)
		}
	}
	}

shutdown :: proc(state: ^App_State) {
	if state.marketdata.shutdown != nil {
		state.marketdata.shutdown()
	}
	if state.stream_views != nil {
		for i in 0 ..< STREAM_VIEW_CAP {
			stream_view_clear_stream_info(&state.stream_views.slots[i])
		}
		free(state.stream_views)
		state.stream_views = nil
	}
	services.settings_flush(&state.settings)
	ui.destroy_buffer(&state.cmd_buf)
}

set_runtime_connection_defaults :: proc(state: ^App_State, ws_url: string, api_key_ref: string = "") {
	if state == nil do return

	prev_url := string(state.conn.runtime_ws_url[:int(state.conn.runtime_ws_url_len)])
	prev_key := string(state.conn.runtime_api_key_ref[:int(state.conn.runtime_api_key_ref_len)])
	url_changed := ws_url != prev_url
	key_changed := api_key_ref != prev_key

	wsn := min(len(ws_url), len(state.conn.runtime_ws_url))
	for i in 0 ..< wsn {
		state.conn.runtime_ws_url[i] = ws_url[i]
	}
	state.conn.runtime_ws_url_len = u16(wsn)

	apin := min(len(api_key_ref), len(state.conn.runtime_api_key_ref))
	for i in 0 ..< apin {
		state.conn.runtime_api_key_ref[i] = api_key_ref[i]
	}
	state.conn.runtime_api_key_ref_len = u8(apin)

	// Keep profile store bootstrapped with runtime defaults when the app starts.
	if state.settings.port.load != nil {
		active := services.profile_store_active(&state.profiles)
		if active != nil && len(services.profile_ws_url(active)) == 0 {
			default_name := services.profile_name(active)
			if len(default_name) == 0 do default_name = "Default"
			profile := services.profile_make(
				default_name,
				ws_url,
				services.profile_venue(active),
				services.profile_symbol(active),
				services.profile_market_type(active),
				api_key_ref,
				true,
			)
			_ = services.profile_store_upsert(&state.profiles, profile)
			services.profile_store_save(&state.profiles, &state.settings)
			services.settings_flush(&state.settings)
		}
	}

	// Sync effective URL/auth into transport so diagnostics and lifecycle logs
	// reflect the runtime override, not the stale compile-time defaults.
	// Only trigger on subsequent calls (prev_url non-empty = not initial bootstrap).
	if len(prev_url) > 0 && (url_changed || key_changed) && state.marketdata.reconnect_transport != nil {
		_ = state.marketdata.reconnect_transport(ws_url, api_key_ref)
		fmt.printf("[md-lifecycle] runtime_override url_changed=%v auth_changed=%v\n", url_changed, key_changed)
	}
}

runtime_probe :: proc(state: ^App_State) -> Runtime_Probe {
	p: Runtime_Probe
	if state == nil do return p

	p.frame = state.frame
	p.conn_status = current_conn_status(state)
	p.candle_health = state.candle_health
	p.active_tf_idx = state.active_tf_idx
	p.pending_restore = state.has_pending_active_subject

	if reg := state.stream_views; reg != nil {
		p.stream_count = reg.count
		p.has_active_stream = reg.has_active
		p.active_subject_id = reg.active_subject_id
		p.stream_evictions = reg.eviction_count
		p.stream_repairs = reg.repair_count
	}
	p.layer_stream_entries = state.layer_store.stream_count
	p.layer_stream_evictions = state.layer_store.stream_evictions

	if state.marketdata.metrics != nil {
		p.has_md_metrics = state.marketdata.metrics(&p.md_metrics)
	}
	if ok, qmax, drop_delta, rc_delta := metrics_history_summary(state); ok {
		p.md_qmax_recent = qmax
		p.md_drop_delta_recent = drop_delta
		p.md_rc_delta_recent = rc_delta
	}
	p.md_candle_backlog_recent = state.active_metrics.candle_backlog
	p.md_msg_rate = state.active_metrics.msg_rate
	p.md_bytes_rate = state.active_metrics.bytes_rate
	p.md_parsed_msgs_total = state.active_metrics.parsed_msgs_total
	p.md_parsed_bytes_total = state.active_metrics.parsed_bytes_total
	p.md_parse_arena_resets_total = state.active_metrics.parse_arena_resets
	p.md_alloc_estimate_total = state.active_metrics.alloc_estimate_total
	p.md_alloc_estimate_frame = state.active_metrics.alloc_estimate_frame
	p.md_canonical_stats_frames = state.active_metrics.canonical_stats_frames
	p.md_stats_fallback_frames = state.active_metrics.stats_fallback_frames
	p.md_canonical_evidence_frames = state.active_metrics.canonical_evidence_frames
	p.md_legacy_evidence_frames = state.active_metrics.legacy_evidence_frames
	p.md_evidence_fallback_frames = state.active_metrics.evidence_fallback_frames
	p.md_canonical_signal_frames = state.active_metrics.canonical_signal_frames
	p.md_legacy_signal_frames = state.active_metrics.legacy_signal_frames
	p.md_signal_fallback_frames = state.active_metrics.signal_fallback_frames
	p.md_legacy_evidence_rejected = state.active_metrics.legacy_evidence_rejected
	p.md_legacy_signal_rejected = state.active_metrics.legacy_signal_rejected
	p.ui_action_drops = state.ui_action_drops
	p.ui_actions_enqueued_total = state.ui_actions_enqueued_total
	p.stream_switches_total = state.stream_switches_total
	p.timeframe_switches_total = state.timeframe_switches_total
	p.active_live_stats = state.active_metrics.has_live_stats
	p.active_live_heatmap = state.active_metrics.has_live_heatmap
	p.active_live_vpvr = state.active_metrics.has_live_vpvr
	p.active_live_candle = state.active_metrics.has_live_candle
	p.active_synth_heatmap = !state.active_metrics.has_live_heatmap && state.stores.heatmap.count > 0
	p.active_synth_vpvr = !state.active_metrics.has_live_vpvr && state.stores.vpvr.count > 0
	p.compare_mode = state.compare.active
	p.compare_widget_idx = state.compare.widget_idx
	p.compare_count = state.compare.count
	p.w_trades_count = state.stores.trades.count
	p.w_orderbook_asks = state.stores.orderbook.ask_count
	p.w_orderbook_bids = state.stores.orderbook.bid_count
	p.w_stats_count = state.stores.stats.count
	p.w_heatmap_snaps = state.stores.heatmap.count
	p.w_vpvr_levels = state.stores.vpvr.count
	p.w_candle_count = state.stores.candle.count
	p.w_evidence_count = state.evidence.count
	active_subject := p.active_subject_id
	if active_subject == 0 {
		active_subject = state.layer_store.active_subject_id
	}
	recent_signals: [32]services.Signal_Entry
	if active_subject != 0 {
		p.w_signal_count = services.signal_store_recent_for_subject(&state.stores.signals, active_subject, recent_signals[:])
	}
	active_stream := layers.market_store_active_stream(&state.layer_store)
	if active_stream != nil {
		p.w_signal_link_total = active_stream.signal_evidence_links
		p.w_signal_link_evidence_seq = active_stream.last_linked_evidence_seq
	}
	layer_diags: [layers.LAYER_REGISTRY_CAP]layers.Layer_Diagnostics
	diag_count := layers.layer_registry_collect_diagnostics(&state.layer_registry, &state.layer_store, layer_diags[:])
	for i in 0 ..< diag_count {
		d := layer_diags[i]
		#partial switch d.id {
		case .Price_Candles:
			p.w_stats_parse_total = d.parse_total
			p.w_stats_fallback_total = d.fallback_total
			p.w_stats_drop_capacity_total = d.drop_capacity_total
			p.w_stats_drop_render_overflow_total = d.drop_render_overflow_total
			p.w_stats_drop_total = d.drop_capacity_total + d.drop_render_overflow_total
			p.w_stats_entries = d.entries
			p.w_stats_max_entries = d.max_entries
			p.w_stats_evicted_total = d.evicted_total
			p.w_stats_render_p95_us = d.render_p95_us
			p.w_stats_render_p99_us = d.render_p99_us
			p.w_stats_render_budget_us = d.render_budget_us
			p.w_stats_render_over_budget = d.render_over_budget
			p.w_stats_state = d.state
		case .OrderBook_DOM:
			p.w_dom_parse_total = d.parse_total
			p.w_dom_fallback_total = d.fallback_total
			p.w_dom_drop_capacity_total = d.drop_capacity_total
			p.w_dom_drop_render_overflow_total = d.drop_render_overflow_total
			p.w_dom_drop_total = d.drop_capacity_total + d.drop_render_overflow_total
			p.w_dom_entries = d.entries
			p.w_dom_max_entries = d.max_entries
			p.w_dom_evicted_total = d.evicted_total
			p.w_dom_render_p95_us = d.render_p95_us
			p.w_dom_render_p99_us = d.render_p99_us
			p.w_dom_render_budget_us = d.render_budget_us
			p.w_dom_render_over_budget = d.render_over_budget
		case .Trades_Tape:
			p.w_tape_parse_total = d.parse_total
			p.w_tape_fallback_total = d.fallback_total
			p.w_tape_drop_capacity_total = d.drop_capacity_total
			p.w_tape_drop_render_overflow_total = d.drop_render_overflow_total
			p.w_tape_drop_total = d.drop_capacity_total + d.drop_render_overflow_total
			p.w_tape_entries = d.entries
			p.w_tape_max_entries = d.max_entries
			p.w_tape_evicted_total = d.evicted_total
			p.w_tape_render_p95_us = d.render_p95_us
			p.w_tape_render_p99_us = d.render_p99_us
			p.w_tape_render_budget_us = d.render_budget_us
			p.w_tape_render_over_budget = d.render_over_budget
		case .Evidence:
			p.w_evidence_parse_total = d.parse_total
			p.w_evidence_fallback_total = d.fallback_total
			p.w_evidence_drop_capacity_total = d.drop_capacity_total
			p.w_evidence_drop_render_overflow_total = d.drop_render_overflow_total
			p.w_evidence_drop_total = d.drop_capacity_total + d.drop_render_overflow_total
			p.w_evidence_entries = d.entries
			p.w_evidence_max_entries = d.max_entries
			p.w_evidence_evicted_total = d.evicted_total
			p.w_evidence_render_p95_us = d.render_p95_us
			p.w_evidence_render_p99_us = d.render_p99_us
			p.w_evidence_render_budget_us = d.render_budget_us
			p.w_evidence_render_over_budget = d.render_over_budget
			p.w_evidence_state = d.state
		case .Signal:
			p.w_signal_parse_total = d.parse_total
			p.w_signal_fallback_total = d.fallback_total
			p.w_signal_drop_capacity_total = d.drop_capacity_total
			p.w_signal_drop_render_overflow_total = d.drop_render_overflow_total
			p.w_signal_drop_total = d.drop_capacity_total + d.drop_render_overflow_total
			p.w_signal_entries = d.entries
			p.w_signal_max_entries = d.max_entries
			p.w_signal_evicted_total = d.evicted_total
			p.w_signal_render_p95_us = d.render_p95_us
			p.w_signal_render_p99_us = d.render_p99_us
			p.w_signal_render_budget_us = d.render_budget_us
			p.w_signal_render_over_budget = d.render_over_budget
			p.w_signal_state = d.state
			p.w_signal_link_total = d.signal_link_total
			p.w_signal_link_evidence_seq = d.signal_link_evidence_seq
		}
	}
	p.layout_link_enabled = state.signal_evidence_link_enabled
	if _, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V5); ok {
		p.layout_version = 5
	} else if _, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V4); ok {
		p.layout_version = 4
	} else if _, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V3); ok {
		p.layout_version = 3
	} else if _, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V2); ok {
		p.layout_version = 2
	} else if _, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT); ok {
		p.layout_version = 1
	}
	if p.layout_version == 5 {
		if _, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_V4); ok {
			p.layout_migrated = true
		}
	}
	p.ind_rsi_enabled = state.telemetry.last_indicator_probe.rsi_enabled
	p.ind_macd_enabled = state.telemetry.last_indicator_probe.macd_enabled
	p.ind_funding_enabled = state.telemetry.last_indicator_probe.funding_enabled
	p.ind_liq_enabled = state.telemetry.last_indicator_probe.liq_enabled
	p.ind_trade_counter_enabled = state.telemetry.last_indicator_probe.trade_counter_enabled
	p.ind_rsi_rendered = state.telemetry.last_indicator_probe.rsi_rendered
	p.ind_macd_rendered = state.telemetry.last_indicator_probe.macd_rendered
	p.ind_funding_rendered = state.telemetry.last_indicator_probe.funding_rendered
	p.ind_liq_rendered = state.telemetry.last_indicator_probe.liq_rendered
	p.ind_trade_counter_rendered = state.telemetry.last_indicator_probe.trade_counter_rendered

	// Performance metrics.
	p.cmd_buf_count = len(state.cmd_buf.commands)
	p.cmd_frame_arena_bytes = ui.frame_arena_usage(&state.cmd_buf)
	p.cmd_frame_arena_capacity = ui.frame_arena_capacity(&state.cmd_buf)
	p.cell_count = state.world.count
	for ci in 0 ..< state.world.count {
		p.cell_tf_idxs[ci] = state.world.timeframes[ci].tf_idx
	}
	if state.telemetry.frame_time_count > 0 {
		p.frame_time_p50_us, p.frame_time_p95_us, p.frame_time_p99_us = frame_time_percentiles(state)
	}
	return p
}

// Record a frame time sample (microseconds). Called by platform after each frame.
record_frame_time :: proc(state: ^App_State, us: i64) {
	state.telemetry.frame_times_us[state.telemetry.frame_time_head] = us
	state.telemetry.frame_time_head = (state.telemetry.frame_time_head + 1) % len(state.telemetry.frame_times_us)
	if state.telemetry.frame_time_count < len(state.telemetry.frame_times_us) {
		state.telemetry.frame_time_count += 1
	}
}

// Compute p50/p95/p99 from the frame time ring buffer using insertion sort on a small copy.
frame_time_percentiles :: proc(state: ^App_State) -> (p50, p95, p99: i64) {
	count := state.telemetry.frame_time_count
	if count <= 0 do return
	p50 = services.ring_percentile_i64(
		state.telemetry.frame_times_us,
		state.telemetry.frame_time_head,
		count,
		50,
	)
	p95 = services.ring_percentile_i64(
		state.telemetry.frame_times_us,
		state.telemetry.frame_time_head,
		count,
		95,
	)
	p99 = services.ring_percentile_i64(
		state.telemetry.frame_times_us,
		state.telemetry.frame_time_head,
		count,
		99,
	)
	return
}

update :: proc(state: ^App_State, input: ports.Input_State) -> ^ui.Command_Buffer {
	state.frame += 1
	frame_input := dedupe_mouse_pressed_edges(state, input)

	t0 := time.tick_now()
	_ = drain_layer_marketdata(state)
	t1 := time.tick_now()

	queue_ui_actions_from_input(state, frame_input)
	_, _ = apply_ui_actions(state)
	t2 := time.tick_now()

	sample_marketdata_metrics(state)
	observe_candle_health(state)
	poll_freshness(state)
	cache_render_observations(state, frame_input)
	buf := build_ui(state, frame_input)
	if state.ui_action_count > 0 {
		_, _ = apply_ui_actions(state)
		// Second pass is for visual consistency after state mutation.
		// Consume edge-triggered input so click/keyboard actions aren't enqueued twice.
		buf = build_ui(state, consume_edge_input_for_rerender(frame_input))
	}
	t3 := time.tick_now()

	state.telemetry.drain_us = i64(time.duration_microseconds(time.tick_diff(t0, t1)))
	state.telemetry.actions_us = i64(time.duration_microseconds(time.tick_diff(t1, t2)))
	state.telemetry.render_us = i64(time.duration_microseconds(time.tick_diff(t2, t3)))

	persist_draw_tools(state)
	return buf
}

update_web :: proc(state: ^App_State, input: ports.Input_State) -> (buf: ^ui.Command_Buffer, should_render: bool) {
	state.frame += 1
	frame_input := dedupe_mouse_pressed_edges(state, input)
	input_interaction := has_input_interaction(frame_input)

	t0 := time.tick_now()
	events_processed := drain_layer_marketdata(state)
	t1 := time.tick_now()

	queue_ui_actions_from_input(state, frame_input)
	stream_switched, tf_switched := apply_ui_actions(state)
	t2 := time.tick_now()

	sample_marketdata_metrics(state)
	poll_freshness(state)

	conn := current_conn_status(state)
	candle_health_changed := observe_candle_health(state)
	needs_render := !state.has_last_render
	if !needs_render {
		needs_render = events_processed > 0
	}
	if !needs_render {
		needs_render = state.last_viewport.x != frame_input.viewport_size.x || state.last_viewport.y != frame_input.viewport_size.y
	}
	if !needs_render {
		needs_render = state.conn.last_conn != conn
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
		needs_render = input_interaction
	}
	if !needs_render {
		state.telemetry.drain_us = i64(time.duration_microseconds(time.tick_diff(t0, t1)))
		state.telemetry.actions_us = i64(time.duration_microseconds(time.tick_diff(t1, t2)))
		state.telemetry.render_us = 0
		return &state.cmd_buf, false
	}

	cache_render_observations(state, frame_input)
	buf = build_ui(state, frame_input)
	if state.ui_action_count > 0 {
		sw2, tf2 := apply_ui_actions(state)
		if sw2 do stream_switched = true
		if tf2 do tf_switched = true
		// Second pass is for visual consistency after state mutation.
		// Consume edge-triggered input so click/keyboard actions aren't enqueued twice.
		buf = build_ui(state, consume_edge_input_for_rerender(frame_input))
	}
	t3 := time.tick_now()

	state.telemetry.drain_us = i64(time.duration_microseconds(time.tick_diff(t0, t1)))
	state.telemetry.actions_us = i64(time.duration_microseconds(time.tick_diff(t1, t2)))
	state.telemetry.render_us = i64(time.duration_microseconds(time.tick_diff(t2, t3)))

	persist_draw_tools(state)
	return buf, true
}

persist_draw_tools :: proc(state: ^App_State) {
	if !state.draw_tools.dirty do return
	buf: [512]u8
	serialized := widgets.draw_tools_serialize(&state.draw_tools, buf[:])
	services.settings_set(&state.settings, services.SETTING_DRAW_TOOLS, serialized)
	services.settings_flush(&state.settings)
	state.draw_tools.dirty = false
}

has_input_interaction :: proc(input: ports.Input_State) -> bool {
	if input.mouse.scroll.x != 0 || input.mouse.scroll.y != 0 do return true
	if input.mouse.pressed[.Left] || input.mouse.pressed[.Right] || input.mouse.pressed[.Middle] do return true
	if input.mouse.released[.Left] || input.mouse.released[.Right] || input.mouse.released[.Middle] do return true
	if input.keys.just_pressed != {} do return true
	if input.keys.just_released != {} do return true
	return false
}

dedupe_mouse_pressed_edges :: proc(state: ^App_State, input: ports.Input_State) -> ports.Input_State {
	out := input
	for btn in ports.Mouse_Button {
		if !out.mouse.pressed[btn] do continue
		last := state.last_mouse_press_frame[btn]
		// Some runtimes can emit duplicate pressed edges across adjacent frames for
		// a single physical click. Collapse those duplicates to keep UI actions deterministic.
		if last > 0 && state.frame <= last + 1 {
			out.mouse.pressed[btn] = false
			continue
		}
		state.last_mouse_press_frame[btn] = state.frame
	}
	return out
}

consume_edge_input_for_rerender :: proc(input: ports.Input_State) -> ports.Input_State {
	out := input
	out.mouse.pressed = {}
	out.mouse.released = {}
	out.mouse.scroll = {}
	out.keys.just_pressed = {}
	out.keys.just_released = {}
	return out
}

cache_render_observations :: proc(state: ^App_State, input: ports.Input_State) {
	state.last_viewport = input.viewport_size
	state.conn.last_conn = current_conn_status(state)
	state.has_last_render = true
}
