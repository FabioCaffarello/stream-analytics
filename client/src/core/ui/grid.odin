package ui

// Grid layout engine — computes panel rects from a weighted grid definition.
// Replaces hardcoded rect_cut_* sequences with data-driven layout.

import "core:math"

GRID_MAX_CELLS :: 12
GRID_MAX_COLS  :: 4
GRID_MAX_ROWS  :: 8

Grid_Cell :: struct {
	col:      int,
	row:      int,
	col_span: int,
	row_span: int,
	min_h:    f32,
}

Grid_Def :: struct {
	cells:       [GRID_MAX_CELLS]Grid_Cell,
	cell_count:  int,
	col_weights: [GRID_MAX_COLS]f32,
	col_count:   int,
	row_weights: [GRID_MAX_ROWS]f32,
	row_count:   int,
	gap:         f32,
}

Grid_Result :: struct {
	rects: [GRID_MAX_CELLS]Rect,
}

// Compute cell rects from a grid definition within given bounds.
compute_grid :: proc(def: Grid_Def, bounds: Rect) -> Grid_Result {
	result: Grid_Result
	if def.cell_count <= 0 || def.col_count <= 0 || def.row_count <= 0 do return result

	// Resolve column widths from weights.
	col_total_weight: f32 = 0
	for c in 0 ..< def.col_count {
		col_total_weight += def.col_weights[c]
	}
	if col_total_weight <= 0 do col_total_weight = 1

	total_col_gap := def.gap * f32(max(def.col_count - 1, 0))
	avail_w := bounds.size.x - total_col_gap
	if avail_w < 0 do avail_w = 0

	col_xs: [GRID_MAX_COLS]f32
	col_ws: [GRID_MAX_COLS]f32
	cx := bounds.pos.x
	for c in 0 ..< def.col_count {
		col_xs[c] = cx
		col_ws[c] = avail_w * (def.col_weights[c] / col_total_weight)
		cx += col_ws[c] + def.gap
	}

	// Resolve row heights from weights.
	row_total_weight: f32 = 0
	for r in 0 ..< def.row_count {
		row_total_weight += def.row_weights[r]
	}
	if row_total_weight <= 0 do row_total_weight = 1

	total_row_gap := def.gap * f32(max(def.row_count - 1, 0))
	avail_h := bounds.size.y - total_row_gap
	if avail_h < 0 do avail_h = 0

	row_ys: [GRID_MAX_ROWS]f32
	row_hs: [GRID_MAX_ROWS]f32
	ry := bounds.pos.y
	for r in 0 ..< def.row_count {
		row_ys[r] = ry
		row_hs[r] = avail_h * (def.row_weights[r] / row_total_weight)
		// Enforce minimum height.
		ry += row_hs[r] + def.gap
	}

	// Compute each cell's rect.
	for i in 0 ..< def.cell_count {
		cell := def.cells[i]
		c0 := clamp(cell.col, 0, def.col_count - 1)
		r0 := clamp(cell.row, 0, def.row_count - 1)
		cspan := max(cell.col_span, 1)
		rspan := max(cell.row_span, 1)
		c1 := min(c0 + cspan - 1, def.col_count - 1)
		r1 := min(r0 + rspan - 1, def.row_count - 1)

		x := col_xs[c0]
		y := row_ys[r0]
		w: f32 = 0
		for c in c0 ..= c1 {
			w += col_ws[c]
			if c < c1 do w += def.gap
		}
		h: f32 = 0
		for r in r0 ..= r1 {
			h += row_hs[r]
			if r < r1 do h += def.gap
		}

		// Apply min height.
		if cell.min_h > 0 && h < cell.min_h {
			h = math.min(cell.min_h, bounds.size.y)
		}

		result.rects[i] = Rect{pos = {x, y}, size = {w, h}}
	}

	return result
}

// Panel indices for the default 7-panel layout.
PANEL_CANDLE   :: 0
PANEL_STATS    :: 1
PANEL_COUNTER  :: 2
PANEL_HEATMAP  :: 3
PANEL_VPVR     :: 4
PANEL_TRADES   :: 5
PANEL_ORDERBOOK :: 6
PANEL_COUNT    :: 7

// Build the default desktop grid matching current hardcoded proportions.
build_default_grid :: proc(gap: f32) -> Grid_Def {
	def: Grid_Def
	def.gap = gap
	def.col_count = 2
	def.col_weights = {0.50, 0.50, 0, 0}
	def.row_count = 4
	def.row_weights = {0.40, 0.18, 0.22, 0.20, 0, 0, 0, 0}
	def.cell_count = PANEL_COUNT

	// Row 0: Candles (full width).
	def.cells[PANEL_CANDLE] = {col = 0, row = 0, col_span = 2, row_span = 1}
	// Row 1: Stats (left) + Counter (right).
	def.cells[PANEL_STATS]   = {col = 0, row = 1, col_span = 1, row_span = 1, min_h = 80}
	def.cells[PANEL_COUNTER] = {col = 1, row = 1, col_span = 1, row_span = 1, min_h = 80}
	// Row 2: Heatmap (left) + VPVR (right).
	def.cells[PANEL_HEATMAP] = {col = 0, row = 2, col_span = 1, row_span = 1, min_h = 110}
	def.cells[PANEL_VPVR]    = {col = 1, row = 2, col_span = 1, row_span = 1, min_h = 110}
	// Row 3: Trades (left) + Orderbook (right).
	def.cells[PANEL_TRADES]   = {col = 0, row = 3, col_span = 1, row_span = 1, min_h = 95}
	def.cells[PANEL_ORDERBOOK] = {col = 1, row = 3, col_span = 1, row_span = 1, min_h = 95}

	return def
}

// Build an auto-reflow grid for N cells (PRD-0007 M2).
// Maps cell count to an optimal cols x rows layout.
build_auto_grid :: proc(cell_count: int, gap: f32) -> Grid_Def {
	def: Grid_Def
	def.gap = gap
	n := clamp(cell_count, 1, GRID_MAX_CELLS)

	// Determine cols x rows from cell count.
	// NOTE: 3 cells uses 2x2 (not 3x1) so removing one cell from a 2x2 grid
	// doesn't collapse the entire second row.
	cols, rows: int
	switch {
	case n <= 1:  cols = 1; rows = 1
	case n <= 2:  cols = 2; rows = 1
	case n <= 4:  cols = 2; rows = 2
	case n <= 6:  cols = 3; rows = 2
	case n <= 9:  cols = 3; rows = 3
	case:         cols = 4; rows = 3
	}

	def.col_count = cols
	def.row_count = rows
	for c in 0 ..< cols {
		def.col_weights[c] = 1.0 / f32(cols)
	}
	for r in 0 ..< rows {
		def.row_weights[r] = 1.0 / f32(rows)
	}

	// Place cells left-to-right, top-to-bottom.
	def.cell_count = n
	for i in 0 ..< n {
		r := i / cols
		c := i % cols
		def.cells[i] = Grid_Cell{col = c, row = r, col_span = 1, row_span = 1}
	}

	// If last row has 1 cell and cols > 1, span it across all columns.
	last_row := (n - 1) / cols
	cells_in_last_row := n - last_row * cols
	if cells_in_last_row == 1 && cols > 1 {
		def.cells[n - 1].col_span = cols
	}

	return def
}

// Build a single-column mobile grid.
build_mobile_grid :: proc(gap: f32) -> Grid_Def {
	def: Grid_Def
	def.gap = gap
	def.col_count = 1
	def.col_weights = {1.0, 0, 0, 0}
	def.row_count = PANEL_COUNT
	// Taller chart, everything stacked.
	def.row_weights = {0.28, 0.10, 0.10, 0.14, 0.14, 0.12, 0.12, 0}
	def.cell_count = PANEL_COUNT

	for i in 0 ..< PANEL_COUNT {
		def.cells[i] = {col = 0, row = i, col_span = 1, row_span = 1}
	}
	return def
}

// Build a compare mode grid: 2-4 equal panels for side-by-side comparison.
build_compare_grid :: proc(count: int, gap: f32) -> Grid_Def {
	def: Grid_Def
	def.gap = gap
	n := clamp(count, 1, 4)

	switch n {
	case 1:
		def.col_count = 1
		def.col_weights = {1.0, 0, 0, 0}
		def.row_count = 1
		def.row_weights = {1.0, 0, 0, 0, 0, 0, 0, 0}
		def.cell_count = 1
		def.cells[0] = {col = 0, row = 0, col_span = 1, row_span = 1}
	case 2:
		def.col_count = 2
		def.col_weights = {0.5, 0.5, 0, 0}
		def.row_count = 1
		def.row_weights = {1.0, 0, 0, 0, 0, 0, 0, 0}
		def.cell_count = 2
		def.cells[0] = {col = 0, row = 0, col_span = 1, row_span = 1}
		def.cells[1] = {col = 1, row = 0, col_span = 1, row_span = 1}
	case 3:
		def.col_count = 3
		def.col_weights = {0.34, 0.33, 0.33, 0}
		def.row_count = 1
		def.row_weights = {1.0, 0, 0, 0, 0, 0, 0, 0}
		def.cell_count = 3
		def.cells[0] = {col = 0, row = 0, col_span = 1, row_span = 1}
		def.cells[1] = {col = 1, row = 0, col_span = 1, row_span = 1}
		def.cells[2] = {col = 2, row = 0, col_span = 1, row_span = 1}
	case 4:
		def.col_count = 2
		def.col_weights = {0.5, 0.5, 0, 0}
		def.row_count = 2
		def.row_weights = {0.5, 0.5, 0, 0, 0, 0, 0, 0}
		def.cell_count = 4
		def.cells[0] = {col = 0, row = 0, col_span = 1, row_span = 1}
		def.cells[1] = {col = 1, row = 0, col_span = 1, row_span = 1}
		def.cells[2] = {col = 0, row = 1, col_span = 1, row_span = 1}
		def.cells[3] = {col = 1, row = 1, col_span = 1, row_span = 1}
	}

	return def
}

// --- Layout Presets ---

LAYOUT_PRESET_COUNT :: 4
LAYOUT_PRESET_LABELS :: [LAYOUT_PRESET_COUNT]string{"Default", "Chart", "Analysis", "Compact"}

// Build a Chart Focus layout: Candle 70%, OB+Trades bottom 30%.
build_chart_focus_grid :: proc(gap: f32) -> Grid_Def {
	def: Grid_Def
	def.gap = gap
	def.col_count = 2
	def.col_weights = {0.50, 0.50, 0, 0}
	def.row_count = 2
	def.row_weights = {0.70, 0.30, 0, 0, 0, 0, 0, 0}
	def.cell_count = PANEL_COUNT
	// Row 0: Candles full width.
	def.cells[PANEL_CANDLE]    = {col = 0, row = 0, col_span = 2, row_span = 1}
	// Row 0 second half: stats/counter/heatmap/vpvr hidden (0-size, filtered out).
	def.cells[PANEL_STATS]     = {col = 0, row = 1, col_span = 1, row_span = 1, min_h = 80}
	def.cells[PANEL_COUNTER]   = {col = 1, row = 1, col_span = 1, row_span = 1, min_h = 80}
	def.cells[PANEL_HEATMAP]   = {col = 0, row = 1, col_span = 1, row_span = 1}
	def.cells[PANEL_VPVR]      = {col = 1, row = 1, col_span = 1, row_span = 1}
	// Row 1: Trades + Orderbook.
	def.cells[PANEL_TRADES]    = {col = 0, row = 1, col_span = 1, row_span = 1, min_h = 95}
	def.cells[PANEL_ORDERBOOK] = {col = 1, row = 1, col_span = 1, row_span = 1, min_h = 95}
	return def
}

// Chart Focus preset panel visibility: Candle + Trades + Orderbook only.
LAYOUT_CHART_FOCUS_VISIBLE :: [PANEL_COUNT]bool{true, false, false, false, false, true, true}

// Build an Analysis layout: Candle 50%, VPVR+Heatmap 25%, OB+Trades 25%.
build_analysis_grid :: proc(gap: f32) -> Grid_Def {
	def: Grid_Def
	def.gap = gap
	def.col_count = 2
	def.col_weights = {0.50, 0.50, 0, 0}
	def.row_count = 3
	def.row_weights = {0.50, 0.25, 0.25, 0, 0, 0, 0, 0}
	def.cell_count = PANEL_COUNT
	def.cells[PANEL_CANDLE]    = {col = 0, row = 0, col_span = 2, row_span = 1}
	def.cells[PANEL_STATS]     = {col = 0, row = 1, col_span = 1, row_span = 1}
	def.cells[PANEL_COUNTER]   = {col = 1, row = 1, col_span = 1, row_span = 1}
	def.cells[PANEL_HEATMAP]   = {col = 0, row = 1, col_span = 1, row_span = 1}
	def.cells[PANEL_VPVR]      = {col = 1, row = 1, col_span = 1, row_span = 1}
	def.cells[PANEL_TRADES]    = {col = 0, row = 2, col_span = 1, row_span = 1, min_h = 95}
	def.cells[PANEL_ORDERBOOK] = {col = 1, row = 2, col_span = 1, row_span = 1, min_h = 95}
	return def
}

// Analysis preset: Candle + Heatmap + VPVR + Trades + Orderbook.
LAYOUT_ANALYSIS_VISIBLE :: [PANEL_COUNT]bool{true, false, false, true, true, true, true}

// Build a Compact layout: Candle + Orderbook only.
build_compact_grid :: proc(gap: f32) -> Grid_Def {
	def: Grid_Def
	def.gap = gap
	def.col_count = 2
	def.col_weights = {0.65, 0.35, 0, 0}
	def.row_count = 1
	def.row_weights = {1.0, 0, 0, 0, 0, 0, 0, 0}
	def.cell_count = PANEL_COUNT
	def.cells[PANEL_CANDLE]    = {col = 0, row = 0, col_span = 1, row_span = 1}
	def.cells[PANEL_STATS]     = {col = 1, row = 0, col_span = 1, row_span = 1}
	def.cells[PANEL_COUNTER]   = {col = 1, row = 0, col_span = 1, row_span = 1}
	def.cells[PANEL_HEATMAP]   = {col = 1, row = 0, col_span = 1, row_span = 1}
	def.cells[PANEL_VPVR]      = {col = 1, row = 0, col_span = 1, row_span = 1}
	def.cells[PANEL_TRADES]    = {col = 1, row = 0, col_span = 1, row_span = 1}
	def.cells[PANEL_ORDERBOOK] = {col = 1, row = 0, col_span = 1, row_span = 1}
	return def
}

// Compact preset: Candle + Orderbook only.
LAYOUT_COMPACT_VISIBLE :: [PANEL_COUNT]bool{true, false, false, false, false, false, true}

// Get preset grid and visibility by index.
get_layout_preset :: proc(idx: int, gap: f32) -> (Grid_Def, [PANEL_COUNT]bool) {
	switch idx {
	case 1:
		return build_chart_focus_grid(gap), LAYOUT_CHART_FOCUS_VISIBLE
	case 2:
		return build_analysis_grid(gap), LAYOUT_ANALYSIS_VISIBLE
	case 3:
		return build_compact_grid(gap), LAYOUT_COMPACT_VISIBLE
	case:
		all_visible: [PANEL_COUNT]bool
		for i in 0 ..< PANEL_COUNT {
			all_visible[i] = true
		}
		// Default hides heatmap+vpvr panels (shown as overlays).
		all_visible[PANEL_HEATMAP] = false
		all_visible[PANEL_VPVR] = false
		return build_default_grid(gap), all_visible
	}
}

// Build a grid with some panels hidden (reduces cell_count, reflows remaining).
build_filtered_grid :: proc(base: Grid_Def, visible: [PANEL_COUNT]bool, gap: f32) -> Grid_Def {
	// Count visible panels.
	vis_count := 0
	for i in 0 ..< PANEL_COUNT {
		if visible[i] do vis_count += 1
	}
	if vis_count == 0 || vis_count == PANEL_COUNT do return base

	def := base
	def.gap = gap
	def.cell_count = 0

	row_used: [GRID_MAX_ROWS]bool
	col_used: [GRID_MAX_COLS]bool

	// Track rows/cols that are still referenced by visible panels.
	for panel_idx in 0 ..< PANEL_COUNT {
		if !visible[panel_idx] do continue
		cell := base.cells[panel_idx]
		c0 := clamp(cell.col, 0, base.col_count - 1)
		r0 := clamp(cell.row, 0, base.row_count - 1)
		c1 := min(c0 + max(cell.col_span, 1) - 1, base.col_count - 1)
		r1 := min(r0 + max(cell.row_span, 1) - 1, base.row_count - 1)
		for c in c0 ..= c1 {
			col_used[c] = true
		}
		for r in r0 ..= r1 {
			row_used[r] = true
		}
	}

	row_map: [GRID_MAX_ROWS]int
	col_map: [GRID_MAX_COLS]int
	for i in 0 ..< GRID_MAX_ROWS {
		row_map[i] = -1
	}
	for i in 0 ..< GRID_MAX_COLS {
		col_map[i] = -1
	}

	def.row_count = 0
	for r in 0 ..< base.row_count {
		if !row_used[r] do continue
		row_map[r] = def.row_count
		def.row_weights[def.row_count] = base.row_weights[r]
		def.row_count += 1
	}

	def.col_count = 0
	for c in 0 ..< base.col_count {
		if !col_used[c] do continue
		col_map[c] = def.col_count
		def.col_weights[def.col_count] = base.col_weights[c]
		def.col_count += 1
	}

	if def.row_count <= 0 || def.col_count <= 0 do return base

	// Rebuild visible cells preserving original panel order.
	for panel_idx in 0 ..< PANEL_COUNT {
		if !visible[panel_idx] do continue
		cell := base.cells[panel_idx]
		c0 := clamp(cell.col, 0, base.col_count - 1)
		r0 := clamp(cell.row, 0, base.row_count - 1)
		c1 := min(c0 + max(cell.col_span, 1) - 1, base.col_count - 1)
		r1 := min(r0 + max(cell.row_span, 1) - 1, base.row_count - 1)

		new_c0 := col_map[c0]
		new_r0 := row_map[r0]
		if new_c0 < 0 || new_r0 < 0 do continue

		new_c1 := new_c0
		for c in c0 ..= c1 {
			mapped := col_map[c]
			if mapped >= 0 && mapped > new_c1 do new_c1 = mapped
		}
		new_r1 := new_r0
		for r in r0 ..= r1 {
			mapped := row_map[r]
			if mapped >= 0 && mapped > new_r1 do new_r1 = mapped
		}

		def.cells[def.cell_count] = Grid_Cell{
			col      = new_c0,
			row      = new_r0,
			col_span = new_c1 - new_c0 + 1,
			row_span = new_r1 - new_r0 + 1,
			min_h    = cell.min_h,
		}
		def.cell_count += 1
	}

	if def.cell_count <= 0 do return base
	return def
}
