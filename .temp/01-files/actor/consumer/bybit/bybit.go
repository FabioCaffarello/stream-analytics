package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

const wsEndpoint = "wss://stream.bybit.com/v5/public/linear"

var streams = []nats.StreamType{
	nats.StreamTypeTrade,
	nats.StreamTypeBookUpdate,
	nats.StreamTypeLiquidation,
	nats.StreamTypePreStat,
}

type Bybit struct {
	base    *base.BaseConsumer
	tickers []string
	manager *actor.PID
	quitch  chan struct{}
}

func (b *Bybit) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		base, err := base.NewBaseConsumer(config.Bybit, b.quitch, streams)
		if err != nil {
			panic(err)
		}
		b.base = base

		b.start(c)

	case *ws.WsMessage:
		b.process(msg)

	case actor.Stopped:
		slog.Warn("consumer actor stopped", "consumer", config.Bybit)
		if b.base != nil {
			b.base.Close()
		}
		close(b.quitch)
	}
}

func New() actor.Producer {
	return func() actor.Receiver {
		return &Bybit{
			tickers: make([]string, 0),
			quitch:  make(chan struct{}),
		}
	}
}

func (b *Bybit) start(c *actor.Context) {
	market, ok := config.GetMarket(config.Bybit)
	if !ok {
		panic("failed to get bybit config")
	}

	for _, sym := range market.Symbols {
		b.tickers = append(b.tickers, sym.Ticker)
	}

	b.manager = c.SpawnChild(
		ws.NewManager(
			ws.ManagerConfig{
				SendTo:                 c.PID(),
				Exchange:               "bybit",
				Tickers:                b.tickers,
				StreamsPerTicker:       4,
				MaxStreamsPerWebsocket: 1000,
				RespawnOverlap:         5 * time.Second,
				FillStrategy:           ws.FillStrategyFirst,
				MaxWebsockets:          5,
				MaxWebsocketLifetime:   2 * time.Hour,
				EndpointBuilder:        endpointBuilder,
				SubscriptionBuilder:    subscriptionBuilder,
				Heartbeat:              heartbeatBuilder,
			},
		),
		"bybit-manager",
	)

}

func (b *Bybit) process(msg *ws.WsMessage) {
	ctx := context.TODO()
	parser := fastjson.Parser{}
	data, err := parser.ParseBytes(msg.Data)
	if err != nil {
		slog.Error("failed to parse ws message", "err", err.Error(), "consumer", config.Bybit)
		return
	}

	if data.Exists("success") {
		success := data.GetBool("success")
		op := string(data.GetStringBytes("op"))
		slog.Info("Received control message", "success", success, "op", op)
		return
	}

	topic := string(data.GetStringBytes("topic"))
	switch {
	case strings.HasPrefix(topic, "publicTrade"):
		b.handleTrade(ctx, data)
	case strings.HasPrefix(topic, "orderbook"):
		b.handleOrderbook(ctx, data)
	case strings.HasPrefix(topic, "allLiquidation"):
		b.handleLiquidation(ctx, data)
	case strings.HasPrefix(topic, "tickers"):
		b.handleTicker(ctx, data)
	}

}

func (b *Bybit) handleTicker(ctx context.Context, v *fastjson.Value) {
	data := v.Get("data")
	symbol := strings.ToLower(string(data.GetStringBytes("symbol")))
	fundingRate, _ := strconv.ParseFloat(string(data.GetStringBytes("fundingRate")), 64)
	markPrice, _ := strconv.ParseFloat(string(data.GetStringBytes("markPrice")), 64)
	stat := &event.Stat{
		Pair:      event.NewPair(config.Bybit, symbol),
		Unix:      v.GetInt64("ts"),
		MarkPrice: markPrice,
		Funding:   fundingRate * 100,
	}

	msg := base.PublishMessageParams{
		Stream: nats.StreamTypePreStat,
		Symbol: symbol,
		Msg:    stat,
		Key:    stat.Key(),
	}

	err := b.base.PublishMessage(ctx, msg)
	if err != nil {
		slog.Error("failed to publish ticker", "err", err.Error(), "consumer", config.Bybit)
	}
}

func (b *Bybit) handleLiquidation(ctx context.Context, v *fastjson.Value) {
	liqs := v.GetArray("data")

	for _, data := range liqs {
		symbol := strings.ToLower(string(data.GetStringBytes("s")))
		price, _ := strconv.ParseFloat(string(data.GetStringBytes("p")), 64)
		size, _ := strconv.ParseFloat(string(data.GetStringBytes("v")), 64)
		liq := &event.LiquidationUpdate{
			Pair:  event.NewPair(config.Bybit, symbol),
			Unix:  data.GetInt64("T"),
			IsBuy: string(data.GetStringBytes("S")) != "Sell", // true = short liquidated
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
			slog.Error("failed to publish liquidation", "err", err.Error(), "consumer", config.Bybit)
		}
	}
}

func (b *Bybit) handleOrderbook(ctx context.Context, v *fastjson.Value) {
	updateType := string(v.GetStringBytes("type"))
	data := v.Get("data")
	if data == nil {
		log.Printf("No data field in orderbook message")
		return
	}

	topic := string(v.GetStringBytes("topic"))
	parts := strings.Split(topic, ".")
	if len(parts) < 3 {
		log.Printf("Invalid topic format: %s", topic)
		return
	}
	symbol := strings.ToLower(parts[2])

	bids := data.GetArray("b")
	asks := data.GetArray("a")
	if len(bids) == 0 && len(asks) == 0 {
		return
	}

	book := &event.BookUpdate{
		Unix:     v.GetInt64("ts"),
		Pair:     event.NewPair(config.Bybit, symbol),
		Bids:     make([]*event.BookEntry, len(bids)),
		Asks:     make([]*event.BookEntry, len(asks)),
		Snapshot: updateType == "snapshot",
	}

	for i, item := range asks {
		price, _ := strconv.ParseFloat(string(item.GetStringBytes("0")), 64)
		size, _ := strconv.ParseFloat(string(item.GetStringBytes("1")), 64)
		book.Asks[i] = &event.BookEntry{
			Price: price,
			Size:  size,
		}
	}
	for i, item := range bids {
		price, _ := strconv.ParseFloat(string(item.GetStringBytes("0")), 64)
		size, _ := strconv.ParseFloat(string(item.GetStringBytes("1")), 64)
		book.Bids[i] = &event.BookEntry{
			Price: price,
			Size:  size,
		}
	}

	msg := base.PublishMessageParams{
		Stream: nats.StreamTypeBookUpdate,
		Symbol: symbol,
		Msg:    book,
		Key:    book.Key(),
	}

	err := b.base.PublishMessage(ctx, msg)
	if err != nil {
		slog.Error("failed to publish orderbook", "err", err.Error(), "consumer", config.Bybit)
	}
}

func (b *Bybit) handleTrade(ctx context.Context, v *fastjson.Value) {
	data := v.Get("data")
	if data == nil {
		log.Printf("No data field in trade message")
		return
	}

	topic := string(v.GetStringBytes("topic"))
	parts := strings.Split(topic, ".")
	if len(parts) < 2 {
		log.Printf("Invalid topic format: %s", topic)
		return
	}

	symbol := strings.ToLower(parts[1])
	trades := data.GetArray()
	if len(trades) == 0 {
		return
	}

	for _, t := range trades {
		price, _ := strconv.ParseFloat(string(t.GetStringBytes("p")), 64)
		qty, _ := strconv.ParseFloat(string(t.GetStringBytes("v")), 64)
		side := string(t.GetStringBytes("S"))
		timestamp := t.GetInt64("T")

		trade := &event.Trade{
			ID:    string(t.GetStringBytes("i")),
			Price: price,
			Qty:   qty,
			IsBuy: side == "Buy",
			Unix:  timestamp,
			Pair:  event.NewPair(config.Bybit, symbol),
		}

		msg := base.PublishMessageParams{
			Stream: nats.StreamTypeTrade,
			Symbol: symbol,
			Msg:    trade,
			Key:    trade.Key(),
		}

		err := b.base.PublishMessage(ctx, msg)
		if err != nil {
			slog.Error("failed to publish trade", "err", err.Error(), "consumer", config.Bybit)
		}
	}
}

func heartbeatBuilder() ws.Heartbeat {
	return ws.Heartbeat{
		Interval: 20 * time.Second,
		Message:  []byte(`{"op":"ping"}`),
	}
}

func endpointBuilder(_ []string) string {
	return wsEndpoint
}

type subreq struct {
	Req_id string   `json:"req_id"`
	Op     string   `json:"op"`
	Args   []string `json:"args"`
}

func subscriptionBuilder(tickers []string) [][]byte {
	streams := []string{}
	for _, sym := range tickers {
		symUpper := strings.ToUpper(sym)
		streams = append(streams, fmt.Sprintf("orderbook.500.%s", symUpper))
		streams = append(streams, fmt.Sprintf("publicTrade.%s", symUpper))
		streams = append(streams, fmt.Sprintf("allLiquidation.%s", symUpper))
		streams = append(streams, fmt.Sprintf("tickers.%s", symUpper))
	}
	subMsg := subreq{
		Req_id: "marketmonkey",
		Op:     "subscribe",
		Args:   streams,
	}

	b, err := json.Marshal(subMsg)
	if err != nil {
		slog.Error("failed to marshal subscription message", "err", err.Error(), "consumer", config.Bybit)
		return nil
	}

	return [][]byte{b}
}
