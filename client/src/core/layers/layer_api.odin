package layers

import "mr:ports"
import "mr:services"
import "mr:ui"

// Layer Architecture contracts.
// Strategy lifecycle:
// - init: one-time setup
// - on_event: optional per-frame event hook
// - on_snapshot: optional snapshot boundary hook
// - render: emit render primitives only
// - reset: reset internal state
// - diagnostics: emit bounded diagnostics

Layer_ID :: enum u8 {
	Price_Candles,
	Trades_Tape,
	OrderBook_DOM,
	VPVR_Heatmap,
	Evidence,
	Signal,
	Analytics,
	Stats_Panel,
	Trade_Counter,
}

Layer_Bundle :: enum u32 {
	None            = 0,
	Price_Candles   = 1 << 0,
	Trades_Tape     = 1 << 1,
	OrderBook_DOM   = 1 << 2,
	VPVR_Heatmap    = 1 << 3,
	Evidence        = 1 << 4,
	Signal          = 1 << 5,
	Analytics       = 1 << 6,
	Stats_Panel     = 1 << 7,
	Trade_Counter   = 1 << 8,

	Bundle_Candles  = (1 << 0) | (1 << 3) | (1 << 4) | (1 << 5) | (1 << 6),
	Bundle_Trades   = (1 << 1) | (1 << 4) | (1 << 5),
	Bundle_Orderbook = (1 << 2) | (1 << 4) | (1 << 5),
	Bundle_DOM      = (1 << 2) | (1 << 1) | (1 << 4) | (1 << 5),
	Bundle_Heatmap  = (1 << 3) | (1 << 4) | (1 << 5),
	Bundle_VPVR     = (1 << 3) | (1 << 4) | (1 << 5),
	Bundle_Stats    = (1 << 7) | (1 << 5),             // S87: Stats_Panel + Signal (was Price_Candles)
	Bundle_Counter  = (1 << 8) | (1 << 5),             // S87: Trade_Counter + Signal (was Trades_Tape)
	Bundle_Analytics = (1 << 6) | (1 << 4) | (1 << 5),
	Bundle_Empty    = 0,
}

layer_mask_for_id :: proc(id: Layer_ID) -> u32 {
	switch id {
	case .Price_Candles: return u32(Layer_Bundle.Price_Candles)
	case .Trades_Tape: return u32(Layer_Bundle.Trades_Tape)
	case .OrderBook_DOM: return u32(Layer_Bundle.OrderBook_DOM)
	case .VPVR_Heatmap: return u32(Layer_Bundle.VPVR_Heatmap)
	case .Evidence: return u32(Layer_Bundle.Evidence)
	case .Signal: return u32(Layer_Bundle.Signal)
	case .Analytics: return u32(Layer_Bundle.Analytics)
	case .Stats_Panel: return u32(Layer_Bundle.Stats_Panel)
	case .Trade_Counter: return u32(Layer_Bundle.Trade_Counter)
	}
	return 0
}

Layer_Capabilities :: struct {
	has_trades:    bool,
	has_orderbook: bool,
	has_stats:     bool,
	has_heatmap:   bool,
	has_vpvr:      bool,
	has_candles:   bool,
	has_evidence:  bool,
	has_signal:    bool,
	has_analytics: bool,
}

Layer_Widget_State :: enum u8 {
	Loading,
	Live,
	Stale,
	Degraded,
	Empty,
}

// S94: Subplot visibility flags — controls which analytics subplots
// render below the main candle chart. Set from per-cell Indicator_Component.
Subplot_Flags :: struct {
	show_cvd:       bool,
	show_delta_vol: bool,
	show_oi:        bool,
}

subplot_flags_count :: proc(flags: Subplot_Flags) -> int {
	n := 0
	if flags.show_cvd do n += 1
	if flags.show_delta_vol do n += 1
	if flags.show_oi do n += 1
	return n
}

// Read-only context for layer render hooks.
Layer_Context :: struct {
	store:        ^Market_Store,
	stream:       ^Market_Stream,
	subject_id:   u64,
	now_ms:       i64,
	frame_seq:    u64,
	viewport:     ui.Rect,
	text:         ports.Text_Port,
	capabilities: Layer_Capabilities,
	signal_evidence_link_enabled: bool,
	analytics_kind:  services.Analytics_Kind,   // filter: which analytics kind to render (for Analytics cells)
	analytics_filter: bool,                      // true = render only analytics_kind; false = render all
	active_bundle: u32,                          // S86: requested bundle mask — lets render functions conditionally skip irrelevant output
	subplot_flags: Subplot_Flags,               // S94: which analytics subplots are active on this candle cell
}

Layer_Diagnostics :: struct {
	id:               Layer_ID,
	enabled:          bool,
	has_data:         bool,
	state:            Layer_Widget_State,
	entries:          int,
	max_entries:      int,
	evicted_total:    u64,
	render_invocations: u64,
	dropped_outputs:  u64,
	parse_total:      u64,
	fallback_total:   u64,
	drop_total:       u64,
	drop_capacity_total: u64,
	drop_render_overflow_total: u64,
	last_seq:         i64,
	last_unix:        i64,
	signal_link_total: u64,
	signal_link_evidence_seq: i64,
	render_budget_us: i64,
	render_p95_us:    i64,
	render_p99_us:    i64,
	render_over_budget: u64,
}

Layer_Strategy :: struct {
	id:          Layer_ID,
	name:        string,
	bundle_mask: u32,
	z_order:     int,

	init:        proc(store: ^Market_Store),
	on_event:    proc(store: ^Market_Store, evt: ^ports.MD_Event),
	on_snapshot: proc(store: ^Market_Store, subject_id: u64),
	render:      proc(ctx: ^Layer_Context, out: ^Layer_Outputs),
	reset:       proc(store: ^Market_Store),
	diagnostics: proc(store: ^Market_Store, out: ^Layer_Diagnostics),
}

layer_capabilities_from_stream :: proc(stream: ^Market_Stream) -> Layer_Capabilities {
	if stream == nil do return {}
	return Layer_Capabilities{
		has_trades    = stream.trades.count > 0,
		has_orderbook = stream.orderbook.ask_count > 0 || stream.orderbook.bid_count > 0,
		has_stats     = stream.stats.count > 0,
		has_heatmap   = stream.heatmap.count > 0,
		has_vpvr      = stream.vpvr.count > 0,
		has_candles   = stream.candles.count > 0,
		has_evidence  = stream.evidence_count > 0,
		has_signal    = stream.signals.kind_count > 0,
		has_analytics = stream.analytics.count > 0,
	}
}

layer_noop_init :: proc(store: ^Market_Store) {
	_ = store
}

layer_noop_on_event :: proc(store: ^Market_Store, evt: ^ports.MD_Event) {
	_ = store
	_ = evt
}

layer_noop_on_snapshot :: proc(store: ^Market_Store, subject_id: u64) {
	_ = store
	_ = subject_id
}

layer_noop_reset :: proc(store: ^Market_Store) {
	_ = store
}

layer_noop_diagnostics :: proc(store: ^Market_Store, out: ^Layer_Diagnostics) {
	_ = store
	if out == nil do return
}

layer_noop_render :: proc(ctx: ^Layer_Context, out: ^Layer_Outputs) {
	_ = ctx
	_ = out
}

layer_z_order_for_id :: proc(id: Layer_ID) -> int {
	switch id {
	case .VPVR_Heatmap: return 10
	case .Price_Candles: return 20
	case .Stats_Panel: return 22    // S87: between candles and analytics
	case .Trade_Counter: return 23  // S87: between candles and analytics
	case .OrderBook_DOM: return 30
	case .Trades_Tape: return 40
	case .Evidence: return 50
	case .Signal: return 60
	case .Analytics: return 25
	}
	return 100
}
