package app

import "mr:ports"
import "mr:ui"

// S109: Split Tree Runtime — primary dashboard path.
// All panes render through Widget_Contract dispatch. Entity_World provides
// data stores; pane pool owns widget config, TF, analytics.
// DFS traversal order of panes = cell index order.

// Main entry: render dashboard using workspace tree layout.
@(private = "package")
build_workspace_dashboard :: proc(
	state: ^App_State,
	workspace: ui.Rect,
	pointer: ui.Pointer_Input,
	workspace_input: ports.Input_State,
	workspace_pointer: ui.Pointer_Input,
	gap: f32,
	viewport_w, viewport_h: f32,
	mobile: bool,
) {
	ws := workspace_registry_active(&state.ws_registry)
	if ws == nil {
		// S109: Auto-initialize workspace — no grid fallback.
		ws = workspace_registry_alloc(&state.ws_registry)
		if ws != nil {
			workspace_sync_from_world(state)
			ws = workspace_registry_active(&state.ws_registry)
		}
		if ws == nil do return
	}

	// 1. Resolve tree layout → per-pane rects + per-node bounds.
	pane_rects, node_bounds := resolve_tree_layout_full(&ws.tree, workspace)

	// 2. Collect panes in DFS order (position = cell index).
	pane_ids, pane_count := tree_collect_pane_ids(&ws.tree)

	// 3. Ensure focused cell points to a valid candle pane.
	// S112: Focus tracked by Pane_ID (ws.focus.active) as source of truth.
	// Bridge: world.focused still maintained for legacy paths.
	render_count := min(pane_count, state.world.count)
	if state.world.focused < 0 || state.world.focused >= render_count {
		state.world.focused = -1
		ws.focus.active = PANE_ID_NONE
	}
	if state.world.focused >= 0 {
		// Validate that the focused cell is still a candle.
		fpid := pane_ids[state.world.focused]
		fpane := pane_pool_get(&ws.pane_pool, fpid)
		// S119: Accept Primary_Chart role or Candle widget kind.
		is_focus_target := fpane != nil && (fpane.role == .Primary_Chart || fpane.widget.kind == .Candle)
		if !is_focus_target {
			state.world.focused = -1
			ws.focus.active = PANE_ID_NONE
		}
	}
	if state.world.focused < 0 {
		// S119: Find the first Primary_Chart pane, or first Candle as fallback.
		for fi in 0 ..< render_count {
			fpid := pane_ids[fi]
			fpane := pane_pool_get(&ws.pane_pool, fpid)
			if fpane != nil && (fpane.role == .Primary_Chart || fpane.widget.kind == .Candle) {
				state.world.focused = fi
				ws.focus.active = fpid
				break
			}
		}
	} else {
		// S112: Keep ws.focus.active in sync with world.focused.
		ws.focus.active = pane_ids[state.world.focused]
	}

	// 4. Crosshair sync across candle charts.
	// Bridge: crosshair state still lives in Entity_World views (chart interaction writes there).
	sync_price := f64(0)
	sync_active := false
	for si in 0 ..< state.world.count {
		if state.world.widgets[si].kind != .Candle do continue
		if state.world.views[si].crosshair.active {
			sync_price = state.world.views[si].crosshair.price_at_y
			sync_active = true
			break
		}
	}

	// 5. Render each pane through Widget_Contract dispatch.
	for i in 0 ..< render_count {
		pid := pane_ids[i]
		pidx := int(pid) - 1
		if pidx < 0 || pidx >= PANE_MAX do continue
		rect := pane_rects[pidx]

		// Apply gap inset for visual separation between panes.
		half_gap := gap * 0.5
		rect.pos.x += half_gap
		rect.pos.y += half_gap
		rect.size.x -= gap
		rect.size.y -= gap
		if rect.size.x <= 0 || rect.size.y <= 0 do continue

		pane := pane_pool_get(&ws.pane_pool, pid)
		if pane == nil do continue

		// S109: Route through contract-based pane renderer.
		render_pane_via_contract(state, pane, i, rect, workspace_pointer, workspace_input, render_count)
	}

	// 6. Split edge resize handles.
	if !mobile {
		update_split_resize(state, ws, node_bounds, pointer)
	}

	// 7. Right-click context menu.
	if !mobile && workspace_input.mouse.pressed[.Right] {
		for i in 0 ..< render_count {
			pid := pane_ids[i]
			pidx := int(pid) - 1
			if pidx < 0 || pidx >= PANE_MAX do continue
			pane_rect := pane_rects[pidx]
			if ui.rect_contains(pane_rect, workspace_pointer.pos) {
				state.cell_context_menu = ui.Context_Menu_State{
					open = true,
					pos  = workspace_pointer.pos,
				}
				state.cell_context_cell_idx = i
				break
			}
		}
	}

	build_cell_context_menu(state, workspace_pointer, viewport_w, viewport_h)
}

// ---------------------------------------------------------------------------
// Split Edge Resize
// ---------------------------------------------------------------------------

// Detect and handle drag-resize on split edges.
@(private = "package")
update_split_resize :: proc(
	state: ^App_State,
	ws: ^Workspace,
	node_bounds: [TREE_NODE_MAX]ui.Rect,
	pointer: ui.Pointer_Input,
) {
	RESIZE_HIT :: f32(6)

	if ws.resize.active_node >= 0 {
		// Active resize drag.
		if pointer.left_down {
			ni := ws.resize.active_node
			node := ws.tree.nodes[ni]
			bounds := node_bounds[ni]

			if node.kind == .Split_H && bounds.size.x > 0 {
				new_ratio := (pointer.pos.x - bounds.pos.x) / bounds.size.x
				tree_set_ratio(&ws.tree, ni, new_ratio)
			} else if node.kind == .Split_V && bounds.size.y > 0 {
				new_ratio := (pointer.pos.y - bounds.pos.y) / bounds.size.y
				tree_set_ratio(&ws.tree, ni, new_ratio)
			}
		} else {
			// Released — end resize.
			ws.resize.active_node = -1
			// TODO: persist tree ratios to settings.
		}
	} else {
		// Detect hover on split edges.
		for ni in 0 ..< int(ws.tree.count) {
			node := ws.tree.nodes[ni]
			if node.kind != .Split_H && node.kind != .Split_V do continue

			bounds := node_bounds[ni]
			if bounds.size.x <= 0 || bounds.size.y <= 0 do continue

			r := clamp(node.ratio, node.min_size, 1.0 - node.min_size)

			hit: ui.Rect
			indicator: ui.Rect

			if node.kind == .Split_H {
				edge_x := bounds.pos.x + bounds.size.x * r
				hit = ui.Rect{
					pos  = {edge_x - RESIZE_HIT * 0.5, bounds.pos.y},
					size = {RESIZE_HIT, bounds.size.y},
				}
				indicator = ui.Rect{
					pos  = {edge_x - 1, bounds.pos.y},
					size = {2, bounds.size.y},
				}
			} else {
				edge_y := bounds.pos.y + bounds.size.y * r
				hit = ui.Rect{
					pos  = {bounds.pos.x, edge_y - RESIZE_HIT * 0.5},
					size = {bounds.size.x, RESIZE_HIT},
				}
				indicator = ui.Rect{
					pos  = {bounds.pos.x, edge_y - 1},
					size = {bounds.size.x, 2},
				}
			}

			if ui.rect_contains(hit, pointer.pos) {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect  = indicator,
					color = ui.with_alpha(ui.COL_BLUE, 0.35),
				})
				if pointer.left_pressed {
					ws.resize.active_node = i8(ni)
					ws.resize.start_ratio = node.ratio
					ws.resize.start_pos = node.kind == .Split_H ? pointer.pos.x : pointer.pos.y
				}
				break
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Workspace ↔ Entity_World Sync
// ---------------------------------------------------------------------------

// Rebuild the active workspace tree from Entity_World state.
// Called after layout restoration or cell mutations that change world.count.
workspace_sync_from_world :: proc(state: ^App_State) {
	ws := workspace_registry_active(&state.ws_registry)
	if ws == nil do return

	n := state.world.count
	if n <= 0 {
		ws.tree = {}
		ws.tree.root = -1
		ws.pane_pool = {}
		return
	}

	// Reset pane pool for rebuild.
	ws.pane_pool = {}

	// Collect widget kinds from Entity_World.
	kinds: [PANE_MAX]Widget_Kind
	count := min(n, PANE_MAX)
	for i in 0 ..< count {
		kinds[i] = state.world.widgets[i].kind
	}

	// For 7 panels (default layout), use the purpose-built tree factory
	// to get a good visual layout matching the legacy grid proportions.
	if count == 7 {
		ws.tree, _ = build_default_workspace_tree(ws)
	} else {
		ws.tree = build_auto_workspace_tree(ws, kinds[:count])
	}

	// S112: Sync Entity_World state → pane-local state for all panes.
	workspace_sync_panes_from_world(state)
}

// S112: Copy Entity_World component data into corresponding pane fields.
// Called after workspace tree rebuild or layout restore to ensure panes
// own their data context (binding, TF, indicators, chart, analytics).
workspace_sync_panes_from_world :: proc(state: ^App_State) {
	ws := workspace_registry_active(&state.ws_registry)
	if ws == nil do return

	pane_ids, pane_count := tree_collect_pane_ids(&ws.tree)
	count := min(pane_count, state.world.count)

	for i in 0 ..< count {
		pane := pane_pool_get(&ws.pane_pool, pane_ids[i])
		if pane == nil do continue

		// S119: Infer role from widget kind during sync.
		pane.role = infer_pane_role(state.world.widgets[i].kind)

		// Binding: copy from Entity_World → pane.
		pane.binding = state.world.bindings[i]

		// TF override: Entity_World timeframes → pane.tf_override.
		// Entity_World uses -1 for "follow global". Value 0+ is a valid per-cell TF.
		// Only set pane.tf_override if Entity_World has an explicit per-cell TF.
		tf := state.world.timeframes[i].tf_idx
		pane.tf_override = i8(tf) if tf >= 0 else -1

		// Indicators, chart, subplots, analytics: Entity_World → pane.
		pane.indicators = state.world.indicators[i]
		pane.ind_params = state.world.ind_params[i]
		pane.chart = state.world.charts[i]
		pane.subplots = state.world.subplots[i]
		pane.analytics = state.world.analytics[i]
	}
}

// S112: Sync a single pane's data to its Entity_World cell.
// Called after pane-local mutations to keep Entity_World consistent
// for legacy paths (persistence, compare mode, reconciliation).
workspace_sync_pane_to_world :: proc(state: ^App_State, pane: ^Pane, cell_idx: int) {
	if state == nil || pane == nil do return
	if cell_idx < 0 || cell_idx >= state.world.count do return

	state.world.bindings[cell_idx] = pane.binding
	state.world.timeframes[cell_idx].tf_idx = int(pane.tf_override) if pane.tf_override >= 0 else -1
	state.world.indicators[cell_idx] = pane.indicators
	state.world.ind_params[cell_idx] = pane.ind_params
	state.world.charts[cell_idx] = pane.chart
	state.world.subplots[cell_idx] = pane.subplots
	state.world.analytics[cell_idx] = pane.analytics
}

// Sync tree after a cell is added to Entity_World.
// Splits the last pane to accommodate the new cell.
workspace_on_cell_added :: proc(state: ^App_State, ci: int, widget: Widget_Kind) {
	ws := workspace_registry_active(&state.ws_registry)
	if ws == nil do return

	// Find the pane at DFS position ci-1 (the last existing pane) to split.
	pane_ids, pane_count := tree_collect_pane_ids(&ws.tree)
	if pane_count == 0 || ci == 0 {
		// First cell or no tree — full rebuild.
		workspace_sync_from_world(state)
		return
	}

	// Split the last pane to make room for the new cell.
	target_idx := min(ci - 1, pane_count - 1)
	target_id := pane_ids[target_idx]

	new_id := tree_split_pane(&ws.tree, &ws.pane_pool, target_id, .Split_H, widget)
	if new_id == PANE_ID_NONE {
		// Split failed (tree full) — full rebuild.
		workspace_sync_from_world(state)
	}
}

// Sync tree after a cell is removed from Entity_World.
workspace_on_cell_removed :: proc(state: ^App_State, ci: int) {
	ws := workspace_registry_active(&state.ws_registry)
	if ws == nil do return

	// Find the pane at DFS position ci and remove it.
	pane_ids, pane_count := tree_collect_pane_ids(&ws.tree)
	if ci < 0 || ci >= pane_count {
		workspace_sync_from_world(state)
		return
	}

	target_id := pane_ids[ci]
	ok := tree_remove_pane(&ws.tree, target_id)
	if !ok {
		workspace_sync_from_world(state)
		return
	}

	// Free the pane from the pool.
	pane_pool_free(&ws.pane_pool, target_id)
}
