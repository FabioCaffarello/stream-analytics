package app

import "core:fmt"
import "mr:services"
import "mr:ui"

// S76: Portfolio page — positions & execution drilldown with tabbed views.
//
// Tabs:
//   Positions  — venue-grouped positions with stale detection + venue drilldown
//   Exposure   — exposure by venue + aggregated by symbol (cross-venue)
//   Fill Stats — fill summary metrics from account snapshot
//
// Data sources (all from S74 portfolio_data.odin, polled ~10s):
//   Portfolio_Summary_Result           → global equity, PnL, leverage, position count
//   Portfolio_Account_Snapshot_Result   → venue-grouped positions + per-venue equity
//
// The page renders from stores only — no business logic in the UI layer.

PORTFOLIO_PAD_X :: f32(16)
STALE_THRESHOLD_MS :: i64(300_000) // 5 minutes — position considered stale

// --- Page lifecycle ---

@(private = "package")
page_portfolio_enter :: proc(state: ^App_State) {
	// Trigger immediate fetch of summary (global) — always available.
	state.portfolio.summary_status = .Idle
	// S78: Trigger immediate fetch of trading readiness.
	state.portfolio.readiness_status = .Idle
	// Trigger account snapshot if account is configured.
	if state.portfolio.account_id_len > 0 {
		state.portfolio.snapshot_status = .Idle
	}
}

@(private = "package")
page_portfolio_leave :: proc(state: ^App_State) {
	// Keep portfolio state alive for background polling — don't clear.
}

// --- Page render ---

@(private = "package")
page_portfolio_render :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = workspace, color = ui.COL_SURFACE_1})

	x := workspace.pos.x + PORTFOLIO_PAD_X
	y := workspace.pos.y + 20
	content_w := workspace.size.x - PORTFOLIO_PAD_X * 2
	if content_w < 100 do content_w = 100
	bottom := workspace.pos.y + workspace.size.y

	// --- Header ---
	ui.push_text(&state.cmd_buf, {x, y}, "Portfolio",
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_LG, .Bold)

	// Connection badge (right-aligned).
	conn_disp := current_conn_status_display(state)
	badge_w := ui.status_badge_width(conn_disp.label, state.text.measure, ui.FONT_SIZE_XS)
	badge_x := x + content_w - badge_w
	ui.status_badge(&state.cmd_buf,
		ui.rect_xywh(badge_x, y - 4, badge_w, f32(16)),
		conn_disp.label, conn_disp.dot_color, conn_disp.text_color, state.text.measure, ui.FONT_SIZE_XS)
	y += 24

	// --- Fetch status gate (summary is the primary store) ---
	pf := &state.portfolio
	if pf.summary_status == .Idle {
		ui.push_text(&state.cmd_buf, {x, y + 10}, "Loading...",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}
	if pf.summary_status == .Error {
		ui.push_text(&state.cmd_buf, {x, y + 10},
			"Failed to load portfolio data. Check backend connection.",
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		y += 16
		retry_rect := ui.rect_xywh(x, y + 4, f32(60), f32(18))
		retry_hov := ui.rect_contains(retry_rect, pointer.pos)
		ui.push_text(&state.cmd_buf, {x + 4, y + 16}, "Retry",
			retry_hov ? ui.COL_ACCENT_CYAN : ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		if retry_hov && pointer.left_pressed {
			pf.summary_status = .Idle
		}
		return
	}

	// --- Divider ---
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x, y}, to = {x + content_w, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 8

	// --- Summary cards ---
	y = draw_portfolio_summary_cards(state, &pf.summary, x, y, content_w)
	if y > bottom - 20 do return

	// --- Exposure divergence warning ---
	if pf.snapshot_status == .Success && services.portfolio_has_exposure_divergence(&pf.snapshot) {
		ui.push_text(&state.cmd_buf, {x + 4, y + 10},
			"WARN: Opposing positions detected on same symbol across venues",
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Bold)
		y += 18
	}

	// --- Tab bar ---
	y = draw_portfolio_tab_bar(state, x, y, content_w, pointer)
	if y > bottom - 20 do return

	// --- Tab content ---
	// S78: Readiness tab has its own data source (readiness store, not snapshot).
	if pf.active_tab == .Readiness {
		y = draw_portfolio_readiness(state, x, y, content_w, bottom)
	} else if pf.snapshot_status == .Success {
		#partial switch pf.active_tab {
		case .Positions:
			y = draw_portfolio_positions(state, &pf.snapshot, x, y, content_w, bottom, pointer)
		case .Exposure:
			y = draw_portfolio_exposure(state, &pf.snapshot, x, y, content_w, bottom)
		case .Fill_Stats:
			y = draw_portfolio_fill_stats(state, &pf.snapshot, x, y, content_w, bottom)
		}
	} else if pf.snapshot_status == .Error {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10},
			"Account snapshot unavailable",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	} else if pf.summary.total_position_count > 0 {
		pos_buf: [32]u8
		pos_str := fmt.bprintf(pos_buf[:], "%d positions loading...", pf.summary.total_position_count)
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, pos_str,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}
}

// --- Tab bar ---

@(private = "file")
draw_portfolio_tab_bar :: proc(
	state: ^App_State, x, y_in, content_w: f32, pointer: ui.Pointer_Input,
) -> f32 {
	y := y_in
	pf := &state.portfolio

	Tab_Def :: struct { label: string, tab: Portfolio_Tab }
	tabs := [4]Tab_Def{
		{"Positions", .Positions},
		{"Exposure", .Exposure},
		{"Fill Stats", .Fill_Stats},
		{"Readiness", .Readiness},
	}

	tab_x := x
	TAB_PAD :: f32(12)
	TAB_H :: f32(22)

	for ti in 0 ..< 4 {
		td := tabs[ti]
		tw := f32(len(td.label)) * 7 + TAB_PAD * 2
		tab_rect := ui.rect_xywh(tab_x, y, tw, TAB_H)
		is_active := pf.active_tab == td.tab
		is_hov := ui.rect_contains(tab_rect, pointer.pos)

		// Background.
		bg_color := is_active ? ui.COL_SURFACE_0 : (is_hov ? ui.COL_SURFACE_2 : ui.COL_SURFACE_1)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = tab_rect, color = bg_color})

		// Underline for active tab.
		if is_active {
			ui.push(&state.cmd_buf, ui.Cmd_Line{
				from = {tab_x, y + TAB_H - 1}, to = {tab_x + tw, y + TAB_H - 1},
				color = ui.COL_ACCENT_CYAN, thickness = 2,
			})
		}

		// Label.
		text_color := is_active ? ui.COL_ACCENT_CYAN : (is_hov ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_MUTED)
		ui.push_text(&state.cmd_buf, {tab_x + TAB_PAD, y + 14}, td.label,
			text_color, ui.FONT_SIZE_XS, .Bold)

		// Click.
		if is_hov && pointer.left_pressed {
			pf.active_tab = td.tab
			// S80: Persist portfolio tab for restore on restart.
			tab_buf: [4]u8
			tab_buf[0] = '0' + u8(td.tab)
			services.settings_set(&state.settings, services.SETTING_PORTFOLIO_TAB, string(tab_buf[:1]))
			services.settings_flush(&state.settings)
		}

		tab_x += tw + 4
	}

	// Venue filter indicator (right-aligned).
	if pf.venue_filter_len > 0 {
		filter_label := string(pf.venue_filter[:int(pf.venue_filter_len)])
		clear_label := "x"
		filter_x := x + content_w - f32(len(filter_label)) * 7 - 30
		ui.push_text(&state.cmd_buf, {filter_x, y + 14}, filter_label,
			ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Mono)
		// Clear button.
		clear_rect := ui.rect_xywh(filter_x + f32(len(filter_label)) * 7 + 6, y + 2, f32(16), f32(16))
		clear_hov := ui.rect_contains(clear_rect, pointer.pos)
		ui.push_text(&state.cmd_buf, {clear_rect.pos.x + 4, y + 14}, clear_label,
			clear_hov ? ui.COL_RED : ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		if clear_hov && pointer.left_pressed {
			pf.venue_filter_len = 0
		}
	}

	y += TAB_H + 8
	return y
}

// --- Summary cards: Equity / Realized PnL / Unrealized PnL / Leverage ---

@(private = "file")
draw_portfolio_summary_cards :: proc(
	state: ^App_State, summary: ^services.Portfolio_Summary_Result,
	x, y_in, content_w: f32,
) -> f32 {
	y := y_in

	// 4 cards in a row. Each card: label (muted) + value.
	CARD_COUNT :: 4
	card_w := (content_w - f32(CARD_COUNT - 1) * 12) / f32(CARD_COUNT)
	if card_w < 80 do card_w = 80

	Card :: struct { label: string, value_buf: [32]u8, value_len: int, color: ui.Color }
	cards: [CARD_COUNT]Card

	// Card 0: Equity
	cards[0].label = "Equity"
	cards[0].value_len = len(fmt.bprintf(cards[0].value_buf[:], "$%.2f", summary.global_equity_usd))
	cards[0].color = ui.COL_TEXT_PRIMARY

	// Card 1: Realized PnL
	cards[1].label = "Realized PnL"
	cards[1].value_len = len(fmt.bprintf(cards[1].value_buf[:], "$%.2f", summary.global_realized_usd))
	cards[1].color = pnl_color(summary.global_realized_usd)

	// Card 2: Unrealized PnL
	cards[2].label = "Unrealized PnL"
	cards[2].value_len = len(fmt.bprintf(cards[2].value_buf[:], "$%.2f", summary.global_unrealized_usd))
	cards[2].color = pnl_color(summary.global_unrealized_usd)

	// Card 3: Leverage
	cards[3].label = "Leverage"
	cards[3].value_len = len(fmt.bprintf(cards[3].value_buf[:], "%.2fx", summary.global_leverage))
	cards[3].color = leverage_color(summary.global_leverage)

	for ci in 0 ..< CARD_COUNT {
		cx := x + f32(ci) * (card_w + 12)

		// Card background.
		card_rect := ui.rect_xywh(cx, y, card_w, f32(52))
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = card_rect, color = ui.COL_SURFACE_0})

		// Label.
		ui.push_text(&state.cmd_buf, {cx + 8, y + 14}, cards[ci].label,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

		// Value.
		value_str := string(cards[ci].value_buf[:cards[ci].value_len])
		ui.push_text(&state.cmd_buf, {cx + 8, y + 36}, value_str,
			cards[ci].color, ui.FONT_SIZE_MD, .Mono)
	}

	y += 60

	// Position count + account count summary row.
	meta_buf: [64]u8
	meta_str := fmt.bprintf(meta_buf[:], "%d positions  %d accounts  %d open orders",
		summary.total_position_count, summary.account_count, summary.total_open_orders)
	ui.push_text(&state.cmd_buf, {x + 4, y + 10}, meta_str,
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	y += 24
	return y
}

// --- Positions table (venue-grouped) with stale detection and venue drilldown ---

@(private = "file")
draw_portfolio_positions :: proc(
	state: ^App_State, snapshot: ^services.Portfolio_Account_Snapshot_Result,
	x, y_in, content_w, bottom: f32, pointer: ui.Pointer_Input,
) -> f32 {
	y := y_in
	pf := &state.portfolio
	venue_filter := pf.venue_filter_len > 0 ? string(pf.venue_filter[:int(pf.venue_filter_len)]) : ""

	if snapshot.venue_count == 0 {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10},
			"No open positions",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return y + 22
	}

	// Section header.
	ui.push_text(&state.cmd_buf, {x, y + 10}, "POSITIONS", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	// Column headers.
	COL_SYMBOL   :: f32(12)
	COL_SIDE     :: f32(120)
	COL_QTY      :: f32(180)
	COL_ENTRY    :: f32(280)
	COL_NOTIONAL :: f32(380)
	COL_UPNL     :: f32(480)
	COL_RPNL     :: f32(570)
	COL_STATUS   :: f32(660)

	hdr_y := y + 10
	ui.push_text(&state.cmd_buf, {x + COL_SYMBOL, hdr_y}, "Symbol", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + COL_SIDE, hdr_y}, "Side", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + COL_QTY, hdr_y}, "Qty", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + COL_ENTRY, hdr_y}, "Entry", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + COL_NOTIONAL, hdr_y}, "Notional", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + COL_UPNL, hdr_y}, "Unrlzd PnL", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + COL_RPNL, hdr_y}, "Rlzd PnL", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	if content_w > 700 {
		ui.push_text(&state.cmd_buf, {x + COL_STATUS, hdr_y}, "Status", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	}
	y += 18

	// Divider below headers.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x + 4, y}, to = {x + content_w - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 4

	// Approximate now_ms from projected_at_ms (backend timestamp as reference).
	now_ms := snapshot.projected_at_ms

	// Render venue groups.
	for vi in 0 ..< snapshot.venue_count {
		if y + 30 > bottom do break
		venue := &snapshot.venues[vi]
		venue_label := len(venue.venue) > 0 ? venue.venue : "unknown"

		// Apply venue filter.
		if len(venue_filter) > 0 && venue_label != venue_filter do continue

		// Venue header — clickable for drilldown.
		venue_rect := ui.rect_xywh(x, y, content_w, f32(18))
		venue_hov := ui.rect_contains(venue_rect, pointer.pos)
		label_color := venue_hov ? ui.COL_TEXT_PRIMARY : ui.COL_ACCENT_CYAN
		ui.push_text(&state.cmd_buf, {x + 4, y + 10}, venue_label,
			label_color, ui.FONT_SIZE_XS, .Bold)

		// Click venue header to toggle filter.
		if venue_hov && pointer.left_pressed {
			if len(venue_filter) > 0 {
				// Already filtered — clear.
				pf.venue_filter_len = 0
			} else {
				// Set filter to this venue.
				n := min(len(venue_label), len(pf.venue_filter))
				for i in 0 ..< n {
					pf.venue_filter[i] = venue_label[i]
				}
				pf.venue_filter_len = u8(n)
			}
		}

		// Venue equity + margin (right of venue name).
		eq_buf: [48]u8
		eq_str := fmt.bprintf(eq_buf[:], "$%.2f  margin: $%.2f", venue.equity_usd, venue.margin_used_usd)
		ui.push_text(&state.cmd_buf, {x + COL_SIDE, y + 10}, eq_str,
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		y += 18

		// Position rows.
		for pi in 0 ..< venue.position_count {
			if y + 16 > bottom do break
			pos := &venue.positions[pi]
			row_y := y + 10

			// Stale detection.
			is_stale := services.portfolio_position_is_stale(pos, now_ms, STALE_THRESHOLD_MS)

			// Symbol.
			sym := len(pos.symbol) > 0 ? pos.symbol : "?"
			sym_color := is_stale ? ui.COL_WARNING : ui.COL_TEXT_PRIMARY
			ui.push_text(&state.cmd_buf, {x + COL_SYMBOL, row_y}, sym,
				sym_color, ui.FONT_SIZE_XS, .Mono)

			// Side.
			side := len(pos.side) > 0 ? pos.side : "?"
			side_color := side == "long" ? ui.COL_GREEN : (side == "short" ? ui.COL_RED : ui.COL_TEXT_SECONDARY)
			ui.push_text(&state.cmd_buf, {x + COL_SIDE, row_y}, side,
				side_color, ui.FONT_SIZE_XS, .Mono)

			// Qty.
			qty_buf: [24]u8
			qty_str := fmt.bprintf(qty_buf[:], "%.4f", pos.quantity)
			ui.push_text(&state.cmd_buf, {x + COL_QTY, row_y}, qty_str,
				ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

			// Entry price.
			entry_buf: [24]u8
			entry_str := fmt.bprintf(entry_buf[:], "%.2f", pos.avg_entry_price)
			ui.push_text(&state.cmd_buf, {x + COL_ENTRY, row_y}, entry_str,
				ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

			// Notional.
			not_buf: [24]u8
			not_str := fmt.bprintf(not_buf[:], "$%.2f", pos.notional_usd)
			ui.push_text(&state.cmd_buf, {x + COL_NOTIONAL, row_y}, not_str,
				ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

			// Unrealized PnL.
			upnl_buf: [24]u8
			upnl_str := fmt.bprintf(upnl_buf[:], "$%.2f", pos.unrealized_pnl)
			ui.push_text(&state.cmd_buf, {x + COL_UPNL, row_y}, upnl_str,
				pnl_color(pos.unrealized_pnl), ui.FONT_SIZE_XS, .Mono)

			// Realized PnL.
			rpnl_buf: [24]u8
			rpnl_str := fmt.bprintf(rpnl_buf[:], "$%.2f", pos.realized_pnl)
			ui.push_text(&state.cmd_buf, {x + COL_RPNL, row_y}, rpnl_str,
				pnl_color(pos.realized_pnl), ui.FONT_SIZE_XS, .Mono)

			// Status column (stale indicator).
			if content_w > 700 {
				if is_stale {
					ui.push_text(&state.cmd_buf, {x + COL_STATUS, row_y}, "STALE",
						ui.COL_WARNING, ui.FONT_SIZE_XS, .Bold)
				} else {
					ui.push_text(&state.cmd_buf, {x + COL_STATUS, row_y}, "LIVE",
						ui.COL_GREEN, ui.FONT_SIZE_XS, .Mono)
				}
			}

			y += 16
		}

		y += 6 // spacing between venue groups
	}

	return y
}

// --- Exposure tab ---

@(private = "file")
draw_portfolio_exposure :: proc(
	state: ^App_State, snapshot: ^services.Portfolio_Account_Snapshot_Result,
	x, y_in, content_w, bottom: f32,
) -> f32 {
	y := y_in

	// Section 1: Exposure by venue.
	ui.push_text(&state.cmd_buf, {x, y + 10}, "EXPOSURE BY VENUE", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	ECOL_VENUE    :: f32(12)
	ECOL_EQUITY   :: f32(120)
	ECOL_MARGIN   :: f32(250)
	ECOL_UPNL     :: f32(380)
	ECOL_POS      :: f32(480)

	hdr_y := y + 10
	ui.push_text(&state.cmd_buf, {x + ECOL_VENUE, hdr_y}, "Venue", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + ECOL_EQUITY, hdr_y}, "Equity", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + ECOL_MARGIN, hdr_y}, "Margin Used", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + ECOL_UPNL, hdr_y}, "Unrlzd PnL", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + ECOL_POS, hdr_y}, "Positions", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x + 4, y}, to = {x + content_w - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 4

	for vi in 0 ..< snapshot.venue_count {
		if y + 16 > bottom do break
		venue := &snapshot.venues[vi]
		row_y := y + 10

		venue_label := len(venue.venue) > 0 ? venue.venue : "unknown"
		ui.push_text(&state.cmd_buf, {x + ECOL_VENUE, row_y}, venue_label,
			ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Bold)

		eq_buf: [24]u8
		eq_str := fmt.bprintf(eq_buf[:], "$%.2f", venue.equity_usd)
		ui.push_text(&state.cmd_buf, {x + ECOL_EQUITY, row_y}, eq_str,
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)

		mg_buf: [24]u8
		mg_str := fmt.bprintf(mg_buf[:], "$%.2f", venue.margin_used_usd)
		ui.push_text(&state.cmd_buf, {x + ECOL_MARGIN, row_y}, mg_str,
			venue.margin_used_usd > 0 ? ui.COL_TEXT_SECONDARY : ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

		upnl_buf: [24]u8
		upnl_str := fmt.bprintf(upnl_buf[:], "$%.2f", venue.unrealized_pnl_usd)
		ui.push_text(&state.cmd_buf, {x + ECOL_UPNL, row_y}, upnl_str,
			pnl_color(venue.unrealized_pnl_usd), ui.FONT_SIZE_XS, .Mono)

		pos_buf: [16]u8
		pos_str := fmt.bprintf(pos_buf[:], "%d", venue.position_count)
		ui.push_text(&state.cmd_buf, {x + ECOL_POS, row_y}, pos_str,
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

		y += 16
	}

	y += 16

	// Section 2: Exposure by symbol (cross-venue aggregation).
	ui.push_text(&state.cmd_buf, {x, y + 10}, "EXPOSURE BY SYMBOL", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	SCOL_SYMBOL   :: f32(12)
	SCOL_NETQTY   :: f32(120)
	SCOL_NOTIONAL :: f32(250)
	SCOL_VENUES   :: f32(380)

	shdr_y := y + 10
	ui.push_text(&state.cmd_buf, {x + SCOL_SYMBOL, shdr_y}, "Symbol", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + SCOL_NETQTY, shdr_y}, "Net Qty", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + SCOL_NOTIONAL, shdr_y}, "Gross Notional", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + SCOL_VENUES, shdr_y}, "Venues", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x + 4, y}, to = {x + content_w - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 4

	// Compute symbol exposures.
	sym_exposures: [services.PORTFOLIO_SYMBOL_EXPOSURE_CAP]services.Portfolio_Symbol_Exposure
	sym_count := services.portfolio_compute_symbol_exposures(snapshot, &sym_exposures)

	if sym_count == 0 {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10},
			"No symbol exposures",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 22
	} else {
		for si in 0 ..< sym_count {
			if y + 16 > bottom do break
			se := &sym_exposures[si]
			row_y := y + 10

			sym := len(se.symbol) > 0 ? se.symbol : "?"
			ui.push_text(&state.cmd_buf, {x + SCOL_SYMBOL, row_y}, sym,
				ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)

			// Net qty — colored by direction.
			nq_buf: [24]u8
			nq_str := fmt.bprintf(nq_buf[:], "%.4f", se.net_qty)
			nq_color := se.net_qty > 0 ? ui.COL_GREEN : (se.net_qty < 0 ? ui.COL_RED : ui.COL_TEXT_MUTED)
			ui.push_text(&state.cmd_buf, {x + SCOL_NETQTY, row_y}, nq_str,
				nq_color, ui.FONT_SIZE_XS, .Mono)

			gn_buf: [24]u8
			gn_str := fmt.bprintf(gn_buf[:], "$%.2f", se.gross_notional_usd)
			ui.push_text(&state.cmd_buf, {x + SCOL_NOTIONAL, row_y}, gn_str,
				ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

			vc_buf: [8]u8
			vc_str := fmt.bprintf(vc_buf[:], "%d", se.venue_count)
			// Highlight if same symbol appears on >1 venue.
			vc_color := se.venue_count > 1 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
			ui.push_text(&state.cmd_buf, {x + SCOL_VENUES, row_y}, vc_str,
				vc_color, ui.FONT_SIZE_XS, .Mono)

			y += 16
		}
	}

	return y
}

// --- Fill Stats tab ---

@(private = "file")
draw_portfolio_fill_stats :: proc(
	state: ^App_State, snapshot: ^services.Portfolio_Account_Snapshot_Result,
	x, y_in, content_w, bottom: f32,
) -> f32 {
	y := y_in
	fs := &snapshot.fill_summary

	ui.push_text(&state.cmd_buf, {x, y + 10}, "FILL STATISTICS", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 24

	// Metric cards: 3 columns x 3 rows.
	METRIC_W :: f32(200)
	METRIC_H :: f32(52)
	METRIC_GAP :: f32(12)

	Metric :: struct { label: string, value_buf: [32]u8, value_len: int, color: ui.Color }
	metrics: [9]Metric

	metrics[0].label = "Total Trades"
	metrics[0].value_len = len(fmt.bprintf(metrics[0].value_buf[:], "%d", fs.total_trade_count))
	metrics[0].color = ui.COL_TEXT_PRIMARY

	metrics[1].label = "Win Count"
	metrics[1].value_len = len(fmt.bprintf(metrics[1].value_buf[:], "%d", fs.win_count))
	metrics[1].color = ui.COL_GREEN

	metrics[2].label = "Loss Count"
	metrics[2].value_len = len(fmt.bprintf(metrics[2].value_buf[:], "%d", fs.loss_count))
	metrics[2].color = ui.COL_RED

	metrics[3].label = "Volume Traded"
	metrics[3].value_len = len(fmt.bprintf(metrics[3].value_buf[:], "$%.2f", fs.total_volume_traded_usd))
	metrics[3].color = ui.COL_TEXT_PRIMARY

	metrics[4].label = "Largest Win"
	metrics[4].value_len = len(fmt.bprintf(metrics[4].value_buf[:], "$%.2f", fs.largest_win_usd))
	metrics[4].color = ui.COL_GREEN

	metrics[5].label = "Largest Loss"
	metrics[5].value_len = len(fmt.bprintf(metrics[5].value_buf[:], "$%.2f", fs.largest_loss_usd))
	metrics[5].color = ui.COL_RED

	metrics[6].label = "Turnover"
	metrics[6].value_len = len(fmt.bprintf(metrics[6].value_buf[:], "$%.2f", fs.turnover_usd))
	metrics[6].color = ui.COL_TEXT_PRIMARY

	// Win rate.
	total := fs.win_count + fs.loss_count
	win_rate: f64 = 0
	if total > 0 {
		win_rate = f64(fs.win_count) / f64(total) * 100.0
	}
	metrics[7].label = "Win Rate"
	metrics[7].value_len = len(fmt.bprintf(metrics[7].value_buf[:], "%.1f%%", win_rate))
	metrics[7].color = win_rate >= 50.0 ? ui.COL_GREEN : ui.COL_RED

	// Expectancy (win-weighted).
	metrics[8].label = "Avg Win/Loss"
	if fs.win_count > 0 && fs.loss_count > 0 {
		avg_win := fs.largest_win_usd // approximation (we only have largest, not average)
		avg_loss := fs.largest_loss_usd
		metrics[8].value_len = len(fmt.bprintf(metrics[8].value_buf[:], "%.2f", avg_win / (avg_loss > 0 ? avg_loss : 1.0)))
		ratio := avg_win / (avg_loss > 0 ? avg_loss : 1.0)
		metrics[8].color = ratio >= 1.0 ? ui.COL_GREEN : ui.COL_RED
	} else {
		metrics[8].value_len = len(fmt.bprintf(metrics[8].value_buf[:], "-"))
		metrics[8].color = ui.COL_TEXT_MUTED
	}

	cols :: 3
	for mi in 0 ..< 9 {
		if y + METRIC_H > bottom do break
		col := mi % cols
		row := mi / cols
		mx := x + f32(col) * (METRIC_W + METRIC_GAP)
		my := y + f32(row) * (METRIC_H + METRIC_GAP)

		// Card background.
		card_rect := ui.rect_xywh(mx, my, METRIC_W, METRIC_H)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = card_rect, color = ui.COL_SURFACE_0})

		// Label.
		ui.push_text(&state.cmd_buf, {mx + 8, my + 14}, metrics[mi].label,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

		// Value.
		value_str := string(metrics[mi].value_buf[:metrics[mi].value_len])
		ui.push_text(&state.cmd_buf, {mx + 8, my + 36}, value_str,
			metrics[mi].color, ui.FONT_SIZE_MD, .Mono)
	}

	// Advance y past the grid.
	row_count := (8 / cols) + 1 // 9 metrics / 3 cols = 3 rows
	y += f32(row_count) * (METRIC_H + METRIC_GAP) + 8

	// Per-venue fill breakdown (recent fills proxy: position trade counts).
	y += 8
	ui.push_text(&state.cmd_buf, {x, y + 10}, "RECENT FILLS BY VENUE", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	RF_SYMBOL :: f32(12)
	RF_VENUE  :: f32(120)
	RF_TRADES :: f32(220)
	RF_VOLUME :: f32(320)

	rfhdr_y := y + 10
	ui.push_text(&state.cmd_buf, {x + RF_SYMBOL, rfhdr_y}, "Symbol", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + RF_VENUE, rfhdr_y}, "Venue", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + RF_TRADES, rfhdr_y}, "Trades", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	ui.push_text(&state.cmd_buf, {x + RF_VOLUME, rfhdr_y}, "Volume", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x + 4, y}, to = {x + content_w - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 4

	for vi in 0 ..< snapshot.venue_count {
		venue := &snapshot.venues[vi]
		for pi in 0 ..< venue.position_count {
			if y + 16 > bottom do break
			pos := &venue.positions[pi]
			if pos.trade_count <= 0 do continue
			row_y := y + 10

			sym := len(pos.symbol) > 0 ? pos.symbol : "?"
			ui.push_text(&state.cmd_buf, {x + RF_SYMBOL, row_y}, sym,
				ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)

			vlabel := len(venue.venue) > 0 ? venue.venue : "?"
			ui.push_text(&state.cmd_buf, {x + RF_VENUE, row_y}, vlabel,
				ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Mono)

			tc_buf: [16]u8
			tc_str := fmt.bprintf(tc_buf[:], "%d", pos.trade_count)
			ui.push_text(&state.cmd_buf, {x + RF_TRADES, row_y}, tc_str,
				ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

			vol_buf: [24]u8
			vol_str := fmt.bprintf(vol_buf[:], "$%.2f", pos.volume_traded_usd)
			ui.push_text(&state.cmd_buf, {x + RF_VOLUME, row_y}, vol_str,
				ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

			y += 16
		}
	}

	return y
}

// --- Readiness tab (S78) ---

@(private = "file")
draw_portfolio_readiness :: proc(
	state: ^App_State, x, y_in, content_w, bottom: f32,
) -> f32 {
	y := y_in
	pf := &state.portfolio

	if pf.readiness_status == .Idle {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, "Loading readiness...",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return y + 22
	}
	if pf.readiness_status == .Error {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10},
			"Readiness unavailable. Backend may not support this endpoint.",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return y + 22
	}

	rd := &pf.readiness

	// --- Section 1: Control Plane State ---
	ui.push_text(&state.cmd_buf, {x, y + 10}, "CONTROL PLANE",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	// State badge.
	state_label := rd.control_plane.state
	state_color := readiness_state_color(state_label)
	ui.push_text(&state.cmd_buf, {x + 12, y + 10}, "State:",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	ui.push_text(&state.cmd_buf, {x + 80, y + 10}, state_label,
		state_color, ui.FONT_SIZE_XS, .Bold)
	y += 16

	// Simulation profile.
	if len(rd.control_plane.simulation_profile) > 0 {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, "Simulation:",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		ui.push_text(&state.cmd_buf, {x + 110, y + 10}, rd.control_plane.simulation_profile,
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		y += 16
	}

	// Disabled strategies.
	if rd.control_plane.disabled_strategy_count > 0 {
		ds_buf: [64]u8
		ds_str := fmt.bprintf(ds_buf[:], "Disabled Strategies: %d", rd.control_plane.disabled_strategy_count)
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, ds_str,
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		y += 16
		for si in 0 ..< rd.control_plane.disabled_strategy_count {
			if y + 16 > bottom do break
			ui.push_text(&state.cmd_buf, {x + 24, y + 10}, rd.control_plane.disabled_strategies[si],
				ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
			y += 14
		}
	}

	// Disabled adapters.
	if rd.control_plane.disabled_adapter_count > 0 {
		da_buf: [64]u8
		da_str := fmt.bprintf(da_buf[:], "Disabled Adapters: %d", rd.control_plane.disabled_adapter_count)
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, da_str,
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		y += 16
		for ai in 0 ..< rd.control_plane.disabled_adapter_count {
			if y + 16 > bottom do break
			ui.push_text(&state.cmd_buf, {x + 24, y + 10}, rd.control_plane.disabled_adapters[ai],
				ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
			y += 14
		}
	}

	// Allowlist restrictions.
	if rd.control_plane.allowlist_restricted {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, "VENUE RESTRICTED",
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Bold)
		y += 16
		for vi in 0 ..< rd.control_plane.restricted_venue_count {
			if y + 16 > bottom do break
			ui.push_text(&state.cmd_buf, {x + 24, y + 10}, rd.control_plane.restricted_venues[vi],
				ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Mono)
			y += 14
		}
	}

	y += 8
	if y + 20 > bottom do return y

	// --- Divider ---
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x + 4, y}, to = {x + content_w - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 8

	// --- Section 2: Safety Flags ---
	ui.push_text(&state.cmd_buf, {x, y + 10}, "SAFETY FLAGS",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	if rd.flag_count == 0 || (rd.flag_count == 1 && rd.safety_flags[0] == "clear") {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, "All clear",
			ui.COL_GREEN, ui.FONT_SIZE_XS, .Bold)
		y += 18
	} else {
		for fi in 0 ..< rd.flag_count {
			if y + 16 > bottom do break
			flag := rd.safety_flags[fi]
			flag_color := readiness_flag_color(flag)
			ui.push_text(&state.cmd_buf, {x + 12, y + 10}, flag,
				flag_color, ui.FONT_SIZE_XS, .Bold)
			y += 16
		}
	}

	y += 8
	if y + 20 > bottom do return y

	// --- Divider ---
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x + 4, y}, to = {x + content_w - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 8

	// --- Section 3: Account/Venue Readiness ---
	ui.push_text(&state.cmd_buf, {x, y + 10}, "VENUE READINESS",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	if rd.account_count == 0 {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, "No accounts",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return y + 22
	}

	// Column headers.
	RCOL_VENUE    :: f32(12)
	RCOL_STATUS   :: f32(120)
	RCOL_EQUITY   :: f32(240)
	RCOL_POS      :: f32(350)
	RCOL_STALE    :: f32(430)

	for ai in 0 ..< rd.account_count {
		if y + 30 > bottom do break
		acct := &rd.accounts[ai]

		// Account header.
		acct_buf: [80]u8
		acct_str := fmt.bprintf(acct_buf[:], "Account: %s", acct.account_id)
		ui.push_text(&state.cmd_buf, {x + 4, y + 10}, acct_str,
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)

		// Account stale indicator.
		if acct.stale {
			ui.push_text(&state.cmd_buf, {x + 300, y + 10}, "STALE",
				ui.COL_WARNING, ui.FONT_SIZE_XS, .Bold)
		}
		y += 18

		// Column headers for venues.
		hdr_y := y + 10
		ui.push_text(&state.cmd_buf, {x + RCOL_VENUE, hdr_y}, "Venue",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		ui.push_text(&state.cmd_buf, {x + RCOL_STATUS, hdr_y}, "Status",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		ui.push_text(&state.cmd_buf, {x + RCOL_EQUITY, hdr_y}, "Equity",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		ui.push_text(&state.cmd_buf, {x + RCOL_POS, hdr_y}, "Positions",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		if content_w > 500 {
			ui.push_text(&state.cmd_buf, {x + RCOL_STALE, hdr_y}, "Freshness",
				ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		}
		y += 18

		// Divider.
		ui.push(&state.cmd_buf, ui.Cmd_Line{
			from = {x + 8, y}, to = {x + content_w - 8, y},
			color = ui.COL_DIVIDER, thickness = 1,
		})
		y += 4

		// Venue rows.
		for vi in 0 ..< acct.venue_count {
			if y + 16 > bottom do break
			venue := &acct.venues[vi]
			row_y := y + 10

			// Venue name.
			venue_label := len(venue.venue) > 0 ? venue.venue : "unknown"
			venue_color := venue.restricted ? ui.COL_TEXT_MUTED : ui.COL_ACCENT_CYAN
			ui.push_text(&state.cmd_buf, {x + RCOL_VENUE, row_y}, venue_label,
				venue_color, ui.FONT_SIZE_XS, .Bold)

			// Trading status badge.
			status_label := services.trading_status_label(venue.trading_status)
			status_color := readiness_trading_status_color(venue.trading_status)
			ui.push_text(&state.cmd_buf, {x + RCOL_STATUS, row_y}, status_label,
				status_color, ui.FONT_SIZE_XS, .Bold)

			// Restricted indicator after status.
			if venue.restricted {
				ui.push_text(&state.cmd_buf, {x + RCOL_STATUS + f32(len(status_label)) * 7 + 8, row_y},
					"[R]", ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
			}

			// Equity.
			eq_buf: [24]u8
			eq_str := fmt.bprintf(eq_buf[:], "$%.2f", venue.equity_usd)
			ui.push_text(&state.cmd_buf, {x + RCOL_EQUITY, row_y}, eq_str,
				ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

			// Position count.
			pos_buf: [16]u8
			pos_str := fmt.bprintf(pos_buf[:], "%d", venue.position_count)
			ui.push_text(&state.cmd_buf, {x + RCOL_POS, row_y}, pos_str,
				ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

			// Freshness.
			if content_w > 500 {
				if venue.stale {
					ui.push_text(&state.cmd_buf, {x + RCOL_STALE, row_y}, "STALE",
						ui.COL_WARNING, ui.FONT_SIZE_XS, .Bold)
				} else {
					ui.push_text(&state.cmd_buf, {x + RCOL_STALE, row_y}, "FRESH",
						ui.COL_GREEN, ui.FONT_SIZE_XS, .Mono)
				}
			}

			y += 16
		}

		y += 8 // spacing between accounts
	}

	return y
}

// --- Readiness color helpers ---

@(private = "file")
readiness_state_color :: proc(state: string) -> ui.Color {
	if state == "active"  do return ui.COL_GREEN
	if state == "paused"  do return ui.COL_WARNING
	if state == "drained" do return ui.COL_WARNING
	if state == "halted"  do return ui.COL_RED
	return ui.COL_TEXT_MUTED
}

@(private = "file")
readiness_flag_color :: proc(flag: string) -> ui.Color {
	if flag == "clear"                      do return ui.COL_GREEN
	if flag == "simulation"                 do return ui.COL_ACCENT_CYAN
	if flag == "halted"                     do return ui.COL_RED
	if flag == "paused" || flag == "drained" do return ui.COL_WARNING
	return ui.COL_WARNING // default for any active flag
}

@(private = "file")
readiness_trading_status_color :: proc(status: services.Trading_Status) -> ui.Color {
	switch status {
	case .Enabled:  return ui.COL_GREEN
	case .Degraded: return ui.COL_WARNING
	case .Disabled: return ui.COL_WARNING
	case .Halted:   return ui.COL_RED
	case .Unknown:  return ui.COL_TEXT_MUTED
	}
	return ui.COL_TEXT_MUTED
}

// --- Detail panel ---

@(private = "package")
page_portfolio_render_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	y := rect.pos.y
	dx := rect.pos.x + 2

	ui.push_text(&state.cmd_buf, {dx, y + 14}, "PORTFOLIO",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	pf := &state.portfolio
	if pf.summary_status == .Success {
		summary := &pf.summary

		// Equity.
		eq_buf: [24]u8
		eq_str := fmt.bprintf(eq_buf[:], "$%.2f", summary.global_equity_usd)
		ui.push_text(&state.cmd_buf, {dx + 2, y + 10}, eq_str,
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)
		y += 16

		// Unrealized PnL.
		upnl_buf: [24]u8
		upnl_str := fmt.bprintf(upnl_buf[:], "$%.2f", summary.global_unrealized_usd)
		ui.push_text(&state.cmd_buf, {dx + 2, y + 10}, upnl_str,
			pnl_color(summary.global_unrealized_usd), ui.FONT_SIZE_XS, .Mono)
		y += 16

		// Positions.
		pos_buf: [16]u8
		pos_str := fmt.bprintf(pos_buf[:], "%d pos", summary.total_position_count)
		ui.push_text(&state.cmd_buf, {dx + 2, y + 10}, pos_str,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 16

		// Leverage.
		lev_buf: [16]u8
		lev_str := fmt.bprintf(lev_buf[:], "%.2fx", summary.global_leverage)
		ui.push_text(&state.cmd_buf, {dx + 2, y + 10}, lev_str,
			leverage_color(summary.global_leverage), ui.FONT_SIZE_XS, .Mono)
		y += 16

		// S76: Exposure divergence indicator.
		if pf.snapshot_status == .Success && services.portfolio_has_exposure_divergence(&pf.snapshot) {
			ui.push_text(&state.cmd_buf, {dx + 2, y + 10}, "DIVERGENT",
				ui.COL_WARNING, ui.FONT_SIZE_XS, .Bold)
			y += 16
		}

		// S78: Trading readiness indicator.
		if pf.readiness_status == .Success {
			cp_state := pf.readiness.control_plane.state
			cp_color := readiness_state_color(cp_state)
			ui.push_text(&state.cmd_buf, {dx + 2, y + 10}, cp_state,
				cp_color, ui.FONT_SIZE_XS, .Bold)
		}
	} else if pf.summary_status == .Error {
		ui.push_text(&state.cmd_buf, {dx + 2, y + 10}, "Error",
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
	} else {
		ui.push_text(&state.cmd_buf, {dx + 2, y + 10}, "Loading...",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}
}

// --- Color helpers ---

@(private = "file")
pnl_color :: proc(value: f64) -> ui.Color {
	if value > 0 do return ui.COL_GREEN
	if value < 0 do return ui.COL_RED
	return ui.COL_TEXT_SECONDARY
}

@(private = "file")
leverage_color :: proc(leverage: f64) -> ui.Color {
	if leverage > 10 do return ui.COL_RED
	if leverage > 5  do return ui.COL_WARNING
	if leverage > 0  do return ui.COL_TEXT_PRIMARY
	return ui.COL_TEXT_MUTED
}
