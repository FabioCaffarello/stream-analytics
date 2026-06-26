package app

// S117: Dashboard Operating Model — explicit context tiers.
//
// Formalizes the three-tier context hierarchy that governs all dashboard state:
//
//   Tier 1: Global_Context   — app-wide: connection, route, modes, settings
//   Tier 2: Workspace_Context — workspace-scoped: active stream, default TF, layout, focus
//   Tier 3: Pane_Context      — per-pane: binding, TF, widget, indicators, chart, analytics
//
// RULES:
//   - Lower tiers inherit from upper tiers (pane inherits workspace inherits global).
//   - Overrides flow downward only (pane.tf_override shadows workspace.default_tf).
//   - Mutations always target the owning tier (never write upward).
//   - Widgets receive resolved Pane_Context via Widget_Data_Context (read-only).
//   - mr:layers remains the sole visual runtime; this module is pure state contracts.
//
// See ADR-0031 for full rationale and ownership table.

import "mr:ports"
import "mr:services"

// ---------------------------------------------------------------------------
// Tier 1: Global Context
// ---------------------------------------------------------------------------
// Owned by App_State. Read-only for workspaces and panes.
// Scope: connection, route, zen/focus/compare modes, global settings.

Global_Context :: struct {
	// Connection.
	conn_status:     ports.MD_Conn_Status,
	connected:       bool,
	was_ever_connected: bool,

	// Route.
	active_route:    Route,

	// Exclusive display modes.
	zen_active:      bool,
	focus_active:    bool,
	compare_active:  bool,

	// Global timeframe (fallback for panes that inherit).
	active_tf_idx:   int,
}

// Resolve Global_Context from App_State. Pure read — no mutations.
resolve_global_context :: proc(state: ^App_State) -> Global_Context {
	if state == nil do return {}
	return Global_Context{
		conn_status        = state.conn.last_conn,
		connected          = state.conn.last_conn == .Connected,
		was_ever_connected = state.was_ever_connected,
		active_route       = state.chrome.active_route,
		zen_active         = state.zen.active,
		focus_active       = state.focus_mode,
		compare_active     = state.compare.active,
		active_tf_idx      = state.active_tf_idx,
	}
}

// ---------------------------------------------------------------------------
// Tier 2: Workspace Context
// ---------------------------------------------------------------------------
// Owned by Workspace. Reads from Global_Context for defaults.
// Scope: active stream, default TF, layout tree metadata, focus pane.

Resolved_Workspace_Context :: struct {
	// Identity.
	workspace_id:    Workspace_ID,

	// Active stream (workspace-scoped).
	active_stream_idx: int,      // -1 = none
	active_venue:      string,
	active_symbol:     string,

	// Default TF (workspace-level, overridden by panes).
	default_tf_idx:    int,

	// Layout summary.
	pane_count:        int,
	focused_pane_id:   Pane_ID,

	// Mode.
	mode:              Workspace_Mode,
}

// Resolve Resolved_Workspace_Context from Workspace + Global_Context.
resolve_workspace_context :: proc(ws: ^Workspace, gctx: Global_Context) -> Resolved_Workspace_Context {
	if ws == nil do return {}

	_, pane_count := tree_collect_pane_ids(&ws.tree)

	// Default TF: workspace-level if set (>= 0), else inherit from global.
	default_tf := ws.data_ctx.default_tf_idx
	if default_tf < 0 {
		default_tf = gctx.active_tf_idx
	}

	// Resolve active venue/symbol from data_ctx fixed buffers.
	venue_str := string(ws.data_ctx.active_venue[:ws.data_ctx.active_venue_len])
	symbol_str := string(ws.data_ctx.active_symbol[:ws.data_ctx.active_symbol_len])

	return Resolved_Workspace_Context{
		workspace_id     = ws.id,
		active_stream_idx = ws.data_ctx.active_stream_idx,
		active_venue     = venue_str,
		active_symbol    = symbol_str,
		default_tf_idx   = default_tf,
		pane_count       = pane_count,
		focused_pane_id  = ws.focus.active,
		mode             = ws.mode,
	}
}

// ---------------------------------------------------------------------------
// Tier 3: Pane Context
// ---------------------------------------------------------------------------
// Owned by Pane. Reads from Workspace_Context for inheritance.
// Scope: stream binding, effective TF, widget kind, indicators, chart.

Resolved_Pane_Context :: struct {
	// Identity.
	pane_id:         Pane_ID,
	focused:         bool,

	// Stream binding (resolved).
	venue:           string,
	symbol:          string,
	stream_idx:      int,       // resolved slot (-1 = none)
	stream_bound:    bool,      // explicit binding vs follow-active
	follows_active:  bool,      // true if pane inherits workspace active stream

	// Effective timeframe (resolved: pane override > workspace default > global).
	effective_tf_idx: int,
	tf_source:        TF_Source, // which tier provided the TF

	// Widget identity.
	widget_kind:     Widget_Kind,
	role:            Pane_Role,  // S119: pane operational role

	// Compare group (-1 = not in compare mode).
	compare_group:   int,
}

// TF_Source indicates which context tier resolved the effective timeframe.
TF_Source :: enum u8 {
	Global,        // inherited from global active_tf_idx
	Workspace,     // inherited from workspace default_tf_idx
	Pane_Override, // explicit per-pane tf_override
}

// Resolve Resolved_Pane_Context from a Pane + Resolved_Workspace_Context + Global_Context.
// Pure function — no side effects, no mutation.
resolve_pane_context :: proc(
	pane: ^Pane,
	wctx: Resolved_Workspace_Context,
	gctx: Global_Context,
) -> Resolved_Pane_Context {
	if pane == nil do return {}

	ctx: Resolved_Pane_Context
	ctx.pane_id = pane.id
	ctx.focused = (pane.id == wctx.focused_pane_id)
	ctx.widget_kind = pane.widget.kind
	ctx.role = pane.role  // S119
	ctx.compare_group = -1

	// --- TF resolution: pane override > workspace default > global ---
	if pane.tf_override >= 0 {
		ctx.effective_tf_idx = int(pane.tf_override)
		ctx.tf_source = .Pane_Override
	} else if wctx.default_tf_idx >= 0 {
		ctx.effective_tf_idx = wctx.default_tf_idx
		ctx.tf_source = .Workspace
	} else {
		ctx.effective_tf_idx = gctx.active_tf_idx
		ctx.tf_source = .Global
	}

	// --- Stream binding resolution ---
	has_binding := pane.binding.bound_venue_len > 0 && pane.binding.bound_symbol_len > 0
	has_stream := pane.binding.stream_idx >= 0

	if has_binding {
		ctx.venue = string(pane.binding.bound_venue[:pane.binding.bound_venue_len])
		ctx.symbol = string(pane.binding.bound_symbol[:pane.binding.bound_symbol_len])
		ctx.stream_idx = pane.binding.stream_idx
		ctx.stream_bound = true
		ctx.follows_active = false
	} else if has_stream {
		ctx.stream_idx = pane.binding.stream_idx
		ctx.stream_bound = true
		ctx.follows_active = false
	} else {
		// Follow active: inherit workspace active stream.
		ctx.venue = wctx.active_venue
		ctx.symbol = wctx.active_symbol
		ctx.stream_idx = wctx.active_stream_idx
		ctx.stream_bound = false
		ctx.follows_active = true
	}

	return ctx
}

// ---------------------------------------------------------------------------
// Ownership Validation
// ---------------------------------------------------------------------------

// Ownership_Tier classifies where a piece of state is owned.
Ownership_Tier :: enum u8 {
	Global,
	Workspace,
	Pane,
}

// ownership_of returns which tier owns a given state category.
// Compile-time reference for auditing implicit coupling.
ownership_of :: proc(category: State_Category) -> Ownership_Tier {
	switch category {
	case .Connection:        return .Global
	case .Route:             return .Global
	case .Zen_Mode:          return .Global
	case .Focus_Mode:        return .Global
	case .Compare_Mode:      return .Global
	case .Global_TF:         return .Global
	case .Settings:          return .Global
	case .Active_Stream:     return .Workspace
	case .Default_TF:        return .Workspace
	case .Layout_Tree:       return .Workspace
	case .Focus_Pane:        return .Workspace
	case .Workspace_Mode:    return .Workspace
	case .Stream_Binding:    return .Pane
	case .TF_Override:       return .Pane
	case .Widget_Kind:       return .Pane
	case .Indicators:        return .Pane
	case .Chart_Config:      return .Pane
	case .Analytics_Config:  return .Pane
	case .View_State:        return .Pane
	case .Pane_Role_Cat:     return .Pane  // S119
	}
	return .Global
}

State_Category :: enum u8 {
	// Global.
	Connection,
	Route,
	Zen_Mode,
	Focus_Mode,
	Compare_Mode,
	Global_TF,
	Settings,
	// Workspace.
	Active_Stream,
	Default_TF,
	Layout_Tree,
	Focus_Pane,
	Workspace_Mode,
	// Pane.
	Stream_Binding,
	TF_Override,
	Widget_Kind,
	Indicators,
	Chart_Config,
	Analytics_Config,
	View_State,
	Pane_Role_Cat,   // S119: pane role assignment
}

// ---------------------------------------------------------------------------
// Context Invariants (runtime checks for debug builds)
// ---------------------------------------------------------------------------

// Validate that a pane context is well-formed.
pane_context_valid :: proc(ctx: Resolved_Pane_Context) -> bool {
	// Pane must have an ID.
	if ctx.pane_id == PANE_ID_NONE do return false

	// TF index must be in valid range.
	if ctx.effective_tf_idx < 0 || ctx.effective_tf_idx >= len(TF_OPTIONS) do return false

	// Follow-active panes must not claim stream_bound.
	if ctx.follows_active && ctx.stream_bound do return false

	return true
}

// Validate that a workspace context is well-formed.
workspace_context_valid :: proc(ctx: Resolved_Workspace_Context) -> bool {
	// Workspace must have an ID.
	if ctx.workspace_id == Workspace_ID(0) do return false

	// Default TF must be in valid range.
	if ctx.default_tf_idx < 0 || ctx.default_tf_idx >= len(TF_OPTIONS) do return false

	return true
}
