package orderbook

import (
	"log"
	"log/slog"
	"marketmonkey/common"
	"marketmonkey/config"
	"marketmonkey/event"
	"marketmonkey/pkg/metrics"
	"marketmonkey/pkg/nats"
	"marketmonkey/types"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/tidwall/btree"
)

type (
	checkDepth       struct{}
	publishOrderbook struct{}
	publishHeatmap   struct{}
)

type Orderbook struct {
	pair       *event.Pair
	asks       *btree.Map[float64, float64]
	bids       *btree.Map[float64, float64]
	lastPrice  float64
	upperBound float64
	lowerBound float64
	priceGroup float64
	tickSize   float64
	bestBid    float64
	bestAsk    float64
	lastUnix   int64
	producer   *nats.NatsProducer
}

func New(pair *event.Pair) actor.Producer {
	return func() actor.Receiver {
		market, ok := config.GetMarket(pair.Exchange)
		if !ok {
			log.Fatalf("failed to get market from config exchange [%s] symbol [%s]", pair.Exchange, pair.Symbol)
		}
		symbol, ok := market.GetSymbol(pair.Symbol)
		if !ok {
			log.Fatalf("failed to get symbol from config exchange [%s] symbol [%s]", pair.Exchange, pair.Symbol)
		}
		return &Orderbook{
			pair:     pair,
			asks:     btree.NewMap[float64, float64](0),
			bids:     btree.NewMap[float64, float64](0),
			tickSize: symbol.TickSize,
		}
	}
}

func (o *Orderbook) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		o.producer = c.Context().Value("producer").(*nats.NatsProducer)
		c.SendRepeat(c.PID(), publishOrderbook{}, time.Millisecond*200)
		c.SendRepeat(c.PID(), publishHeatmap{}, time.Millisecond*200)
		c.SendRepeat(c.PID(), checkDepth{}, time.Second*10)
	case *event.Trade:
		st := time.Now()
		if o.lastPrice == 0 {
			o.lastPrice = msg.Price
			o.rebalanceDepth()
		}
		o.lastPrice = msg.Price
		metrics.ReportProcessTrade("orderbook", o.pair.Exchange, st)
	case *event.BookUpdate:
		st := time.Now()
		if msg.Snapshot {
			o.lastUnix = msg.Unix
			o.handleBookSnapshot(msg)
			metrics.ReportProcessOrderbook("orderbook", o.pair.Exchange, st)
			return
		}
		if o.lastPrice > 0 {
			o.processUpdate(msg)
		}
		o.lastUnix = msg.Unix
		metrics.ReportProcessOrderbook("orderbook", o.pair.Exchange, st)
	case types.Tick:
		// only store the 1 min
		if msg.Value == 60 {
			o.publishHeatmap(c, nats.StreamTypeStoreHeatmap)
		}
	case checkDepth:
		o.rebalanceDepth()
	case publishOrderbook:
		o.publishOrderbook(c)
	case publishHeatmap:
		o.publishHeatmap(c, nats.StreamTypeRealTimeHeatmap)
	case actor.Stopped:
	}
}

func (o *Orderbook) rebalanceDepth() {
	if o.lastPrice == 0 {
		return
	}

	if o.priceGroup == 0 {
		o.priceGroup = common.CalculateHeatmapBinSize(o.lastPrice, o.tickSize)
		slog.Debug("calculated price group", "pg", o.priceGroup, "pair", o.pair)
	}

	o.lowerBound = common.Round(o.lastPrice * 0.9)
	o.upperBound = common.Round(o.lastPrice * 1.1)

	// Prune the asks
	prunePriceLevels := []float64{}
	asks := o.asks.Iter()
	for asks.Next() {
		price := asks.Key()
		if price < o.lowerBound || price > o.upperBound {
			prunePriceLevels = append(prunePriceLevels, price)
		}
	}
	for _, level := range prunePriceLevels {
		o.asks.Delete(level)
	}

	// prune the bids
	prunePriceLevels = prunePriceLevels[:0]
	bids := o.bids.Iter()
	for bids.Next() {
		price := bids.Key()
		if price < o.lowerBound || price > o.upperBound {
			prunePriceLevels = append(prunePriceLevels, price)
		}
	}
	for _, level := range prunePriceLevels {
		o.bids.Delete(level)
	}

}

func (o *Orderbook) processUpdate(msg *event.BookUpdate) {
	for _, ask := range msg.Asks {
		if ask.Size == 0 {
			o.asks.Delete(ask.Price)
			continue
		}
		if ask.Price <= o.upperBound && ask.Price >= o.lowerBound {
			o.asks.Set(ask.Price, ask.Size)
		}
	}
	for _, bid := range msg.Bids {
		if bid.Size == 0 {
			o.bids.Delete(bid.Price)
			continue
		}
		if bid.Price <= o.upperBound && bid.Price >= o.lowerBound {
			o.bids.Set(bid.Price, bid.Size)
		}
	}
}

func (o *Orderbook) handleBookSnapshot(msg *event.BookUpdate) {
	o.bids.Clear()
	o.asks.Clear()
	for _, ask := range msg.Asks {
		if ask.Price <= o.upperBound && ask.Price >= o.lowerBound {
			o.asks.Set(ask.Price, ask.Size)
		}
	}
	for _, bid := range msg.Bids {
		if bid.Price <= o.upperBound && bid.Price >= o.lowerBound {
			o.bids.Set(bid.Price, bid.Size)
		}
	}
}

func (o *Orderbook) publishHeatmap(c *actor.Context, streamType nats.StreamType) {
	if o.asks.Len() == 0 || o.bids.Len() == 0 || o.lastPrice == 0 {
		return
	}
	heatmap := o.calculateHeatmap()
	if heatmap == nil {
		return
	}

	hm := event.Heatmaps{
		Pair:   heatmap.Pair,
		Values: []*event.Heatmap{heatmap},
	}

	b, err := cbor.Marshal(hm)
	if err != nil {
		slog.Error("failed to marshal heatmap", "error", err)
		return
	}

	// realtime data cant use heatmap.Key()
	// because it pushes updates to the same key
	// so we need to generate a new uuid for each message
	key := heatmap.Key()
	if streamType == nats.StreamTypeRealTimeHeatmap {
		key = uuid.NewString()
	}

	o.producer.Publish(c.Context(), nats.PublishParams{
		Subject: nats.Subject{
			StreamType: streamType,
			Exchange:   o.pair.Exchange,
			Symbol:     o.pair.Symbol,
		}.PubString(),
		Msg:   b,
		MsgID: key,
	})
}

func (o *Orderbook) publishOrderbook(c *actor.Context) {
	if o.asks.Len() == 0 || o.bids.Len() == 0 || o.lastPrice == 0 {
		return
	}
	depth := 2048
	orderbook := event.Orderbook{
		Pair:      o.pair,
		LastPrice: o.lastPrice,
		AskPrices: make([]float64, 0, depth),
		AskSizes:  make([]float64, 0, depth),
		BidPrices: make([]float64, 0, depth),
		BidSizes:  make([]float64, 0, depth),
	}
	i := 0
	o.bids.Descend(1000000, func(price float64, size float64) bool {
		if i == depth {
			return false
		}
		orderbook.BidPrices = append(orderbook.BidPrices, price)
		orderbook.BidSizes = append(orderbook.BidSizes, size)
		i++
		return true
	})
	i = 0
	o.asks.Ascend(0, func(price float64, size float64) bool {
		if i == depth {
			return false
		}
		orderbook.AskPrices = append(orderbook.AskPrices, price)
		orderbook.AskSizes = append(orderbook.AskSizes, size)
		i++
		return true
	})
	o.bestBid = orderbook.BidPrices[0]
	o.bestAsk = orderbook.AskPrices[0]

	b, err := cbor.Marshal(orderbook)
	if err != nil {
		slog.Error("failed to marshal orderbook", "error", err)
		return
	}
	o.producer.Publish(c.Context(), nats.PublishParams{
		Subject: nats.Subject{
			StreamType: nats.StreamTypeRealTimeOrderbook,
			Exchange:   o.pair.Exchange,
			Symbol:     o.pair.Symbol,
		}.PubString(),
		Msg:   b,
		MsgID: uuid.NewString(),
	})
}
