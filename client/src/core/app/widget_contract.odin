package app

// S108: Widget Host & Data Context Contracts.
//
// Defines the standard contract between widgets and panes:
// - Widget_Data_Context: all data a widget needs, pre-resolved per frame
// - Widget_Contract: vtable with lifecycle procs (create → bind → update → render → dispose)
// - WIDGET_CONTRACTS: compile-time registration table (Widget_Kind → Widget_Contract)
// - Lifecycle state machine: validates transitions, prevents invalid states
//
// Widgets NEVER access global stores directly. All data flows through
// Widget_Data_Context, resolved once per pane per frame by the host.

import "mr:ports"
import "mr:services"
import "mr:ui"

// ---------------------------------------------------------------------------
// Widget Data Context (ADR-0028 formalized)
// ---------------------------------------------------------------------------

// Widget_Data_Context bundles all data a widget needs for a single frame.
// Resolved by the host (pane runtime), consumed by widget contract procs.
// Widgets receive this as a value — no mutation, no back-references.
Widget_Data_Context :: struct {
	// Market identity (resolved from stream binding).
	venue:          string,   // e.g. "binance-futures"
	symbol:         string,   // e.g. "BTCUSDT"
	stream_idx:     int,      // resolved stream slot (-1 = none)
	stream_bound:   bool,     // has explicit stream binding (vs follow-active)

	// Timeframe (resolved per-pane or inherited from workspace).
	tf_idx:         int,
	tf_string:      string,   // e.g. "1m", "5m"
	tf_ms:          i64,

	// Compare group (-1 = not in compare mode, 0..3 = pane index within group).
	compare_group:  int,

	// Analytics configuration.
	analytics_kind: services.Analytics_Kind,
	show_history:   bool,

	// Pre-resolved data store pointers (widget reads from these, never from globals).
	stores:         Cell_Stores,

	// Composition & health read model.
	surface:        Cell_Surface_View,

	// Chart + indicator config (value copy — immutable for this frame).
	chart:          Chart_Component,
	indicators:     Indicator_Component,
	ind_params:     Indicator_Params,

	// Pane identity.
	pane_id:        Pane_ID,
	cell_idx:       int,
	focused:        bool,

	// Pane dimensions (set by tree layout).
	rect:           ui.Rect,
}

// ---------------------------------------------------------------------------
// Widget Contract (ADR-0027 formalized)
// ---------------------------------------------------------------------------

// Widget_Contract defines the standard lifecycle interface for all widgets.
// Each Widget_Kind has a corresponding contract in WIDGET_CONTRACTS.
//
// Lifecycle order:
//   Created → on_create
//   Bound   → on_bind_context (called when stream/data binding resolves)
//   Active  → on_update + on_render + on_handle_input (each frame)
//   Suspended → on_suspend (paused, e.g. Zen mode or off-screen)
//   Disposing → on_dispose (cleanup)
Widget_Contract :: struct {
	// Called once when pane is allocated. Initialize widget-specific state.
	on_create:       proc(pane: ^Pane),

	// Called when data context becomes available or changes binding.
	// Widget should prepare for rendering with the new context.
	on_bind_context: proc(pane: ^Pane, ctx: Widget_Data_Context),

	// Called each frame for active widgets. Update internal state.
	on_update:       proc(pane: ^Pane, ctx: Widget_Data_Context, dt_ms: f32),

	// Called each frame to render widget content into the given rect.
	// state is passed for access to cmd_buf and text measurement only.
	on_render:       proc(state: ^App_State, pane: ^Pane, ctx: Widget_Data_Context, rect: ui.Rect),

	// Called when input events occur within the pane rect.
	// Returns true if the widget consumed the input.
	on_handle_input: proc(pane: ^Pane, ctx: Widget_Data_Context, input: ports.Input_State, pointer: ui.Pointer_Input) -> bool,

	// Serialize widget-specific view state for persistence.
	on_serialize:    proc(pane: ^Pane) -> Widget_Serialized_State,

	// Called when pane is being disposed. Release any widget-specific resources.
	on_dispose:      proc(pane: ^Pane),
}

// Serialized widget view state for persistence (WORKSPACE_SCHEMA contract).
Widget_Serialized_State :: struct {
	scroll_x:    f32,
	zoom_level:  f32,
	ob_scroll_y: f32,
	tr_scroll_y: f32,
	chart:       Chart_Component,
	indicators:  Indicator_Component,
	ind_params:  Indicator_Params,
	subplots:    Subplot_Component,
	analytics:   Analytics_Component,
}

// ---------------------------------------------------------------------------
// Contract Registration Table
// ---------------------------------------------------------------------------

// Compile-time registration: Widget_Kind → Widget_Contract.
// Exhaustive — compiler enforces coverage via [Widget_Kind] indexing.
// Default implementations delegate to existing render pipelines.
@(rodata)
WIDGET_CONTRACTS := [Widget_Kind]Widget_Contract {
	.Candle       = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = default_on_update, on_render = render_candle_contract,    on_handle_input = default_on_input, on_serialize = default_on_serialize, on_dispose = default_on_dispose },
	.Stats        = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = default_on_update, on_render = render_generic_contract,   on_handle_input = default_on_input, on_serialize = default_on_serialize, on_dispose = default_on_dispose },
	.Counter      = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = default_on_update, on_render = render_generic_contract,   on_handle_input = default_on_input, on_serialize = default_on_serialize, on_dispose = default_on_dispose },
	.Heatmap      = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = default_on_update, on_render = render_generic_contract,   on_handle_input = default_on_input, on_serialize = default_on_serialize, on_dispose = default_on_dispose },
	.VPVR         = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = default_on_update, on_render = render_generic_contract,   on_handle_input = default_on_input, on_serialize = default_on_serialize, on_dispose = default_on_dispose },
	.Trades       = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = default_on_update, on_render = render_generic_contract,   on_handle_input = default_on_input, on_serialize = default_on_serialize, on_dispose = default_on_dispose },
	.Orderbook    = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = default_on_update, on_render = render_generic_contract,   on_handle_input = default_on_input, on_serialize = default_on_serialize, on_dispose = default_on_dispose },
	.DOM          = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = default_on_update, on_render = render_generic_contract,   on_handle_input = default_on_input, on_serialize = default_on_serialize, on_dispose = default_on_dispose },
	.Empty        = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = nil,               on_render = render_empty_contract,     on_handle_input = nil,              on_serialize = default_on_serialize, on_dispose = default_on_dispose },
	.Analytics    = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = default_on_update, on_render = render_analytics_contract, on_handle_input = default_on_input, on_serialize = default_on_serialize, on_dispose = default_on_dispose },
	.Session_VPVR = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = default_on_update, on_render = render_profile_contract,   on_handle_input = default_on_input, on_serialize = default_on_serialize, on_dispose = default_on_dispose },
	.TPO          = { on_create = default_on_create, on_bind_context = default_on_bind, on_update = default_on_update, on_render = render_profile_contract,   on_handle_input = default_on_input, on_serialize = default_on_serialize, on_dispose = default_on_dispose },
}

// ---------------------------------------------------------------------------
// Lifecycle State Machine
// ---------------------------------------------------------------------------

// Validate a lifecycle transition. Returns true if the transition is valid.
widget_lifecycle_valid :: proc(from, to: Widget_Lifecycle_State) -> bool {
	switch from {
	case .Created:
		return to == .Bound || to == .Disposing
	case .Bound:
		return to == .Active || to == .Disposing
	case .Active:
		return to == .Suspended || to == .Bound || to == .Disposing
	case .Suspended:
		return to == .Active || to == .Disposing
	case .Disposing:
		return false  // terminal state
	}
	return false
}

// Attempt a lifecycle transition. Returns true if the transition succeeded.
widget_lifecycle_transition :: proc(host: ^Widget_Host, to: Widget_Lifecycle_State) -> bool {
	if host == nil do return false
	if !widget_lifecycle_valid(host.state, to) do return false
	host.state = to
	return true
}

// ---------------------------------------------------------------------------
// Data Context Resolution
// ---------------------------------------------------------------------------

// Resolve a Widget_Data_Context for a pane from current app state.
// Single entry point — widgets never read App_State directly.
//
// S112: Reads exclusively from pane-local state (binding, tf_override,
// indicators, chart, analytics). No Entity_World array reads.
// Stores are resolved via resolve_stores_for_pane (pane binding + TF).
// Focus is determined by workspace focus (Pane_ID), not cell index.
resolve_widget_data_context :: proc(state: ^App_State, pane: ^Pane, cell_idx: int, rect: ui.Rect) -> Widget_Data_Context {
	ctx: Widget_Data_Context
	if state == nil || pane == nil do return ctx

	ctx.pane_id = pane.id
	ctx.cell_idx = cell_idx
	ctx.rect = rect
	ctx.compare_group = -1  // default: not in compare mode

	// S112: Focus via workspace Pane_ID (not Entity_World cell index).
	ws := workspace_registry_active(&state.ws_registry)
	ctx.focused = (ws != nil && ws.focus.active == pane.id)

	// S112: Effective TF from pane-local state (pane.tf_override + workspace default).
	ctx.tf_idx = pane_effective_tf_idx(pane, ws, state.active_tf_idx)
	tf_opts := TF_OPTIONS
	ctx.tf_string = tf_opts[ctx.tf_idx] if ctx.tf_idx >= 0 && ctx.tf_idx < len(tf_opts) else tf_opts[0]
	tf_ms_opts := TF_OPTION_MS
	ctx.tf_ms = tf_ms_opts[ctx.tf_idx] if ctx.tf_idx >= 0 && ctx.tf_idx < len(tf_ms_opts) else tf_ms_opts[0]

	// Analytics config from pane (not from Entity_World).
	ctx.analytics_kind = pane.analytics.analytics_kind
	ctx.show_history = pane.analytics.show_history

	// Chart + indicator state from pane.
	ctx.chart = pane.chart
	ctx.indicators = pane.indicators
	ctx.ind_params = pane.ind_params

	// S112: Stores resolved from pane binding + effective TF (no Entity_World).
	ctx.stores = resolve_stores_for_pane(state, &pane.binding, ctx.tf_idx)

	// Surface view (composition, health, identity).
	if cell_idx >= 0 && cell_idx < state.world.count {
		ctx.surface = resolve_cell_surface_view_with_stores(state, cell_idx, ctx.stores, ctx.tf_ms)
	}
	ctx.venue = ctx.surface.venue
	ctx.symbol = ctx.surface.symbol
	ctx.stream_bound = ctx.surface.stream_bound

	// S112: Stream index from pane binding (not Entity_World).
	ctx.stream_idx = pane.binding.stream_idx

	return ctx
}

// Convenience: build a Widget_Data_Context from an existing Cell_View_Model.
// Used during migration while both paths coexist.
widget_data_context_from_vm :: proc(vm: Cell_View_Model, pane_id: Pane_ID, rect: ui.Rect) -> Widget_Data_Context {
	return Widget_Data_Context{
		venue          = vm.surface.venue,
		symbol         = vm.surface.symbol,
		stream_idx     = -1,
		stream_bound   = vm.surface.stream_bound,
		tf_idx         = vm.tf_idx,
		tf_string      = vm.tf_string,
		tf_ms          = vm.tf_ms,
		compare_group  = -1,
		analytics_kind = vm.analytics_kind,
		show_history   = vm.show_history,
		stores         = vm.stores,
		surface        = vm.surface,
		chart          = vm.chart,
		indicators     = vm.indicators,
		ind_params     = vm.ind_params,
		pane_id        = pane_id,
		cell_idx       = vm.cell_idx,
		focused        = vm.focused,
		rect           = rect,
	}
}

// ---------------------------------------------------------------------------
// Widget Contract Dispatch
// ---------------------------------------------------------------------------

// Get the contract for a widget kind.
widget_contract_for :: proc(kind: Widget_Kind) -> Widget_Contract {
	return WIDGET_CONTRACTS[kind]
}

// Dispatch create lifecycle to the widget contract.
widget_contract_create :: proc(pane: ^Pane) {
	if pane == nil do return
	contract := WIDGET_CONTRACTS[pane.widget.kind]
	if contract.on_create != nil {
		contract.on_create(pane)
	}
	widget_lifecycle_transition(&pane.widget, .Created)
}

// Dispatch bind_context lifecycle to the widget contract.
widget_contract_bind :: proc(pane: ^Pane, ctx: Widget_Data_Context) {
	if pane == nil do return
	contract := WIDGET_CONTRACTS[pane.widget.kind]
	if contract.on_bind_context != nil {
		contract.on_bind_context(pane, ctx)
	}
	widget_lifecycle_transition(&pane.widget, .Bound)
}

// Dispatch update to the widget contract (per-frame).
widget_contract_update :: proc(pane: ^Pane, ctx: Widget_Data_Context, dt_ms: f32) {
	if pane == nil do return
	contract := WIDGET_CONTRACTS[pane.widget.kind]
	if contract.on_update != nil {
		contract.on_update(pane, ctx, dt_ms)
	}
}

// Dispatch render to the widget contract.
widget_contract_render :: proc(state: ^App_State, pane: ^Pane, ctx: Widget_Data_Context, rect: ui.Rect) {
	if state == nil || pane == nil do return
	contract := WIDGET_CONTRACTS[pane.widget.kind]
	if contract.on_render != nil {
		contract.on_render(state, pane, ctx, rect)
	}
}

// Dispatch input handling to the widget contract.
widget_contract_handle_input :: proc(pane: ^Pane, ctx: Widget_Data_Context, input: ports.Input_State, pointer: ui.Pointer_Input) -> bool {
	if pane == nil do return false
	contract := WIDGET_CONTRACTS[pane.widget.kind]
	if contract.on_handle_input != nil {
		return contract.on_handle_input(pane, ctx, input, pointer)
	}
	return false
}

// Dispatch serialize to the widget contract.
widget_contract_serialize :: proc(pane: ^Pane) -> Widget_Serialized_State {
	if pane == nil do return {}
	contract := WIDGET_CONTRACTS[pane.widget.kind]
	if contract.on_serialize != nil {
		return contract.on_serialize(pane)
	}
	return {}
}

// Dispatch dispose lifecycle to the widget contract.
widget_contract_dispose :: proc(pane: ^Pane) {
	if pane == nil do return
	contract := WIDGET_CONTRACTS[pane.widget.kind]
	if contract.on_dispose != nil {
		contract.on_dispose(pane)
	}
	widget_lifecycle_transition(&pane.widget, .Disposing)
}

// ---------------------------------------------------------------------------
// Default Contract Implementations
// ---------------------------------------------------------------------------

// Default create: initialize pane view state defaults.
@(private = "file")
default_on_create :: proc(pane: ^Pane) {
	if pane == nil do return
	pane.view.zoom_level = 1.0
	pane.tf_override = -1
}

// Default bind_context: update lifecycle state (no widget-specific work).
@(private = "file")
default_on_bind :: proc(pane: ^Pane, ctx: Widget_Data_Context) {
	// Base implementation: nothing to do on bind.
	// Widget-specific contracts can override.
}

// Default update: no-op (stateless widgets).
@(private = "file")
default_on_update :: proc(pane: ^Pane, ctx: Widget_Data_Context, dt_ms: f32) {
	// No-op for widgets without frame-to-frame state.
}

// Default input handler: no consumption.
@(private = "file")
default_on_input :: proc(pane: ^Pane, ctx: Widget_Data_Context, input: ports.Input_State, pointer: ui.Pointer_Input) -> bool {
	return false
}

// Default serialize: capture pane view state.
@(private = "file")
default_on_serialize :: proc(pane: ^Pane) -> Widget_Serialized_State {
	if pane == nil do return {}
	return Widget_Serialized_State{
		scroll_x    = pane.view.scroll_x,
		zoom_level  = pane.view.zoom_level,
		ob_scroll_y = pane.view.ob_scroll_y,
		tr_scroll_y = pane.view.trades_scroll_y,
		chart       = pane.chart,
		indicators  = pane.indicators,
		ind_params  = pane.ind_params,
		subplots    = pane.subplots,
		analytics   = pane.analytics,
	}
}

// Default dispose: no-op (no resources to release).
@(private = "file")
default_on_dispose :: proc(pane: ^Pane) {
	// No-op. Widget-specific contracts can release resources.
}

// ---------------------------------------------------------------------------
// Render Contract Implementations
// ---------------------------------------------------------------------------

// Candle chart: delegates to existing layer canvas with subplot support.
@(private = "file")
render_candle_contract :: proc(state: ^App_State, pane: ^Pane, ctx: Widget_Data_Context, rect: ui.Rect) {
	render_cell_layer_canvas(state, ctx.cell_idx, .Candle, rect)
}

// Generic widget: delegates to existing layer canvas dispatch.
@(private = "file")
render_generic_contract :: proc(state: ^App_State, pane: ^Pane, ctx: Widget_Data_Context, rect: ui.Rect) {
	render_cell_layer_canvas(state, ctx.cell_idx, pane.widget.kind, rect)
}

// Analytics widget: delegates to existing analytics layer canvas.
@(private = "file")
render_analytics_contract :: proc(state: ^App_State, pane: ^Pane, ctx: Widget_Data_Context, rect: ui.Rect) {
	render_cell_layer_canvas_analytics(state, ctx.cell_idx, rect, ctx.analytics_kind)
}

// Session profile widgets (SVPVR, TPO): use dedicated renderer.
@(private = "file")
render_profile_contract :: proc(state: ^App_State, pane: ^Pane, ctx: Widget_Data_Context, rect: ui.Rect) {
	vm := resolve_cell_view_model(state, ctx.cell_idx)
	render_session_profile_cell_vm(&state.cmd_buf, vm, rect)
}

// Empty widget: render nothing (placeholder).
@(private = "file")
render_empty_contract :: proc(state: ^App_State, pane: ^Pane, ctx: Widget_Data_Context, rect: ui.Rect) {
	// Empty pane — no content to render.
}
