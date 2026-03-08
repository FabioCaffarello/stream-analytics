package layers

import "core:time"
import "mr:ports"
import "mr:services"

LAYER_REGISTRY_CAP :: 10

SETTING_LAYER_PRICE_CANDLES  :: "layer_price_candles"
SETTING_LAYER_TRADES_TAPE    :: "layer_trades_tape"
SETTING_LAYER_ORDERBOOK_DOM  :: "layer_orderbook_dom"
SETTING_LAYER_VPVR_HEATMAP   :: "layer_vpvr_heatmap"
SETTING_LAYER_EVIDENCE       :: "layer_evidence"
SETTING_LAYER_SIGNAL         :: "layer_signal"
SETTING_LAYER_ANALYTICS      :: "layer_analytics"
SETTING_LAYER_STATS_PANEL    :: "layer_stats_panel"
SETTING_LAYER_TRADE_COUNTER  :: "layer_trade_counter"

Layer_Entry :: struct {
	strategy:           Layer_Strategy,
	enabled:            bool,
	render_invocations: u64,
	dropped_outputs:    u64,
	render_samples_us:  [120]i64,
	render_sample_head: int,
	render_sample_count: int,
	render_over_budget: u64,
}

Layer_Registry :: struct {
	entries: [LAYER_REGISTRY_CAP]Layer_Entry,
	count:   int,
}
