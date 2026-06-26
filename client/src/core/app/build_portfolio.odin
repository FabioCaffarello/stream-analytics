package app

import "mr:ui"

// Stream Analytics pipeline overview page — replaces the former Portfolio page (route .Portfolio).

ANALYTICS_PAD_X :: f32(20)
ANALYTICS_ROW_H :: f32(38)
ANALYTICS_DOT_R :: f32(4)

@(private = "package")
page_portfolio_enter :: proc(state: ^App_State) {}

@(private = "package")
page_portfolio_leave :: proc(state: ^App_State) {}

@(private = "package")
page_portfolio_render :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = workspace, color = ui.COL_SURFACE_1})

	x := workspace.pos.x + ANALYTICS_PAD_X
	y := workspace.pos.y + 24

	// --- Title ---
	ui.push_text(&state.cmd_buf, {x, y}, "Stream Analytics Pipeline",
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_LG, .Bold)
	y += 30

	ui.push_text(&state.cmd_buf, {x, y},
		"Real-time market data flowing from exchanges to the analytics DW.",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Default)
	y += 24

	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from  = {workspace.pos.x + 4, y},
		to    = {workspace.pos.x + workspace.size.x - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 14

	// --- Pipeline stages ---
	draw_analytics_stage :: proc(state: ^App_State, x, y: f32, label, desc: string, dot_color: ui.Color) {
		dot_cx := x + ANALYTICS_DOT_R
		dot_cy := y + ANALYTICS_ROW_H * 0.45
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect  = ui.rect_xywh(dot_cx - ANALYTICS_DOT_R, dot_cy - ANALYTICS_DOT_R,
				ANALYTICS_DOT_R * 2, ANALYTICS_DOT_R * 2),
			color = dot_color,
		})
		ui.push_text(&state.cmd_buf, {x + ANALYTICS_DOT_R * 2 + 8, y + ANALYTICS_ROW_H * 0.22},
			label, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_SM, .Bold)
		ui.push_text(&state.cmd_buf, {x + ANALYTICS_DOT_R * 2 + 8, y + ANALYTICS_ROW_H * 0.60},
			desc, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}

	draw_analytics_stage(state, x, y, "Consumer",    "Exchange WebSocket  →  NATS JetStream + Kafka",   ui.COL_GREEN)
	y += ANALYTICS_ROW_H
	draw_analytics_stage(state, x, y, "Kafka",       "market.trades   |   market.orderbook",            ui.COL_GREEN)
	y += ANALYTICS_ROW_H
	draw_analytics_stage(state, x, y, "Flink",       "OHLCV windows  ·  volume stats  ·  trade tape",  ui.COL_GREEN)
	y += ANALYTICS_ROW_H
	draw_analytics_stage(state, x, y, "TimescaleDB", "analytics schema  — fact_trades / candles / vol", ui.COL_GREEN)
	y += ANALYTICS_ROW_H
	draw_analytics_stage(state, x, y, "Metabase",    "BI dashboards  →  localhost:3001",                ui.COL_ACCENT_CYAN)
	y += ANALYTICS_ROW_H

	// Separator between analytics path and real-time path
	y += 4
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from  = {workspace.pos.x + 4, y},
		to    = {workspace.pos.x + workspace.size.x - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 14

	draw_analytics_stage(state, x, y, "Real-time WS",
		"NATS  →  Processor  →  Server  →  WebSocket  →  this client", ui.COL_GREEN)
	y += ANALYTICS_ROW_H + 8

	// --- DW schema section ---
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from  = {workspace.pos.x + 4, y},
		to    = {workspace.pos.x + workspace.size.x - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 14

	ui.push_text(&state.cmd_buf, {x, y}, "DW SCHEMA",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	dw_lines := [3]string{
		"analytics.fact_trades       — append-only trade tape from Kafka",
		"analytics.fact_candles      — OHLCV aggregations (1m / 5m / 15m / 1h)",
		"analytics.fact_volume_stats — buy/sell volume windows (5m)",
	}
	for line in dw_lines {
		ui.push_text(&state.cmd_buf, {x + 4, y}, line,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 15
	}
}

@(private = "package")
page_portfolio_render_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	y := rect.pos.y
	dx := rect.pos.x + 2

	ui.push_text(&state.cmd_buf, {dx, y + 14}, "ANALYTICS",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 28

	links := [3]string{"Consumer", "Kafka", "Flink"}
	for link in links {
		ui.push_text(&state.cmd_buf, {dx + 2, y + 10}, link,
			ui.COL_GREEN, ui.FONT_SIZE_XS, .Mono)
		y += 14
	}
	y += 4

	ui.push_text(&state.cmd_buf, {dx + 2, y + 10}, "Metabase",
		ui.COL_ACCENT_CYAN, ui.FONT_SIZE_XS, .Bold)
	y += 14
	ui.push_text(&state.cmd_buf, {dx + 2, y + 10}, "localhost:3001",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
}
