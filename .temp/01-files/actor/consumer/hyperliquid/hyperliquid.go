package hyperliquid

import (
	"context"
	"encoding/json"
	"log/slog"
	"marketmonkey/actor/consumer/base"
	"marketmonkey/actor/consumer/ws"
	"marketmonkey/config"
	"marketmonkey/event"
	"marketmonkey/pkg/nats"
	"strconv"
	"strings"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/valyala/fastjson"
)

const wsEndpoint = "wss://api.hyperliquid.xyz/ws"

var streams = []nats.StreamType{
	nats.StreamTypeTrade,
	nats.StreamTypeBookUpdate,
}

type Hyperliquid struct {
	base    *base.BaseConsumer
	tickers []string
	manager *actor.PID
	quitch  chan struct{}
}

func (h *Hyperliquid) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		base, err := base.NewBaseConsumer(config.Hyperliquid, h.quitch, streams)
		if err != nil {
			panic(err)
		}
		h.base = base

		h.start(c)

	case *ws.WsMessage:
		h.process(msg)

	case actor.Stopped:
		slog.Warn("consumer actor stopped", "consumer", config.Hyperliquid)
		if h.base != nil {
			h.base.Close()
		}
		close(h.quitch)
	}
}

func New() actor.Producer {
	return func() actor.Receiver {
		return &Hyperliquid{
			tickers: make([]string, 0),
			quitch:  make(chan struct{}),
		}
	}
}

func (h *Hyperliquid) start(c *actor.Context) {
	market, ok := config.GetMarket(config.Hyperliquid)
	if !ok {
		panic("failed to get hyperliquid config")
	}

	for _, sym := range market.Symbols {
		h.tickers = append(h.tickers, sym.Ticker)
	}

	h.manager = c.SpawnChild(
		ws.NewManager(
			ws.ManagerConfig{
				SendTo:                 c.PID(),
				Exchange:               "hyperliquid",
				Tickers:                h.tickers,
				StreamsPerTicker:       2,
				MaxStreamsPerWebsocket: 1000,
				RespawnOverlap:         5 * time.Second,
				FillStrategy:           ws.FillStrategyFirst,
				MaxWebsockets:          5,
				MaxWebsocketLifetime:   2 * time.Hour,
				EndpointBuilder:        endpointBuilder,
				SubscriptionBuilder:    subscriptionBuilder,
			},
		),
		"hyperliquid-manager",
	)
}

func (h *Hyperliquid) process(msg *ws.WsMessage) {
	ctx := context.TODO()
	parser := fastjson.Parser{}
	v, err := parser.ParseBytes(msg.Data)
	if err != nil {
		slog.Error("failed to parse ws message", "err", err.Error(), "consumer", config.Hyperliquid)
		return
	}

	channel := string(v.GetStringBytes("channel"))
	data := v.Get("data")
	switch channel {
	case "l2Book":
		h.handleOrderbook(ctx, data)
	case "trades":
		h.handleTrade(ctx, data)
	}
}

// {"channel":"l2Book","data":{"coin":"BTC","time":1739988505821,"levels":[[{"px":"96220.0","sz":"4.19382","n":14},{"px":"96219.0","sz":"0.47952","n":3},{"px":"96218.0","sz":"1.37945","n":3},{"px":"96217.0","sz":"0.83379","n":2},{"px":"96216.0","sz":"0.50155","n":3},{"px":"96215.0","sz":"0.17684","n":1},{"px":"96214.0","sz":"2.181","n":2},{"px":"96212.0","sz":"1.25388","n":3},{"px":"96211.0","sz":"0.78373","n":4},{"px":"96210.0","sz":"2.30655","n":5},{"px":"96208.0","sz":"0.08776","n":2},{"px":"96207.0","sz":"0.25","n":3},{"px":"96206.0","sz":"0.36405","n":2},{"px":"96205.0","sz":"0.63","n":1},{"px":"96204.0","sz":"0.6364","n":4},{"px":"96203.0","sz":"1.34594","n":3},{"px":"96201.0","sz":"2.3875","n":3},{"px":"96200.0","sz":"2.07706","n":1},{"px":"96199.0","sz":"1.45539","n":2},{"px":"96198.0","sz":"2.70055","n":3}],[{"px":"96221.0","sz":"0.43765","n":2},{"px":"96222.0","sz":"0.00015","n":1},{"px":"96223.0","sz":"0.00015","n":1},{"px":"96224.0","sz":"0.00015","n":1},{"px":"96225.0","sz":"0.19407","n":2},{"px":"96230.0","sz":"0.07274","n":1},{"px":"96232.0","sz":"0.15588","n":1},{"px":"96233.0","sz":"0.1085","n":4},{"px":"96234.0","sz":"2.08225","n":2},{"px":"96235.0","sz":"0.38072","n":3},{"px":"96236.0","sz":"0.20905","n":1},{"px":"96237.0","sz":"0.51756","n":2},{"px":"96239.0","sz":"0.15484","n":1},{"px":"96240.0","sz":"6.19046","n":8},{"px":"96241.0","sz":"1.14638","n":5},{"px":"96242.0","sz":"3.01216","n":2},{"px":"96243.0","sz":"0.38009","n":2},{"px":"96244.0","sz":"0.2096","n":1},{"px":"96245.0","sz":"0.74865","n":3},{"px":"96246.0","sz":"0.97675","n":2}]]}}
func (h *Hyperliquid) handleOrderbook(ctx context.Context, data *fastjson.Value) {
	coin := strings.ToLower(string(data.GetStringBytes("coin")))
	unix := data.GetInt64("time")
	levels := data.GetArray("levels")
	book := &event.BookUpdate{
		Unix: unix,
		Pair: &event.Pair{
			Exchange: config.Hyperliquid,
			Symbol:   coin,
		},
		Bids:     make([]*event.BookEntry, 0),
		Asks:     make([]*event.BookEntry, 0),
		Snapshot: true,
	}

	for _, bid := range levels[0].GetArray() {
		price, _ := strconv.ParseFloat(string(bid.GetStringBytes("px")), 64)
		size, _ := strconv.ParseFloat(string(bid.GetStringBytes("sz")), 64)
		book.Bids = append(book.Bids, &event.BookEntry{
			Price: price,
			Size:  size,
		})
	}

	for _, ask := range levels[1].GetArray() {
		price, _ := strconv.ParseFloat(string(ask.GetStringBytes("px")), 64)
		size, _ := strconv.ParseFloat(string(ask.GetStringBytes("sz")), 64)
		book.Asks = append(book.Asks, &event.BookEntry{
			Price: price,
			Size:  size,
		})
	}

	msg := base.PublishMessageParams{
		Stream: nats.StreamTypeBookUpdate,
		Symbol: coin,
		Msg:    book,
		Key:    book.Key(),
	}

	err := h.base.PublishMessage(ctx, msg)
	if err != nil {
		slog.Error("failed to publish book update", "err", err)
	}

}

// {"channel":"trades","data":[{"coin":"BTC","side":"B","px":"96221.0","sz":"0.00016","time":1739988506620,"hash":"0x3fffd02ebafd2e28906f041e12db8801fb00133139a4f2950d67387c8883a4bd","tid":951635025884506,"users":["0x1dd4af3383fce2ee4a40d9fb6766cbfcce2ddbce","0x0b1ace05eb9ef1c3a1951b763700ecad24f27741"]}]}
func (h *Hyperliquid) handleTrade(ctx context.Context, data *fastjson.Value) {
	trades := data.GetArray()
	for _, t := range trades {
		coin := strings.ToLower(string(t.GetStringBytes("coin")))
		price, _ := strconv.ParseFloat(string(t.GetStringBytes("px")), 64)
		qty, _ := strconv.ParseFloat(string(t.GetStringBytes("sz")), 64)
		side := string(t.GetStringBytes("side"))

		trade := &event.Trade{
			ID:    string(t.GetStringBytes("hash")),
			Price: price,
			Qty:   qty,
			IsBuy: side == "B",
			Unix:  t.GetInt64("time"),
			Pair:  event.NewPair(config.Hyperliquid, coin),
		}

		msg := base.PublishMessageParams{
			Stream: nats.StreamTypeTrade,
			Symbol: coin,
			Msg:    trade,
			Key:    trade.Key(),
		}

		err := h.base.PublishMessage(ctx, msg)
		if err != nil {
			slog.Error("failed to publish trade", "err", err)
		}
	}
}

func endpointBuilder(_ []string) string {
	return wsEndpoint
}

type hyperliquidSubscription struct {
	Type string `json:"type"`
	Coin string `json:"coin"`
}

type hyperliquidSubscribe struct {
	Method       string                  `json:"method"`
	Subscription hyperliquidSubscription `json:"subscription"`
}

func (h *hyperliquidSubscribe) json() []byte {
	json, _ := json.Marshal(h)
	return json
}

func subscriptionBuilder(tickers []string) [][]byte {
	subMessages := make([][]byte, 0, 2*len(tickers))

	for _, ticker := range tickers {
		trades := hyperliquidSubscribe{
			Method: "subscribe",
			Subscription: hyperliquidSubscription{
				Type: "trades",
				Coin: ticker,
			},
		}

		book := hyperliquidSubscribe{
			Method: "subscribe",
			Subscription: hyperliquidSubscription{
				Type: "l2Book",
				Coin: ticker,
			},
		}

		subMessages = append(subMessages, trades.json())
		subMessages = append(subMessages, book.json())
	}

	return subMessages
}
