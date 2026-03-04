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
}

Layer_Bundle :: enum u32 {
	None            = 0,
	Price_Candles   = 1 << 0,
	Trades_Tape     = 1 << 1,
	OrderBook_DOM   = 1 << 2,
	VPVR_Heatmap    = 1 << 3,
	Evidence        = 1 << 4,
	Signal          = 1 << 5,

	Bundle_Candles  = (1 << 0) | (1 << 3) | (1 << 4) | (1 << 5),
	Bundle_Trades   = (1 << 1) | (1 << 4) | (1 << 5),
	Bundle_Orderbook = (1 << 2) | (1 << 4) | (1 << 5),
	Bundle_DOM      = (1 << 2) | (1 << 1) | (1 << 4) | (1 << 5),
	Bundle_Heatmap  = (1 << 3) | (1 << 4) | (1 << 5),
	Bundle_VPVR     = (1 << 3) | (1 << 4) | (1 << 5),
	Bundle_Stats    = (1 << 0) | (1 << 5),
	Bundle_Counter  = (1 << 1) | (1 << 5),
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
}

Layer_Diagnostics :: struct {
	id:               Layer_ID,
	enabled:          bool,
	has_data:         bool,
	render_invocations: u64,
	dropped_outputs:  u64,
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
	case .OrderBook_DOM: return 30
	case .Trades_Tape: return 40
	case .Evidence: return 50
	case .Signal: return 60
	}
	return 100
}
