package app

import "core:testing"
import "mr:services"
import "mr:ui"

// S105/S109: Workspace architecture tests.
// Covers: Split_Tree layout resolution, tree factories, pane pool, tree mutations, validation,
// S109 contract-based dashboard migration.

// ---------------------------------------------------------------------------
// Pane Pool
// ---------------------------------------------------------------------------

@(test)
test_pane_pool_alloc_returns_unique_ids :: proc(t: ^testing.T) {
	pool: Pane_Pool
	ids: [PANE_MAX]Pane_ID

	for i in 0 ..< PANE_MAX {
		pane, id := pane_pool_alloc(&pool)
		testing.expect(t, pane != nil, "alloc should succeed")
		testing.expect(t, id != PANE_ID_NONE, "id should be non-zero")
		ids[i] = id
	}
	testing.expect_value(t, int(pool.count), PANE_MAX)

	// All IDs unique.
	for i in 0 ..< PANE_MAX {
		for j in i + 1 ..< PANE_MAX {
			testing.expect(t, ids[i] != ids[j], "ids must be unique")
		}
	}

	// Pool full — next alloc fails.
	pane, id := pane_pool_alloc(&pool)
	testing.expect(t, pane == nil, "alloc should fail when full")
	testing.expect(t, id == PANE_ID_NONE, "id should be NONE when full")
}

@(test)
test_pane_pool_free_and_realloc :: proc(t: ^testing.T) {
	pool: Pane_Pool
	_, id1 := pane_pool_alloc(&pool)
	_, id2 := pane_pool_alloc(&pool)
	testing.expect_value(t, int(pool.count), 2)

	pane_pool_free(&pool, id1)
	testing.expect_value(t, int(pool.count), 1)

	// id1's slot is reusable but gets a new ID.
	pane, id3 := pane_pool_alloc(&pool)
	testing.expect(t, pane != nil, "realloc should succeed")
	testing.expect(t, id3 != id1, "reused slot gets new ID")
	testing.expect(t, id3 != id2, "new ID differs from id2")
	testing.expect_value(t, int(pool.count), 2)
}

@(test)
test_pane_pool_get :: proc(t: ^testing.T) {
	pool: Pane_Pool
	_, id := pane_pool_alloc(&pool)

	pane := pane_pool_get(&pool, id)
	testing.expect(t, pane != nil, "get should find alive pane")
	testing.expect_value(t, pane.id, id)

	pane_pool_free(&pool, id)
	pane2 := pane_pool_get(&pool, id)
	testing.expect(t, pane2 == nil, "get should return nil after free")
}

@(test)
test_pane_pool_get_none :: proc(t: ^testing.T) {
	pool: Pane_Pool
	pane := pane_pool_get(&pool, PANE_ID_NONE)
	testing.expect(t, pane == nil, "get PANE_ID_NONE returns nil")
}

@(test)
test_pane_defaults :: proc(t: ^testing.T) {
	pool: Pane_Pool
	pane, _ := pane_pool_alloc(&pool)
	testing.expect(t, pane != nil, "alloc succeeds")
	testing.expect_value(t, pane.tf_override, i8(-1))
	testing.expect_value(t, pane.view.zoom_level, f32(1.0))
	testing.expect_value(t, pane.alive, true)
}

// ---------------------------------------------------------------------------
// Tree Builder Helpers
// ---------------------------------------------------------------------------

@(test)
test_tree_add_node_respects_capacity :: proc(t: ^testing.T) {
	tree: Split_Tree
	tree.root = -1

	for i in 0 ..< TREE_NODE_MAX {
		idx := tree_add_node(&tree, .Pane, -1, 0, 0.05)
		testing.expect(t, idx >= 0, "add should succeed within capacity")
	}
	// Now full.
	idx := tree_add_node(&tree, .Pane, -1, 0, 0.05)
	testing.expect_value(t, idx, i8(-1))
}

@(test)
test_tree_set_children_updates_parents :: proc(t: ^testing.T) {
	tree: Split_Tree
	tree.root = -1

	parent := tree_add_node(&tree, .Split_H, -1, 0.5, 0.08)
	left := tree_add_node(&tree, .Pane, -1, 0, 0.05)
	right := tree_add_node(&tree, .Pane, -1, 0, 0.05)
	tree.nodes[left].children[0] = 1  // dummy pane_id
	tree.nodes[right].children[0] = 2

	tree_set_children(&tree, parent, left, right)

	testing.expect_value(t, tree.nodes[left].parent, parent)
	testing.expect_value(t, tree.nodes[right].parent, parent)
	testing.expect_value(t, tree.nodes[parent].children[0], left)
	testing.expect_value(t, tree.nodes[parent].children[1], right)
}

// ---------------------------------------------------------------------------
// Layout Resolution
// ---------------------------------------------------------------------------

@(test)
test_resolve_empty_tree :: proc(t: ^testing.T) {
	tree: Split_Tree
	tree.root = -1
	bounds := ui.Rect{pos = {0, 0}, size = {1000, 800}}
	result := resolve_tree_layout(&tree, bounds)

	// All rects should be zero.
	for i in 0 ..< PANE_MAX {
		testing.expect_value(t, result[i].size.x, f32(0))
		testing.expect_value(t, result[i].size.y, f32(0))
	}
}

@(test)
test_resolve_single_pane :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	pid := workspace_alloc_pane(&ws, .Candle)
	tree: Split_Tree
	tree.root = -1
	leaf := tree_add_pane_leaf(&tree, -1, pid)
	tree.root = leaf

	bounds := ui.Rect{pos = {10, 20}, size = {800, 600}}
	result := resolve_tree_layout(&tree, bounds)

	// Pane_ID is 1-based, stored at index pid-1.
	idx := int(pid) - 1
	testing.expect_value(t, result[idx].pos.x, f32(10))
	testing.expect_value(t, result[idx].pos.y, f32(20))
	testing.expect_value(t, result[idx].size.x, f32(800))
	testing.expect_value(t, result[idx].size.y, f32(600))
}

@(test)
test_resolve_horizontal_split :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	pid_l := workspace_alloc_pane(&ws, .Candle)
	pid_r := workspace_alloc_pane(&ws, .Orderbook)

	tree: Split_Tree
	tree.root = -1
	l := tree_add_pane_leaf(&tree, -1, pid_l)
	r := tree_add_pane_leaf(&tree, -1, pid_r)
	root := tree_add_node(&tree, .Split_H, -1, 0.6, 0.05)
	tree_set_children(&tree, root, l, r)
	tree.root = root

	bounds := ui.Rect{pos = {0, 0}, size = {1000, 500}}
	result := resolve_tree_layout(&tree, bounds)

	li := int(pid_l) - 1
	ri := int(pid_r) - 1

	// Left: x=0, w=600
	assert_f32_near(t, result[li].pos.x, 0, 0.1)
	assert_f32_near(t, result[li].size.x, 600, 0.1)
	assert_f32_near(t, result[li].size.y, 500, 0.1)

	// Right: x=600, w=400
	assert_f32_near(t, result[ri].pos.x, 600, 0.1)
	assert_f32_near(t, result[ri].size.x, 400, 0.1)
	assert_f32_near(t, result[ri].size.y, 500, 0.1)
}

@(test)
test_resolve_vertical_split :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	pid_t := workspace_alloc_pane(&ws, .Candle)
	pid_b := workspace_alloc_pane(&ws, .Trades)

	tree: Split_Tree
	tree.root = -1
	top := tree_add_pane_leaf(&tree, -1, pid_t)
	bot := tree_add_pane_leaf(&tree, -1, pid_b)
	root := tree_add_node(&tree, .Split_V, -1, 0.7, 0.05)
	tree_set_children(&tree, root, top, bot)
	tree.root = root

	bounds := ui.Rect{pos = {0, 0}, size = {800, 1000}}
	result := resolve_tree_layout(&tree, bounds)

	ti := int(pid_t) - 1
	bi := int(pid_b) - 1

	// Top: y=0, h=700
	assert_f32_near(t, result[ti].pos.y, 0, 0.1)
	assert_f32_near(t, result[ti].size.y, 700, 0.1)

	// Bottom: y=700, h=300
	assert_f32_near(t, result[bi].pos.y, 700, 0.1)
	assert_f32_near(t, result[bi].size.y, 300, 0.1)
}

@(test)
test_resolve_nested_splits :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	// H(candle @ 0.5, V(trades @ 0.5, ob))
	pid_c := workspace_alloc_pane(&ws, .Candle)
	pid_t := workspace_alloc_pane(&ws, .Trades)
	pid_o := workspace_alloc_pane(&ws, .Orderbook)

	tree: Split_Tree
	tree.root = -1
	cl := tree_add_pane_leaf(&tree, -1, pid_c)
	tl := tree_add_pane_leaf(&tree, -1, pid_t)
	ol := tree_add_pane_leaf(&tree, -1, pid_o)

	right := tree_add_node(&tree, .Split_V, -1, 0.5, 0.05)
	tree_set_children(&tree, right, tl, ol)

	root := tree_add_node(&tree, .Split_H, -1, 0.5, 0.05)
	tree_set_children(&tree, root, cl, right)
	tree.root = root

	bounds := ui.Rect{pos = {0, 0}, size = {1000, 800}}
	result := resolve_tree_layout(&tree, bounds)

	ci := int(pid_c) - 1
	ti := int(pid_t) - 1
	oi := int(pid_o) - 1

	// Candle: left half (0,0) 500x800
	assert_f32_near(t, result[ci].size.x, 500, 0.1)
	assert_f32_near(t, result[ci].size.y, 800, 0.1)

	// Trades: right-top (500,0) 500x400
	assert_f32_near(t, result[ti].pos.x, 500, 0.1)
	assert_f32_near(t, result[ti].size.x, 500, 0.1)
	assert_f32_near(t, result[ti].size.y, 400, 0.1)

	// Orderbook: right-bottom (500,400) 500x400
	assert_f32_near(t, result[oi].pos.x, 500, 0.1)
	assert_f32_near(t, result[oi].pos.y, 400, 0.1)
	assert_f32_near(t, result[oi].size.x, 500, 0.1)
	assert_f32_near(t, result[oi].size.y, 400, 0.1)
}

// ---------------------------------------------------------------------------
// Tree Factories
// ---------------------------------------------------------------------------

@(test)
test_default_tree_has_7_panes :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_default_workspace_tree(&ws)

	testing.expect_value(t, tree_pane_count(&tree), ui.PANEL_COUNT)
	testing.expect(t, tree_validate(&tree), "tree should be valid")

	for i in 0 ..< ui.PANEL_COUNT {
		testing.expect(t, pane_ids[i] != PANE_ID_NONE, "pane ID should be allocated")
	}
}

@(test)
test_default_tree_layout_covers_bounds :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_default_workspace_tree(&ws)
	bounds := ui.Rect{pos = {0, 0}, size = {1200, 900}}
	result := resolve_tree_layout(&tree, bounds)

	// Candle pane should span full width.
	ci := int(pane_ids[ui.PANEL_CANDLE]) - 1
	assert_f32_near(t, result[ci].size.x, 1200, 0.1)
	testing.expect(t, result[ci].size.y > 100, "candle should have significant height")

	// All panes should have non-zero area.
	for i in 0 ..< ui.PANEL_COUNT {
		idx := int(pane_ids[i]) - 1
		if idx >= 0 && idx < PANE_MAX {
			testing.expect(t, result[idx].size.x > 0, "pane width > 0")
			testing.expect(t, result[idx].size.y > 0, "pane height > 0")
		}
	}
}

@(test)
test_chart_focus_tree :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_chart_focus_workspace_tree(&ws)
	testing.expect_value(t, tree_pane_count(&tree), 3)
	testing.expect(t, tree_validate(&tree), "tree should be valid")

	bounds := ui.Rect{pos = {0, 0}, size = {1000, 1000}}
	result := resolve_tree_layout(&tree, bounds)

	candle_idx := int(pane_ids[0]) - 1
	// Candle should be ~70% height.
	assert_f32_near(t, result[candle_idx].size.y, 700, 1)
	assert_f32_near(t, result[candle_idx].size.x, 1000, 0.1)
}

@(test)
test_compact_tree :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_compact_workspace_tree(&ws)
	testing.expect_value(t, tree_pane_count(&tree), 2)
	testing.expect(t, tree_validate(&tree), "tree should be valid")

	bounds := ui.Rect{pos = {0, 0}, size = {1000, 800}}
	result := resolve_tree_layout(&tree, bounds)

	candle_idx := int(pane_ids[0]) - 1
	ob_idx := int(pane_ids[1]) - 1

	assert_f32_near(t, result[candle_idx].size.x, 650, 1)
	assert_f32_near(t, result[ob_idx].size.x, 350, 1)
}

@(test)
test_focus_tree :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_focus_workspace_tree(&ws)
	testing.expect_value(t, tree_pane_count(&tree), 2)
	testing.expect(t, tree_validate(&tree), "tree should be valid")

	bounds := ui.Rect{pos = {0, 0}, size = {1000, 800}}
	result := resolve_tree_layout(&tree, bounds)

	candle_idx := int(pane_ids[0]) - 1
	assert_f32_near(t, result[candle_idx].size.x, 750, 1)
}

@(test)
test_compare_tree_2_panes :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_compare_workspace_tree(&ws, 2, .Candle)
	testing.expect_value(t, tree_pane_count(&tree), 2)
	testing.expect(t, tree_validate(&tree), "tree should be valid")

	bounds := ui.Rect{pos = {0, 0}, size = {1000, 800}}
	result := resolve_tree_layout(&tree, bounds)

	p0 := int(pane_ids[0]) - 1
	p1 := int(pane_ids[1]) - 1

	assert_f32_near(t, result[p0].size.x, 500, 1)
	assert_f32_near(t, result[p1].size.x, 500, 1)
}

@(test)
test_compare_tree_4_panes :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_compare_workspace_tree(&ws, 4, .Orderbook)
	testing.expect_value(t, tree_pane_count(&tree), 4)
	testing.expect(t, tree_validate(&tree), "tree should be valid")

	bounds := ui.Rect{pos = {0, 0}, size = {1000, 800}}
	result := resolve_tree_layout(&tree, bounds)

	// 2x2 grid: each pane ~500x400.
	for i in 0 ..< 4 {
		idx := int(pane_ids[i]) - 1
		if idx >= 0 && idx < PANE_MAX {
			assert_f32_near(t, result[idx].size.x, 500, 1)
			assert_f32_near(t, result[idx].size.y, 400, 1)
		}
	}
}

@(test)
test_analysis_tree :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_analysis_workspace_tree(&ws)
	testing.expect_value(t, tree_pane_count(&tree), 5)
	testing.expect(t, tree_validate(&tree), "tree should be valid")

	bounds := ui.Rect{pos = {0, 0}, size = {1000, 1000}}
	result := resolve_tree_layout(&tree, bounds)

	// Candle should be ~50% height = 500.
	candle_idx := int(pane_ids[0]) - 1
	assert_f32_near(t, result[candle_idx].size.y, 500, 1)
	assert_f32_near(t, result[candle_idx].size.x, 1000, 0.1)
}

// ---------------------------------------------------------------------------
// Tree Mutations
// ---------------------------------------------------------------------------

@(test)
test_tree_swap_panes :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_compact_workspace_tree(&ws)
	pid_a := pane_ids[0]
	pid_b := pane_ids[1]

	tree_swap_panes(&tree, pid_a, pid_b)

	// After swap, resolve should flip positions.
	bounds := ui.Rect{pos = {0, 0}, size = {1000, 800}}
	result := resolve_tree_layout(&tree, bounds)

	// pid_a was left (65%), after swap it's right (35%).
	a_idx := int(pid_a) - 1
	assert_f32_near(t, result[a_idx].size.x, 350, 1)

	b_idx := int(pid_b) - 1
	assert_f32_near(t, result[b_idx].size.x, 650, 1)
}

@(test)
test_tree_set_ratio :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, _ := build_compact_workspace_tree(&ws)

	// Root is the last node (Split_H @ 0.65).
	tree_set_ratio(&tree, tree.root, 0.5)
	testing.expect_value(t, tree.nodes[tree.root].ratio, f32(0.5))

	// Clamping: too small.
	tree_set_ratio(&tree, tree.root, 0.01)
	testing.expect(t, tree.nodes[tree.root].ratio >= 0.08, "ratio clamped to min_size")
}

@(test)
test_tree_rotate :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, _ := build_compact_workspace_tree(&ws)
	testing.expect_value(t, tree.nodes[tree.root].kind, Split_Node_Kind.Split_H)

	tree_rotate(&tree, tree.root)
	testing.expect_value(t, tree.nodes[tree.root].kind, Split_Node_Kind.Split_V)

	tree_rotate(&tree, tree.root)
	testing.expect_value(t, tree.nodes[tree.root].kind, Split_Node_Kind.Split_H)
}

@(test)
test_tree_remove_pane :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_compact_workspace_tree(&ws)
	// Remove orderbook — candle should become root.
	ok := tree_remove_pane(&tree, pane_ids[1])
	testing.expect(t, ok, "remove should succeed")

	// Root should now be the candle leaf.
	testing.expect_value(t, tree.nodes[tree.root].kind, Split_Node_Kind.Pane)
	testing.expect_value(t, tree_leaf_pane_id(&tree, tree.root), pane_ids[0])
}

@(test)
test_tree_remove_root_pane :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	pid := workspace_alloc_pane(&ws, .Candle)
	tree: Split_Tree
	tree.root = -1
	leaf := tree_add_pane_leaf(&tree, -1, pid)
	tree.root = leaf

	ok := tree_remove_pane(&tree, pid)
	testing.expect(t, ok, "remove root pane should succeed")
	testing.expect_value(t, tree.root, i8(-1))
}

// ---------------------------------------------------------------------------
// Widget Host
// ---------------------------------------------------------------------------

@(test)
test_widget_host_create :: proc(t: ^testing.T) {
	host := widget_host_create(.Candle)
	testing.expect_value(t, host.kind, Widget_Kind.Candle)
	testing.expect_value(t, host.state, Widget_Lifecycle_State.Created)
	testing.expect(t, host.channels != 0, "candle should have channels")
	testing.expect(t, host.bundle != 0, "candle should have bundle")
	testing.expect(t, host.min_w > 0, "min width > 0")
	testing.expect(t, host.min_h > 0, "min height > 0")
}

@(test)
test_widget_host_create_empty :: proc(t: ^testing.T) {
	host := widget_host_create(.Empty)
	testing.expect_value(t, host.kind, Widget_Kind.Empty)
	testing.expect_value(t, host.channels, u16(0))
	testing.expect_value(t, host.bundle, u32(0))
}

@(test)
test_widget_descriptor_table_exhaustive :: proc(t: ^testing.T) {
	// Every Widget_Kind should have a matching descriptor.
	for kind in Widget_Kind {
		d := WIDGET_DESCRIPTORS[kind]
		testing.expect_value(t, d.kind, kind)
		testing.expect(t, len(d.label) > 0, "descriptor must have a label")
	}
}

// ---------------------------------------------------------------------------
// Workspace Init
// ---------------------------------------------------------------------------

@(test)
test_workspace_init :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(42))

	testing.expect_value(t, ws.id, Workspace_ID(42))
	testing.expect_value(t, ws.schema_ver, u16(WORKSPACE_SCHEMA_VERSION))
	testing.expect_value(t, ws.tree.root, i8(-1))
	testing.expect_value(t, ws.resize.active_node, i8(-1))
	testing.expect_value(t, ws.focus.active, PANE_ID_NONE)
	testing.expect_value(t, ws.data_ctx.active_stream_idx, -1)
	testing.expect_value(t, ws.data_ctx.default_tf_idx, 2)
}

@(test)
test_workspace_alloc_pane :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	pid := workspace_alloc_pane(&ws, .Trades)
	testing.expect(t, pid != PANE_ID_NONE, "alloc should succeed")

	pane := pane_pool_get(&ws.pane_pool, pid)
	testing.expect(t, pane != nil, "pane should be findable")
	testing.expect_value(t, pane.widget.kind, Widget_Kind.Trades)
	testing.expect_value(t, pane.widget.state, Widget_Lifecycle_State.Created)
}

// ---------------------------------------------------------------------------
// Workspace Registry
// ---------------------------------------------------------------------------

@(test)
test_workspace_registry_alloc :: proc(t: ^testing.T) {
	reg: Workspace_Registry

	ws := workspace_registry_alloc(&reg)
	testing.expect(t, ws != nil, "first alloc succeeds")
	testing.expect_value(t, int(reg.count), 1)

	ws2 := workspace_registry_alloc(&reg)
	testing.expect(t, ws2 != nil, "second alloc succeeds")
	testing.expect(t, ws.id != ws2.id, "IDs differ")
}

@(test)
test_workspace_registry_active :: proc(t: ^testing.T) {
	reg: Workspace_Registry

	// No workspaces → nil.
	testing.expect(t, workspace_registry_active(&reg) == nil, "no active when empty")

	workspace_registry_alloc(&reg)
	active := workspace_registry_active(&reg)
	testing.expect(t, active != nil, "active should exist")
	testing.expect_value(t, active.id, reg.workspaces[0].id)
}

@(test)
test_workspace_registry_capacity :: proc(t: ^testing.T) {
	reg: Workspace_Registry

	for i in 0 ..< WORKSPACE_MAX {
		ws := workspace_registry_alloc(&reg)
		testing.expect(t, ws != nil, "alloc within capacity")
	}

	ws := workspace_registry_alloc(&reg)
	testing.expect(t, ws == nil, "alloc beyond capacity fails")
}

// ---------------------------------------------------------------------------
// Tree Validation
// ---------------------------------------------------------------------------

@(test)
test_tree_validate_empty :: proc(t: ^testing.T) {
	tree: Split_Tree
	tree.root = -1
	testing.expect(t, tree_validate(&tree), "empty tree is valid")
}

@(test)
test_tree_validate_default :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))
	tree, _ := build_default_workspace_tree(&ws)
	testing.expect(t, tree_validate(&tree), "default tree is valid")
}

@(test)
test_tree_validate_all_factories :: proc(t: ^testing.T) {
	ws: Workspace

	workspace_init(&ws, Workspace_ID(1))
	tree1, _ := build_chart_focus_workspace_tree(&ws)
	testing.expect(t, tree_validate(&tree1), "chart focus valid")

	workspace_init(&ws, Workspace_ID(2))
	tree2, _ := build_compact_workspace_tree(&ws)
	testing.expect(t, tree_validate(&tree2), "compact valid")

	workspace_init(&ws, Workspace_ID(3))
	tree3, _ := build_focus_workspace_tree(&ws)
	testing.expect(t, tree_validate(&tree3), "focus valid")

	workspace_init(&ws, Workspace_ID(4))
	tree4, _ := build_analysis_workspace_tree(&ws)
	testing.expect(t, tree_validate(&tree4), "analysis valid")

	for n in 1 ..= 4 {
		workspace_init(&ws, Workspace_ID(u32(4 + n)))
		tree5, _ := build_compare_workspace_tree(&ws, n, .Candle)
		testing.expect(t, tree_validate(&tree5), "compare valid")
	}
}

// ---------------------------------------------------------------------------
// S106: Tree Collect Pane IDs
// ---------------------------------------------------------------------------

@(test)
test_tree_collect_pane_ids_empty :: proc(t: ^testing.T) {
	tree: Split_Tree
	tree.root = -1
	_, count := tree_collect_pane_ids(&tree)
	testing.expect_value(t, count, 0)
}

@(test)
test_tree_collect_pane_ids_single :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	pid := workspace_alloc_pane(&ws, .Candle)
	tree: Split_Tree
	tree.root = -1
	leaf := tree_add_pane_leaf(&tree, -1, pid)
	tree.root = leaf

	ids, count := tree_collect_pane_ids(&tree)
	testing.expect_value(t, count, 1)
	testing.expect_value(t, ids[0], pid)
}

@(test)
test_tree_collect_pane_ids_default_tree :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_default_workspace_tree(&ws)
	ids, count := tree_collect_pane_ids(&tree)

	testing.expect_value(t, count, ui.PANEL_COUNT)

	// DFS order should match: Candle, Stats, Counter, Heatmap, VPVR, Trades, OB.
	for i in 0 ..< ui.PANEL_COUNT {
		testing.expect_value(t, ids[i], pane_ids[i])
	}
}

@(test)
test_tree_collect_compare_4 :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_compare_workspace_tree(&ws, 4, .Candle)
	ids, count := tree_collect_pane_ids(&tree)

	testing.expect_value(t, count, 4)
	for i in 0 ..< 4 {
		testing.expect_value(t, ids[i], pane_ids[i])
	}
}

// ---------------------------------------------------------------------------
// S106: Full Layout Resolution
// ---------------------------------------------------------------------------

@(test)
test_resolve_tree_layout_full :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, _ := build_compact_workspace_tree(&ws)
	bounds := ui.Rect{pos = {0, 0}, size = {1000, 800}}

	pane_rects, node_bounds := resolve_tree_layout_full(&tree, bounds)

	// Root node should get full bounds.
	root_bounds := node_bounds[tree.root]
	assert_f32_near(t, root_bounds.size.x, 1000, 0.1)
	assert_f32_near(t, root_bounds.size.y, 800, 0.1)

	// Pane rects should match regular layout resolution.
	regular := resolve_tree_layout(&tree, bounds)
	for i in 0 ..< PANE_MAX {
		assert_f32_near(t, pane_rects[i].pos.x, regular[i].pos.x, 0.01)
		assert_f32_near(t, pane_rects[i].pos.y, regular[i].pos.y, 0.01)
		assert_f32_near(t, pane_rects[i].size.x, regular[i].size.x, 0.01)
		assert_f32_near(t, pane_rects[i].size.y, regular[i].size.y, 0.01)
	}
}

// ---------------------------------------------------------------------------
// S106: Tree Split Pane
// ---------------------------------------------------------------------------

@(test)
test_tree_split_pane_horizontal :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	// Start with a single pane.
	pid := workspace_alloc_pane(&ws, .Candle)
	ws.tree.root = -1
	leaf := tree_add_pane_leaf(&ws.tree, -1, pid)
	ws.tree.root = leaf

	// Split it horizontally.
	new_id := tree_split_pane(&ws.tree, &ws.pane_pool, pid, .Split_H, .Orderbook)
	testing.expect(t, new_id != PANE_ID_NONE, "split should succeed")
	testing.expect(t, tree_validate(&ws.tree), "tree should be valid after split")
	testing.expect_value(t, tree_pane_count(&ws.tree), 2)

	// Verify layout: original pane on left (50%), new pane on right (50%).
	bounds := ui.Rect{pos = {0, 0}, size = {1000, 800}}
	result := resolve_tree_layout(&ws.tree, bounds)

	orig_idx := int(pid) - 1
	new_idx := int(new_id) - 1

	assert_f32_near(t, result[orig_idx].size.x, 500, 1)
	assert_f32_near(t, result[new_idx].size.x, 500, 1)
	assert_f32_near(t, result[new_idx].pos.x, 500, 1)
}

@(test)
test_tree_split_pane_vertical :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	pid := workspace_alloc_pane(&ws, .Candle)
	ws.tree.root = -1
	leaf := tree_add_pane_leaf(&ws.tree, -1, pid)
	ws.tree.root = leaf

	new_id := tree_split_pane(&ws.tree, &ws.pane_pool, pid, .Split_V, .Trades)
	testing.expect(t, new_id != PANE_ID_NONE, "split should succeed")
	testing.expect(t, tree_validate(&ws.tree), "tree should be valid")
	testing.expect_value(t, tree_pane_count(&ws.tree), 2)

	bounds := ui.Rect{pos = {0, 0}, size = {1000, 800}}
	result := resolve_tree_layout(&ws.tree, bounds)

	orig_idx := int(pid) - 1
	new_idx := int(new_id) - 1

	assert_f32_near(t, result[orig_idx].size.y, 400, 1)
	assert_f32_near(t, result[new_idx].size.y, 400, 1)
	assert_f32_near(t, result[new_idx].pos.y, 400, 1)
}

@(test)
test_tree_split_pane_nested :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	// Build: split candle H → (candle | OB), then split OB V → (OB | Trades).
	pid_c := workspace_alloc_pane(&ws, .Candle)
	ws.tree.root = -1
	leaf := tree_add_pane_leaf(&ws.tree, -1, pid_c)
	ws.tree.root = leaf

	pid_ob := tree_split_pane(&ws.tree, &ws.pane_pool, pid_c, .Split_H, .Orderbook)
	testing.expect(t, pid_ob != PANE_ID_NONE, "first split")

	pid_tr := tree_split_pane(&ws.tree, &ws.pane_pool, pid_ob, .Split_V, .Trades)
	testing.expect(t, pid_tr != PANE_ID_NONE, "nested split")
	testing.expect(t, tree_validate(&ws.tree), "tree valid after nested split")
	testing.expect_value(t, tree_pane_count(&ws.tree), 3)
}

@(test)
test_tree_split_pane_invalid_direction :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	pid := workspace_alloc_pane(&ws, .Candle)
	ws.tree.root = -1
	leaf := tree_add_pane_leaf(&ws.tree, -1, pid)
	ws.tree.root = leaf

	// Pane kind is not a valid split direction.
	result := tree_split_pane(&ws.tree, &ws.pane_pool, pid, .Pane, .Empty)
	testing.expect_value(t, result, PANE_ID_NONE)
}

// ---------------------------------------------------------------------------
// S106: Auto Workspace Tree
// ---------------------------------------------------------------------------

@(test)
test_auto_workspace_tree_1 :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	kinds := [1]Widget_Kind{.Candle}
	tree := build_auto_workspace_tree(&ws, kinds[:])

	testing.expect(t, tree_validate(&tree), "1-pane tree valid")
	testing.expect_value(t, tree_pane_count(&tree), 1)
}

@(test)
test_auto_workspace_tree_2 :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	kinds := [2]Widget_Kind{.Candle, .Orderbook}
	tree := build_auto_workspace_tree(&ws, kinds[:])

	testing.expect(t, tree_validate(&tree), "2-pane tree valid")
	testing.expect_value(t, tree_pane_count(&tree), 2)

	// Should be H split (side by side).
	testing.expect_value(t, tree.nodes[tree.root].kind, Split_Node_Kind.Split_H)
}

@(test)
test_auto_workspace_tree_4 :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	kinds := [4]Widget_Kind{.Candle, .Stats, .Trades, .Orderbook}
	tree := build_auto_workspace_tree(&ws, kinds[:])

	testing.expect(t, tree_validate(&tree), "4-pane tree valid")
	testing.expect_value(t, tree_pane_count(&tree), 4)

	// 4 panes collected in DFS order.
	ids, count := tree_collect_pane_ids(&tree)
	testing.expect_value(t, count, 4)

	// Verify all panes get reasonable rects.
	bounds := ui.Rect{pos = {0, 0}, size = {1000, 800}}
	result := resolve_tree_layout(&tree, bounds)
	for i in 0 ..< 4 {
		idx := int(ids[i]) - 1
		if idx >= 0 && idx < PANE_MAX {
			testing.expect(t, result[idx].size.x > 0, "pane width > 0")
			testing.expect(t, result[idx].size.y > 0, "pane height > 0")
		}
	}
}

@(test)
test_auto_workspace_tree_5 :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	kinds := [5]Widget_Kind{.Candle, .Stats, .Counter, .Trades, .Orderbook}
	tree := build_auto_workspace_tree(&ws, kinds[:])

	testing.expect(t, tree_validate(&tree), "5-pane tree valid")
	testing.expect_value(t, tree_pane_count(&tree), 5)
}

// ---------------------------------------------------------------------------
// S106: DFS Order Stability
// ---------------------------------------------------------------------------

@(test)
test_dfs_order_stable_after_resize :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_default_workspace_tree(&ws)

	// Collect DFS order before resize.
	ids_before, count_before := tree_collect_pane_ids(&tree)

	// Resize root.
	tree_set_ratio(&tree, tree.root, 0.6)

	// DFS order should be unchanged.
	ids_after, count_after := tree_collect_pane_ids(&tree)
	testing.expect_value(t, count_after, count_before)
	for i in 0 ..< count_before {
		testing.expect_value(t, ids_after[i], ids_before[i])
	}
}

@(test)
test_split_then_remove_restores_original :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	pid := workspace_alloc_pane(&ws, .Candle)
	ws.tree.root = -1
	leaf := tree_add_pane_leaf(&ws.tree, -1, pid)
	ws.tree.root = leaf

	// Split.
	new_id := tree_split_pane(&ws.tree, &ws.pane_pool, pid, .Split_H, .Orderbook)
	testing.expect(t, new_id != PANE_ID_NONE, "split succeeded")
	testing.expect_value(t, tree_pane_count(&ws.tree), 2)

	// Remove the new pane — should restore to single pane.
	ok := tree_remove_pane(&ws.tree, new_id)
	testing.expect(t, ok, "remove succeeded")

	// Tree should now have the original candle pane as root.
	testing.expect_value(t, ws.tree.nodes[ws.tree.root].kind, Split_Node_Kind.Pane)
	testing.expect_value(t, tree_leaf_pane_id(&ws.tree, ws.tree.root), pid)
}

// ---------------------------------------------------------------------------
// S109: Contract-Based Dashboard Migration
// ---------------------------------------------------------------------------

@(test)
test_workspace_auto_init_on_nil :: proc(t: ^testing.T) {
	// S109: workspace_registry_active returns nil when empty.
	// build_workspace_dashboard auto-initializes in this case.
	reg: Workspace_Registry
	testing.expect(t, workspace_registry_active(&reg) == nil, "empty registry returns nil")

	// After alloc, active is non-nil.
	ws := workspace_registry_alloc(&reg)
	testing.expect(t, ws != nil, "alloc succeeds")
	testing.expect(t, workspace_registry_active(&reg) != nil, "active after alloc")
}

@(test)
test_pane_pool_widget_kind_lookup :: proc(t: ^testing.T) {
	// S109: build_workspace_dashboard uses pane pool for widget kind, not Entity_World.
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pids := build_default_workspace_tree(&ws)
	ws.tree = tree

	// Verify pane pool returns correct widget kinds via DFS lookup.
	ids, count := tree_collect_pane_ids(&tree)
	testing.expect_value(t, count, ui.PANEL_COUNT)

	// The default tree assigns kinds in DFS order matching panel indices.
	for i in 0 ..< count {
		pane := pane_pool_get(&ws.pane_pool, ids[i])
		testing.expect(t, pane != nil, "pane exists in pool")
		testing.expect(t, pane.widget.kind != .Empty || i == 8, "non-empty panes have real kinds")
	}
}

@(test)
test_pane_focused_candle_detection :: proc(t: ^testing.T) {
	// S109: focus detection scans pane pool instead of Entity_World.
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	// Build a tree with Stats, Candle, Orderbook.
	kinds := [3]Widget_Kind{.Stats, .Candle, .Orderbook}
	tree := build_auto_workspace_tree(&ws, kinds[:])

	ids, count := tree_collect_pane_ids(&tree)
	testing.expect_value(t, count, 3)

	// Find first candle pane by scanning pool.
	candle_idx := -1
	for i in 0 ..< count {
		pane := pane_pool_get(&ws.pane_pool, ids[i])
		if pane != nil && pane.widget.kind == .Candle {
			candle_idx = i
			break
		}
	}
	testing.expect_value(t, candle_idx, 1)
}

@(test)
test_pane_tf_override_independent :: proc(t: ^testing.T) {
	// S109: each pane has independent TF override.
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	kinds := [2]Widget_Kind{.Candle, .Candle}
	tree := build_auto_workspace_tree(&ws, kinds[:])

	ids, count := tree_collect_pane_ids(&tree)
	testing.expect_value(t, count, 2)

	// Set TF override on pane 0 only.
	pane0 := pane_pool_get(&ws.pane_pool, ids[0])
	pane1 := pane_pool_get(&ws.pane_pool, ids[1])
	testing.expect(t, pane0 != nil && pane1 != nil, "both panes exist")

	pane0.tf_override = 5  // e.g. 30m
	testing.expect_value(t, pane0.tf_override, i8(5))
	testing.expect_value(t, pane1.tf_override, i8(-1))  // inherit
}

@(test)
test_pane_analytics_config_independent :: proc(t: ^testing.T) {
	// S109: each analytics pane has independent config.
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	kinds := [2]Widget_Kind{.Analytics, .Analytics}
	tree := build_auto_workspace_tree(&ws, kinds[:])

	ids, count := tree_collect_pane_ids(&tree)
	testing.expect_value(t, count, 2)

	pane0 := pane_pool_get(&ws.pane_pool, ids[0])
	pane1 := pane_pool_get(&ws.pane_pool, ids[1])
	testing.expect(t, pane0 != nil && pane1 != nil, "both panes exist")

	// Set different analytics kinds.
	pane0.analytics.analytics_kind = .CVD
	pane1.analytics.analytics_kind = .Delta_Volume
	pane0.analytics.show_history = true

	testing.expect_value(t, pane0.analytics.analytics_kind, services.Analytics_Kind.CVD)
	testing.expect_value(t, pane1.analytics.analytics_kind, services.Analytics_Kind.Delta_Volume)
	testing.expect_value(t, pane0.analytics.show_history, true)
	testing.expect_value(t, pane1.analytics.show_history, false)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

@(private = "file")
assert_f32_near :: proc(t: ^testing.T, actual, expected, tolerance: f32, loc := #caller_location) {
	diff := actual - expected
	if diff < 0 do diff = -diff
	testing.expect(t, diff <= tolerance, "f32 value not within tolerance", loc = loc)
}
