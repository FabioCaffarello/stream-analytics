package krakenf

// import (
// 	"errors"
// 	"fmt"
// 	"log"
// 	"marketmonkey/actor/symbol"
// 	"marketmonkey/event"
// 	"net"
// 	"strings"

// 	"github.com/anthdm/hollywood/actor"
// 	"github.com/gorilla/websocket"
// 	"github.com/valyala/fastjson"
// )

// const wsEndpoint = "wss://futures.kraken.com/ws/v1"

// var symbols = []string{
// 	"PI_XBTUSD", // BTC/USD Perpetual
// 	"PI_ETHUSD", // ETH/USD Perpetual
// 	"PI_SOLUSD", // SOL/USD Perpetual
// }

// type Krakenf struct {
// 	ws      *websocket.Conn
// 	symbols map[string]*actor.PID
// 	c       *actor.Context
// }

// func (k *Krakenf) Receive(c *actor.Context) {
// 	switch c.Message().(type) {
// 	case actor.Started:
// 		k.start(c)
// 		k.c = c
// 	}
// }

// func New() actor.Producer {
// 	return func() actor.Receiver {
// 		return &Krakenf{
// 			symbols: make(map[string]*actor.PID),
// 		}
// 	}
// }

// func (k *Krakenf) start(c *actor.Context) {
// 	// Initialize all the symbol actors as childs
// 	for _, sym := range symbols {
// 		pair := event.Pair{
// 			Exchange: "kraken",
// 			Symbol:   strings.ToLower(strings.Replace(sym, "PI_", "", -1)),
// 		}
// 		pid := c.SpawnChild(symbol.New(pair), "symbol", actor.WithID(pair.Symbol))
// 		k.symbols[pair.Symbol] = pid
// 	}

// 	ws, _, err := websocket.DefaultDialer.Dial(wsEndpoint, nil)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	k.ws = ws

// 	subscribeBook := map[string]interface{}{
// 		"event":       "subscribe",
// 		"feed":        "book",
// 		"product_ids": symbols,
// 	}
// 	if err := ws.WriteJSON(subscribeBook); err != nil {
// 		log.Fatal(err)
// 	}

// 	subscribeTrades := map[string]interface{}{
// 		"event":       "subscribe",
// 		"feed":        "trade",
// 		"product_ids": symbols,
// 	}
// 	if err := ws.WriteJSON(subscribeTrades); err != nil {
// 		log.Fatal(err)
// 	}

// 	subscribeTradeSnapshot := map[string]interface{}{
// 		"event":       "subscribe",
// 		"feed":        "trade_snapshot",
// 		"product_ids": symbols,
// 	}
// 	if err := ws.WriteJSON(subscribeTradeSnapshot); err != nil {
// 		log.Fatal(err)
// 	}

// 	go k.wsLoop()
// }

// func (k *Krakenf) wsLoop() {
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

// 		feed := string(v.GetStringBytes("feed"))

// 		productID := string(v.GetStringBytes("product_id"))
// 		if productID == "" {
// 			continue
// 		}

// 		symbol := strings.ToLower(strings.Replace(productID, "PI_", "", -1))

// 		switch feed {
// 		case "book_snapshot":
// 			k.handleOrderbookSnapshot(symbol, v)
// 		case "book":
// 			k.handleOrderbookDelta(symbol, v)
// 		case "trade", "trade_snapshot":
// 			k.handleTrade(symbol, v)
// 		}
// 	}
// }

// func (k *Krakenf) handleOrderbookSnapshot(symbol string, data *fastjson.Value) {
// 	var msg = event.BookUpdate{
// 		Pair: event.Pair{
// 			Exchange: "kraken",
// 			Symbol:   symbol,
// 		},
// 		Unix: data.GetInt64("timestamp"),
// 		Bids: make([]event.BookEntry, 0),
// 		Asks: make([]event.BookEntry, 0),
// 	}

// 	bids := data.GetArray("bids")
// 	for _, item := range bids {
// 		msg.Bids = append(msg.Bids, event.BookEntry{
// 			Price: item.GetFloat64("price"),
// 			Size:  item.GetFloat64("qty"),
// 		})
// 	}

// 	asks := data.GetArray("asks")
// 	for _, item := range asks {
// 		msg.Asks = append(msg.Asks, event.BookEntry{
// 			Price: item.GetFloat64("price"),
// 			Size:  item.GetFloat64("qty"),
// 		})
// 	}

// 	k.c.Send(k.symbols[symbol], msg)
// }

// func (k *Krakenf) handleOrderbookDelta(symbol string, data *fastjson.Value) {
// 	var msg = event.BookUpdate{
// 		Pair: event.Pair{
// 			Exchange: "kraken",
// 			Symbol:   symbol,
// 		},
// 		Unix: data.GetInt64("timestamp"),
// 		Bids: make([]event.BookEntry, 0, 1),
// 		Asks: make([]event.BookEntry, 0, 1),
// 	}

// 	side := string(data.GetStringBytes("side"))
// 	price := data.GetFloat64("price")
// 	qty := data.GetFloat64("qty")

// 	entry := event.BookEntry{
// 		Price: price,
// 		Size:  qty,
// 	}

// 	if side == "buy" {
// 		msg.Bids = append(msg.Bids, entry)
// 	} else {
// 		msg.Asks = append(msg.Asks, entry)
// 	}

// 	k.c.Send(k.symbols[symbol], msg)
// }

// func (k *Krakenf) handleTrade(symbol string, data *fastjson.Value) {
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

// func (k *Krakenf) processTrade(symbol string, data *fastjson.Value) {
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
