package app

// S105: Dashboard Workspace Architecture — Foundation types.
// Implements ADR-0024 (Workspace), ADR-0025 (Split Tree), ADR-0026 (Pane),
// ADR-0027 (Widget Host).
//
// These types exist alongside the legacy Entity_World / Grid_Def system.
// No rendering paths are changed in this stage — only structural types,
// layout resolution, tree factories, and pane pool management.

import "mr:services"
import "mr:ui"
import "mr:widgets"

// ---------------------------------------------------------------------------
// Split Tree Layout Model (ADR-0025)
// ---------------------------------------------------------------------------

TREE_NODE_MAX :: 31  // 16 panes → at most 15 internal nodes + 16 leaves

Split_Node_Kind :: enum u8 {
	Split_H,  // horizontal split: children arranged left → right
	Split_V,  // vertical split: children arranged top → bottom
	Pane,     // leaf: hosts a single widget (references Pane_ID)
	Stack,    // leaf: tabbed stack of widgets (references N Pane_IDs)
}

Split_Node :: struct {
	kind:     Split_Node_Kind,
	parent:   i8,          // -1 = root
	children: [2]i8,       // left/right or top/bottom; for Pane: [0]=pane_id, [1]=-1
	ratio:    f32,          // 0.0–1.0 split position (Split_H / Split_V only)
	min_size: f32,          // minimum fraction of parent (e.g. 0.08)
}

Split_Tree :: struct {
	nodes: [TREE_NODE_MAX]Split_Node,
	count: u8,
	root:  i8,  // index of root node (-1 = empty)
}

// Interactive resize state for tree edges (ADR-0025 §5).
Split_Resize_State :: struct {
	active_node: i8,    // -1 = idle, else index of Split node being resized
	start_ratio: f32,   // ratio at drag start
	start_pos:   f32,   // mouse position at drag start
}

// ---------------------------------------------------------------------------
// Pane Runtime Model (ADR-0026)
// ---------------------------------------------------------------------------

PANE_MAX :: 16

Pane_ID :: distinct u16
PANE_ID_NONE :: Pane_ID(0)

// S119: Pane role — classifies pane operational intent.
Pane_Role :: enum u8 {
	Primary_Chart,  // Main chart pane — candle, focus target
	Auxiliary,      // Supporting data pane — trades, orderbook, stats, etc.
	Context,        // Rendered inside context stack, not as a tree leaf
}

// S119: Infer default role from widget kind.
infer_pane_role :: proc(kind: Widget_Kind) -> Pane_Role {
	switch kind {
	case .Candle:
		return .Primary_Chart
	case .Stats, .Counter, .Trades, .Orderbook, .Heatmap, .VPVR, .DOM, .Analytics, .Session_VPVR, .TPO, .Footprint, .Empty:
		return .Auxiliary
	}
	return .Auxiliary
}

Pane :: struct {
	id:          Pane_ID,
	alive:       bool,
	role:        Pane_Role,  // S119: operational role

	// Widget host (ADR-0027).
	widget:      Widget_Host,

	// Data binding.
	binding:     Stream_Binding,
	tf_override: i8,  // -1 = inherit from workspace data context

	// View state (viewport, scroll, zoom).
	view:        Pane_View_State,

	// Per-pane indicator config.
	indicators:  Indicator_Component,
	ind_params:  Indicator_Params,
	chart:       Chart_Component,
	subplots:    Subplot_Component,
	analytics:   Analytics_Component,
}

Pane_View_State :: struct {
	// Scroll & zoom (candle-relative).
	scroll_x:          f32,
	zoom_level:        f32,

	// Widget-specific scroll.
	ob_scroll_y:       f32,
	trades_scroll_y:   f32,

	// Crosshair.
	crosshair:         widgets.Crosshair_State,

	// Resolved rect (set by tree layout each frame).
	rect:              ui.Rect,
	rect_valid:        bool,
}

// Focus tracking with stable IDs (ADR-0026 §3).
Focus_State :: struct {
	active:   Pane_ID,
	previous: Pane_ID,
	locked:   bool,
}

// Fixed-capacity pane pool (ADR-0026 §6).
Pane_Pool :: struct {
	panes:   [PANE_MAX]Pane,
	count:   u8,
	next_id: u16,  // monotonic counter (starts at 1, 0 = PANE_ID_NONE)
}

// ---------------------------------------------------------------------------
// Widget Host Contract (ADR-0027)
// ---------------------------------------------------------------------------

Widget_Lifecycle_State :: enum u8 {
	Created,
	Bound,
	Active,
	Suspended,
	Disposing,
}

Widget_Host :: struct {
	kind:     Widget_Kind,
	state:    Widget_Lifecycle_State,
	channels: u16,  // MD_Channel bitmask
	bundle:   u32,  // Layer_Bundle mask
	min_w:    f32,
	min_h:    f32,
}

// Compile-time widget descriptor table (ADR-0027 §3).
// Channels and bundle are resolved at runtime via channels_for_widget / layer_bundle_for_widget
// because those procs require a context and cannot be called at global scope.
Widget_Descriptor :: struct {
	kind:                Widget_Kind,
	label:               string,
	min_w:               f32,
	min_h:               f32,
	supports_tf:         bool,
	supports_indicators: bool,
	supports_subplots:   bool,
	supports_analytics:  bool,
}

// Single source of truth for widget static capabilities.
// Exhaustive over Widget_Kind — compiler enforces coverage via [Widget_Kind] indexing.
@(rodata)
WIDGET_DESCRIPTORS := [Widget_Kind]Widget_Descriptor {
	.Candle = {
		kind = .Candle, label = "Candle Chart",
		min_w = 200, min_h = 150,
		supports_tf = true, supports_indicators = true,
		supports_subplots = true, supports_analytics = true,
	},
	.Stats = {
		kind = .Stats, label = "Stats",
		min_w = 100, min_h = 60,
	},
	.Counter = {
		kind = .Counter, label = "Counter",
		min_w = 100, min_h = 60,
	},
	.Heatmap = {
		kind = .Heatmap, label = "Heatmap",
		min_w = 120, min_h = 100,
	},
	.VPVR = {
		kind = .VPVR, label = "VPVR",
		min_w = 120, min_h = 100,
	},
	.Trades = {
		kind = .Trades, label = "Trades",
		min_w = 120, min_h = 80,
	},
	.Orderbook = {
		kind = .Orderbook, label = "Orderbook",
		min_w = 120, min_h = 100,
	},
	.DOM = {
		kind = .DOM, label = "DOM",
		min_w = 140, min_h = 120,
	},
	.Empty = {
		kind = .Empty, label = "Empty",
		min_w = 40, min_h = 40,
	},
	.Analytics = {
		kind = .Analytics, label = "Analytics",
		min_w = 120, min_h = 80,
		supports_analytics = true,
	},
	.Session_VPVR = {
		kind = .Session_VPVR, label = "Session VPVR",
		min_w = 120, min_h = 100,
	},
	.TPO = {
		kind = .TPO, label = "TPO",
		min_w = 120, min_h = 100,
	},
	.Footprint = {
		kind = .Footprint, label = "Footprint",
		min_w = 140, min_h = 120,
	},
}

// Create a Widget_Host from the descriptor table + runtime channel/bundle resolution.
widget_host_create :: proc(kind: Widget_Kind) -> Widget_Host {
	d := WIDGET_DESCRIPTORS[kind]
	return Widget_Host{
		kind     = kind,
		state    = .Created,
		channels = channels_for_widget(kind),
		bundle   = layer_bundle_for_widget(kind),
		min_w    = d.min_w,
		min_h    = d.min_h,
	}
}

// ---------------------------------------------------------------------------
// Pane Data Context (ADR-0030, S112)
// ---------------------------------------------------------------------------
//
// Documents the data each pane owns. All fields are on the Pane struct.
// Widget_Data_Context is resolved exclusively from pane-local state.
//
// Ownership hierarchy:
//   Workspace → Pane → Widget
//     Workspace: active_stream, default_tf, default_analytics
//     Pane: binding (venue/symbol/stream), tf_override, indicators, chart, analytics
//     Widget: receives Widget_Data_Context (immutable, resolved per frame)
//
// Sync protocol (S112):
//   Entity_World → Pane: workspace_sync_panes_from_world (startup/restore)
//   Action → Pane: apply_set_cell_{timeframe,stream,widget}_action writes to both
//   Pane → Entity_World: workspace_sync_pane_to_world (for legacy paths)

Pane_Data_Context :: struct {
	venue:          string,
	symbol:         string,
	stream_idx:     int,       // resolved stream slot (-1 = follow active)
	stream_bound:   bool,      // has explicit venue/symbol binding
	tf_idx:         int,       // effective TF index (resolved)
	analytics_kind: services.Analytics_Kind,
	compare_group:  int,       // -1 = normal, 0-3 = compare pane
}

// ---------------------------------------------------------------------------
// Workspace Data Context (ADR-0028, simplified for S105)
// ---------------------------------------------------------------------------

Workspace_Data_Context :: struct {
	active_stream_idx:  int,     // -1 = none
	active_venue:       [24]u8,
	active_venue_len:   u8,
	active_symbol:      [32]u8,
	active_symbol_len:  u8,
	default_tf_idx:     int,     // default TF for panes that inherit
	default_analytics:  services.Analytics_Kind,
}

// ---------------------------------------------------------------------------
// Workspace Mode
// ---------------------------------------------------------------------------

Workspace_Mode :: enum u8 {
	Normal,
	Zen,
}

// ---------------------------------------------------------------------------
// Workspace Aggregate Root (ADR-0024)
// ---------------------------------------------------------------------------

Workspace_ID :: distinct u32

WORKSPACE_MAX :: 8

Workspace :: struct {
	id:          Workspace_ID,
	label:       [48]u8,
	label_len:   u8,
	schema_ver:  u16,

	// Layout tree (ADR-0025).
	tree:        Split_Tree,

	// Pane pool (ADR-0026).
	pane_pool:   Pane_Pool,

	// Data context (ADR-0028).
	data_ctx:    Workspace_Data_Context,

	// Focus tracking.
	focus:       Focus_State,

	// Mode.
	mode:        Workspace_Mode,

	// Resize interaction.
	resize:      Split_Resize_State,
}

// Workspace Registry — holds all workspaces (ADR-0024 §4).
Workspace_Registry :: struct {
	workspaces: [WORKSPACE_MAX]Workspace,
	count:      u8,
	active_idx: u8,
	next_ws_id: u32,  // monotonic
}

// ---------------------------------------------------------------------------
// Pane Pool Operations (ADR-0026 §6)
// ---------------------------------------------------------------------------

// Allocate a new pane from the pool. Returns nil if full.
pane_pool_alloc :: proc(pool: ^Pane_Pool) -> (pane: ^Pane, id: Pane_ID) {
	if pool.count >= PANE_MAX do return nil, PANE_ID_NONE

	// Find first dead slot.
	for i in 0 ..< PANE_MAX {
		if !pool.panes[i].alive {
			pool.next_id += 1
			pid := Pane_ID(pool.next_id)
			pool.panes[i] = Pane{}
			pool.panes[i].id = pid
			pool.panes[i].alive = true
			pool.panes[i].role = .Auxiliary  // S119: safe default, overridden by workspace_alloc_pane
			pool.panes[i].tf_override = -1
			pool.panes[i].view.zoom_level = 1.0
			pool.count += 1
			return &pool.panes[i], pid
		}
	}
	return nil, PANE_ID_NONE
}

// Free a pane by ID.
pane_pool_free :: proc(pool: ^Pane_Pool, id: Pane_ID) {
	if id == PANE_ID_NONE do return
	for i in 0 ..< PANE_MAX {
		if pool.panes[i].alive && pool.panes[i].id == id {
			pool.panes[i].alive = false
			pool.count -= 1
			return
		}
	}
}

// Get a pane by ID. Returns nil if not alive.
pane_pool_get :: proc(pool: ^Pane_Pool, id: Pane_ID) -> ^Pane {
	if id == PANE_ID_NONE do return nil
	for i in 0 ..< PANE_MAX {
		if pool.panes[i].alive && pool.panes[i].id == id {
			return &pool.panes[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Split Tree Builder Helpers
// ---------------------------------------------------------------------------

// Add a split node to the tree. Returns the node index, or -1 if full.
tree_add_node :: proc(tree: ^Split_Tree, kind: Split_Node_Kind, parent: i8, ratio: f32, min_size: f32) -> i8 {
	if int(tree.count) >= TREE_NODE_MAX do return -1
	idx := i8(tree.count)
	tree.nodes[idx] = Split_Node{
		kind     = kind,
		parent   = parent,
		children = {-1, -1},
		ratio    = ratio,
		min_size = min_size,
	}
	tree.count += 1
	return idx
}

// Set children of a node.
tree_set_children :: proc(tree: ^Split_Tree, node_idx: i8, left, right: i8) {
	if node_idx < 0 || int(node_idx) >= int(tree.count) do return
	tree.nodes[node_idx].children = {left, right}
	if left >= 0 && int(left) < int(tree.count) {
		tree.nodes[left].parent = node_idx
	}
	if right >= 0 && int(right) < int(tree.count) {
		tree.nodes[right].parent = node_idx
	}
}

// Create a leaf pane node. children[0] stores the Pane_ID as i8 (cast).
tree_add_pane_leaf :: proc(tree: ^Split_Tree, parent: i8, pane_id: Pane_ID) -> i8 {
	idx := tree_add_node(tree, .Pane, parent, 0, 0.05)
	if idx >= 0 {
		tree.nodes[idx].children[0] = i8(pane_id)
	}
	return idx
}

// Get the Pane_ID stored in a leaf node.
tree_leaf_pane_id :: proc(tree: ^Split_Tree, node_idx: i8) -> Pane_ID {
	if node_idx < 0 || int(node_idx) >= int(tree.count) do return PANE_ID_NONE
	node := tree.nodes[node_idx]
	if node.kind != .Pane do return PANE_ID_NONE
	return Pane_ID(node.children[0])
}

// ---------------------------------------------------------------------------
// Layout Resolution (ADR-0025 §3)
// ---------------------------------------------------------------------------

// Resolve the tree layout into per-pane rects. Recursive descent from root.
resolve_tree_layout :: proc(tree: ^Split_Tree, bounds: ui.Rect) -> [PANE_MAX]ui.Rect {
	result: [PANE_MAX]ui.Rect
	if tree.root < 0 || int(tree.root) >= int(tree.count) do return result
	resolve_node(tree, tree.root, bounds, &result)
	return result
}

@(private = "file")
resolve_node :: proc(tree: ^Split_Tree, idx: i8, bounds: ui.Rect, out: ^[PANE_MAX]ui.Rect) {
	if idx < 0 || int(idx) >= int(tree.count) do return
	node := tree.nodes[idx]

	switch node.kind {
	case .Pane:
		pid := Pane_ID(node.children[0])
		if pid != PANE_ID_NONE && int(pid) > 0 && int(pid) <= PANE_MAX {
			out[int(pid) - 1] = bounds  // Pane_ID is 1-based
		}
	case .Stack:
		// Stack: assign full bounds to active tab (children[0] = active pane_id).
		pid := Pane_ID(node.children[0])
		if pid != PANE_ID_NONE && int(pid) > 0 && int(pid) <= PANE_MAX {
			out[int(pid) - 1] = bounds
		}
	case .Split_H:
		// Split horizontally at ratio.
		r := clamp(node.ratio, node.min_size, 1.0 - node.min_size)
		left_w := bounds.size.x * r
		right_w := bounds.size.x - left_w

		left_bounds := ui.Rect{
			pos  = bounds.pos,
			size = {left_w, bounds.size.y},
		}
		right_bounds := ui.Rect{
			pos  = {bounds.pos.x + left_w, bounds.pos.y},
			size = {right_w, bounds.size.y},
		}
		resolve_node(tree, node.children[0], left_bounds, out)
		resolve_node(tree, node.children[1], right_bounds, out)
	case .Split_V:
		// Split vertically at ratio.
		r := clamp(node.ratio, node.min_size, 1.0 - node.min_size)
		top_h := bounds.size.y * r
		bottom_h := bounds.size.y - top_h

		top_bounds := ui.Rect{
			pos  = bounds.pos,
			size = {bounds.size.x, top_h},
		}
		bottom_bounds := ui.Rect{
			pos  = {bounds.pos.x, bounds.pos.y + top_h},
			size = {bounds.size.x, bottom_h},
		}
		resolve_node(tree, node.children[0], top_bounds, out)
		resolve_node(tree, node.children[1], bottom_bounds, out)
	}
}

// ---------------------------------------------------------------------------
// Tree Mutations (ADR-0025 §6)
// ---------------------------------------------------------------------------

// Swap two leaf pane IDs in the tree (drag-drop reorder).
tree_swap_panes :: proc(tree: ^Split_Tree, a, b: Pane_ID) {
	if a == PANE_ID_NONE || b == PANE_ID_NONE || a == b do return

	a_idx: i8 = -1
	b_idx: i8 = -1
	for i in 0 ..< int(tree.count) {
		if tree.nodes[i].kind == .Pane {
			pid := Pane_ID(tree.nodes[i].children[0])
			if pid == a do a_idx = i8(i)
			if pid == b do b_idx = i8(i)
		}
	}
	if a_idx >= 0 && b_idx >= 0 {
		tree.nodes[a_idx].children[0], tree.nodes[b_idx].children[0] =
			tree.nodes[b_idx].children[0], tree.nodes[a_idx].children[0]
	}
}

// Set ratio on a split node (resize).
tree_set_ratio :: proc(tree: ^Split_Tree, node_idx: i8, ratio: f32) {
	if node_idx < 0 || int(node_idx) >= int(tree.count) do return
	node := &tree.nodes[node_idx]
	if node.kind != .Split_H && node.kind != .Split_V do return
	node.ratio = clamp(ratio, node.min_size, 1.0 - node.min_size)
}

// Rotate a split node: Split_H ↔ Split_V.
tree_rotate :: proc(tree: ^Split_Tree, node_idx: i8) {
	if node_idx < 0 || int(node_idx) >= int(tree.count) do return
	node := &tree.nodes[node_idx]
	switch node.kind {
	case .Split_H: node.kind = .Split_V
	case .Split_V: node.kind = .Split_H
	case .Pane, .Stack: // no-op
	}
}

// Remove a pane from the tree. Promotes sibling to parent's position.
tree_remove_pane :: proc(tree: ^Split_Tree, pane_id: Pane_ID) -> bool {
	if pane_id == PANE_ID_NONE do return false

	// Find the leaf node.
	leaf_idx: i8 = -1
	for i in 0 ..< int(tree.count) {
		if tree.nodes[i].kind == .Pane && Pane_ID(tree.nodes[i].children[0]) == pane_id {
			leaf_idx = i8(i)
			break
		}
	}
	if leaf_idx < 0 do return false

	parent_idx := tree.nodes[leaf_idx].parent
	if parent_idx < 0 {
		// Removing root pane — tree becomes empty.
		tree.root = -1
		tree.count = 0
		return true
	}

	// Find sibling.
	parent := tree.nodes[parent_idx]
	sibling_idx: i8 = -1
	if parent.children[0] == leaf_idx {
		sibling_idx = parent.children[1]
	} else {
		sibling_idx = parent.children[0]
	}

	// Promote sibling to parent's position.
	grandparent_idx := parent.parent
	if grandparent_idx >= 0 {
		gp := &tree.nodes[grandparent_idx]
		if gp.children[0] == parent_idx {
			gp.children[0] = sibling_idx
		} else {
			gp.children[1] = sibling_idx
		}
	}
	if sibling_idx >= 0 && int(sibling_idx) < int(tree.count) {
		tree.nodes[sibling_idx].parent = grandparent_idx
	}
	if tree.root == parent_idx {
		tree.root = sibling_idx
	}

	return true
}

// ---------------------------------------------------------------------------
// Workspace Operations
// ---------------------------------------------------------------------------

// Initialize a workspace with default settings.
workspace_init :: proc(ws: ^Workspace, id: Workspace_ID) {
	ws^ = Workspace{}
	ws.id = id
	ws.schema_ver = WORKSPACE_SCHEMA_VERSION
	ws.tree.root = -1
	ws.resize.active_node = -1
	ws.data_ctx.active_stream_idx = -1
	ws.data_ctx.default_tf_idx = 2  // 1m default
	ws.focus.active = PANE_ID_NONE
	ws.focus.previous = PANE_ID_NONE
	ws.pane_pool.next_id = 0
}

// Allocate a pane in the workspace pool and return its ID.
workspace_alloc_pane :: proc(ws: ^Workspace, kind: Widget_Kind) -> Pane_ID {
	pane, id := pane_pool_alloc(&ws.pane_pool)
	if pane == nil do return PANE_ID_NONE
	pane.widget = widget_host_create(kind)
	pane.role = infer_pane_role(kind)  // S119: auto-assign role from widget kind
	return id
}

// Registry: allocate a new workspace.
workspace_registry_alloc :: proc(reg: ^Workspace_Registry) -> ^Workspace {
	if reg.count >= WORKSPACE_MAX do return nil
	idx := reg.count
	reg.next_ws_id += 1
	ws := &reg.workspaces[idx]
	workspace_init(ws, Workspace_ID(reg.next_ws_id))
	reg.count += 1
	return ws
}

// Registry: get active workspace.
workspace_registry_active :: proc(reg: ^Workspace_Registry) -> ^Workspace {
	if reg.count == 0 do return nil
	idx := clamp(int(reg.active_idx), 0, int(reg.count) - 1)
	return &reg.workspaces[idx]
}

// ---------------------------------------------------------------------------
// S106: Tree Runtime Helpers
// ---------------------------------------------------------------------------

// Collect pane IDs in DFS order (left-to-right, top-to-bottom).
// Position in the returned array corresponds to Entity_World cell index.
tree_collect_pane_ids :: proc(tree: ^Split_Tree) -> (ids: [PANE_MAX]Pane_ID, count: int) {
	count = 0
	if tree.root < 0 || int(tree.root) >= int(tree.count) do return ids, 0
	collect_pane_dfs(tree, tree.root, &ids, &count)
	return ids, count
}

@(private = "file")
collect_pane_dfs :: proc(tree: ^Split_Tree, idx: i8, out: ^[PANE_MAX]Pane_ID, count: ^int) {
	if idx < 0 || int(idx) >= int(tree.count) do return
	node := tree.nodes[idx]

	switch node.kind {
	case .Pane, .Stack:
		pid := Pane_ID(node.children[0])
		if pid != PANE_ID_NONE && count^ < PANE_MAX {
			out[count^] = pid
			count^ += 1
		}
	case .Split_H, .Split_V:
		collect_pane_dfs(tree, node.children[0], out, count)
		collect_pane_dfs(tree, node.children[1], out, count)
	}
}

// Full tree layout resolution: pane rects + per-node bounds (for resize hit detection).
resolve_tree_layout_full :: proc(tree: ^Split_Tree, bounds: ui.Rect) -> (pane_rects: [PANE_MAX]ui.Rect, node_bounds: [TREE_NODE_MAX]ui.Rect) {
	if tree.root < 0 || int(tree.root) >= int(tree.count) do return
	resolve_node_full(tree, tree.root, bounds, &pane_rects, &node_bounds)
	return
}

@(private = "file")
resolve_node_full :: proc(tree: ^Split_Tree, idx: i8, bounds: ui.Rect, pane_out: ^[PANE_MAX]ui.Rect, node_out: ^[TREE_NODE_MAX]ui.Rect) {
	if idx < 0 || int(idx) >= int(tree.count) do return
	node_out[idx] = bounds
	node := tree.nodes[idx]

	switch node.kind {
	case .Pane:
		pid := Pane_ID(node.children[0])
		if pid != PANE_ID_NONE && int(pid) > 0 && int(pid) <= PANE_MAX {
			pane_out[int(pid) - 1] = bounds
		}
	case .Stack:
		pid := Pane_ID(node.children[0])
		if pid != PANE_ID_NONE && int(pid) > 0 && int(pid) <= PANE_MAX {
			pane_out[int(pid) - 1] = bounds
		}
	case .Split_H:
		r := clamp(node.ratio, node.min_size, 1.0 - node.min_size)
		left_w := bounds.size.x * r
		right_w := bounds.size.x - left_w
		left_bounds := ui.Rect{pos = bounds.pos, size = {left_w, bounds.size.y}}
		right_bounds := ui.Rect{pos = {bounds.pos.x + left_w, bounds.pos.y}, size = {right_w, bounds.size.y}}
		resolve_node_full(tree, node.children[0], left_bounds, pane_out, node_out)
		resolve_node_full(tree, node.children[1], right_bounds, pane_out, node_out)
	case .Split_V:
		r := clamp(node.ratio, node.min_size, 1.0 - node.min_size)
		top_h := bounds.size.y * r
		bottom_h := bounds.size.y - top_h
		top_bounds := ui.Rect{pos = bounds.pos, size = {bounds.size.x, top_h}}
		bottom_bounds := ui.Rect{pos = {bounds.pos.x, bounds.pos.y + top_h}, size = {bounds.size.x, bottom_h}}
		resolve_node_full(tree, node.children[0], top_bounds, pane_out, node_out)
		resolve_node_full(tree, node.children[1], bottom_bounds, pane_out, node_out)
	}
}

// Split a pane leaf into a split node with two children.
// Original pane becomes left child, new pane becomes right child.
// Returns the new pane's ID, or PANE_ID_NONE on failure.
tree_split_pane :: proc(tree: ^Split_Tree, pool: ^Pane_Pool, target: Pane_ID, dir: Split_Node_Kind, new_widget: Widget_Kind) -> Pane_ID {
	if dir != .Split_H && dir != .Split_V do return PANE_ID_NONE

	// Find leaf node for target pane.
	leaf_idx: i8 = -1
	for i in 0 ..< int(tree.count) {
		if tree.nodes[i].kind == .Pane && Pane_ID(tree.nodes[i].children[0]) == target {
			leaf_idx = i8(i)
			break
		}
	}
	if leaf_idx < 0 do return PANE_ID_NONE

	// Need 2 more node slots.
	if int(tree.count) + 2 > TREE_NODE_MAX do return PANE_ID_NONE

	// Allocate new pane.
	new_pane, new_id := pane_pool_alloc(pool)
	if new_pane == nil do return PANE_ID_NONE
	new_pane.widget = widget_host_create(new_widget)

	// Add two new leaf nodes (children of the converted split).
	orig_leaf := tree_add_pane_leaf(tree, leaf_idx, target)
	new_leaf := tree_add_pane_leaf(tree, leaf_idx, new_id)

	// Convert the original leaf node into a split node.
	tree.nodes[leaf_idx].kind = dir
	tree.nodes[leaf_idx].ratio = 0.5
	tree.nodes[leaf_idx].min_size = 0.08
	tree.nodes[leaf_idx].children = {orig_leaf, new_leaf}

	return new_id
}

// Build a workspace tree from a list of widget kinds (auto-layout).
// Uses recursive balanced splitting for arbitrary pane counts.
build_auto_workspace_tree :: proc(ws: ^Workspace, kinds: []Widget_Kind) -> Split_Tree {
	tree: Split_Tree
	tree.root = -1
	n := min(len(kinds), PANE_MAX)
	if n == 0 do return tree

	// Allocate panes and create leaf nodes.
	leaves: [PANE_MAX]i8
	for i in 0 ..< n {
		pid := workspace_alloc_pane(ws, kinds[i])
		leaves[i] = tree_add_pane_leaf(&tree, -1, pid)
	}

	// Build balanced split tree from leaves.
	tree.root = build_balanced_subtree(&tree, leaves[:n], 0)
	return tree
}

@(private = "file")
build_balanced_subtree :: proc(tree: ^Split_Tree, leaves: []i8, depth: int) -> i8 {
	if len(leaves) == 1 do return leaves[0]
	if len(leaves) == 0 do return -1

	mid := len(leaves) / 2
	left := build_balanced_subtree(tree, leaves[:mid], depth + 1)
	right := build_balanced_subtree(tree, leaves[mid:], depth + 1)

	// Alternate V/H: even depth = V (rows), odd depth = H (columns).
	kind := depth % 2 == 0 ? Split_Node_Kind.Split_V : Split_Node_Kind.Split_H
	// For 2 items at any depth, prefer H (side-by-side).
	if len(leaves) == 2 do kind = .Split_H

	ratio := f32(mid) / f32(len(leaves))
	parent := tree_add_node(tree, kind, -1, ratio, 0.08)
	tree_set_children(tree, parent, left, right)
	return parent
}
