package app

// ECS-like Entity Component System for grid cells.
// Each cell is an entity (0..CELL_MAX-1). Components are parallel arrays.

import "mr:md_common"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:ui"
import "mr:widgets"

// --- Component Types ---

// What widget type this entity displays.
Widget_Component :: struct {
	kind: Widget_Kind,
}

// Which stream this entity is bound to.
Stream_Binding :: struct {
	stream_idx:      int,    // -1 = follow active
	bound_venue:     [24]u8,
	bound_venue_len: u8,
	bound_symbol:    [32]u8,
	bound_symbol_len: u8,
}

// Scroll, zoom, crosshair for chart-type entities.
View_Component :: struct {
	candle_scroll_x: f32,
	candle_zoom:     f32,
	ob_scroll_y:     f32,
	trades_scroll_y: f32,
	crosshair:       widgets.Crosshair_State,
}

// Indicator toggle state per entity.
Indicator_Component :: struct {
	show_ma:            bool,
	show_bbands:        bool,
	show_vwap:          bool,
	show_rsi:           bool,
	show_macd:          bool,
	show_funding:       bool,
	show_liq:           bool,
	show_trade_counter: bool,
	// S81: Analytics chart subplot indicators.
	show_cvd:           bool,   // CVD line subplot below candle chart
	show_delta_vol:     bool,   // Delta volume bars subplot below candle chart
	// S82: Open Interest indicator.
	show_oi:            bool,   // OI line subplot below candle chart
}

// Indicator parameter values per entity.
Indicator_Params :: struct {
	ma_periods:  [3]int,
	bb_period:   int,
	bb_sigma:    f64,
	rsi_period:  int,
	macd_fast:   int,
	macd_slow:   int,
	macd_signal: int,
}

// Chart display options per entity.
Chart_Component :: struct {
	chart_type:            widgets.Chart_Type,
	show_vol:              bool,
	show_heatmap:          bool,
	show_vpvr:             bool,
	heatmap_intensity_idx: int,
	ob_group_idx:          int,
	dom_group_idx:         int,
	trade_filter_idx:      int,
}

// Subplot layout per entity.
Subplot_Component :: struct {
	sub_main_split: f32,
	sub_ratios:     [5]f32,
	sub_resize_idx: int,
}

// Grid span per entity.
Span_Component :: struct {
	col_span: int,
	row_span: int,
}

// Timeframe per entity.
Timeframe_Component :: struct {
	tf_idx: int,  // -1 = follow global
}

// S48: Analytics display mode per entity.
Analytics_Component :: struct {
	analytics_kind: services.Analytics_Kind, // which analytics to display
	show_history:   bool,                     // show ring buffer bar chart
}

// GetRange state per entity.
GetRange_Component :: struct {
	pending:    bool,
	seeded:     bool,
	oldest_ts:  i64,
	sent_frame: u64,
}

// --- Entity World (component storage) ---

Entity_World :: struct {
	// Parallel arrays indexed by entity_id (0..CELL_MAX-1).
	widgets:    [CELL_MAX]Widget_Component,
	bindings:   [CELL_MAX]Stream_Binding,
	views:      [CELL_MAX]View_Component,
	indicators: [CELL_MAX]Indicator_Component,
	ind_params: [CELL_MAX]Indicator_Params,
	charts:     [CELL_MAX]Chart_Component,
	subplots:   [CELL_MAX]Subplot_Component,
	spans:      [CELL_MAX]Span_Component,
	timeframes: [CELL_MAX]Timeframe_Component,
	analytics:  [CELL_MAX]Analytics_Component,  // S48: per-cell analytics kind
	getranges:  [CELL_MAX]GetRange_Component,
	count:      int,
	focused:    int,  // focused entity index
}

// --- Sub-state structs (extracted from App_State god struct) ---

Connection_State :: struct {
	last_conn:               ports.MD_Conn_Status,
	prev_conn_for_reconcile: ports.MD_Conn_Status,
	needs_reconcile:         bool,
	runtime_ws_url:          [256]u8,
	runtime_ws_url_len:      u16,
	runtime_api_key_ref:     [64]u8,
	runtime_api_key_ref_len: u8,
}

Telemetry_State :: struct {
	hud_enabled:          bool,
	hud_cache:            Telemetry_HUD_Cache,
	metrics_history:      [MD_METRICS_HISTORY_CAP]MD_Metrics_History_Sample,
	metrics_head:         int,
	metrics_count:        int,
	frame_times_us:       [120]i64,
	frame_time_head:      int,
	frame_time_count:     int,
	last_indicator_probe: widgets.Indicator_Render_Probe,
	// Sub-phase timing (microseconds, updated each frame).
	drain_us:             i64,
	actions_us:           i64,
	render_us:            i64,
}

Error_State :: struct {
	text:       [128]u8,
	len:        int,
	frame:      u64,    // frame when error was recorded
	error_kind: Error_Kind,
}

// Client-side log buffer for telemetry HUD / health panel.
Log_State :: struct {
	buf: services.Log_Buffer,
}

Evidence_Entry :: struct {
	kind:          [24]u8,
	kind_len:      u8,
	confidence:    f64,
	reason:        [96]u8,
	reason_len:    u8,
	feature_tags:  [4][24]u8,
	feature_vals:  [4]f64,
	feature_count: int,
	unix:          i64,
	subject_id:    u64,
}

EVIDENCE_HISTORY_CAP :: 64

Evidence_State :: struct {
	entries: [EVIDENCE_HISTORY_CAP]Evidence_Entry,
	head:    int,
	count:   int,
}

Error_Kind :: enum u8 {
	None,
	Parse_Failure,
	GetRange_Timeout,
	Subscribe_Failure,
	Connection,
}

// S39: Per-pane GetRange state for compare mode (mirrors GetRange_Component for cells).
Compare_Pane_GetRange :: struct {
	pending:    bool,
	seeded:     bool,
	oldest_ts:  i64,
	sent_frame: u64,
}

Compare_State :: struct {
	active:       bool,
	slots:        [4]u64,
	count:        int,
	widget_idx:   int,
	focused_pane: int,       // S39: focused pane index (-1 = none)
	tf_idx:       [4]int,    // S38: per-pane TF override (-1 = follow global)
	getranges:    [4]Compare_Pane_GetRange, // S39: per-pane getrange state
	scroll_x:     [4]f32,
	zoom:         [4]f32,
	show_vol:     [4]bool,
	show_heatmap: [4]bool,
	show_vpvr:    [4]bool,
	heatmap_idx:  [4]int,
	ob_scroll:    [4]f32,
	ob_grp:       [4]int,
	trade_scroll: [4]f32,
	trade_filter: [4]int,
}

Overlay_State :: struct {
	show_help:                 bool,
	show_exchange_manager:     bool,
	show_widget_catalog:       bool,
	show_stream_picker:        bool,
	catalog_step:              int,
	catalog_selected:          Widget_Kind,
	catalog_analytics_kind:    services.Analytics_Kind, // S48: analytics sub-kind for catalog
	cell_stream_picker_open:   int,
	cell_stream_picker_scroll: f32,
	exchange_sections:         [services.EXCHANGE_CAP]ui.Section_State,
}

UI_Chrome_State :: struct {
	sidebar:           ui.Sidebar_State,
	panel_visible:     [ui.PANEL_COUNT]bool,
	detail_expanded:   bool,
	detail_w:          f32,
	detail_resizing:   bool,
	section_streams:   ui.Section_State,
	section_analytics: ui.Section_State, // S55: analytics inspector
	section_layers:    ui.Section_State,
	section_panels:    ui.Section_State,
	active_route:      Route,
}

Zen_State :: struct {
	active:       bool,
	top_alpha:    f32,
	bottom_alpha: f32,
	left_alpha:   f32,
	compact_top:  bool,
	tf_osd_frame: u64,
}

Whale_Alert_State :: struct {
	avg_qty: f64,
	price:   f64,
	qty:     f64,
	buy:     bool,
	frame:   u64,
}

Toast_State :: struct {
	text:  [64]u8,
	len:   int,
	frame: u64,
}

// --- Global sub-states (Phase 3: App_State decomposition) ---

// Global indicator toggles + parameters (extracted from App_State).
Global_Indicator_State :: struct {
	show_ma:            bool,
	show_bbands:        bool,
	show_vwap:          bool,
	show_rsi:           bool,
	show_macd:          bool,
	show_funding:       bool,
	show_liq:           bool,
	show_trade_counter: bool,
	// S81: Analytics chart subplot indicators.
	show_cvd:           bool,
	show_delta_vol:     bool,
	// S82: Open Interest indicator.
	show_oi:            bool,
	ma_periods:         [3]int,
	bb_period:          int,
	bb_sigma:           f64,
	rsi_period:         int,
	macd_fast:          int,
	macd_slow:          int,
	macd_signal:        int,
}

// Global getrange state for active stream (extracted from App_State).
GetRange_Global_State :: struct {
	pending:                  bool,
	seeded:                   bool,
	subject_id:               u64,
	sent_frame:               u64,
	active_candle_subject_id: u64,
	oldest_ts:                i64,
}

// Global chart display toggles (extracted from App_State).
Chart_Display_State :: struct {
	show_vol:              bool,
	show_heatmap:          bool,
	show_vpvr:             bool,
	heatmap_intensity_idx: int,
}

// Global data stores (extracted from App_State).
Global_Stores :: struct {
	trades:    services.Trades_Store,
	orderbook: services.Orderbook_Store,
	heatmap:   services.Heatmap_Store,
	vpvr:      services.VPVR_Store,
	stats:     services.Stats_Store,
	candle:    services.Candle_Store,
	signals:   services.Signal_Store,
	dom:       services.DOM_Store,
	footprint: services.Footprint_Store,
	markets:   services.Markets_Store,
	analytics:    services.Analytics_Store,       // S47: global analytics ring
	session_vpvr: services.Session_VPVR_Store,   // S49: global session VP
	tpo:          services.TPO_Store,             // S49: global TPO profile
}

Active_Stream_Metrics :: struct {
	state:                streams.Stream_State,
	desync_reason:        streams.Stream_Desync_Reason,
	rtt_ms:               i64,
	lag_ms:               i64,
	last_msg_ts_ms:       i64,
	last_server_ts_ms:    i64,
	last_stats_ts_ms:     i64,
	last_orderbook_ts_ms: i64,
	drop_count:           int,
	drop_trade_ring:      int,
	drop_candle_ring:     int,
	drop_ws_queue:        int,
	drop_payload_oversize: int,
	reconnect_count:      int,
	subscribe_acks:       int,
	last_ack_metric:      int,
	seq_gap_count:        int,
	resync_count:         int,
	candle_backlog:       int,
	candle_backlog_cap:   int,
	trade_backlog_cap:    int,
	signal_backlog:       int,
	signal_backlog_cap:   int,
	msg_rate:             f64,
	bytes_rate:           f64,
	parsed_msgs_total:    u64,
	parsed_bytes_total:   u64,
	parse_arena_resets:   u64,
	alloc_estimate_total: u64,
	alloc_estimate_frame: i64,
	parse_time_p95_us:    i64,
	parse_time_p99_us:    i64,
	apply_time_p95_us:    i64,
	apply_time_p99_us:    i64,
	batched_decode_time_p95_us: i64,
	batched_decode_time_p99_us: i64,
	has_live_stats:       bool,
	has_live_heatmap:     bool,
	has_live_vpvr:        bool,
	has_live_candle:      bool,
	context_stage:        md_common.Composition_Stage,
	transport_state:       ports.MD_Transport_State,
	ws_error_category:     ports.MD_WS_Error_Category,
	ws_error_action:       ports.MD_WS_Error_Action,
	backend_gap_no_metrics:         int,
	backend_gap_pong_timeout:       int,
	backend_gap_resync_ack_timeout: int,
	backend_gap_missing_ts_server:  int,
	backend_gap_seq_gap_recurring:  int,
	backend_gap_frequent_drops:     int,
	// Terminal_V1 protocol fields.
	transport_mode:         u8,
	auth_mode:              u8,
	protocol_version:       int,
	server_instance_id:     [32]u8,
	server_instance_id_len: u8,
	server_instance_id_hash: u64,
	hello_timeout_count:    int,
	pong_rtt_ms:            i64,
	active_subs:            int,
	// Server-pushed metrics (from METRICS frame).
	server_ws_dropped:      i64,
	server_ws_queue_len:    int,
	server_ws_lag_ms:       i64,
	server_serialize_errors: i64,
	server_resync_total:    i64,
	server_pub_deliver_ms:  i64,
	// Server capability limits (from HELLO).
	server_max_subscriptions:    int,
	server_max_frame_bytes:      int,
	server_metrics_cadence_ms:   int,
	server_keepalive_interval_ms: int,
	server_rate_limit_enabled:   bool,
	// Backpressure (from METRICS).
	server_backpressure_level:    int,
	server_queue_capacity:        int,
	server_queue_high_watermark:  int,
	server_recommended_action:    [32]u8,
	server_recommended_action_len: u8,
	// Feature negotiation.
	negotiated_feature_count:     int,
	negotiated_feature_names:     [8][24]u8,
	negotiated_feature_name_lens: [8]u8,
	batched_frames_received:      u64,
	batched_events_received:      u64,
	batched_fastpath_events:      u64,
	batched_fallback_events:      u64,
	canonical_stats_frames:       u64,
	stats_fallback_frames:        u64,
	canonical_evidence_frames:    u64,
	legacy_evidence_frames:       u64,
	evidence_fallback_frames:     u64,
	canonical_signal_frames:      u64,
	legacy_signal_frames:         u64,
	signal_fallback_frames:       u64,
	legacy_evidence_rejected:     u64,
	legacy_signal_rejected:       u64,
	// Integrity counters.
	snapshot_hash_mismatches:     int,
	snapshot_seq_violations:      int,
	prev_seq_violations:         int,
	hash_validation_skipped:     int,
	// Legacy tracking.
	legacy_downgrade_count:       int,
	legacy_connected_since_ms:    i64,
	// Assisted backpressure tuning.
	assist_enabled:               bool,
	assist_degrade_heatmap:       bool,
	assist_degrade_vpvr:          bool,
	assist_getrange_divisor:      int,
	assist_reason:                [32]u8,
	assist_reason_len:            u8,
	assist_user_enabled:          bool,
	assist_drop_heatmap:          int,
	assist_drop_vpvr:             int,
	assist_drop_evidence:         int,
}

Backpressure_Assist_State :: struct {
	enabled:          bool,
	user_enabled:     bool,
	recommended_action_pending: bool,
	degrade_heatmap:  bool,
	degrade_vpvr:     bool,
	getrange_divisor: int,
	reason:           [32]u8,
	reason_len:       u8,
	cooldown_frames:  int,
	dropped_heatmap:  u64,
	dropped_vpvr:     u64,
	dropped_evidence: u64,
}

// S26: Context_Stage removed — replaced by md_common.Composition_Stage (5 values, derived).
