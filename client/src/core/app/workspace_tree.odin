package app

// S105: Tree factory functions — replace grid preset builders with tree constructors.
// Each factory produces a Split_Tree + allocates panes in the workspace pool.

import "mr:ui"

// ---------------------------------------------------------------------------
// Tree Factories (ADR-0025 §4)
// ---------------------------------------------------------------------------

// Default 7-panel layout matching build_default_grid proportions:
//   V root
//   ├── Candle (full width, 40%)
//   ├── H(Stats, Counter) (18%)
//   ├── H(Heatmap, VPVR) (22%)
//   └── H(Trades, Orderbook) (20%)
//
// Returns the tree and an array mapping panel index → Pane_ID.
build_default_workspace_tree :: proc(ws: ^Workspace) -> (tree: Split_Tree, pane_ids: [ui.PANEL_COUNT]Pane_ID) {
	tree.root = -1

	// Allocate panes for all 7 panels.
	PANEL_KINDS := [ui.PANEL_COUNT]Widget_Kind{
		.Candle, .Stats, .Counter, .Heatmap, .VPVR, .Trades, .Orderbook,
	}
	for i in 0 ..< ui.PANEL_COUNT {
		pane_ids[i] = workspace_alloc_pane(ws, PANEL_KINDS[i])
	}

	// Build tree: V(candle, V(H(stats,counter), V(H(heatmap,vpvr), H(trades,ob))))
	// This produces the same 4-row proportions as the grid.

	// Leaves.
	candle_leaf  := tree_add_pane_leaf(&tree, -1, pane_ids[ui.PANEL_CANDLE])
	stats_leaf   := tree_add_pane_leaf(&tree, -1, pane_ids[ui.PANEL_STATS])
	counter_leaf := tree_add_pane_leaf(&tree, -1, pane_ids[ui.PANEL_COUNTER])
	heatmap_leaf := tree_add_pane_leaf(&tree, -1, pane_ids[ui.PANEL_HEATMAP])
	vpvr_leaf    := tree_add_pane_leaf(&tree, -1, pane_ids[ui.PANEL_VPVR])
	trades_leaf  := tree_add_pane_leaf(&tree, -1, pane_ids[ui.PANEL_TRADES])
	ob_leaf      := tree_add_pane_leaf(&tree, -1, pane_ids[ui.PANEL_ORDERBOOK])

	// H splits for paired panels (all 50/50).
	stats_counter := tree_add_node(&tree, .Split_H, -1, 0.5, 0.08)
	tree_set_children(&tree, stats_counter, stats_leaf, counter_leaf)

	heatmap_vpvr := tree_add_node(&tree, .Split_H, -1, 0.5, 0.08)
	tree_set_children(&tree, heatmap_vpvr, heatmap_leaf, vpvr_leaf)

	trades_ob := tree_add_node(&tree, .Split_H, -1, 0.5, 0.08)
	tree_set_children(&tree, trades_ob, trades_leaf, ob_leaf)

	// V splits for rows. Bottom-up nesting to express 4 rows:
	// row2_3 = V(heatmap_vpvr @ 0.22/(0.22+0.20)=0.524, trades_ob)
	row2_3 := tree_add_node(&tree, .Split_V, -1, 0.524, 0.08)
	tree_set_children(&tree, row2_3, heatmap_vpvr, trades_ob)

	// row1_3 = V(stats_counter @ 0.18/(0.18+0.22+0.20)=0.30, row2_3)
	row1_3 := tree_add_node(&tree, .Split_V, -1, 0.30, 0.08)
	tree_set_children(&tree, row1_3, stats_counter, row2_3)

	// S113: root = V(candle @ 0.50, row1_3) — chart-dominant layout.
	root := tree_add_node(&tree, .Split_V, -1, 0.50, 0.08)
	tree_set_children(&tree, root, candle_leaf, row1_3)

	tree.root = root
	return tree, pane_ids
}

// Chart Focus layout: Candle 70% top, H(Trades, Orderbook) 30% bottom.
build_chart_focus_workspace_tree :: proc(ws: ^Workspace) -> (tree: Split_Tree, pane_ids: [3]Pane_ID) {
	tree.root = -1

	pane_ids[0] = workspace_alloc_pane(ws, .Candle)
	pane_ids[1] = workspace_alloc_pane(ws, .Trades)
	pane_ids[2] = workspace_alloc_pane(ws, .Orderbook)

	candle_leaf := tree_add_pane_leaf(&tree, -1, pane_ids[0])
	trades_leaf := tree_add_pane_leaf(&tree, -1, pane_ids[1])
	ob_leaf     := tree_add_pane_leaf(&tree, -1, pane_ids[2])

	bottom := tree_add_node(&tree, .Split_H, -1, 0.5, 0.08)
	tree_set_children(&tree, bottom, trades_leaf, ob_leaf)

	root := tree_add_node(&tree, .Split_V, -1, 0.70, 0.08)
	tree_set_children(&tree, root, candle_leaf, bottom)

	tree.root = root
	return tree, pane_ids
}

// Compact layout: Candle 65% left, Orderbook 35% right.
build_compact_workspace_tree :: proc(ws: ^Workspace) -> (tree: Split_Tree, pane_ids: [2]Pane_ID) {
	tree.root = -1

	pane_ids[0] = workspace_alloc_pane(ws, .Candle)
	pane_ids[1] = workspace_alloc_pane(ws, .Orderbook)

	candle_leaf := tree_add_pane_leaf(&tree, -1, pane_ids[0])
	ob_leaf     := tree_add_pane_leaf(&tree, -1, pane_ids[1])

	root := tree_add_node(&tree, .Split_H, -1, 0.65, 0.08)
	tree_set_children(&tree, root, candle_leaf, ob_leaf)

	tree.root = root
	return tree, pane_ids
}

// Focus mode: Candle 75% left, Orderbook 25% right.
build_focus_workspace_tree :: proc(ws: ^Workspace) -> (tree: Split_Tree, pane_ids: [2]Pane_ID) {
	tree.root = -1

	pane_ids[0] = workspace_alloc_pane(ws, .Candle)
	pane_ids[1] = workspace_alloc_pane(ws, .Orderbook)

	candle_leaf := tree_add_pane_leaf(&tree, -1, pane_ids[0])
	ob_leaf     := tree_add_pane_leaf(&tree, -1, pane_ids[1])

	root := tree_add_node(&tree, .Split_H, -1, 0.75, 0.08)
	tree_set_children(&tree, root, candle_leaf, ob_leaf)

	tree.root = root
	return tree, pane_ids
}

// Compare mode: N equal horizontal panes (1-4).
// For N=4, uses a 2x2 grid: V(H(p0,p1), H(p2,p3)).
build_compare_workspace_tree :: proc(ws: ^Workspace, count: int, kind: Widget_Kind) -> (tree: Split_Tree, pane_ids: [4]Pane_ID) {
	tree.root = -1
	n := clamp(count, 1, 4)

	for i in 0 ..< n {
		pane_ids[i] = workspace_alloc_pane(ws, kind)
	}

	if n == 1 {
		leaf := tree_add_pane_leaf(&tree, -1, pane_ids[0])
		tree.root = leaf
		return tree, pane_ids
	}

	if n == 2 {
		l := tree_add_pane_leaf(&tree, -1, pane_ids[0])
		r := tree_add_pane_leaf(&tree, -1, pane_ids[1])
		root := tree_add_node(&tree, .Split_H, -1, 0.5, 0.08)
		tree_set_children(&tree, root, l, r)
		tree.root = root
		return tree, pane_ids
	}

	if n == 3 {
		// H(p0, H(p1, p2)) — each ~33%
		l := tree_add_pane_leaf(&tree, -1, pane_ids[0])
		m := tree_add_pane_leaf(&tree, -1, pane_ids[1])
		r := tree_add_pane_leaf(&tree, -1, pane_ids[2])
		right_split := tree_add_node(&tree, .Split_H, -1, 0.5, 0.08)
		tree_set_children(&tree, right_split, m, r)
		root := tree_add_node(&tree, .Split_H, -1, 0.333, 0.08)
		tree_set_children(&tree, root, l, right_split)
		tree.root = root
		return tree, pane_ids
	}

	// n == 4: V(H(p0,p1), H(p2,p3))
	p0 := tree_add_pane_leaf(&tree, -1, pane_ids[0])
	p1 := tree_add_pane_leaf(&tree, -1, pane_ids[1])
	p2 := tree_add_pane_leaf(&tree, -1, pane_ids[2])
	p3 := tree_add_pane_leaf(&tree, -1, pane_ids[3])

	top := tree_add_node(&tree, .Split_H, -1, 0.5, 0.08)
	tree_set_children(&tree, top, p0, p1)

	bot := tree_add_node(&tree, .Split_H, -1, 0.5, 0.08)
	tree_set_children(&tree, bot, p2, p3)

	root := tree_add_node(&tree, .Split_V, -1, 0.5, 0.08)
	tree_set_children(&tree, root, top, bot)

	tree.root = root
	return tree, pane_ids
}

// Analysis layout: V(Candle 50%, H(Heatmap,VPVR) 25%, H(Trades,OB) 25%).
build_analysis_workspace_tree :: proc(ws: ^Workspace) -> (tree: Split_Tree, pane_ids: [5]Pane_ID) {
	tree.root = -1

	pane_ids[0] = workspace_alloc_pane(ws, .Candle)
	pane_ids[1] = workspace_alloc_pane(ws, .Heatmap)
	pane_ids[2] = workspace_alloc_pane(ws, .VPVR)
	pane_ids[3] = workspace_alloc_pane(ws, .Trades)
	pane_ids[4] = workspace_alloc_pane(ws, .Orderbook)

	candle_leaf  := tree_add_pane_leaf(&tree, -1, pane_ids[0])
	heatmap_leaf := tree_add_pane_leaf(&tree, -1, pane_ids[1])
	vpvr_leaf    := tree_add_pane_leaf(&tree, -1, pane_ids[2])
	trades_leaf  := tree_add_pane_leaf(&tree, -1, pane_ids[3])
	ob_leaf      := tree_add_pane_leaf(&tree, -1, pane_ids[4])

	heatmap_vpvr := tree_add_node(&tree, .Split_H, -1, 0.5, 0.08)
	tree_set_children(&tree, heatmap_vpvr, heatmap_leaf, vpvr_leaf)

	trades_ob := tree_add_node(&tree, .Split_H, -1, 0.5, 0.08)
	tree_set_children(&tree, trades_ob, trades_leaf, ob_leaf)

	// bottom = V(heatmap_vpvr @ 0.5, trades_ob)
	bottom := tree_add_node(&tree, .Split_V, -1, 0.5, 0.08)
	tree_set_children(&tree, bottom, heatmap_vpvr, trades_ob)

	// root = V(candle @ 0.50, bottom)
	root := tree_add_node(&tree, .Split_V, -1, 0.50, 0.08)
	tree_set_children(&tree, root, candle_leaf, bottom)

	tree.root = root
	return tree, pane_ids
}

// S119: Workstation layout — 1 primary candle chart + 2 auxiliary panes.
// Context stack provides OB/Counter/DOM/Analytics without separate panes.
// Produces: H(Candle @ 0.70, V(Stats @ 0.5, Trades))
build_workstation_workspace_tree :: proc(ws: ^Workspace) -> (tree: Split_Tree, pane_ids: [3]Pane_ID) {
	tree.root = -1

	pane_ids[0] = workspace_alloc_pane(ws, .Candle)   // Primary_Chart (auto via infer)
	pane_ids[1] = workspace_alloc_pane(ws, .Stats)     // Auxiliary
	pane_ids[2] = workspace_alloc_pane(ws, .Trades)    // Auxiliary

	candle_leaf := tree_add_pane_leaf(&tree, -1, pane_ids[0])
	stats_leaf  := tree_add_pane_leaf(&tree, -1, pane_ids[1])
	trades_leaf := tree_add_pane_leaf(&tree, -1, pane_ids[2])

	right := tree_add_node(&tree, .Split_V, -1, 0.5, 0.08)
	tree_set_children(&tree, right, stats_leaf, trades_leaf)

	root := tree_add_node(&tree, .Split_H, -1, 0.70, 0.08)
	tree_set_children(&tree, root, candle_leaf, right)

	tree.root = root
	return tree, pane_ids
}

// ---------------------------------------------------------------------------
// Tree Validation
// ---------------------------------------------------------------------------

// Validate tree structural invariants. Returns true if valid.
tree_validate :: proc(tree: ^Split_Tree) -> bool {
	if tree.count == 0 do return tree.root == -1
	if tree.root < 0 || int(tree.root) >= int(tree.count) do return false

	// Root must have parent = -1.
	if tree.nodes[tree.root].parent != -1 do return false

	// Every split must have two valid children.
	for i in 0 ..< int(tree.count) {
		node := tree.nodes[i]
		switch node.kind {
		case .Split_H, .Split_V:
			if node.children[0] < 0 || int(node.children[0]) >= int(tree.count) do return false
			if node.children[1] < 0 || int(node.children[1]) >= int(tree.count) do return false
			if node.ratio <= 0 || node.ratio >= 1.0 do return false
		case .Pane:
			// children[0] stores pane_id — must be valid (> 0).
			if node.children[0] <= 0 do return false
		case .Stack:
			// Stack leaf — children[0] = active pane_id.
			if node.children[0] <= 0 do return false
		}
	}

	return true
}

// Count leaf (pane) nodes in the tree.
tree_pane_count :: proc(tree: ^Split_Tree) -> int {
	count := 0
	for i in 0 ..< int(tree.count) {
		if tree.nodes[i].kind == .Pane || tree.nodes[i].kind == .Stack {
			count += 1
		}
	}
	return count
}
