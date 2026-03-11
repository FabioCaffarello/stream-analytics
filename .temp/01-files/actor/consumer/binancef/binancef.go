package binancef

import (
	"context"
	"fmt"
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

const wsEndpoint = "wss://fstream.binance.com/stream?streams="

var streams = []nats.StreamType{
	nats.StreamTypeTrade,
	nats.StreamTypeBookUpdate,
	nats.StreamTypePreStat,
	nats.StreamTypeLiquidation,
}

type Binancef struct {
	base    *base.BaseConsumer
	tickers []string
	manager *actor.PID
	quitch  chan struct{}
}

func (b *Binancef) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		base, err := base.NewBaseConsumer(config.Binancef, b.quitch, streams)
		if err != nil {
			panic(err)
		}
		b.base = base
		b.start(c)

	case *ws.WsMessage:
		b.process(msg)

	case actor.Stopped:
		slog.Warn("consumer actor stopped", "consumer", config.Binance)
		if b.base != nil {
			b.base.Close()
		}
		close(b.quitch)
	}
}

func New() actor.Producer {
	return func() actor.Receiver {
		return &Binancef{
			tickers: make([]string, 0),
			quitch:  make(chan struct{}),
		}
	}
}

func (b *Binancef) start(c *actor.Context) {
	market, ok := config.GetMarket(config.Binancef)
	if !ok {
		panic("failed to start binance market: invalid configuration")
	}
	for _, sym := range market.Symbols {
		b.tickers = append(b.tickers, sym.Ticker)
	}

	b.manager = c.SpawnChild(
		ws.NewManager(
			ws.ManagerConfig{
				SendTo:                 c.PID(),
				Exchange:               "binancef",
				Tickers:                b.tickers,
				StreamsPerTicker:       4,
				MaxStreamsPerWebsocket: 1000,
				RespawnOverlap:         5 * time.Second,
				FillStrategy:           ws.FillStrategyFirst,
				MaxWebsockets:          5,
				MaxWebsocketLifetime:   2 * time.Hour,
				EndpointBuilder:        endpointBuilder,
			},
		),
		"binancef-manager",
	)
}

func (b *Binancef) process(msg *ws.WsMessage) {
	ctx := context.Background()

	parser := fastjson.Parser{}
	v, err := parser.ParseBytes(msg.Data)
	if err != nil {
		slog.Error("failed to parse ws message", "err", err.Error(), "consumer", config.Binancef)
		return
	}

	data := v.Get("data")
	stream := string(v.GetStringBytes("stream"))
	symbol, kind := splitStream(stream)

	switch {
	case strings.HasSuffix(stream, "markPrice"):
		b.handleMarkprice(ctx, data)
	case strings.HasSuffix(stream, "depth"):
		b.handleOrderbook(ctx, data)
	case strings.HasSuffix(stream, "forceOrder"):
		b.handleLiquidation(ctx, data)
	}
	if kind == "aggTrade" {
		b.handleAggTrade(ctx, symbol, data)
	}

}

func (b *Binancef) handleMarkprice(ctx context.Context, data *fastjson.Value) {
	markPrice, _ := strconv.ParseFloat(string(data.GetStringBytes("p")), 64)
	funding, _ := strconv.ParseFloat(string(data.GetStringBytes("r")), 64)
	symbol := strings.ToLower(string((data.GetStringBytes("s"))))
	stat := &event.Stat{
		Pair:      event.NewPair(config.Binancef, symbol),
		Unix:      data.GetInt64("E"),
		MarkPrice: markPrice,
		Funding:   funding * 100,
	}

	msg := base.PublishMessageParams{
		Stream: nats.StreamTypePreStat,
		Symbol: symbol,
		Msg:    stat,
		Key:    stat.Key(),
	}

	err := b.base.PublishMessage(ctx, msg)
	if err != nil {
		slog.Error("failed to publish stat", "err", err)
	}
}

// {"e":"forceOrder","E":1738661434388,"o":{"s":"ETHUSDT","S":"SELL","o":"LIMIT","f":"IOC","q":"0.036","p":"2694.06","ap":"2704.51","X":"FILLED","l":"0.036","z":"0.036","T":1738661434384}}
func (b *Binancef) handleLiquidation(ctx context.Context, data *fastjson.Value) {
	data = data.Get("o")
	symbol := strings.ToLower(string(data.GetStringBytes("s")))
	price, _ := strconv.ParseFloat(string(data.GetStringBytes("p")), 64)
	size, _ := strconv.ParseFloat(string(data.GetStringBytes("q")), 64)
	liq := &event.LiquidationUpdate{
		Pair:  event.NewPair(config.Binancef, symbol),
		Unix:  data.GetInt64("T"),
		IsBuy: string(data.GetStringBytes("S")) != "SELL",
		Price: price,
		Size:  size,
	}

	msg := base.PublishMessageParams{
		Stream: nats.StreamTypeLiquidation,
		Symbol: symbol,
		Msg:    liq,
		Key:    liq.Key(),
	}

	err := b.base.PublishMessage(ctx, msg)
	if err != nil {
		slog.Error("failed to publish liquidation", "err", err)
	}
}

func (b *Binancef) handleOrderbook(ctx context.Context, data *fastjson.Value) {
	var (
		asks   = data.GetArray("a")
		bids   = data.GetArray("b")
		symbol = strings.ToLower(string(data.GetStringBytes("s")))
		book   = &event.BookUpdate{
			Unix: data.GetInt64("T"),
			Pair: event.NewPair(config.Binancef, symbol),
			Bids: make([]*event.BookEntry, 0, len(bids)),
			Asks: make([]*event.BookEntry, 0, len(asks)),
		}
	)
	for _, item := range asks {
		price, _ := strconv.ParseFloat(string(item.GetStringBytes("0")), 64)
		size, _ := strconv.ParseFloat(string(item.GetStringBytes("1")), 64)
		book.Asks = append(book.Asks, &event.BookEntry{
			Price: price,
			Size:  size,
		})
	}
	for _, item := range bids {
		price, _ := strconv.ParseFloat(string(item.GetStringBytes("0")), 64)
		size, _ := strconv.ParseFloat(string(item.GetStringBytes("1")), 64)
		book.Bids = append(book.Bids, &event.BookEntry{
			Price: price,
			Size:  size,
		})
	}

	msg := base.PublishMessageParams{
		Stream: nats.StreamTypeBookUpdate,
		Symbol: symbol,
		Msg:    book,
		Key:    book.Key(),
	}

	err := b.base.PublishMessage(ctx, msg)
	if err != nil {
		slog.Error("failed to publish book update", "err", err)
	}
}

func (b *Binancef) handleAggTrade(ctx context.Context, symbol string, data *fastjson.Value) {
	price, _ := strconv.ParseFloat(string(data.GetStringBytes("p")), 64)
	qty, _ := strconv.ParseFloat(string(data.GetStringBytes("q")), 64)
	trade := &event.Trade{
		ID:    strconv.FormatInt(data.GetInt64("a"), 10),
		Price: price,
		Qty:   qty,
		IsBuy: !data.GetBool("m"),
		Unix:  data.GetInt64("T"),
		Pair:  event.NewPair(config.Binancef, symbol),
	}

	msg := base.PublishMessageParams{
		Stream: nats.StreamTypeTrade,
		Symbol: symbol,
		Msg:    trade,
		Key:    trade.Key(),
	}

	err := b.base.PublishMessage(ctx, msg)
	if err != nil {
		slog.Error("failed to publish trade", "err", err)
	}
}

func endpointBuilder(tickers []string) string {
	results := []string{}
	for _, sym := range tickers {
		results = append(results, fmt.Sprintf("%s@aggTrade", strings.ToLower(sym)))
		results = append(results, fmt.Sprintf("%s@markPrice", strings.ToLower(sym)))
		results = append(results, fmt.Sprintf("%s@depth", strings.ToLower(sym)))
		results = append(results, fmt.Sprintf("%s@forceOrder", strings.ToLower(sym)))
	}
	return fmt.Sprintf("%s%s", wsEndpoint, strings.Join(results, "/"))
}

func splitStream(stream string) (string, string) {
	parts := strings.Split(stream, "@")
	return parts[0], parts[1]
}
