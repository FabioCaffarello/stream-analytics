package coinbase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"marketmonkey/config"
	"marketmonkey/event"
	"marketmonkey/pkg/nats"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"marketmonkey/actor/consumer/base"
	"marketmonkey/actor/consumer/ws"

	"github.com/anthdm/hollywood/actor"
	"github.com/valyala/fastjson"
)

const wsEndpoint = "wss://ws-feed.exchange.coinbase.com"

var streams = []nats.StreamType{
	nats.StreamTypeTrade,
	nats.StreamTypeBookUpdate,
	nats.StreamTypePreStat,
	nats.StreamTypeLiquidation,
}

type Coinbase struct {
	consumer *base.BaseConsumer
	symbols  []string
	manager  *actor.PID
	quitch   chan struct{}
}

func New() actor.Producer {
	return func() actor.Receiver {
		return &Coinbase{
			symbols: make([]string, 0),
			quitch:  make(chan struct{}),
		}
	}
}

func (cb *Coinbase) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		consumer, err := base.NewBaseConsumer(config.Coinbase, cb.quitch, streams)
		if err != nil {
			panic(err)
		}
		cb.consumer = consumer
		cb.start(c)
	case *ws.WsMessage:
		cb.process(msg)
	case actor.Stopped:
		slog.Warn("consumer actor stopped", "consumer", config.Coinbase)
		if cb.consumer != nil {
			cb.consumer.Close()
		}
		close(cb.quitch)
	}
}

func (cb *Coinbase) start(c *actor.Context) {
	market, ok := config.GetMarket(config.Coinbase)
	if !ok {
		panic("failed to start coinbase market: invalid configuration")
	}

	for _, sym := range market.Symbols {
		ticker := fmt.Sprintf("%s-%s", strings.ToUpper(sym.Base), strings.ToUpper(sym.Quote))
		cb.symbols = append(cb.symbols, ticker)
	}

	cb.manager = c.SpawnChild(
		ws.NewManager(
			ws.ManagerConfig{
				SendTo:                 c.PID(),
				Exchange:               config.Coinbase,
				Tickers:                cb.symbols,
				StreamsPerTicker:       4,
				MaxStreamsPerWebsocket: 1000,
				RespawnOverlap:         5 * time.Second,
				FillStrategy:           ws.FillStrategyFirst,
				MaxWebsockets:          5,
				MaxWebsocketLifetime:   2 * time.Hour,
				EndpointBuilder:        endpointBuilder,
				SubscriptionBuilder:    subscriptionBuilder,
			},
		),
		"coinbase-manager",
	)
}

func (cb *Coinbase) process(msg *ws.WsMessage) {
	ctx := context.Background()
	parser := fastjson.Parser{}
	v, err := parser.ParseBytes(msg.Data)
	if err != nil {
		slog.Error("failed to parse ws message", "err", err.Error(), "consumer", config.Coinbase)
		return
	}

	msgType := string(v.GetStringBytes("type"))
	switch msgType {
	case "ticker":
		cb.handleTicker(ctx, v)
	case "snapshot":
		cb.handleSnapshot(ctx, v)
	case "l2update":
		cb.handleOrderbookUpdate(ctx, v)
	case "match":
		cb.handleTrade(ctx, v)
	case "error":
		fmt.Println(v)
	}
}

func (cb *Coinbase) handleTicker(ctx context.Context, data *fastjson.Value) {
	productID := string(data.GetStringBytes("product_id"))
	symbol := strings.ToLower(strings.Replace(productID, "-", "", -1))
	unix, _ := time.Parse(time.RFC3339, string(data.GetStringBytes("time")))
	price, _ := strconv.ParseFloat(string(data.GetStringBytes("price")), 64)

	stat := &event.Stat{
		Pair:      event.NewPair(config.Coinbase, symbol),
		Unix:      unix.Unix() * 1000,
		MarkPrice: price,
	}

	msg := base.PublishMessageParams{
		Stream: nats.StreamTypePreStat,
		Symbol: symbol,
		Msg:    stat,
		Key:    stat.Key(),
	}

	err := cb.consumer.PublishMessage(ctx, msg)
	if err != nil {
		slog.Error("failed to publish ticker", "err", err.Error(), "consumer", config.Coinbase)
	}
}

func (cb *Coinbase) handleSnapshot(ctx context.Context, data *fastjson.Value) {
	productID := string(data.GetStringBytes("product_id"))
	symbol := strings.ToLower(strings.Replace(productID, "-", "", -1))

	bidsValue := data.Get("bids")
	asksValue := data.Get("asks")
	if bidsValue == nil || asksValue == nil {
		return
	}

	bids := bidsValue.GetArray()
	asks := asksValue.GetArray()

	unix, _ := time.Parse(time.RFC3339, string(data.GetStringBytes("time")))
	bookUpdate := event.BookUpdate{
		Unix: unix.Unix() * 1000,
		Pair: &event.Pair{
			Exchange: "coinbase",
			Symbol:   symbol,
		},
		Bids:     make([]*event.BookEntry, 0, len(bids)),
		Asks:     make([]*event.BookEntry, 0, len(asks)),
		Snapshot: true,
	}

	for _, bid := range bids {
		bidArr := bid.GetArray()
		if len(bidArr) < 2 {
			continue
		}
		price, err := strconv.ParseFloat(string(bidArr[0].GetStringBytes()), 64)
		if err != nil {
			continue
		}
		size, err := strconv.ParseFloat(string(bidArr[1].GetStringBytes()), 64)
		if err != nil {
			continue
		}
		bookUpdate.Bids = append(bookUpdate.Bids, &event.BookEntry{
			Price: price,
			Size:  size,
		})
	}

	for _, ask := range asks {
		askArr := ask.GetArray()
		if len(askArr) < 2 {
			continue
		}
		price, err := strconv.ParseFloat(string(askArr[0].GetStringBytes()), 64)
		if err != nil {
			continue
		}
		size, err := strconv.ParseFloat(string(askArr[1].GetStringBytes()), 64)
		if err != nil {
			continue
		}
		bookUpdate.Asks = append(bookUpdate.Asks, &event.BookEntry{
			Price: price,
			Size:  size,
		})
	}

	msg := base.PublishMessageParams{
		Stream: nats.StreamTypeBookUpdate,
		Symbol: symbol,
		Msg:    bookUpdate,
		Key:    bookUpdate.Key(),
	}

	err := cb.consumer.PublishMessage(ctx, msg)
	if err != nil {
		fmt.Println(unsafe.Sizeof(bookUpdate.Asks))
		fmt.Println(unsafe.Sizeof(bookUpdate.Bids))
		slog.Error("failed to publish book update", "err", err.Error(), "snapshot", true, "consumer", config.Coinbase)
	}
}

func (cb *Coinbase) handleOrderbookUpdate(ctx context.Context, data *fastjson.Value) {
	productID := string(data.GetStringBytes("product_id"))
	symbol := strings.ToLower(strings.Replace(productID, "-", "", -1))

	changesValue := data.Get("changes")
	if changesValue == nil {
		return
	}
	changes := changesValue.GetArray()

	unix, _ := time.Parse(time.RFC3339, string(data.GetStringBytes("time")))
	bookUpdate := event.BookUpdate{
		Unix: unix.Unix() * 1000,
		Pair: &event.Pair{
			Exchange: "coinbase",
			Symbol:   symbol,
		},
		Bids: make([]*event.BookEntry, 0, len(changes)),
		Asks: make([]*event.BookEntry, 0, len(changes)),
	}

	for _, change := range changes {
		changeArr := change.GetArray()
		if len(changeArr) != 3 {
			continue
		}

		side := string(changeArr[0].GetStringBytes())
		price, err := strconv.ParseFloat(string(changeArr[1].GetStringBytes()), 64)
		if err != nil {
			continue
		}
		size, err := strconv.ParseFloat(string(changeArr[2].GetStringBytes()), 64)
		if err != nil {
			continue
		}

		entry := &event.BookEntry{
			Price: price,
			Size:  size,
		}

		if side == "buy" {
			bookUpdate.Bids = append(bookUpdate.Bids, entry)
		} else if side == "sell" {
			bookUpdate.Asks = append(bookUpdate.Asks, entry)
		}
	}

	msg := base.PublishMessageParams{
		Stream: nats.StreamTypeBookUpdate,
		Symbol: symbol,
		Msg:    bookUpdate,
		Key:    bookUpdate.Key(),
	}

	err := cb.consumer.PublishMessage(ctx, msg)
	if err != nil {
		slog.Error("failed to publish book update", "err", err.Error(), "consumer", config.Coinbase)
	}
}

func (cb *Coinbase) handleTrade(ctx context.Context, data *fastjson.Value) {
	productID := string(data.GetStringBytes("product_id"))
	symbol := strings.ToLower(strings.Replace(productID, "-", "", -1))

	price, _ := strconv.ParseFloat(string(data.GetStringBytes("price")), 64)
	size, _ := strconv.ParseFloat(string(data.GetStringBytes("size")), 64)
	side := string(data.GetStringBytes("side"))
	tradeID := strconv.FormatInt(data.GetInt64("trade_id"), 10)
	unix, _ := time.Parse(time.RFC3339, string(data.GetStringBytes("time")))

	trade := &event.Trade{
		Price: price,
		Qty:   size,
		IsBuy: side == "buy",
		Unix:  unix.Unix() * 1000,
		Pair: &event.Pair{
			Exchange: config.Coinbase,
			Symbol:   symbol,
		},
		ID: tradeID,
	}

	msg := base.PublishMessageParams{
		Stream: nats.StreamTypeTrade,
		Symbol: symbol,
		Msg:    trade,
		Key:    trade.Key(),
	}

	err := cb.consumer.PublishMessage(ctx, msg)
	if err != nil {
		slog.Error("failed to publish trade", "err", err.Error(), "consumer", config.Coinbase)
	}
}

func endpointBuilder(_ []string) string {
	return wsEndpoint
}

func subscriptionBuilder(symbols []string) [][]byte {
	subscribeMsg := map[string]any{
		"type":        "subscribe",
		"product_ids": symbols,
		"channels":    []string{"matches", "level2_batch", "ticker"},
	}

	b, err := json.Marshal(subscribeMsg)
	if err != nil {
		slog.Error("failed to marshal subscription message", "err", err.Error(), "consumer", config.Coinbase)
		return nil
	}

	return [][]byte{b}
}
