package app

import "mr:model"
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

Cell_Assignment :: struct {
	widget:              Widget_Kind,
	stream_idx:          int,    // -1 = unresolved/active stream, else index into stream_views.slots
	tf_idx:              int,    // -1 = follow global active_tf_idx, 0..8 = per-cell TF
	// Intent-driven binding (PRD-0009): venue/symbol the cell WANTS, independent of slot resolution.
	bound_venue:         [24]u8,
	bound_venue_len:     u8,
	bound_symbol:        [32]u8,
	bound_symbol_len:    u8,
	getrange_pending:    bool,   // per-cell: waiting for range response
	getrange_seeded:     bool,   // per-cell: initial historical seed sent
	getrange_oldest_ts:  i64,    // per-cell: oldest candle window_start_ts loaded
	getrange_sent_frame: u64,    // per-cell: frame when getrange was sent (for timeout)
	candle_scroll_x:     f32,
	candle_zoom:         f32,
	crosshair:           widgets.Crosshair_State,
	ob_scroll_y:         f32,
	ob_group_idx:        int,
	dom_group_idx:       int,
	trades_scroll_y:     f32,
	trade_filter_idx:    int,
	show_vol:            bool,
	show_heatmap:        bool,
	show_vpvr:           bool,
	heatmap_intensity_idx: int,
	chart_type:          widgets.Chart_Type,
	// Per-cell indicator toggles (PRD-0006-B M0).
	show_ma:             bool,
	show_bbands:         bool,
	show_vwap:           bool,
	show_rsi:            bool,
	show_macd:           bool,
	show_funding:        bool,
	show_liq:            bool,
	show_trade_counter:  bool,
	// Per-cell subplot resize (PRD-0007 M1).
	sub_main_split: f32,     // 0.0 = auto (15% * count), >0 = user override ratio for subplot area
	sub_ratios: [5]f32,      // custom ratios per sub-indicator slot; 0 = equal division
	sub_resize_idx: int,     // -1 = none, else separator index being dragged
	// Cell span (PRD-0007 M2) — for free-form grid layout.
	col_span: int,           // 0 or 1 = default; >1 = span multiple columns
	row_span: int,           // 0 or 1 = default; >1 = span multiple rows
	// Per-cell indicator parameters.
	ma_periods:          [3]int,
	bb_period:           int,
	bb_sigma:            f64,
	rsi_period:          int,
	macd_fast:           int,
	macd_slow:           int,
	macd_signal:         int,
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
	Resync_Active_Stream,
	Select_Profile,
	Add_Profile,
	Remove_Profile,
	Apply_Profile,
	Connect_Profile,
	Disconnect_Profile,
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
	has_heatmap_snapshot: bool,
	heatmap_snapshot:     services.Heatmap_Snapshot,
	heatmap_store:        services.Heatmap_Store,
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
	active_tf_idx:         int,
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
	w_heatmap_snaps:       int,
	w_vpvr_levels:         int,
	w_candle_count:        int,
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
	cmd_buf_count:         int,
	cell_count:            int,
	cell_tf_idxs:          [CELL_MAX]int,  // per-cell tf_idx (-1 = global)
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
	dom_store:        services.DOM_Store,
	footprint_store:  services.Footprint_Store,
	markets_store:    services.Markets_Store,
	stream_views:    ^Stream_View_Registry,
	stream_registry: streams.Stream_Registry,
	stream_controller: streams.Stream_Controller,
	settings:        services.Settings_Store,
	profiles:        services.Profile_Store,
	connection_manager_selected_profile: int,
	runtime_ws_url:  [256]u8,
	runtime_ws_url_len: u16,
	runtime_api_key_ref: [64]u8,
	runtime_api_key_ref_len: u8,
	scroll_y:        f32,
	ob_scroll_y:     f32,
	candle_scroll_x: f32,
	candle_zoom:     f32,
	candle_crosshair: widgets.Crosshair_State,
	candle_chart_type: widgets.Chart_Type,
	frame:           u64,
	last_viewport:   ui.Vec2,
	last_conn:       ports.MD_Conn_Status,
	prev_conn_for_reconcile: ports.MD_Conn_Status, // for reconnect detection
	last_keys_pressed: bit_set[ports.Key],
	ui_actions:      [UI_ACTION_CAP]UI_Action,
	ui_action_count: int,
	ui_actions_enqueued_total: u64,
	ui_action_drops: u64,
	stream_switches_total: u64,
	timeframe_switches_total: u64,
	has_last_render: bool,
	has_pending_active_subject: bool,
	pending_active_subject_id:  u64,
	md_metrics_history: [MD_METRICS_HISTORY_CAP]MD_Metrics_History_Sample,
	md_metrics_head:    int,
	md_metrics_count:   int,

	candle_last_recv_local_ms: i64,
	candle_health:             Candle_Health,

	needs_reconcile:  bool,     // set by platform after reconnect; cleared after reconcile
	active_tf_idx:    int,      // index into TF_OPTIONS
	getrange_pending: bool,     // true while waiting for range response
	getrange_seeded:  bool,     // true after initial historical seed request for active stream
	getrange_subject_id: u64,   // active-stream candle subject_id currently awaited via getrange
	getrange_sent_frame: u64,   // frame when getrange was sent (for timeout)
	active_candle_subject_id: u64, // current active candle subject for stale getrange guard
	getrange_oldest_ts: i64,    // oldest candle window_start_ts we've loaded (for lazy loading)
	active_has_live_stats:   bool,
	active_has_live_heatmap: bool,
	active_has_live_vpvr:    bool,
	active_has_live_candle:  bool,
	active_stream_state:             streams.Stream_State,
	active_stream_desync_reason:     streams.Stream_Desync_Reason,
	active_stream_rtt_ms:            i64,
	active_stream_lag_ms:            i64,
	active_stream_last_msg_ts_ms:    i64,
	active_stream_last_stats_ts_ms:  i64,
	active_stream_last_orderbook_ts_ms: i64,
	active_stream_drop_count:        int,
	active_stream_reconnect_count:   int,
	active_stream_subscribe_acks:    int,

	// Synthetic heatmap throttle (1-per-TF-window).
	synth_heatmap_last_window: i64,

	// Widget controls (Phase 2).
	ob_group_idx:      int,
	ob_group_options:  [5]f64,
	ob_group_labels:   [5][12]u8,
	ob_group_count:    int,
	trade_filter_idx:  int,
	show_candle_vol:   bool,
	show_candle_heatmap: bool,
	show_candle_vpvr:    bool,
	candle_heatmap_intensity_idx: int,

	// Layout (Phase 3).
	sidebar:       ui.Sidebar_State,
	panel_visible: [ui.PANEL_COUNT]bool,

	// Help overlay (Phase 4).
	show_help_overlay: bool,

	// Exchange manager (PRD-0006-B M1, replaces connection modal).
	show_exchange_manager: bool,
	exchange_sections: [services.EXCHANGE_CAP]ui.Section_State,

	// Compare mode (Phase 5).
	compare_mode:       bool,
	compare_slots:      [4]u64,       // subject_ids for comparison panels
	compare_count:      int,          // 1-4 active comparison slots
	compare_widget_idx: int,          // 0=candle, 1=orderbook, 2=trades (which widget to compare)
	compare_candle_scroll_x: [4]f32,
	compare_candle_zoom:     [4]f32,
	compare_show_candle_vol: [4]bool,
	compare_show_heatmap:    [4]bool,
	compare_show_vpvr:       [4]bool,
	compare_heatmap_intensity_idx: [4]int,
	compare_ob_scroll:       [4]f32,
	compare_ob_grp_idx:      [4]int,
	compare_trade_scroll:    [4]f32,
	compare_trade_filter:    [4]int,
	venue_dropdown:     ui.Dropdown_State,

	// Route + detail panel.
	active_route:          Route,    // zero = .Dashboard
	detail_panel_expanded: bool,
	detail_panel_w:        f32,     // configurable detail panel width
	detail_resizing:       bool,    // true while drag-resizing detail panel

	// Sidebar section states (PRD-0005 Phase 2).
	section_streams:  ui.Section_State,
	section_layers:   ui.Section_State,
	section_panels:   ui.Section_State,

	// Indicator visibility toggles.
	show_ma:       bool,
	show_bbands:   bool,
	show_vwap:     bool,
	show_rsi:      bool,
	show_macd:     bool,
	show_funding:       bool,
	show_liq:           bool,
	show_trade_counter: bool,

	// Indicator parameters (configurable via sidebar).
	ma_periods:     [3]int,    // 3 MA lines (default 9, 21, 50)
	bb_period:      int,       // Bollinger period (default 20)
	bb_sigma:       f64,       // Bollinger std multiplier (default 2.0)
	rsi_period:     int,       // RSI period (default 14)
	macd_fast:      int,       // MACD fast EMA (default 12)
	macd_slow:      int,       // MACD slow EMA (default 26)
	macd_signal:    int,       // MACD signal EMA (default 9)

	// Draw tools state.
	draw_tools: widgets.Draw_Tools_State,

	// Drag-drop layout (Phase 3).
	panel_drag:      ui.Panel_Drag_State,
	layout_preset:   int,        // 0=Default, 1=Chart, 2=Analysis, 3=Compact
	custom_grid_def: ui.Grid_Def, // custom grid after swaps (updated from preset base)

	// Multi-chart cell assignments (PRD-0005-B M0).
	cell_assignments: [CELL_MAX]Cell_Assignment,
	cell_count:       int,
	focused_candle_cell_idx: int, // which candle cell keyboard shortcuts target (PRD-0006-B M0)

	// Cell context menu (PRD-0005-B M4).
	cell_context_menu:     ui.Context_Menu_State,
	cell_context_cell_idx: int,

	// Frame time tracking (PRD-0005-B M5).
	frame_times_us:   [120]i64,
	frame_time_head:  int,
	frame_time_count: int,
	last_indicator_probe: widgets.Indicator_Render_Probe,

	// Grid column/row resize (PRD-0005-B M4, PRD-0007 M0).
	grid_col_resize: int, // -1 = not resizing, else col index of left column being resized
	grid_row_resize: int, // -1 = not resizing, else row index of top row being resized

	// Focus mode (MVP-2): F key → scalper cockpit (candle 75% + orderbook 25%).
	focus_mode: bool,

	// Whale alert (MVP-4): flash status bar on large trades.
	whale_avg_qty:     f64,    // EMA of trade qty
	whale_alert_price: f64,    // price of whale trade
	whale_alert_qty:   f64,    // qty of whale trade
	whale_alert_buy:   bool,   // true = buy, false = sell
	whale_alert_frame: u64,    // frame when alert was set

	// Stream picker (MVP-7): G key opens overlay to switch streams.
	show_stream_picker: bool,

	// Cell stream picker (PRD-0006-B M2).
	cell_stream_picker_open: int, // -1 = none, else cell index
	cell_stream_picker_scroll: f32,

	// Widget catalog (PRD-0006-B M3).
	show_widget_catalog:       bool,
	catalog_step:              int,  // 0 = pick widget, 1 = pick stream
	catalog_selected_widget:   Widget_Kind,

	// Layout mode (PRD-0007 M2).
	layout_mode: Layout_Mode,

	// Zen mode (PRD-0007 M4): auto-hide chrome for max chart area.
	zen_mode:         bool,
	zen_top_alpha:    f32,  // fade alpha for top bar (0..1)
	zen_bottom_alpha: f32,  // fade alpha for status bar (0..1)
	zen_left_alpha:   f32,  // fade alpha for nav rail (0..1)
	compact_top_bar:  bool, // compact single-row top bar

	// TF OSD (on-screen display) — shows TF name briefly after change in zen mode.
	tf_osd_frame: u64,      // frame when TF OSD was triggered (0 = hidden)

	// Toast notifications (MVP-21): brief feedback messages.
	toast_text:  [64]u8,
	toast_len:   int,
	toast_frame: u64, // frame when toast was set

	// Subscription reconcile: previous wanted set for diff-aware unsubscribe (BUG-1 fix).
	prev_subs:       [SUB_WANT_CAP]Prev_Sub_Entry,
	prev_subs_count: int,
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
	state.active_stream_state = .Offline
	state.show_candle_vol = true
	state.show_candle_heatmap = true
	state.show_candle_vpvr = true
	state.candle_heatmap_intensity_idx = 1
	for i in 0 ..< len(state.compare_show_candle_vol) {
		state.compare_show_candle_vol[i] = true
		state.compare_show_heatmap[i] = true
		state.compare_show_vpvr[i] = true
		state.compare_heatmap_intensity_idx[i] = 1
	}
	state.active_tf_idx = 2 // default to "1m"
	// All panels visible by default.
	for i in 0 ..< ui.PANEL_COUNT {
		state.panel_visible[i] = true
	}
	// Overlay-first default inspired by MarketMonkey: heatmap/VPVR on candle chart.
	state.panel_visible[ui.PANEL_HEATMAP] = false
	state.panel_visible[ui.PANEL_VPVR] = false
	ui.init_sidebar(&state.sidebar, &state.panel_visible)

	state.detail_panel_w = ui.DETAIL_PANEL_W
	ui.init_drag_state(&state.panel_drag)
	state.custom_grid_def = ui.build_default_grid(6)

	state.grid_col_resize = -1
	state.grid_row_resize = -1
	state.cell_stream_picker_open = -1

	// Initialize cell assignments from default panel layout.
	layout_from_legacy(state)

	// Sidebar sections: panels expanded by default, others collapsed.
	state.section_streams = {expanded = false}
	state.section_layers  = {expanded = false}
	state.section_panels  = {expanded = true}

	// Indicator parameter defaults.
	state.ma_periods = {9, 21, 50}
	state.bb_period = 20
	state.bb_sigma = 2.0
	state.rsi_period = 14
	state.macd_fast = 12
	state.macd_slow = 26
	state.macd_signal = 9

	// Only fill demo data in offline mode; real data overwrites stores when live.
	if offline {
		services.fill_demo_trades(&state.trades_store)
		services.fill_demo_orderbook(&state.orderbook_store)
		services.fill_demo_heatmaps(&state.heatmap_store)
		services.fill_demo_vpvr(&state.vpvr_store)
		services.fill_demo_stats(&state.stats_store)
		services.fill_demo_candles(&state.candle_store)
	}

	// Load market discovery (defaults + HTTP fetch from backend).
	services.markets_load_defaults(&state.markets_store)
	if state.marketdata.fetch_markets != nil {
		markets_buf: [4096]u8
		n := state.marketdata.fetch_markets(raw_data(markets_buf[:]), i32(len(markets_buf)))
		if n > 0 {
			services.markets_parse_json(&state.markets_store, markets_buf[:int(n)])
		}
	}

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
		ch_s, ok_channel := services.settings_get(&state.settings, services.SETTING_ACTIVE_STREAM_CHANNEL)
		if ok_venue && ok_symbol && ok_channel {
			if ch, ok := parse_channel_short_label(ch_s); ok {
				state.has_pending_active_subject = true
				state.pending_active_subject_id = util.subject_id64_for_stream(venue, symbol, ch)
			}
		}

		// Restore UI widget state.
		if v, ok := services.settings_get(&state.settings, services.SETTING_SIDEBAR_EXPANDED); ok {
			state.detail_panel_expanded = v == "1"
			state.sidebar.expanded = true // sidebar items always shown when detail panel is up
		}
		if v, ok := services.settings_get(&state.settings, services.SETTING_OB_GROUP_IDX); ok {
			state.ob_group_idx = parse_small_int(v, 0, 4)
		}
		if v, ok := services.settings_get(&state.settings, services.SETTING_TRADE_FILTER_IDX); ok {
			state.trade_filter_idx = parse_small_int(v, 0, 3)
		}
		if v, ok := services.settings_get(&state.settings, services.SETTING_ACTIVE_TF_IDX); ok {
			state.active_tf_idx = parse_small_int(v, 0, len(TF_OPTIONS) - 1)
		}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_CANDLE_VOL); ok {
				state.show_candle_vol = v != "0"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_CANDLE_HEATMAP); ok {
				state.show_candle_heatmap = v != "0"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_CANDLE_VPVR); ok {
				state.show_candle_vpvr = v != "0"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_CANDLE_HEATMAP_INTENSITY_IDX); ok {
				state.candle_heatmap_intensity_idx = parse_small_int(v, 0, 2)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_MA); ok {
				state.show_ma = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_BBANDS); ok {
				state.show_bbands = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_VWAP); ok {
				state.show_vwap = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_RSI); ok {
				state.show_rsi = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_MACD); ok {
				state.show_macd = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_FUNDING); ok {
				state.show_funding = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_LIQ); ok {
				state.show_liq = v == "1"
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_SHOW_TRADE_COUNTER); ok {
				state.show_trade_counter = v == "1"
			}
			// Restore indicator parameters.
			if v, ok := services.settings_get(&state.settings, services.SETTING_MA_PERIOD_0); ok {
				state.ma_periods[0] = parse_int_clamped(v, 2, 200, 9)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_MA_PERIOD_1); ok {
				state.ma_periods[1] = parse_int_clamped(v, 2, 200, 21)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_MA_PERIOD_2); ok {
				state.ma_periods[2] = parse_int_clamped(v, 2, 200, 50)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_BB_PERIOD); ok {
				state.bb_period = parse_int_clamped(v, 2, 200, 20)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_BB_SIGMA); ok {
				state.bb_sigma = parse_float_clamped(v, 0.5, 5.0, 2.0)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_RSI_PERIOD); ok {
				state.rsi_period = parse_int_clamped(v, 2, 100, 14)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_MACD_FAST); ok {
				state.macd_fast = parse_int_clamped(v, 2, 100, 12)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_MACD_SLOW); ok {
				state.macd_slow = parse_int_clamped(v, 2, 200, 26)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_MACD_SIGNAL); ok {
				state.macd_signal = parse_int_clamped(v, 2, 100, 9)
			}
			if v, ok := services.settings_get(&state.settings, services.SETTING_PANEL_VISIBLE_MASK); ok {
				if panel_visibility_mask_decode(v, &state.panel_visible) {
					ui.sync_sidebar_visibility(&state.sidebar, state.panel_visible)
				}
			}
			// Restore layout preset.
			if v, ok := services.settings_get(&state.settings, services.SETTING_LAYOUT_PRESET); ok {
				state.layout_preset = parse_small_int(v, 0, ui.LAYOUT_PRESET_COUNT - 1)
				grid_def, vis := ui.get_layout_preset(state.layout_preset, 6)
				state.custom_grid_def = grid_def
				state.panel_visible = vis
				ui.sync_sidebar_visibility(&state.sidebar, state.panel_visible)
			}
			// Restore cell layout (V4 → V3 → V2 → V1 fallback chain).
			layout_from_legacy(state) // rebuild from panel_visible
			if !restore_layout_v4(state) {
				if !restore_layout_v3(state) {
					if !restore_layout_v2(state) {
						restore_layout(state) // V1 fallback
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
		for i in 0 ..< len(state.compare_show_candle_vol) {
			state.compare_show_candle_vol[i] = state.show_candle_vol
			state.compare_show_heatmap[i] = state.show_candle_heatmap
			state.compare_show_vpvr[i] = state.show_candle_vpvr
			state.compare_heatmap_intensity_idx[i] = state.candle_heatmap_intensity_idx
		}

	// PRD-0009: If no cells have bindings (first-ever startup), set default binding on cell 0.
	any_bound := false
	for ci in 0 ..< state.cell_count {
		if cell_has_binding(&state.cell_assignments[ci]) {
			any_bound = true
			break
		}
	}
	if !any_bound && state.cell_count > 0 {
		cell_set_binding(&state.cell_assignments[0], "binance", "BTCUSDT:SPOT")
	}

	// Reconcile subscriptions after layout restore so cells bound to
	// non-default venues/symbols get their data channels subscribed on startup.
	if !offline {
		reconcile_subscriptions(state)
		// Prime historical candles even before first live event, so high TF startup
		// does not stay empty waiting for a subject event to create an active slot.
		if state.candle_store.count <= 0 {
			request_active_stream_candle_range(state)
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

set_runtime_connection_defaults :: proc(state: ^App_State, ws_url: string, api_key_ref: string = "") {
	if state == nil do return
	wsn := min(len(ws_url), len(state.runtime_ws_url))
	for i in 0 ..< wsn {
		state.runtime_ws_url[i] = ws_url[i]
	}
	state.runtime_ws_url_len = u16(wsn)

	apin := min(len(api_key_ref), len(state.runtime_api_key_ref))
	for i in 0 ..< apin {
		state.runtime_api_key_ref[i] = api_key_ref[i]
	}
	state.runtime_api_key_ref_len = u8(apin)

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

	if state.marketdata.metrics != nil {
		p.has_md_metrics = state.marketdata.metrics(&p.md_metrics)
	}
	if ok, qmax, drop_delta, rc_delta := metrics_history_summary(state); ok {
		p.md_qmax_recent = qmax
		p.md_drop_delta_recent = drop_delta
		p.md_rc_delta_recent = rc_delta
	}
	p.ui_action_drops = state.ui_action_drops
	p.ui_actions_enqueued_total = state.ui_actions_enqueued_total
	p.stream_switches_total = state.stream_switches_total
	p.timeframe_switches_total = state.timeframe_switches_total
	p.active_live_stats = state.active_has_live_stats
	p.active_live_heatmap = state.active_has_live_heatmap
	p.active_live_vpvr = state.active_has_live_vpvr
	p.active_live_candle = state.active_has_live_candle
	p.active_synth_heatmap = !state.active_has_live_heatmap && state.heatmap_store.count > 0
	p.active_synth_vpvr = !state.active_has_live_vpvr && state.vpvr_store.count > 0
	p.compare_mode = state.compare_mode
	p.compare_widget_idx = state.compare_widget_idx
	p.compare_count = state.compare_count
	p.w_trades_count = state.trades_store.count
	p.w_orderbook_asks = state.orderbook_store.ask_count
	p.w_orderbook_bids = state.orderbook_store.bid_count
	p.w_stats_count = state.stats_store.count
	p.w_heatmap_snaps = state.heatmap_store.count
	p.w_vpvr_levels = state.vpvr_store.count
	p.w_candle_count = state.candle_store.count
	p.ind_rsi_enabled = state.last_indicator_probe.rsi_enabled
	p.ind_macd_enabled = state.last_indicator_probe.macd_enabled
	p.ind_funding_enabled = state.last_indicator_probe.funding_enabled
	p.ind_liq_enabled = state.last_indicator_probe.liq_enabled
	p.ind_trade_counter_enabled = state.last_indicator_probe.trade_counter_enabled
	p.ind_rsi_rendered = state.last_indicator_probe.rsi_rendered
	p.ind_macd_rendered = state.last_indicator_probe.macd_rendered
	p.ind_funding_rendered = state.last_indicator_probe.funding_rendered
	p.ind_liq_rendered = state.last_indicator_probe.liq_rendered
	p.ind_trade_counter_rendered = state.last_indicator_probe.trade_counter_rendered

	// Performance metrics.
	p.cmd_buf_count = len(state.cmd_buf.commands)
	p.cell_count = state.cell_count
	for ci in 0 ..< state.cell_count {
		p.cell_tf_idxs[ci] = state.cell_assignments[ci].tf_idx
	}
	if state.frame_time_count > 0 {
		p.frame_time_p50_us, p.frame_time_p95_us, p.frame_time_p99_us = frame_time_percentiles(state)
	}
	return p
}

// Record a frame time sample (microseconds). Called by platform after each frame.
record_frame_time :: proc(state: ^App_State, us: i64) {
	state.frame_times_us[state.frame_time_head] = us
	state.frame_time_head = (state.frame_time_head + 1) % len(state.frame_times_us)
	if state.frame_time_count < len(state.frame_times_us) {
		state.frame_time_count += 1
	}
}

// Compute p50/p95/p99 from the frame time ring buffer using insertion sort on a small copy.
frame_time_percentiles :: proc(state: ^App_State) -> (p50, p95, p99: i64) {
	n := state.frame_time_count
	if n <= 0 do return

	// Copy into sortable buffer.
	sorted: [120]i64
	start := (state.frame_time_head - n + len(state.frame_times_us)) % len(state.frame_times_us)
	for i in 0 ..< n {
		sorted[i] = state.frame_times_us[(start + i) % len(state.frame_times_us)]
	}

	// Insertion sort (n <= 120, negligible overhead).
	for i in 1 ..< n {
		key := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j] > key {
			sorted[j + 1] = sorted[j]
			j -= 1
		}
		sorted[j + 1] = key
	}

	p50 = sorted[n * 50 / 100]
	p95 = sorted[min(n * 95 / 100, n - 1)]
	p99 = sorted[min(n * 99 / 100, n - 1)]
	return
}

update :: proc(state: ^App_State, input: ports.Input_State) -> ^ui.Command_Buffer {
	state.frame += 1

	_ = drain_marketdata(state)
	queue_ui_actions_from_input(state, input)
	_, _ = apply_ui_actions(state)
	sample_marketdata_metrics(state)
	observe_candle_health(state)
	cache_render_observations(state, input)
	buf := build_ui(state, input)
	if state.ui_action_count > 0 {
		_, _ = apply_ui_actions(state)
		buf = build_ui(state, input)
	}
	persist_draw_tools(state)
	return buf
}

update_web :: proc(state: ^App_State, input: ports.Input_State) -> (buf: ^ui.Command_Buffer, should_render: bool) {
	state.frame += 1
	input_interaction := has_input_interaction(input)
	events_processed := drain_marketdata(state)
	queue_ui_actions_from_input(state, input)
	stream_switched, tf_switched := apply_ui_actions(state)
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
		needs_render = input_interaction
	}
	if !needs_render {
		return &state.cmd_buf, false
	}

	cache_render_observations(state, input)
	buf = build_ui(state, input)
	if state.ui_action_count > 0 {
		sw2, tf2 := apply_ui_actions(state)
		if sw2 do stream_switched = true
		if tf2 do tf_switched = true
		buf = build_ui(state, input)
	}
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

cache_render_observations :: proc(state: ^App_State, input: ports.Input_State) {
	state.last_viewport = input.viewport_size
	state.last_conn = current_conn_status(state)
	state.has_last_render = true
}
