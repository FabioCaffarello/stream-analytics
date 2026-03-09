package app

import "mr:services"

// S45/S48/S51/S80/S81/S82: Workspace State Schema — canonical contract for persisted vs derived state.
//
// WORKSPACE_SCHEMA_VERSION is bumped on every persistence format change.
// Current format: V10 (extends V9 with OI indicator flag bit 10).
//
// --- Persisted Per-Cell Fields (V6 layout string, unchanged) ---
//   widget_kind       Widget_Kind enum (0-9, where 9=Analytics)
//   stream_binding    "venue/symbol" or "-1" (follow active)
//   indicator_flags   11 bools packed into int (S81: bits 8-9 = CVD, DV; S82: bit 10 = OI)
//   col_span          grid column span (1+)
//   row_span          grid row span (1+)
//   sub_main_split    subplot main ratio (x1000)
//   sub_ratios[5]     subplot sub-ratios (x1000)
//   tf_idx            per-cell TF (-1=global, 0-8)
//   chart_display     packed: vol/heatmap/vpvr/heatmap_idx/ob_grp/dom_grp/trade_filter
//                     bits17-18: analytics_kind (S48, 0-3)
//
// --- Persisted Global Fields ---
//   layout_mode, layout_preset, col_weights, row_weights
//   active_tf_idx, active_stream (venue/symbol/channel/subject_id)
//   signal_evidence_link, panel_visible_mask
//   draw_tools, indicator_params (global), chart_display (global defaults)
//   connection_profiles (12 slots), layer_registry, assist_mode
//   active_route (S80)           — Route ordinal persisted as settings key
//   portfolio_tab (S80)          — Portfolio_Tab ordinal persisted as settings key
//
// --- Derived State (never persisted) ---
//   Cell_Surface_View           derived per-frame from apply_state
//   Stream_Apply_State          populated from protocol events
//   GetRange_Component          transient backfill (reseeded on connect)
//   Compare_State               ephemeral comparison session
//   View_Component              scroll/zoom/crosshair (reset on start)
//   Overlay_State               UI modals (transient)
//   Telemetry/Connection/Error  runtime-only
//
// --- Runtime Snapshot (V3, S82) ---
//   Per-cell: chart_display (packed), indicator_flags (packed)
//   Global: active_route

// S119: V11 — Pane_Role field on Pane struct, extended Context_Tab enum.
WORKSPACE_SCHEMA_VERSION :: 11

// S111: Persist result — replaces bool return for restore functions.
Persist_Result :: enum u8 {
	Ok,               // Restore succeeded.
	No_Data,          // No persisted layout found (first run or cleared).
	Version_Mismatch, // Stored schema version is newer than runtime supports.
	Corrupted,        // Data exists but failed to parse.
	Too_Many_Cells,   // Cell count exceeds CELL_MAX.
}

persist_result_ok :: proc(r: Persist_Result) -> bool {
	return r == .Ok
}

// Pack per-cell chart display into a single integer for V6 persistence.
// Layout: bit0=show_vol, bit1=show_heatmap, bit2=show_vpvr,
//         bits3-4=heatmap_intensity_idx (0-3),
//         bits5-8=ob_group_idx (0-15),
//         bits9-12=dom_group_idx (0-15),
//         bits13-16=trade_filter_idx (0-15),
//         bits17-18=analytics_kind (S48, 0-3)
pack_chart_display_with_analytics :: proc(c: ^Chart_Component, a: ^Analytics_Component) -> int {
	f := 0
	if c.show_vol     do f |= 1 << 0
	if c.show_heatmap do f |= 1 << 1
	if c.show_vpvr    do f |= 1 << 2
	f |= (c.heatmap_intensity_idx & 0x3) << 3
	f |= (c.ob_group_idx & 0xF) << 5
	f |= (c.dom_group_idx & 0xF) << 9
	f |= (c.trade_filter_idx & 0xF) << 13
	if a != nil {
		f |= (int(a.analytics_kind) & 0x3) << 17
	}
	return f
}

pack_chart_display :: proc(c: ^Chart_Component) -> int {
	return pack_chart_display_with_analytics(c, nil)
}

// Unpack chart display integer into Chart_Component + Analytics_Component fields.
unpack_chart_display_with_analytics :: proc(c: ^Chart_Component, a: ^Analytics_Component, f: int) {
	c.show_vol              = (f & (1 << 0)) != 0
	c.show_heatmap          = (f & (1 << 1)) != 0
	c.show_vpvr             = (f & (1 << 2)) != 0
	c.heatmap_intensity_idx = (f >> 3) & 0x3
	c.ob_group_idx          = (f >> 5) & 0xF
	c.dom_group_idx         = (f >> 9) & 0xF
	c.trade_filter_idx      = (f >> 13) & 0xF
	if a != nil {
		a.analytics_kind = services.Analytics_Kind((f >> 17) & 0x3)
		a.show_history = true
	}
}

unpack_chart_display :: proc(c: ^Chart_Component, f: int) {
	unpack_chart_display_with_analytics(c, nil, f)
}
