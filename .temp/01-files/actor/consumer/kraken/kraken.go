package kraken

// const wsEndpoint = "wss://ws.kraken.com/v2"

// var symbols = []string{
// 	"TRUMP/USD", // BTC/USD Perpetual
// }

// type Kraken struct {
// 	ws      *websocket.Conn
// 	symbols map[string]*actor.PID
// 	c       *actor.Context
// }

// func (k *Kraken) Receive(c *actor.Context) {
// 	switch c.Message().(type) {
// 	case actor.Started:
// 		k.start(c)
// 		k.c = c
// 	}
// }

// func New() actor.Producer {
// 	return func() actor.Receiver {
// 		return &Kraken{
// 			symbols: make(map[string]*actor.PID),
// 		}
// 	}
// }

// func (k *Kraken) start(c *actor.Context) {
// 	for _, sym := range symbols {
// 		pair := event.Pair{
// 			Exchange: "kraken",
// 			Symbol:   sym,
// 		}
// 		pid := c.SpawnChild(symbol.New(pair), "symbol", actor.WithID(pair.Symbol))
// 		k.symbols[pair.Symbol] = pid
// 	}

// 	ws, _, err := websocket.DefaultDialer.Dial(wsEndpoint, nil)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	k.ws = ws

// 	subscribeBook := map[string]any{
// 		"method": "subscribe",
// 		"params": map[string]any{
// 			"channel": "book",
// 			"symbol":  []string{"TRUMP/USD"},
// 		},
// 	}
// 	subscribeTrades := map[string]any{
// 		"method": "subscribe",
// 		"params": map[string]any{
// 			"channel": "trade",
// 			"symbol":  []string{"TRUMP/USD"},
// 		},
// 	}

// 	// subscribeTrades := map[string]interface{}{
// 	// 	"event":       "subscribe",
// 	// 	"feed":        "trade",
// 	// 	"product_ids": symbols,
// 	// }
// 	// if err := ws.WriteJSON(subscribeTrades); err != nil {
// 	// 	log.Fatal(err)
// 	// }

// 	// subscribeTradeSnapshot := map[string]interface{}{
// 	// 	"event":       "subscribe",
// 	// 	"feed":        "trade_snapshot",
// 	// 	"product_ids": symbols,
// 	// }
// 	if err := ws.WriteJSON(subscribeBook); err != nil {
// 		log.Fatal(err)
// 	}
// 	if err := ws.WriteJSON(subscribeTrades); err != nil {
// 		log.Fatal(err)
// 	}

// 	go k.wsLoop()
// }

// func (k *Kraken) wsLoop() {
// 	for {
// 		_, msg, err := k.ws.ReadMessage()
// 		if err != nil {
// 			if errors.Is(err, net.ErrClosed) {
// 				break
// 			}
// 			fmt.Println("error reading from ws connection", err)
// 			continue
// 		}

// 		parser := fastjson.Parser{}
// 		v, err := parser.ParseBytes(msg)
// 		if err != nil {
// 			fmt.Println("failed to parse msg", err)
// 			continue
// 		}

// 		channel := string(v.GetStringBytes("channel"))
// 		symbol := string(v.GetStringBytes("symbol"))
// 		data := v.GetArray("data")

// 		switch channel {
// 		case "book":
// 			k.handleOrderbookDelta(symbol, data)
// 		case "trade":
// 			k.handleTrades(data)
// 		}
// 	}
// }

// func (k *Kraken) handleTrades(values []*fastjson.Value) {
// 	for _, data := range values {
// 		// {"symbol":"TRUMP/USD","side":"buy","price":69.796,"qty":5.57000,"ord_type":"market","trade_id":146163,"timestamp":"2025-01-19T09:59:44.811645Z"}
// 		tsRaw := data.GetStringBytes("timestamp")
// 		ts, _ := time.Parse(time.RFC3339Nano, string(tsRaw))

// 		trade := event.Trade{
// 			Price: data.GetFloat64("price"),
// 			Qty:   data.GetFloat64("qty"),
// 			IsBuy: string(data.GetStringBytes("side")) == "buy",
// 			Unix:  ts.Unix() * 1000,
// 			Pair: event.Pair{
// 				Exchange: "kraken",
// 				Symbol:   "TRUMP/USD",
// 			},
// 		}
// 		k.c.Send(k.symbols[trade.Pair.Symbol], trade)
// 	}
// }

// func (k *Kraken) handleOrderbookDelta(symbol string, data []*fastjson.Value) {
// 	foo := data[0]
// 	tsRaw := foo.GetStringBytes("timestamp")
// 	ts, _ := time.Parse(time.RFC3339, string(tsRaw))

// 	var (
// 		asks = foo.GetArray("asks")
// 		bids = foo.GetArray("bids")
// 		msg  = event.BookUpdate{
// 			Pair: event.Pair{
// 				Exchange: "kraken",
// 				Symbol:   symbol,
// 			},
// 			Unix: ts.Unix(),
// 			Bids: make([]event.BookEntry, 0, len(asks)),
// 			Asks: make([]event.BookEntry, 0, len(bids)),
// 		}
// 	)
// 	for _, item := range asks {
// 		price := item.GetFloat64("price")
// 		size := item.GetFloat64("qty")
// 		msg.Asks = append(msg.Asks, event.BookEntry{
// 			Price: price,
// 			Size:  size,
// 		})
// 	}
// 	for _, item := range bids {
// 		price := item.GetFloat64("price")
// 		size := item.GetFloat64("qty")
// 		msg.Bids = append(msg.Bids, event.BookEntry{
// 			Price: price,
// 			Size:  size,
// 		})
// 	}

// 	k.c.Send(k.symbols["TRUMP/USD"], msg)
// }

// func (k *Kraken) handleTrade(symbol string, data *fastjson.Value) {
// 	feed := string(data.GetStringBytes("feed"))

// 	if feed == "trade_snapshot" {
// 		trades := data.GetArray("trades")
// 		if trades == nil {
// 			return
// 		}
// 		for _, t := range trades {
// 			k.processTrade(symbol, t)
// 		}
// 		return
// 	}

// 	k.processTrade(symbol, data)
// }

// func (k *Kraken) processTrade(symbol string, data *fastjson.Value) {
// 	qty := data.GetFloat64("qty")
// 	price := data.GetFloat64("price")
// 	side := string(data.GetStringBytes("side"))
// 	timestamp := data.GetInt64("time")

// 	if price <= 0 || qty <= 0 {
// 		return
// 	}

// 	trade := event.Trade{
// 		Price: price,
// 		Qty:   qty,
// 		IsBuy: side == "buy",
// 		Unix:  timestamp,
// 		Pair: event.Pair{
// 			Exchange: "kraken",
// 			Symbol:   symbol,
// 		},
// 	}
// 	k.c.Send(k.symbols[symbol], trade)
// }
