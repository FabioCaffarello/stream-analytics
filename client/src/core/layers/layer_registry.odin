package layers

import "mr:ports"
import "mr:services"

LAYER_REGISTRY_CAP :: 8

SETTING_LAYER_PRICE_CANDLES :: "layer_price_candles"
SETTING_LAYER_TRADES_TAPE   :: "layer_trades_tape"
SETTING_LAYER_ORDERBOOK_DOM :: "layer_orderbook_dom"
SETTING_LAYER_VPVR_HEATMAP  :: "layer_vpvr_heatmap"
SETTING_LAYER_EVIDENCE      :: "layer_evidence"
SETTING_LAYER_SIGNAL        :: "layer_signal"

Layer_Entry :: struct {
	strategy:           Layer_Strategy,
	enabled:            bool,
	render_invocations: u64,
	dropped_outputs:    u64,
}

Layer_Registry :: struct {
	entries: [LAYER_REGISTRY_CAP]Layer_Entry,
	count:   int,
}

layer_registry_register :: proc(reg: ^Layer_Registry, strategy: Layer_Strategy, enabled: bool) {
	if reg == nil do return
	if reg.count >= LAYER_REGISTRY_CAP do return
	reg.entries[reg.count] = Layer_Entry{
		strategy = strategy,
		enabled  = enabled,
	}
	reg.count += 1
}

layer_registry_init :: proc(reg: ^Layer_Registry, store: ^Market_Store) {
	if reg == nil do return
	reg^ = {}

	layer_registry_register(reg, price_candles_layer_strategy(), true)
	layer_registry_register(reg, trades_tape_layer_strategy(), true)
	layer_registry_register(reg, orderbook_dom_layer_strategy(), true)
	layer_registry_register(reg, vpvr_heatmap_layer_strategy(), true)
	layer_registry_register(reg, evidence_layer_strategy(), true)
	layer_registry_register(reg, signal_layer_strategy(), true)

	for i in 0 ..< reg.count {
		if reg.entries[i].strategy.init != nil {
			reg.entries[i].strategy.init(store)
		}
	}
}

layer_registry_reset :: proc(reg: ^Layer_Registry, store: ^Market_Store) {
	if reg == nil do return
	for i in 0 ..< reg.count {
		if reg.entries[i].strategy.reset != nil {
			reg.entries[i].strategy.reset(store)
		}
		reg.entries[i].render_invocations = 0
		reg.entries[i].dropped_outputs = 0
	}
}

layer_registry_set_enabled :: proc(reg: ^Layer_Registry, id: Layer_ID, enabled: bool) {
	if reg == nil do return
	for i in 0 ..< reg.count {
		if reg.entries[i].strategy.id == id {
			reg.entries[i].enabled = enabled
			return
		}
	}
}

layer_registry_is_enabled :: proc(reg: ^Layer_Registry, id: Layer_ID) -> bool {
	if reg == nil do return false
	for i in 0 ..< reg.count {
		if reg.entries[i].strategy.id == id {
			return reg.entries[i].enabled
		}
	}
	return false
}

layer_setting_key_for_id :: proc(id: Layer_ID) -> string {
	switch id {
	case .Price_Candles: return SETTING_LAYER_PRICE_CANDLES
	case .Trades_Tape: return SETTING_LAYER_TRADES_TAPE
	case .OrderBook_DOM: return SETTING_LAYER_ORDERBOOK_DOM
	case .VPVR_Heatmap: return SETTING_LAYER_VPVR_HEATMAP
	case .Evidence: return SETTING_LAYER_EVIDENCE
	case .Signal: return SETTING_LAYER_SIGNAL
	}
	return ""
}

layer_registry_load_settings :: proc(reg: ^Layer_Registry, settings: ^services.Settings_Store) {
	if reg == nil || settings == nil do return
	for i in 0 ..< reg.count {
		id := reg.entries[i].strategy.id
		key := layer_setting_key_for_id(id)
		if len(key) == 0 do continue
		if raw, ok := services.settings_get(settings, key); ok {
			reg.entries[i].enabled = raw != "0"
		}
	}
}

layer_registry_save_settings :: proc(reg: ^Layer_Registry, settings: ^services.Settings_Store) {
	if reg == nil || settings == nil do return
	for i in 0 ..< reg.count {
		id := reg.entries[i].strategy.id
		key := layer_setting_key_for_id(id)
		if len(key) == 0 do continue
		services.settings_set(settings, key, reg.entries[i].enabled ? "1" : "0")
	}
}

layer_registry_on_event :: proc(reg: ^Layer_Registry, store: ^Market_Store, evt: ^ports.MD_Event) {
	if reg == nil || evt == nil do return
	for i in 0 ..< reg.count {
		entry := &reg.entries[i]
		if !entry.enabled do continue
		if entry.strategy.on_event != nil {
			entry.strategy.on_event(store, evt)
		}
	}
}

layer_registry_render_bundle :: proc(reg: ^Layer_Registry, bundle_mask: u32, ctx: ^Layer_Context, out: ^Layer_Outputs) {
	if reg == nil || ctx == nil || out == nil do return
	for i in 0 ..< reg.count {
		entry := &reg.entries[i]
		if !entry.enabled do continue
		if (entry.strategy.bundle_mask & bundle_mask) == 0 do continue
		if entry.strategy.render == nil do continue
		before_overflow := out.overflowed
		entry.strategy.render(ctx, out)
		entry.render_invocations += 1
		if out.overflowed > before_overflow {
			entry.dropped_outputs += (out.overflowed - before_overflow)
		}
	}
	layer_outputs_stable_sort(out)
}

layer_registry_collect_diagnostics :: proc(reg: ^Layer_Registry, store: ^Market_Store, out: []Layer_Diagnostics) -> int {
	if reg == nil || len(out) == 0 do return 0
	count := min(reg.count, len(out))
	for i in 0 ..< count {
		entry := &reg.entries[i]
		diag := Layer_Diagnostics{
			id                 = entry.strategy.id,
			enabled            = entry.enabled,
			render_invocations = entry.render_invocations,
			dropped_outputs    = entry.dropped_outputs,
		}
		if entry.strategy.diagnostics != nil {
			entry.strategy.diagnostics(store, &diag)
		}
		out[i] = diag
	}
	return count
}
