package app

import "mr:model"
import "mr:ports"
import "mr:services"
import "mr:ui"
import "mr:widgets"

MD_POLL_CAP :: 64

App_State :: struct {
	cmd_buf:         ui.Command_Buffer,
	text:            ports.Text_Port,
	fonts:           ports.Font_Port,
	marketdata:      ports.Marketdata_Port,
	trades_store:    services.Trades_Store,
	orderbook_store: services.Orderbook_Store,
	heatmap_store:   services.Heatmap_Store,
	vpvr_store:      services.VPVR_Store,
	stats_store:     services.Stats_Store,
	settings:        services.Settings_Store,
	scroll_y:        f32,
	ob_scroll_y:     f32,
	frame:           u64,
}

init :: proc(
	state: ^App_State,
	text: ports.Text_Port,
	md: ports.Marketdata_Port,
	fonts: ports.Font_Port = {},
	settings_port: ports.Settings_Port = {},
	offline: bool = true,
) {
	state.cmd_buf = ui.make_buffer()
	state.text = text
	state.fonts = fonts
	state.marketdata = md

	// Only fill demo data in offline mode; real data overwrites stores when live.
	if offline {
		services.fill_demo_trades(&state.trades_store)
		services.fill_demo_orderbook(&state.orderbook_store)
		services.fill_demo_heatmaps(&state.heatmap_store)
		services.fill_demo_vpvr(&state.vpvr_store)
		services.fill_demo_stats(&state.stats_store)
	}

	// Initialize settings store.
	if settings_port.load != nil {
		services.settings_init(&state.settings, settings_port)
	}
}

shutdown :: proc(state: ^App_State) {
	services.settings_flush(&state.settings)
	ui.destroy_buffer(&state.cmd_buf)
}

update :: proc(state: ^App_State, input: ports.Input_State) -> ^ui.Command_Buffer {
	ui.reset(&state.cmd_buf)
	state.frame += 1

	// Drain marketdata events (non-blocking).
	if state.marketdata.poll != nil {
		events: [MD_POLL_CAP]ports.MD_Event
		n := state.marketdata.poll(events[:])
		for i in 0 ..< n {
			evt := events[i]
			switch evt.kind {
			case .Trade:
				t := evt.data.trade
				services.push_trade(&state.trades_store, services.Trade_Entry{
					price = t.price,
					qty   = t.qty,
					side  = t.is_buy ? .Buy : .Sell,
					unix  = t.unix,
				})
			case .Orderbook_Snapshot:
				ob := evt.data.ob
				services.update_orderbook(&state.orderbook_store,
					ob.ask_prices[:ob.ask_count], ob.ask_sizes[:ob.ask_count],
					ob.bid_prices[:ob.bid_count], ob.bid_sizes[:ob.bid_count],
					ob.last_price, ob.unix,
				)
			case .Stats:
				st := evt.data.stats
				services.push_stats(&state.stats_store, services.Stats_Entry{
					mark_price = st.mark_price,
					funding    = st.funding,
					liq_buy    = f64(st.tbuy),
					liq_sell   = f64(st.tsell),
					unix       = st.unix,
				})
			case .Heatmap:
				hm := evt.data.heatmap
				snap: services.Heatmap_Snapshot
				snap.unix = hm.unix
				snap.price_group = hm.price_group
				snap.min_price = hm.min_price
				snap.max_price = hm.max_price
				snap.max_size = hm.max_size
				snap.level_count = min(hm.level_count, services.HEATMAP_LEVEL_CAP)
				for j in 0 ..< snap.level_count {
					snap.levels[j] = services.Heatmap_Level{
						price = hm.prices[j],
						size  = hm.sizes[j],
					}
				}
				services.push_heatmap_snapshot(&state.heatmap_store, snap)
			case .VPVR:
				vpvr := evt.data.vpvr
				count := min(vpvr.level_count, services.VPVR_BUCKET_CAP)
				services.update_vpvr(
					&state.vpvr_store,
					vpvr.prices, vpvr.buys, vpvr.sells,
					count, vpvr.price_group,
				)
			}
		}
	}

	ui.push(&state.cmd_buf, ui.Cmd_Clear{color = {0.04, 0.04, 0.04, 1.0}})
	widgets.hello(&state.cmd_buf, state.text)

	// Connection status indicator (top-right).
	draw_conn_indicator(state)

	// Trade counter bar chart (live stats from store).
	stats_buf: [services.STATS_CAP]model.Stat
	sc := 0
	for i in 0 ..< state.stats_store.count {
		e := services.get_stats(&state.stats_store, i)
		stats_buf[sc] = model.Stat{
			unix       = e.unix,
			tbuy       = i64(e.liq_buy),
			tsell      = i64(e.liq_sell),
			mark_price = e.mark_price,
			funding    = e.funding,
		}
		sc += 1
	}

	x_min, x_max: f64
	if sc > 0 {
		oldest := stats_buf[sc - 1].unix
		newest := stats_buf[0].unix
		x_min = f64(oldest) - 60
		x_max = f64(newest) + 60
	}

	widgets.trade_counter(&state.cmd_buf, widgets.Trade_Counter_Data{
		stats         = stats_buf[:sc],
		viewport      = {pos = {20, 80}, size = {760, 200}},
		timeframe     = 60,
		x_min         = x_min,
		x_max         = x_max,
		bar_width_pct = CANDLE_WIDTH_PCT,
		text          = state.text,
	})

	// Trades list (scrollable, below the bar chart).
	widgets.trades_widget(&state.cmd_buf, widgets.Trades_Widget_Data{
		store    = &state.trades_store,
		viewport = {pos = {20, 300}, size = {370, 280}},
		text     = state.text,
		scroll_y = &state.scroll_y,
		input    = input,
	})

	// Orderbook (right of trades list).
	widgets.orderbook_widget(&state.cmd_buf, widgets.Orderbook_Widget_Data{
		store       = &state.orderbook_store,
		viewport    = {pos = {400, 300}, size = {380, 500}},
		text        = state.text,
		scroll_y    = &state.ob_scroll_y,
		input       = input,
		price_group = 10.0,
		max_rows    = 20,
	})

	// Heatmap (below trades + orderbook).
	widgets.heatmap_widget(&state.cmd_buf, widgets.Heatmap_Widget_Data{
		store    = &state.heatmap_store,
		viewport = {pos = {20, 600}, size = {760, 250}},
		text     = state.text,
	})

	// VPVR (below heatmap).
	widgets.vpvr_widget(&state.cmd_buf, widgets.VPVR_Widget_Data{
		store    = &state.vpvr_store,
		viewport = {pos = {20, 870}, size = {380, 400}},
		text     = state.text,
		input    = input,
	})

	return &state.cmd_buf
}

// --- Connection indicator ---

@(private = "file")
draw_conn_indicator :: proc(state: ^App_State) {
	status: ports.MD_Conn_Status = .Offline
	if state.marketdata.conn_status != nil {
		status = state.marketdata.conn_status()
	}

	label: string
	color: ui.Color
	switch status {
	case .Connected:
		label = "LIVE"
		color = ui.COL_GREEN
	case .Connecting:
		label = "CONNECTING..."
		color = ui.COL_YELLOW_ACCENT
	case .Reconnecting:
		label = "RECONNECTING..."
		color = ui.COL_YELLOW_ACCENT
	case .Offline:
		label = "OFFLINE"
		color = ui.with_alpha(ui.COL_WHITE, 0.4)
	}

	// Position: top-right corner.
	label_w := state.text.measure(ui.FONT_SIZE_SM, label).x
	x := f32(780) - label_w
	y := f32(16)

	// Dot indicator.
	dot_size: f32 = 6
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {x - dot_size - 4, y - dot_size + 2}, size = {dot_size, dot_size}},
		color = color,
	})

	// Label text.
	ui.push_text(&state.cmd_buf, {x, y}, label, color, ui.FONT_SIZE_SM, .Mono)
}
