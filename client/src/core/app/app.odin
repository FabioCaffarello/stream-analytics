package app

import "mr:model"
import "mr:ui"
import "mr:widgets"

App_State :: struct {
	cmd_buf: ui.Command_Buffer,
	frame:   u64,
}

init :: proc(state: ^App_State) {
	state.cmd_buf = ui.make_buffer()
}

shutdown :: proc(state: ^App_State) {
	ui.destroy_buffer(&state.cmd_buf)
}

update :: proc(state: ^App_State) -> ^ui.Command_Buffer {
	ui.reset(&state.cmd_buf)
	state.frame += 1

	ui.push(&state.cmd_buf, ui.Cmd_Clear{color = {0.04, 0.04, 0.04, 1.0}})
	widgets.hello(&state.cmd_buf)

	// Sample trade counter (hardcoded stats for Fase 4 demo).
	sample_stats := [?]model.Stat{
		{unix = 1000, tbuy = 42, tsell = 18},
		{unix = 1060, tbuy = 15, tsell = 55},
		{unix = 1120, tbuy = 70, tsell = 30},
		{unix = 1180, tbuy = 25, tsell = 60},
		{unix = 1240, tbuy = 50, tsell = 50},
		{unix = 1300, tbuy = 80, tsell = 10},
		{unix = 1360, tbuy = 12, tsell = 75},
		{unix = 1420, tbuy = 45, tsell = 35},
	}
	widgets.trade_counter(&state.cmd_buf, widgets.Trade_Counter_Data{
		stats         = sample_stats[:],
		viewport      = {pos = {20, 80}, size = {760, 200}},
		timeframe     = 60,
		x_min         = 950,
		x_max         = 1470,
		bar_width_pct = CANDLE_WIDTH_PCT,
	})

	return &state.cmd_buf
}
